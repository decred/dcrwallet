// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/internal/prompt"
	"github.com/decred/dcrwallet/internal/zero"
	ldr "github.com/decred/dcrwallet/loader"
	"github.com/decred/dcrwallet/rpc/legacyrpc"
	"github.com/decred/dcrwallet/rpc/rpcserver"
	"github.com/decred/dcrwallet/version"
	"github.com/decred/dcrwallet/wallet"
)

var (
	cfg *config
)

func main() {
	// Create a context that is cancelled when a shutdown request is received
	// through an interrupt signal or an RPC request.
	ctx := withShutdownCancel(context.Background())
	go shutdownListener()

	// Run the wallet until permanent failure or shutdown is requested.
	if err := run(ctx); err != nil && err != context.Canceled {
		os.Exit(1)
	}
}

// done returns whether the context's Done channel was closed due to
// cancellation or exceeded deadline.
func done(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// run is the main startup and teardown logic performed by the main package.  It
// is responsible for parsing the config, starting RPC servers, loading and
// syncing the wallet (if necessary), and stopping all started services when the
// context is cancelled.
func run(ctx context.Context) error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	tcfg, _, err := loadConfig(ctx)
	if err != nil {
		return err
	}
	cfg = tcfg
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	// Show version at startup.
	log.Infof("Version %s (Go version %s)", version.String(), runtime.Version())

	// Read IPC messages from the read end of a pipe created and passed by the
	// parent process, if any.  When this pipe is closed, shutdown is
	// initialized.
	if cfg.PipeRx != nil {
		go serviceControlPipeRx(uintptr(*cfg.PipeRx))
	}
	if cfg.PipeTx != nil {
		go serviceControlPipeTx(uintptr(*cfg.PipeTx))
	} else {
		go drainOutgoingPipeMessages()
	}

	// Run the pprof profiler if enabled.
	if len(cfg.Profile) > 0 {
		if done(ctx) {
			return ctx.Err()
		}

		profileRedirect := http.RedirectHandler("/debug/pprof", http.StatusSeeOther)
		http.Handle("/", profileRedirect)
		for _, listenAddr := range cfg.Profile {
			listenAddr := listenAddr // copy for closure
			go func() {
				log.Infof("Starting profile server on %s", listenAddr)
				err := http.ListenAndServe(listenAddr, nil)
				if err != nil {
					fatalf("Unable to run profiler: %v", err)
				}
			}()
		}
	}

	// Write mem profile if requested.
	if cfg.MemProfile != "" {
		if done(ctx) {
			return ctx.Err()
		}

		f, err := os.Create(cfg.MemProfile)
		if err != nil {
			log.Errorf("Unable to create cpu profile: %v", err)
			return err
		}
		timer := time.NewTimer(time.Minute * 5) // 5 minutes
		go func() {
			<-timer.C
			pprof.WriteHeapProfile(f)
			f.Close()
		}()
	}

	if done(ctx) {
		return ctx.Err()
	}

	// Create the loader which is used to load and unload the wallet.  If
	// --noinitialload is not set, this function is responsible for loading the
	// wallet.  Otherwise, loading is deferred so it can be performed over RPC.
	dbDir := networkDir(cfg.AppDataDir.Value, activeNet.Params)
	stakeOptions := &ldr.StakeOptions{
		VotingEnabled:       cfg.EnableVoting,
		AddressReuse:        cfg.ReuseAddresses,
		VotingAddress:       cfg.TBOpts.VotingAddress.Address,
		PoolAddress:         cfg.PoolAddress.Address,
		PoolFees:            cfg.PoolFees,
		StakePoolColdExtKey: cfg.StakePoolColdExtKey,
		TicketFee:           cfg.TicketFee.ToCoin(),
	}
	loader := ldr.NewLoader(activeNet.Params, dbDir, stakeOptions,
		cfg.AddrIdxScanLen, cfg.AllowHighFees, cfg.RelayFee.ToCoin())

	// Stop any services started by the loader after the shutdown procedure is
	// initialized and this function returns.
	defer func() {
		err := loader.UnloadWallet()
		if err != nil && err != ldr.ErrWalletNotLoaded {
			log.Errorf("Failed to close wallet: %v", err)
		} else if err == nil {
			log.Infof("Closed wallet")
		}
	}()

	// Open the wallet when --noinitialload was not set.
	passphrase := []byte{}
	if !cfg.NoInitialLoad {
		walletPass := []byte(cfg.WalletPass)
		defer zero.Bytes(walletPass)

		if cfg.PromptPublicPass {
			walletPass, _ = passPrompt(ctx, "Enter public wallet passphrase", false)
		}

		if done(ctx) {
			return ctx.Err()
		}

		// Load the wallet.  It must have been created already or this will
		// return an appropriate error.
		w, err := loader.OpenExistingWallet(walletPass)
		if err != nil {
			log.Errorf("Failed to open wallet: %v", err)
			if apperrors.IsError(err, apperrors.ErrWrongPassphrase) {
				// walletpass not provided, advice using --walletpass or --promptpublicpass
				if len(walletPass) == 0 {
					log.Info("Retry with --walletpass or --promptpublicpass.")
				}
			}

			return err
		}

		if done(ctx) {
			return ctx.Err()
		}

		// TODO(jrick): I think that this prompt should be removed
		// entirely instead of enabling it when --noinitialload is
		// unset.  It can be replaced with an RPC request (either
		// providing the private passphrase as a parameter, or require
		// unlocking the wallet first) to trigger a full accounts
		// rescan.
		//
		// Until then, since --noinitialload users are expecting to use
		// the wallet only over RPC, disable this feature for them.
		if cfg.Pass != "" {
			passphrase = []byte(cfg.Pass)
			err = w.Unlock(passphrase, nil)
			if err != nil {
				log.Errorf("Incorrect passphrase in pass config setting.")
				return err
			}
			w.SetInitiallyUnlocked(true)
		} else {
			passphrase = startPromptPass(ctx, w)
		}
	}

	if done(ctx) {
		return ctx.Err()
	}

	// Create and start the RPC servers to serve wallet client connections.  If
	// any of the servers can not be started, it will be nil.  If none of them
	// can be started, this errors since at least one server must run for the
	// wallet to be useful.
	//
	// Servers will be associated with a loaded wallet if it has already been
	// loaded, or after it is loaded later on.
	gRPCServer, jsonRPCServer, err := startRPCServers(loader)
	if err != nil {
		log.Errorf("Unable to create RPC servers: %v", err)
		return err
	}
	if gRPCServer != nil {
		// Start wallet and voting gRPC services after a wallet is loaded.
		loader.RunAfterLoad(func(w *wallet.Wallet) {
			rpcserver.StartWalletService(gRPCServer, w)
			rpcserver.StartVotingService(gRPCServer, w)
		})
		defer func() {
			log.Warn("Stopping gRPC server...")
			gRPCServer.Stop()
			log.Info("gRPC server shutdown")
		}()
	}
	if jsonRPCServer != nil {
		go func() {
			for range jsonRPCServer.RequestProcessShutdown() {
				requestShutdown()
			}
		}()
		defer func() {
			log.Warn("Stopping JSON-RPC server...")
			jsonRPCServer.Stop()
			log.Info("JSON-RPC server shutdown")
		}()
	}

	// Stop the ticket buyer (if running) on shutdown.  This returns an error
	// that can be ignored when the ticket buyer was never started.
	defer loader.StopTicketPurchase()

	// When not running with --noinitialload, it is the main package's
	// responsibility to connect the loaded wallet to the dcrd RPC server for
	// wallet synchronization.  This function blocks until cancelled.
	if !cfg.NoInitialLoad {
		if done(ctx) {
			return ctx.Err()
		}

		rpcClientConnectLoop(ctx, passphrase, jsonRPCServer, loader)
	}

	// Wait until shutdown is signaled before returning and running deferred
	// shutdown tasks.
	<-ctx.Done()
	return ctx.Err()
}

