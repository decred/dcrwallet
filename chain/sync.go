// Copyright (c) 2017-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"hash"
	"net"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/rpc/client/dcrd"
	"decred.org/dcrwallet/v5/validate"
	"decred.org/dcrwallet/v5/wallet"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/mixing/mixpool"
	"github.com/decred/dcrd/wire"
	"github.com/jrick/wsrpc/v2"
	"golang.org/x/sync/errgroup"
)

var requiredAPIVersion = semver{Major: 8, Minor: 3, Patch: 0}

// Syncer implements wallet synchronization services by processing
// notifications from a dcrd JSON-RPC server.
type Syncer struct {
	atomicWalletSynced     atomic.Uint32 // CAS (synced=1) when wallet syncing complete
	atomicTargetSyncHeight atomic.Int32

	wallet   *wallet.Wallet
	opts     *RPCOptions
	rpc      *dcrd.RPC
	notifier *notifier

	blake256Hasher   hash.Hash
	blake256HasherMu sync.Mutex

	discoverAccts bool
	mu            sync.Mutex

	// Sidechain management
	sidechains   wallet.SidechainForest
	sidechainsMu sync.Mutex
	relevantTxs  map[chainhash.Hash][]*wire.MsgTx

	cb *Callbacks
}

// RPCOptions specifies the network and security settings for establishing a
// websocket connection to a dcrd JSON-RPC server.
type RPCOptions struct {
	Address     string
	DefaultPort string
	User        string
	Pass        string
	Dial        func(ctx context.Context, network, address string) (net.Conn, error)
	CA          []byte
	ClientCert  []byte
	ClientKey   []byte
	Insecure    bool
}

// NewSyncer creates a Syncer that will sync the wallet using dcrd JSON-RPC.
func NewSyncer(w *wallet.Wallet, r *RPCOptions) *Syncer {
	return &Syncer{
		wallet:         w,
		opts:           r,
		blake256Hasher: blake256.New(),
		discoverAccts:  !w.Locked(),
		relevantTxs:    make(map[chainhash.Hash][]*wire.MsgTx),
	}
}

// Callbacks contains optional callback functions to notify events during
// the syncing process.  All callbacks are called synchronously and block the
// syncer from continuing.
type Callbacks struct {
	Synced                       func(synced bool)
	FetchMissingCFiltersStarted  func()
	FetchMissingCFiltersProgress func(startCFiltersHeight, endCFiltersHeight int32)
	FetchMissingCFiltersFinished func()
	FetchHeadersStarted          func()
	FetchHeadersProgress         func(lastHeaderHeight int32, lastHeaderTime int64)
	FetchHeadersFinished         func()
	DiscoverAddressesStarted     func()
	DiscoverAddressesFinished    func()
	RescanStarted                func()
	RescanProgress               func(rescannedThrough int32)
	RescanFinished               func()
}

// SetCallbacks sets the possible various callbacks that are used
// to notify interested parties to the syncing progress.
func (s *Syncer) SetCallbacks(cb *Callbacks) {
	s.cb = cb
}

// RPC returns the JSON-RPC client to the underlying dcrd node.
func (s *Syncer) RPC() *dcrd.RPC {
	return s.rpc
}

// DisableDiscoverAccounts disables account discovery. This has an effect only
// if called before the main Run() executes the account discovery process.
func (s *Syncer) DisableDiscoverAccounts() {
	s.mu.Lock()
	s.discoverAccts = false
	s.mu.Unlock()
}

// Synced returns whether the syncer has completed syncing to the backend and
// the target height it is attempting to sync to.
func (s *Syncer) Synced(ctx context.Context) (bool, int32) {
	synced := s.atomicWalletSynced.Load() == 1
	var targetHeight int32
	if !synced {
		targetHeight = s.atomicTargetSyncHeight.Load()
	} else {
		_, targetHeight = s.wallet.MainChainTip(ctx)
	}
	return synced, targetHeight
}

// synced checks the atomic that controls wallet syncness and if previously
// unsynced, updates to synced and notifies the callback, if set.
func (s *Syncer) synced() {
	swapped := s.atomicWalletSynced.CompareAndSwap(0, 1)
	if swapped && s.cb != nil && s.cb.Synced != nil {
		s.cb.Synced(true)
	}
}

