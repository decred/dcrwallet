// Copyright (c) 2017-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"runtime/trace"
	"sync"
	"sync/atomic"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	"github.com/decred/dcrwallet/wallet/v3"
	"github.com/jrick/wsrpc/v2"
	"golang.org/x/sync/errgroup"
)

var requiredAPIVersion = semver{Major: 6, Minor: 0, Patch: 0}

// Syncer implements wallet synchronization services by processing
// notifications from a dcrd JSON-RPC server.
type Syncer struct {
	atomicWalletSynced uint32 // CAS (synced=1) when wallet syncing complete

	wallet   *wallet.Wallet
	opts     *RPCOptions
	rpc      *dcrd.RPC
	notifier *notifier

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
	Insecure    bool
}

// NewSyncer creates a Syncer that will sync the wallet using dcrd JSON-RPC.
func NewSyncer(w *wallet.Wallet, r *RPCOptions) *Syncer {
	return &Syncer{
		wallet:        w,
		opts:          r,
		discoverAccts: !w.Locked(),
		relevantTxs:   make(map[chainhash.Hash][]*wire.MsgTx),
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

// synced checks the atomic that controls wallet syncness and if previously
// unsynced, updates to synced and notifies the callback, if set.
func (s *Syncer) synced() {
	swapped := atomic.CompareAndSwapUint32(&s.atomicWalletSynced, 0, 1)
	if swapped && s.cb != nil && s.cb.Synced != nil {
		s.cb.Synced(true)
	}
}

// unsynced checks the atomic that controls wallet syncness and if previously
// synced, updates to unsynced and notifies the callback, if set.
func (s *Syncer) unsynced() {
	swapped := atomic.CompareAndSwapUint32(&s.atomicWalletSynced, 1, 0)
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
	opts := make([]wsrpc.Option, 0, 4)
	opts = append(opts, wsrpc.WithBasicAuth(s.opts.User, s.opts.Pass))
	opts = append(opts, wsrpc.WithNotifier(s.notifier))
	if s.opts.Dial != nil {
		opts = append(opts, wsrpc.WithDial(s.opts.Dial))
	}
	if len(s.opts.CA) != 0 && !s.opts.Insecure {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(s.opts.CA)
		tc := &tls.Config{
			RootCAs: pool,
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
	s.wallet.SetNetworkBackend(s.rpc)
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

	err = s.rpc.Call(ctx, "notifyspentandmissedtickets", nil)
	if err != nil {
		return err
	}

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
	go s.wallet.FetchMissingCFiltersWithProgress(ctx, s.rpc, progress)
	for p := range progress {
		if p.Err != nil {
			return p.Err
		}
		s.fetchMissingCfiltersProgress(p.BlockHeightStart, p.BlockHeightEnd)
	}
	s.fetchMissingCfiltersFinished()

	// Request notifications for connected and disconnected blocks.
	err = s.rpc.Call(ctx, "notifyblocks", nil)
	if err != nil {
		return err
	}

	// Fetch new headers and cfilters from the server.
	locators, err := s.wallet.BlockLocators(ctx, nil)
	if err != nil {
		return err
	}

	s.fetchHeadersStart()
	for {
		if err := ctx.Err(); err != nil {
			return err
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
				filter, err := s.rpc.CFilter(ctx, &hash)
				if err != nil {
					return err
				}
				nodes[i] = wallet.NewBlockNode(header, &hash, filter)
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
			added, nodes[len(nodes)-1].Header.Height, client)

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

		locators, err = s.wallet.BlockLocators(ctx, nil)
		if err != nil {
			return err
		}
	}
	s.fetchHeadersFinished()

	rescanPoint, err = s.wallet.RescanPoint(ctx)
	if err != nil {
		return err
	}
	if rescanPoint != nil {
		s.mu.Lock()
		discoverAccts := s.discoverAccts
		s.mu.Unlock()
		s.discoverAddressesStart()
		err = s.wallet.DiscoverActiveAddresses(ctx, s.rpc, rescanPoint, discoverAccts)
		if err != nil {
			return err
		}
		s.discoverAddressesFinished()
		s.mu.Lock()
		s.discoverAccts = false
		s.mu.Unlock()
		err = s.wallet.LoadActiveDataFilters(ctx, s.rpc, true)
		if err != nil {
			return err
		}

		s.rescanStart()
		rescanBlock, err := s.wallet.BlockHeader(ctx, rescanPoint)
		if err != nil {
			return err
		}
		progress := make(chan wallet.RescanProgress, 1)
		go s.wallet.RescanProgressFromHeight(ctx, s.rpc, int32(rescanBlock.Height), progress)

		for p := range progress {
			if p.Err != nil {
				return p.Err
			}
			s.rescanProgress(p.ScannedThrough)
		}
		s.rescanFinished()

	} else {
		err = s.wallet.LoadActiveDataFilters(ctx, s.rpc, true)
		if err != nil {
			return err
		}
	}
	s.synced()

	// Rebroadcast unmined transactions
	err = s.wallet.PublishUnminedTransactions(ctx, s.rpc)
	if err != nil {
		// Returning this error would end and (likely) restart sync in
		// an endless loop.  It's possible a transaction should be
		// removed, but this is difficult to reliably detect over RPC.
		log.Warnf("Could not publish one or more unmined transactions: %v", err)
	}

	err = s.rpc.Call(ctx, "rebroadcastwinners", nil)
	if err != nil {
		return err
	}
	err = s.rpc.Call(ctx, "rebroadcastmissed", nil)
	if err != nil {
		return err
	}

	log.Infof("Blockchain sync completed, wallet ready for general usage.")

	// Wait for notifications to finish before returning
	defer func() {
		<-s.notifier.closed
	}()

	select {
	case <-ctx.Done():
		client.Close()
		return ctx.Err()
	case <-client.Done():
		return client.Err()
	}
}

type notifier struct {
	atomicClosed     uint32
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
	case "spentandmissedtickets":
		err := s.spentAndMissedTickets(ctx, params)
		if err != nil {
			log.Error(errors.E(op, err))
		}
	}
	return nil
}

func (n *notifier) Close() error {
	if atomic.CompareAndSwapUint32(&n.atomicClosed, 0, 1) {
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

	blockHash := header.BlockHash()
	filter, err := s.rpc.CFilter(ctx, &blockHash)
	if err != nil {
		return err
	}

	s.sidechainsMu.Lock()
	defer s.sidechainsMu.Unlock()

	blockNode := wallet.NewBlockNode(header, &blockHash, filter)
	s.sidechains.AddBlockNode(blockNode)
	s.relevantTxs[blockHash] = relevant

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
	return s.wallet.AcceptMempoolTx(ctx, tx)
}

func (s *Syncer) spentAndMissedTickets(ctx context.Context, params json.RawMessage) error {
	missed, err := dcrd.MissedTickets(params)
	if err != nil {
		return err
	}
	return s.wallet.RevokeOwnedTickets(ctx, missed)
}