func passPrompt(ctx context.Context, prefix string, confirm bool) (passphrase []byte, err error) {
	os.Stdout.Sync()
	c := make(chan struct{}, 1)
	go func() {
		passphrase, err = prompt.PassPrompt(bufio.NewReader(os.Stdin), prefix, confirm)
		c <- struct{}{}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c:
		return passphrase, err
	}
}

// startPromptPass prompts the user for a password to unlock their wallet in
// the event that it was restored from seed or --promptpass flag is set.
func startPromptPass(ctx context.Context, w *wallet.Wallet) []byte {
	promptPass := cfg.PromptPass

	// Watching only wallets never require a password.
	if w.Manager.WatchingOnly() {
		return nil
	}

	// The wallet is totally desynced, so we need to resync accounts.
	// Prompt for the password. Then, set the flag it wallet so it
	// knows which address functions to call when resyncing.
	needSync, err := w.NeedsAccountsSync()
	if err != nil {
		log.Errorf("Error determining whether an accounts sync is necessary: %v", err)
	}
	if err == nil && needSync {
		fmt.Println("*** ATTENTION ***")
		fmt.Println("Since this is your first time running we need to sync accounts. Please enter")
		fmt.Println("the private wallet passphrase. This will complete syncing of the wallet")
		fmt.Println("accounts and then leave your wallet unlocked. You may relock wallet after by")
		fmt.Println("calling 'walletlock' through the RPC.")
		fmt.Println("*****************")
		promptPass = true
	}
	if cfg.EnableTicketBuyer {
		promptPass = true
	}

	if !promptPass {
		return nil
	}

	// We need to rescan accounts for the initial sync. Unlock the
	// wallet after prompting for the passphrase. The special case
	// of a --createtemp simnet wallet is handled by first
	// attempting to automatically open it with the default
	// passphrase. The wallet should also request to be unlocked
	// if stake mining is currently on, so users with this flag
	// are prompted here as well.
	for {
		if w.ChainParams() == &chaincfg.SimNetParams {
			err := w.Unlock(wallet.SimulationPassphrase, nil)
			if err == nil {
				// Unlock success with the default password.
				w.SetInitiallyUnlocked(true)
				return wallet.SimulationPassphrase
			}
		}

		passphrase, err := passPrompt(ctx, "Enter private passphrase", false)
		if err != nil {
			return nil
		}

		err = w.Unlock(passphrase, nil)
		if err != nil {
			fmt.Println("Incorrect password entered. Please " +
				"try again.")
			continue
		}
		w.SetInitiallyUnlocked(true)
		return passphrase
	}
}