// unsynced checks the atomic that controls wallet syncness and if previously
// synced, updates to unsynced and notifies the callback, if set.
func (s *Syncer) unsynced() {
	swapped := s.atomicWalletSynced.CompareAndSwap(1, 0)
	if swapped && s.cb != nil && s.cb.Synced != nil {
		s.cb.Synced(false)
	}
}

func (s *Syncer) fetchMissingCfiltersStart() {
	if s.cb != nil && s.cb.FetchMissingCFiltersStarted != nil {
		s.cb.FetchMissingCFiltersStarted()
	}
}

func (s *Syncer) fetchMissingCfiltersProgress(startMissingCFilterHeight, endMissinCFilterHeight int32) {
	if s.cb != nil && s.cb.FetchMissingCFiltersProgress != nil {
		s.cb.FetchMissingCFiltersProgress(startMissingCFilterHeight, endMissinCFilterHeight)
	}
}

func (s *Syncer) fetchMissingCfiltersFinished() {
	if s.cb != nil && s.cb.FetchMissingCFiltersFinished != nil {
		s.cb.FetchMissingCFiltersFinished()
	}
}

func (s *Syncer) fetchHeadersStart() {
	if s.cb != nil && s.cb.FetchHeadersStarted != nil {
		s.cb.FetchHeadersStarted()
	}
}

func (s *Syncer) fetchHeadersProgress(fetchedHeadersCount int32, lastHeaderTime int64) {
	if s.cb != nil && s.cb.FetchHeadersProgress != nil {
		s.cb.FetchHeadersProgress(fetchedHeadersCount, lastHeaderTime)
	}
}

func (s *Syncer) fetchHeadersFinished() {
	if s.cb != nil && s.cb.FetchHeadersFinished != nil {
		s.cb.FetchHeadersFinished()
	}
}
func (s *Syncer) discoverAddressesStart() {
	if s.cb != nil && s.cb.DiscoverAddressesStarted != nil {
		s.cb.DiscoverAddressesStarted()
	}
}

func (s *Syncer) discoverAddressesFinished() {
	if s.cb != nil && s.cb.DiscoverAddressesFinished != nil {
		s.cb.DiscoverAddressesFinished()
	}
}

func (s *Syncer) rescanStart() {
	if s.cb != nil && s.cb.RescanStarted != nil {
		s.cb.RescanStarted()
	}
}

func (s *Syncer) rescanProgress(rescannedThrough int32) {
	if s.cb != nil && s.cb.RescanProgress != nil {
		s.cb.RescanProgress(rescannedThrough)
	}
}

func (s *Syncer) rescanFinished() {
	if s.cb != nil && s.cb.RescanFinished != nil {
		s.cb.RescanFinished()
	}
}

