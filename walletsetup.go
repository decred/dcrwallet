// Copyright (c) 2014-2015 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/internal/loader"
	"decred.org/dcrwallet/v4/internal/prompt"
	"decred.org/dcrwallet/v4/wallet"
	_ "decred.org/dcrwallet/v4/wallet/drivers/bdb"
	"decred.org/dcrwallet/v4/walletseed"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

// networkDir returns the directory name of a network directory to hold wallet
// files.
func networkDir(dataDir string, chainParams *chaincfg.Params) string {
	netname := chainParams.Name
	// Be cautious of v2+ testnets being named only "testnet".
	switch chainParams.Net {
	case 0x48e7a065: // testnet2
		netname = "testnet2"
	case wire.TestNet3:
		netname = "testnet3"
	}
	return filepath.Join(dataDir, netname)
}

// createWallet prompts the user for information needed to generate a new wallet
// and generates the wallet accordingly.  The new wallet will reside at the
// provided path. The bool passed back gives whether or not the wallet was
// restored from seed, while the []byte passed is the private password required
// to do the initial sync.
func createWallet(ctx context.Context, cfg *config) error {
	dbDir := networkDir(cfg.AppDataDir.Value, activeNet.Params)
	stakeOptions := &loader.StakeOptions{
		VotingEnabled: cfg.EnableVoting,
		VotingAddress: cfg.TBOpts.votingAddress,
	}
	loader := loader.NewLoader(activeNet.Params, dbDir, stakeOptions,
		cfg.GapLimit, cfg.WatchLast, cfg.AllowHighFees, cfg.RelayFee.Amount,
		cfg.AccountGapLimit, cfg.DisableCoinTypeUpgrades, cfg.ManualTickets,
		cfg.MixSplitLimit)

	var privPass, pubPass, seed []byte
	var imported bool
	var err error
	c := make(chan struct{}, 1)
	go func() {
		defer func() { c <- struct{}{} }()
		r := bufio.NewReader(os.Stdin)

		// Start by prompting for the private passphrase.  This function
		// prompts whether any configured private passphrase should be
		// used.
		privPass, err = prompt.PrivatePass(r, []byte(cfg.Pass))
		if err != nil {
			return
		}

		// Ascertain the public passphrase.  This will either be a value
		// specified by the user or the default hard-coded public passphrase if
		// the user does not want the additional public data encryption.
		// This function also prompts whether the configured public data
		// passphrase should be used.
		pubPass, err = prompt.PublicPass(r, privPass,
			[]byte(wallet.InsecurePubPassphrase), []byte(cfg.WalletPass))
		if err != nil {
			return
		}

		// Ascertain the wallet generation seed.  This will either be an
		// automatically generated value the user has already confirmed or a
		// value the user has entered which has already been validated.
		// There is no config flag to set the seed.
		seed, imported, err = prompt.Seed(r)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c:
		if err != nil {
			return err
		}
	}

	fmt.Println("Creating the wallet...")
	w, err := loader.CreateNewWallet(ctx, pubPass, privPass, seed)
	if err != nil {
		return err
	}

	// Upgrade to the SLIP0044 cointype if this is a new (rather than
	// user-provided) seed, and also unconditionally on simnet (to prevent
	// the mining address printed below from ever becoming invalid if a
	// cointype upgrade occurred later).
	if !imported || cfg.SimNet {
		err := w.UpgradeToSLIP0044CoinType(ctx)
		if err != nil {
			return err
		}
	}

	// Display a mining address when creating a simnet wallet.
	if cfg.SimNet {
		xpub, err := w.AccountXpub(ctx, 0)
		if err != nil {
			return err
		}
		branch, err := xpub.Child(0)
		if err != nil {
			return err
		}
		child, err := branch.Child(0)
		if err != nil {
			return err
		}
		pkh := dcrutil.Hash160(child.SerializedPubKey())
		addr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pkh,
			chaincfg.SimNetParams())
		if err != nil {
			return err
		}
		fmt.Println("Mining address:", addr)
	}

	err = loader.UnloadWallet()
	if err != nil {
		return err
	}

	fmt.Println("The wallet has been created successfully.")
	return nil
}

// createSimulationWallet is intended to be called from the rpcclient
// and used to create a wallet for actors involved in simulations.
func createSimulationWallet(ctx context.Context, cfg *config) error {
	// Simulation wallet password is 'password'.
	privPass := wallet.SimulationPassphrase

	// Public passphrase is the default.
	pubPass := []byte(wallet.InsecurePubPassphrase)

	// Generate a random seed.
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return err
	}

	netDir := networkDir(cfg.AppDataDir.Value, activeNet.Params)

	// Write the seed to disk, so that we can restore it later
	// if need be, for testing purposes.
	seedStr := walletseed.EncodeMnemonic(seed)
	err = os.WriteFile(filepath.Join(netDir, "seed"), []byte(seedStr), 0644)
	if err != nil {
		return err
	}

	// Create the wallet.
	dbPath := filepath.Join(netDir, walletDbName)
	fmt.Println("Creating the wallet...")

	// Create the wallet database backed by bolt db.
	db, err := wallet.CreateDB("bdb", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// Create the wallet.
	err = wallet.Create(ctx, db, pubPass, privPass, seed, activeNet.Params)
	if err != nil {
		return err
	}

	fmt.Println("The wallet has been created successfully.")
	return nil
}

// promptHDPublicKey prompts the user for an extended public key.
func promptHDPublicKey(reader *bufio.Reader) (string, error) {
	fmt.Print("Enter HD wallet public key: ")
	keyString, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	keyStringTrimmed := strings.TrimSpace(keyString)
	return keyStringTrimmed, nil
}

// createWatchingOnlyWallet creates a watching only wallet using the passed
// extended public key.
func createWatchingOnlyWallet(ctx context.Context, cfg *config) error {
	// Get the public key.
	reader := bufio.NewReader(os.Stdin)
	pubKeyString, err := promptHDPublicKey(reader)
	if err != nil {
		return err
	}

	// Ask if the user wants to encrypt the wallet with a password.
	pubPass, err := prompt.PublicPass(reader, []byte{},
		[]byte(wallet.InsecurePubPassphrase), []byte(cfg.WalletPass))
	if err != nil {
		return err
	}

	netDir := networkDir(cfg.AppDataDir.Value, activeNet.Params)

	// Create the wallet.
	dbPath := filepath.Join(netDir, walletDbName)
	fmt.Println("Creating the wallet...")

	// Create the wallet database backed by bolt db.
	db, err := wallet.CreateDB("bdb", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	err = wallet.CreateWatchOnly(ctx, db, pubKeyString, pubPass, activeNet.Params)
	if err != nil {
		errOS := os.Remove(dbPath)
		if errOS != nil {
			fmt.Println(errOS)
		}
		return err
	}

	fmt.Println("The watching only wallet has been created successfully.")
	return nil
}

// checkCreateDir checks that the path exists and is a directory.
// If path does not exist, it is created.
func checkCreateDir(path string) error {
	if fi, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Attempt data directory creation
			if err = os.MkdirAll(path, 0700); err != nil {
				return errors.Errorf("cannot create directory: %s", err)
			}
		} else {
			return errors.Errorf("error checking directory: %s", err)
		}
	} else {
		if !fi.IsDir() {
			return errors.Errorf("path '%s' is not a directory", path)
		}
	}

	return nil
}