// rpcClientConnectLoop loops forever, attempting to create a connection to the
// consensus RPC server.  If this connection succeeds, the RPC client is used as
// the loaded wallet's network backend and used to keep the wallet synchronized
// to the network.  If/when the RPC connection is lost, the wallet is
// disassociated from the client and a new connection is attempmted.
//
// The JSON-RPC server is optional.  If set, the connected RPC client will be
// associated with the server for RPC passthrough and to enable additional
// methods.
//
// This function panics if the wallet has not already been loaded.
func rpcClientConnectLoop(ctx context.Context, passphrase []byte, jsonRPCServer *legacyrpc.Server, loader *ldr.Loader) {
	w, ok := loader.LoadedWallet()
	if !ok {
		panic("rpcClientConnectLoop: called without loaded wallet")
	}

	certs := readCAFile()

	for {
		chainClient, err := startChainRPC(ctx, certs)
		if err != nil {
			return
		}

		n := chain.BackendFromRPCClient(chainClient.Client)
		w.SetNetworkBackend(n)
		if jsonRPCServer != nil {
			jsonRPCServer.SetChainServer(chainClient)
		}
		loader.SetChainClient(chainClient.Client)

		if cfg.EnableTicketBuyer {
			err = loader.StartTicketPurchase(passphrase, &cfg.tbCfg)
			if err != nil {
				log.Errorf("Unable to start ticket buyer: %v", err)
			}
		}

		// Run wallet synchronization until it is cancelled or errors.  If the
		// context was cancelled, return immediately instead of trying to
		// reconnect.
		syncer := chain.NewRPCSyncer(w, chainClient)
		err = syncer.Run(ctx, true)
		if err == context.Canceled {
			return
		}
		if err != nil {
			syncLog.Errorf("Wallet synchronization stopped: %v", err)
		}

		// Disassociate the RPC client from all subsystems until reconnection
		// occurs.
		w.SetNetworkBackend(nil)
		if jsonRPCServer != nil {
			jsonRPCServer.SetChainServer(nil)
		}
		loader.SetChainClient(nil)
		loader.StopTicketPurchase()
	}
}

func readCAFile() []byte {
	// Read certificate file if TLS is not disabled.
	var certs []byte
	if !cfg.DisableClientTLS {
		var err error
		certs, err = ioutil.ReadFile(cfg.CAFile.Value)
		if err != nil {
			log.Warnf("Cannot open CA file: %v", err)
			// If there's an error reading the CA file, continue
			// with nil certs and without the client connection.
			certs = nil
		}
	} else {
		log.Info("Chain server RPC TLS is disabled")
	}

	return certs
}

// startChainRPC opens a RPC client connection to a dtcd server for blockchain
// services.  This function uses the RPC options from the global config and
// there is no recovery in case the server is not available or if there is an
// authentication error.  Instead, all requests to the client will simply error.
func startChainRPC(ctx context.Context, certs []byte) (*chain.RPCClient, error) {
	log.Infof("Attempting RPC client connection to %v", cfg.RPCConnect)
	rpcc, err := chain.NewRPCClient(activeNet.Params, cfg.RPCConnect,
		cfg.DcrdUsername, cfg.DcrdPassword, certs, cfg.DisableClientTLS)
	if err != nil {
		return nil, err
	}
	err = rpcc.Start(ctx, true)
	return rpcc, err
}