// getHeaders fetches missing headers and corresponding cfilters from the
// underlying rpc node.
func (s *Syncer) getHeaders(ctx context.Context) error {
	// Fetch new headers and cfilters from the server.
	locators, _, err := s.wallet.BlockLocators(ctx, nil)
	if err != nil {
		return err
	}

	startedSynced := s.atomicWalletSynced.Load() == 1

	cnet := s.wallet.ChainParams().Net
	s.fetchHeadersStart()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		// If unsynced, update the target sync height.
		if !startedSynced {
			info, err := s.rpc.GetBlockchainInfo(ctx)
			if err != nil {
				return err
			}
			s.atomicTargetSyncHeight.Store(int32(info.Headers))
		}

		headers, err := s.rpc.Headers(ctx, locators, &hashStop)
		if err != nil {
			return err
		}
		if len(headers) == 0 {
			break
		}

		nodes := make([]*wallet.BlockNode, len(headers))
		var g errgroup.Group
		for i := range headers {
			i := i
			g.Go(func() error {
				header := headers[i]
				hash := header.BlockHash()
				filter, proofIndex, proof, err := s.rpc.CFilterV2(ctx, &hash)
				if err != nil {
					return err
				}

				err = validate.CFilterV2HeaderCommitment(cnet, header,
					filter, proofIndex, proof)
				if err != nil {
					return err
				}

				nodes[i] = wallet.NewBlockNode(header, &hash, filter)
				if wallet.BadCheckpoint(cnet, &hash, int32(header.Height)) {
					nodes[i].BadCheckpoint()
				}
				return nil
			})
		}
		err = g.Wait()
		if err != nil {
			return err
		}

		var added int
		for _, n := range nodes {
			haveBlock, _, _ := s.wallet.BlockInMainChain(ctx, n.Hash)
			if haveBlock {
				continue
			}
			s.sidechainsMu.Lock()
			if s.sidechains.AddBlockNode(n) {
				added++
			}
			s.sidechainsMu.Unlock()
		}

		s.fetchHeadersProgress(int32(added), headers[len(headers)-1].Timestamp.Unix())

		log.Infof("Fetched %d new header(s) ending at height %d from %s",
			added, nodes[len(nodes)-1].Header.Height, s.rpc)

		// Stop fetching headers when no new blocks are returned.
		// Because getheaders did return located blocks, this indicates
		// that the server is not as far synced as the wallet.  Blocks
		// the server has not processed are not reorged out of the
		// wallet at this time, but a reorg will switch to a better
		// chain later if one is discovered.
		if added == 0 {
			break
		}

		s.sidechainsMu.Lock()
		bestChain, err := s.wallet.EvaluateBestChain(ctx, &s.sidechains)
		s.sidechainsMu.Unlock()
		if err != nil {
			return err
		}
		if len(bestChain) == 0 {
			continue
		}

		_, err = s.wallet.ValidateHeaderChainDifficulties(ctx, bestChain, 0)
		if err != nil {
			return err
		}

		s.sidechainsMu.Lock()
		prevChain, err := s.wallet.ChainSwitch(ctx, &s.sidechains, bestChain, nil)
		s.sidechainsMu.Unlock()
		if err != nil {
			return err
		}

		if len(prevChain) != 0 {
			log.Infof("Reorganize from %v to %v (total %d block(s) reorged)",
				prevChain[len(prevChain)-1].Hash, bestChain[len(bestChain)-1].Hash, len(prevChain))
			s.sidechainsMu.Lock()
			for _, n := range prevChain {
				s.sidechains.AddBlockNode(n)
			}
			s.sidechainsMu.Unlock()
		}
		tip := bestChain[len(bestChain)-1]
		if len(bestChain) == 1 {
			log.Infof("Connected block %v, height %d", tip.Hash, tip.Header.Height)
		} else {
			log.Infof("Connected %d blocks, new tip block %v, height %d, date %v",
				len(bestChain), tip.Hash, tip.Header.Height, tip.Header.Timestamp)
		}

		locators, _, err = s.wallet.BlockLocators(ctx, nil)
		if err != nil {
			return err
		}
	}
	s.fetchHeadersFinished()

	rescanPoint, err := s.wallet.RescanPoint(ctx)
	if err != nil {
		return err
	}
	if rescanPoint != nil {
		s.mu.Lock()
		discoverAccts := s.discoverAccts
		s.mu.Unlock()
		s.discoverAddressesStart()
		err = s.wallet.DiscoverActiveAddresses(ctx, s, rescanPoint, discoverAccts, s.wallet.GapLimit())
		if err != nil {
			return err
		}
		s.discoverAddressesFinished()
		s.mu.Lock()
		s.discoverAccts = false
		s.mu.Unlock()
		err = s.wallet.LoadActiveDataFilters(ctx, s, true)
		if err != nil {
			return err
		}

		s.rescanStart()
		rescanBlock, err := s.wallet.BlockHeader(ctx, rescanPoint)
		if err != nil {
			return err
		}
		progress := make(chan wallet.RescanProgress, 1)
		go s.wallet.RescanProgressFromHeight(ctx, s, int32(rescanBlock.Height), progress)

		for p := range progress {
			if p.Err != nil {
				return p.Err
			}
			s.rescanProgress(p.ScannedThrough)
		}
		s.rescanFinished()

	} else {
		err = s.wallet.LoadActiveDataFilters(ctx, s, true)
		if err != nil {
			return err
		}
	}
	s.synced()

	return nil
}

// getMissingHeaders fetches missing headers one by one from dcrd. This assumes
// that the initial header sync and rescan have been performed and that dcrd's
// data filter (for block rescans) has been loaded.
//
// This falls back to doing a full getHeaders process when a large number of
// missing headers is detected.
func (s *Syncer) getMissingHeaders(ctx context.Context) error {
	locators, _, err := s.wallet.BlockLocators(ctx, nil)
	if err != nil {
		return err
	}
	headers, err := s.rpc.Headers(ctx, locators, &hashStop)
	if err != nil {
		return err
	}
	if len(headers) == 0 {
		return nil
	}
	if len(headers) == wire.MaxBlockHeadersPerMsg {
		// There are too many block headers to go through them one by
		// one. Perform a full getHeaders sync.
		log.Warnf("Too many (%d) missing headers. Performing full header sync.",
			len(headers))
		return s.getHeaders(ctx)
	}

	// Perform a rescan of this block on dcrd's side, to have the same data
	// as if it had been received from a blockConnected notification.
	//
	// This must be done one by one because a new block might have
	// transactions that change the set of active addresses for the wallet,
	// causing yet another set of transactions to be found in a subsequent
	// block.
	rescanHashes := make([]chainhash.Hash, 1)
	for _, header := range headers {
		rescanHashes[0] = header.BlockHash()
		var relevantTxs []*wire.MsgTx
		err := s.rpc.Rescan(ctx, rescanHashes, func(block *chainhash.Hash, txs []*wire.MsgTx) error {
			relevantTxs = append(relevantTxs, txs...)
			return nil
		})
		if err != nil {
			return err
		}

		err = s.handleBlockConnected(ctx, header, relevantTxs, true)
		if err != nil {
			return err
		}
	}

	return nil
}

func normalizeAddress(addr string, defaultPort string) (hostport string, err error) {
	host, port, origErr := net.SplitHostPort(addr)
	if origErr == nil {
		return net.JoinHostPort(host, port), nil
	}
	addr = net.JoinHostPort(addr, defaultPort)
	_, _, err = net.SplitHostPort(addr)
	if err != nil {
		return "", origErr
	}
	return addr, nil
}

// waitRPCSync waits until the underlying node is synced up to (at least) the
// passed height and that is has all blockchain data up to its target header
// height.
func (s *Syncer) waitRPCSync(ctx context.Context, minHeight int64) error {
	isSimnet := s.wallet.ChainParams().Net == wire.SimNet
	for {
		info, err := s.rpc.GetBlockchainInfo(ctx)
		if err != nil {
			return err
		}

		if info.Headers > minHeight {
			minHeight = info.Headers
		}
		if info.Blocks >= minHeight && (isSimnet || !info.InitialBlockDownload) {
			// dcrd is synced.
			return nil
		}

		log.Infof("Waiting dcrd instance to catch up to minimum block "+
			"height %d (%d blocks, %d headers, IBS=%v)",
			minHeight, info.Blocks, info.Headers, info.InitialBlockDownload)

		// Determine when to make the next check. When there are less
		// than 100 blocks to go, use a lower check interval.
		nextDelay := 10 * time.Second
		if minHeight-info.Blocks < 100 {
			nextDelay = time.Second
		}

		select {
		case <-time.After(nextDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// hashStop is a zero value stop hash for fetching all possible data using
// locators.
var hashStop chainhash.Hash

// Run synchronizes the wallet, returning when synchronization fails or the
// context is cancelled.  If startupSync is true, all synchronization tasks
// needed to fully register the wallet for notifications and synchronize it with
// the dcrd server are performed.  Otherwise, it will listen for notifications
// but not register for any updates.
func (s *Syncer) Run(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			const op errors.Op = "rpcsyncer.Run"
			err = errors.E(op, err)
		}
	}()

	params := s.wallet.ChainParams()

	s.notifier = &notifier{
		syncer: s,
		ctx:    ctx,
		closed: make(chan struct{}),
	}
	addr, err := normalizeAddress(s.opts.Address, s.opts.DefaultPort)
	if err != nil {
		return errors.E(errors.Invalid, err)
	}
	if s.opts.Insecure {
		addr = "ws://" + addr + "/ws"
	} else {
		addr = "wss://" + addr + "/ws"
	}
	opts := make([]wsrpc.Option, 0, 5)
	if s.opts.User != "" {
		opts = append(opts, wsrpc.WithBasicAuth(s.opts.User, s.opts.Pass))
	}
	opts = append(opts, wsrpc.WithNotifier(s.notifier))
	opts = append(opts, wsrpc.WithoutPongDeadline())
	if s.opts.Dial != nil {
		opts = append(opts, wsrpc.WithDial(s.opts.Dial))
	}
	if len(s.opts.CA) != 0 && !s.opts.Insecure {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(s.opts.CA)
		tc := &tls.Config{
			MinVersion:       tls.VersionTLS12,
			CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
			CipherSuites: []uint16{ // Only applies to TLS 1.2. TLS 1.3 ciphersuites are not configurable.
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			},
			RootCAs: pool,
		}
		if len(s.opts.ClientCert) != 0 {
			keypair, err := tls.X509KeyPair(s.opts.ClientCert, s.opts.ClientKey)
			if err != nil {
				return err
			}
			tc.Certificates = []tls.Certificate{keypair}
		}
		opts = append(opts, wsrpc.WithTLSConfig(tc))
	}
	client, err := wsrpc.Dial(ctx, addr, opts...)
	if err != nil {
		return err
	}
	defer client.Close()
	s.rpc = dcrd.New(client)

	// Verify that the server is running on the expected network.
	var netID wire.CurrencyNet
	err = s.rpc.Call(ctx, "getcurrentnet", &netID)
	if err != nil {
		return err
	}
	if netID != params.Net {
		return errors.E("mismatched networks")
	}

	// Ensure the RPC server has a compatible API version.
	var api struct {
		Version semver `json:"dcrdjsonrpcapi"`
	}
	err = s.rpc.Call(ctx, "version", &api)
	if err != nil {
		return err
	}
	if !semverCompatible(requiredAPIVersion, api.Version) {
		return errors.Errorf("advertised API version %v incompatible "+
			"with required version %v", api.Version, requiredAPIVersion)
	}

	// Associate the RPC client with the wallet and remove the association on return.
	s.wallet.SetNetworkBackend(s)
	defer s.wallet.SetNetworkBackend(nil)

	tipHash, tipHeight := s.wallet.MainChainTip(ctx)
	rescanPoint, err := s.wallet.RescanPoint(ctx)
	if err != nil {
		return err
	}
	log.Infof("Headers synced through block %v height %d", &tipHash, tipHeight)
	if rescanPoint != nil {
		h, err := s.wallet.BlockHeader(ctx, rescanPoint)
		if err != nil {
			return err
		}
		// The rescan point is the first block that does not have synced
		// transactions, so we are synced with the parent.
		log.Infof("Transactions synced through block %v height %d", &h.PrevBlock, h.Height-1)
	} else {
		log.Infof("Transactions synced through block %v height %d", &tipHash, tipHeight)
	}

	err = s.waitRPCSync(ctx, int64(tipHeight))
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	g, ctx := errgroup.WithContext(ctx)
	defer func() {
		cancel()
		if e := g.Wait(); err == nil {
			err = e
		}
	}()

	if s.wallet.VotingEnabled() {
		err = s.rpc.Call(ctx, "notifywinningtickets", nil)
		if err != nil {
			return err
		}
		vb := s.wallet.VoteBits()
		log.Infof("Wallet voting enabled: vote bits = %#04x, "+
			"extended vote bits = %x", vb.Bits, vb.ExtendedBits)
		log.Infof("Please ensure your wallet remains unlocked so it may vote")
	}

	// Fetch any missing main chain compact filters.
	s.fetchMissingCfiltersStart()
	progress := make(chan wallet.MissingCFilterProgress, 1)
	go s.wallet.FetchMissingCFiltersWithProgress(ctx, s, progress)
	for p := range progress {
		if p.Err != nil {
			return p.Err
		}
		s.fetchMissingCfiltersProgress(p.BlockHeightStart, p.BlockHeightEnd)
	}
	s.fetchMissingCfiltersFinished()

	// Fetch all headers the wallet has not processed yet.
	// XXX: this also loads active addresses.
	err = s.getHeaders(ctx)
	if err != nil {
		return err
	}
	defer s.unsynced()

	// Request notifications for connected and disconnected blocks.
	err = s.rpc.Call(ctx, "notifyblocks", nil)
	if err != nil {
		return err
	}

	// Request notifications for tickets that need voting.
	err = s.rpc.Call(ctx, "rebroadcastwinners", nil)
	if err != nil {
		return err
	}

	// Rebroadcast unmined transactions
	err = s.wallet.PublishUnminedTransactions(ctx, s)
	if err != nil {
		// Returning this error would end and (likely) restart sync in
		// an endless loop.  It's possible a transaction should be
		// removed, but this is difficult to reliably detect over RPC.
		log.Warnf("Could not publish one or more unmined transactions: %v", err)
	}

	// Populate tspends.
	tspends, err := s.rpc.GetMempoolTSpends(ctx)
	if err != nil {
		return err
	}
	for _, v := range tspends {
		s.wallet.AddTSpend(*v)
	}
	log.Tracef("TSpends in mempool: %v", len(tspends))

	// Request notifications for mempool tspend arrivals.
	err = s.rpc.Call(ctx, "notifytspend", nil)
	if err != nil {
		return err
	}

	g.Go(func() error {
		// Run wallet background goroutines (currently, this just runs
		// mixclient).
		return s.wallet.Run(ctx)
	})

	// Request notifications for mixing messages.
	if s.wallet.MixingEnabled() {
		err = s.rpc.Call(ctx, "notifymixmessages", nil)
		if err != nil {
			return err
		}
	}

	log.Infof("Blockchain sync completed, wallet ready for general usage.")

	// Wait for notifications to finish before returning
	defer func() {
		<-s.notifier.closed
	}()

	g.Go(func() error {
		select {
		case <-ctx.Done():
			client.Close()
			return ctx.Err()
		case <-client.Done():
			return client.Err()
		}
	})
	return g.Wait()
}

type notifier struct {
	atomicClosed     atomic.Uint32
	syncer           *Syncer
	ctx              context.Context
	closed           chan struct{}
	connectingBlocks bool
}

func (n *notifier) Notify(method string, params json.RawMessage) error {
	s := n.syncer
	op := errors.Op(method)
	ctx, task := trace.NewTask(n.ctx, method)
	defer task.End()
	switch method {
	case "winningtickets":
		err := s.winningTickets(ctx, params)
		if err != nil {
			log.Error(errors.E(op, err))
		}
	case "blockconnected":
		err := s.blockConnected(ctx, params)
		if err == nil {
			n.connectingBlocks = true
			return nil
		}
		err = errors.E(op, err)
		if !n.connectingBlocks {
			log.Errorf("Failed to connect block: %v", err)
			return nil
		}
		return err
	case "relevanttxaccepted":
		err := s.relevantTxAccepted(ctx, params)
		if err != nil {
			log.Error(errors.E(op, err))
		}
	case "tspend":
		err := s.storeTSpend(ctx, params)
		if err != nil {
			log.Error(errors.E(op, err))
		}
	case "mixmessage":
		err := s.mixMessage(ctx, params)
		if err != nil {
			log.Error(errors.E(op, err))
		}
	}
	return nil
}

func (n *notifier) Close() error {
	if n.atomicClosed.CompareAndSwap(0, 1) {
		close(n.closed)
	}
	return nil
}

func (s *Syncer) winningTickets(ctx context.Context, params json.RawMessage) error {
	block, height, winners, err := dcrd.WinningTickets(params)
	if err != nil {
		return err
	}
	return s.wallet.VoteOnOwnedTickets(ctx, winners, block, height)
}

func (s *Syncer) blockConnected(ctx context.Context, params json.RawMessage) error {
	header, relevant, err := dcrd.BlockConnected(params)
	if err != nil {
		return err
	}

	return s.handleBlockConnected(ctx, header, relevant, false)
}

func (s *Syncer) handleBlockConnected(ctx context.Context, header *wire.BlockHeader, relevantTxs []*wire.MsgTx, isProcessingMissingBlock bool) error {
	// Ensure the ancestor is known to be in the main or in a side chain.
	// If it is not, this means we missed some blocks and should perform a
	// new round of initial header sync.
	s.sidechainsMu.Lock()
	prevInMainChain, _, _ := s.wallet.BlockInMainChain(ctx, &header.PrevBlock)
	prevInSideChain := s.sidechains.HasSideChainBlock(&header.PrevBlock)
	s.sidechainsMu.Unlock()
	if !(prevInMainChain || prevInSideChain) {
		if isProcessingMissingBlock {
			return fmt.Errorf("broken assumption: received missing block " +
				" without its parent in mainchain or sidechain")
		}
		log.Infof("Received header for block %s (height %d) when "+
			"parent %s not in main or side chain. Re-requesting "+
			"missing headers.", header.BlockHash(),
			header.Height, header.PrevBlock)
		return s.getMissingHeaders(ctx)
	}

	blockHash := header.BlockHash()
	filter, proofIndex, proof, err := s.rpc.CFilterV2(ctx, &blockHash)
	if err != nil {
		return err
	}

	cnet := s.wallet.ChainParams().Net
	err = validate.CFilterV2HeaderCommitment(cnet, header, filter, proofIndex, proof)
	if err != nil {
		return err
	}

	s.sidechainsMu.Lock()
	defer s.sidechainsMu.Unlock()

	blockNode := wallet.NewBlockNode(header, &blockHash, filter)
	if wallet.BadCheckpoint(cnet, &blockHash, int32(header.Height)) {
		blockNode.BadCheckpoint()
	}
	s.sidechains.AddBlockNode(blockNode)
	s.relevantTxs[blockHash] = relevantTxs

	bestChain, err := s.wallet.EvaluateBestChain(ctx, &s.sidechains)
	if err != nil {
		return err
	}
	if len(bestChain) != 0 {
		var prevChain []*wallet.BlockNode
		prevChain, err = s.wallet.ChainSwitch(ctx, &s.sidechains, bestChain, s.relevantTxs)
		if err != nil {
			return err
		}

		if len(prevChain) != 0 {
			log.Infof("Reorganize from %v to %v (total %d block(s) reorged)",
				prevChain[len(prevChain)-1].Hash, bestChain[len(bestChain)-1].Hash, len(prevChain))
			for _, n := range prevChain {
				s.sidechains.AddBlockNode(n)

				// TODO: should add txs from the removed blocks
				// to relevantTxs.  Later block connected logs
				// will be missing the transaction counts if a
				// reorg switches back to this older chain.
			}
		}
		for _, n := range bestChain {
			log.Infof("Connected block %v, height %d, %d wallet transaction(s)",
				n.Hash, n.Header.Height, len(s.relevantTxs[*n.Hash]))
			delete(s.relevantTxs, *n.Hash)
		}
	} else {
		log.Infof("Observed sidechain or orphan block %v (height %d)", &blockHash, header.Height)
	}

	return nil
}

func (s *Syncer) relevantTxAccepted(ctx context.Context, params json.RawMessage) error {
	tx, err := dcrd.RelevantTxAccepted(params)
	if err != nil {
		return err
	}
	if s.wallet.ManualTickets() && stake.IsSStx(tx) {
		return nil
	}
	return s.wallet.AddTransaction(ctx, tx, nil)
}

func (s *Syncer) storeTSpend(ctx context.Context, params json.RawMessage) error {
	tx, err := dcrd.TSpend(params)
	if err != nil {
		return err
	}
	return s.wallet.AddTSpend(*tx)
}

func (s *Syncer) mixMessage(ctx context.Context, params json.RawMessage) error {
	if !s.wallet.MixingEnabled() {
		log.Debugf("Ignoring mixmessage notification: mixing disabled")
		return nil
	}

	msg, err := dcrd.MixMessage(params)
	if err != nil {
		return err
	}
	s.blake256HasherMu.Lock()
	msg.WriteHash(s.blake256Hasher)
	s.blake256HasherMu.Unlock()

	err = s.wallet.AcceptMixMessage(msg)
	var e *mixpool.MissingOwnPRError
	if errors.As(err, &e) {
		ke, ok := msg.(*wire.MsgMixKeyExchange)
		if !ok || ke.Run != 0 {
			return err
		}
		pr, err := s.rpc.MixMessage(ctx, &e.MissingPR)
		if err == nil {
			s.blake256HasherMu.Lock()
			pr.WriteHash(s.blake256Hasher)
			s.blake256HasherMu.Unlock()

			err = s.wallet.AcceptMixMessage(pr)
		}
		return err
	}

	return err
}
