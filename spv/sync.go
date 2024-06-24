// Copyright (c) 2018-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package spv

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/lru"
	"decred.org/dcrwallet/v4/p2p"
	"decred.org/dcrwallet/v4/validate"
	"decred.org/dcrwallet/v4/wallet"
	"github.com/decred/dcrd/addrmgr/v2"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
	"github.com/decred/dcrd/gcs/v4/blockcf2"
	"github.com/decred/dcrd/mixing"
	"github.com/decred/dcrd/mixing/mixpool"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/wire"
	"golang.org/x/sync/errgroup"
)

// reqSvcs defines the services that must be supported by outbounded peers.
// After fetching more addresses (if needed), peers are disconnected from if
// they do not provide each of these services.
const reqSvcs = wire.SFNodeNetwork

const (
	targetPeerCount = 8

	// Require a subset of all connected outbounded peers to meet some
	// minimum protocol version.
	minVersion       = wire.MixVersion
	minVersionTarget = 3
)

// Syncer implements wallet synchronization services by over the Decred wire
// protocol using Simplified Payment Verification (SPV) with compact filters.
type Syncer struct {
	// atomics
	atomicWalletSynced atomic.Uint32 // CAS (synced=1) when wallet syncing complete

	wallet *wallet.Wallet
	lp     *p2p.LocalPeer

	// discoverAccounts is true if the initial sync should perform account
	// discovery. Only used during initial sync.
	discoverAccounts bool

	persistentPeers []string

	connectingRemotes map[string]struct{}
	remotes           map[string]*p2p.RemotePeer
	remoteAvailable   chan struct{}
	remotesMu         sync.Mutex

	// initialSyncDone is closed when the initial sync loop has finished.
	initialSyncDone chan struct{}

	// Data filters
	//
	// TODO: Replace precise rescan filter with wallet db accesses to avoid
	// needing to keep all relevant data in memory.
	rescanFilter *wallet.RescanFilter
	filterData   blockcf2.Entries
	filterMu     sync.Mutex

	// These LRU caches record hashes of received inventoried messages.
	// Once a message is fetched and processed from one peer, the hash is
	// added to the cache to avoid fetching it again from other peers that
	// also announce it.
	seenTxs     lru.Cache[chainhash.Hash]
	seenMixMsgs lru.Cache[chainhash.Hash]

	// Sidechain management
	sidechains  wallet.SidechainForest
	sidechainMu sync.Mutex

	// Holds all potential callbacks used to notify clients
	notifications *Notifications

	// Mempool for non-wallet-relevant transactions.
	mempool     sync.Map // k=chainhash.Hash v=*wire.MsgTx
	mempoolAdds chan *chainhash.Hash
}

// Notifications struct to contain all of the upcoming callbacks that will
// be used to update the rpc streams for syncing.
type Notifications struct {
	Synced                       func(sync bool)
	PeerConnected                func(peerCount int32, addr string)
	PeerDisconnected             func(peerCount int32, addr string)
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

	// MempoolTxs is called whenever new relevant unmined transactions are
	// observed and saved.
	MempoolTxs func(txs []*wire.MsgTx)

	// TipChanged is called when the main chain tip block changes.
	// When reorgDepth is zero, the new block is a direct child of the previous tip.
	// If non-zero, one or more blocks described by the parameter were removed from
	// the previous main chain.
	// txs contains all relevant transactions mined in each attached block in
	// unspecified order.
	// reorgDepth is guaranteed to be non-negative.
	TipChanged func(tip *wire.BlockHeader, reorgDepth int32, txs []*wire.MsgTx)
}

// NewSyncer creates a Syncer that will sync the wallet using SPV.
func NewSyncer(w *wallet.Wallet, lp *p2p.LocalPeer) *Syncer {
	return &Syncer{
		wallet:            w,
		discoverAccounts:  !w.Locked(),
		connectingRemotes: make(map[string]struct{}),
		remotes:           make(map[string]*p2p.RemotePeer),
		rescanFilter:      wallet.NewRescanFilter(nil, nil),
		seenTxs:           lru.NewCache[chainhash.Hash](2000),
		seenMixMsgs:       lru.NewCache[chainhash.Hash](2000),
		lp:                lp,
		mempoolAdds:       make(chan *chainhash.Hash),
		initialSyncDone:   make(chan struct{}),
	}
}

// SetPersistentPeers sets each peer as a persistent peer and disables DNS
// seeding and peer discovery.
func (s *Syncer) SetPersistentPeers(peers []string) {
	s.persistentPeers = peers
}

// SetNotifications sets the possible various callbacks that are used
// to notify interested parties to the syncing progress.
func (s *Syncer) SetNotifications(ntfns *Notifications) {
	s.notifications = ntfns
}

// DisableDiscoverAccounts disables account discovery. This has an effect only
// if called before the main Run() executes the account discovery process.
func (s *Syncer) DisableDiscoverAccounts() {
	s.discoverAccounts = false
}

// synced checks the atomic that controls wallet syncness and if previously
// unsynced, updates to synced and notifies the callback, if set.
func (s *Syncer) synced() {
	if s.atomicWalletSynced.CompareAndSwap(0, 1) &&
		s.notifications != nil &&
		s.notifications.Synced != nil {
		s.notifications.Synced(true)
	}
}

// unsynced checks the atomic that controls wallet syncness and if previously
// synced, updates to unsynced and notifies the callback, if set.
func (s *Syncer) unsynced() {
	if s.atomicWalletSynced.CompareAndSwap(1, 0) &&
		s.notifications != nil &&
		s.notifications.Synced != nil {
		s.notifications.Synced(false)
	}
}

// Synced returns whether this wallet is completely synced to the network and
// the target height it is attempting to sync to.
func (s *Syncer) Synced(ctx context.Context) (bool, int32) {
	synced := s.atomicWalletSynced.Load() == 1
	var targetHeight int32
	if !synced {
		s.forRemotes(func(rp *p2p.RemotePeer) error {
			if rp.InitialHeight() > targetHeight {
				targetHeight = rp.InitialHeight()
			}
			return nil
		})
	} else {
		_, targetHeight = s.wallet.MainChainTip(ctx)
	}

	return synced, targetHeight
}

// GetRemotePeers returns a map of connected remote peers.
func (s *Syncer) GetRemotePeers() map[string]*p2p.RemotePeer {
	s.remotesMu.Lock()
	defer s.remotesMu.Unlock()

	remotes := make(map[string]*p2p.RemotePeer, len(s.remotes))
	for k, rp := range s.remotes {
		remotes[k] = rp
	}
	return remotes
}

// peerConnected updates the notification for peer count, if set.
func (s *Syncer) peerConnected(remotesCount int, addr string) {
	if s.notifications != nil && s.notifications.PeerConnected != nil {
		s.notifications.PeerConnected(int32(remotesCount), addr)
	}
}

// peerDisconnected updates the notification for peer count, if set.
func (s *Syncer) peerDisconnected(remotesCount int, addr string) {
	if s.notifications != nil && s.notifications.PeerDisconnected != nil {
		s.notifications.PeerDisconnected(int32(remotesCount), addr)
	}
}

func (s *Syncer) fetchMissingCfiltersStart() {
	if s.notifications != nil && s.notifications.FetchMissingCFiltersStarted != nil {
		s.notifications.FetchMissingCFiltersStarted()
	}
}

func (s *Syncer) fetchMissingCfiltersProgress(startMissingCFilterHeight, endMissinCFilterHeight int32) {
	if s.notifications != nil && s.notifications.FetchMissingCFiltersProgress != nil {
		s.notifications.FetchMissingCFiltersProgress(startMissingCFilterHeight, endMissinCFilterHeight)
	}
}

func (s *Syncer) fetchMissingCfiltersFinished() {
	if s.notifications != nil && s.notifications.FetchMissingCFiltersFinished != nil {
		s.notifications.FetchMissingCFiltersFinished()
	}
}

func (s *Syncer) fetchHeadersStart() {
	if s.notifications != nil && s.notifications.FetchHeadersStarted != nil {
		s.notifications.FetchHeadersStarted()
	}
}

func (s *Syncer) fetchHeadersProgress(lastHeader *wire.BlockHeader) {
	if s.notifications != nil && s.notifications.FetchHeadersProgress != nil {
		s.notifications.FetchHeadersProgress(int32(lastHeader.Height), lastHeader.Timestamp.Unix())
	}
}

func (s *Syncer) fetchHeadersFinished() {
	if s.notifications != nil && s.notifications.FetchHeadersFinished != nil {
		s.notifications.FetchHeadersFinished()
	}
}
func (s *Syncer) discoverAddressesStart() {
	if s.notifications != nil && s.notifications.DiscoverAddressesStarted != nil {
		s.notifications.DiscoverAddressesStarted()
	}
}

func (s *Syncer) discoverAddressesFinished() {
	if s.notifications != nil && s.notifications.DiscoverAddressesFinished != nil {
		s.notifications.DiscoverAddressesFinished()
	}
}

func (s *Syncer) rescanStart() {
	if s.notifications != nil && s.notifications.RescanStarted != nil {
		s.notifications.RescanStarted()
	}
}

func (s *Syncer) rescanProgress(rescannedThrough int32) {
	if s.notifications != nil && s.notifications.RescanProgress != nil {
		s.notifications.RescanProgress(rescannedThrough)
	}
}

func (s *Syncer) rescanFinished() {
	if s.notifications != nil && s.notifications.RescanFinished != nil {
		s.notifications.RescanFinished()
	}
}

func (s *Syncer) mempoolTxs(txs []*wire.MsgTx) {
	if s.notifications != nil && s.notifications.MempoolTxs != nil {
		s.notifications.MempoolTxs(txs)
	}
}

func (s *Syncer) tipChanged(tip *wire.BlockHeader, reorgDepth int32, matchingTxs map[chainhash.Hash][]*wire.MsgTx) {
	if s.notifications != nil && s.notifications.TipChanged != nil {
		var txs []*wire.MsgTx
		for _, matching := range matchingTxs {
			txs = append(txs, matching...)
		}
		s.notifications.TipChanged(tip, reorgDepth, txs)
	}
}

// setRequiredHeight sets the required height a peer must advertise as their
// last height.  Initial height 6 blocks below the current chain tip height
// result in a handshake error.
func (s *Syncer) setRequiredHeight(tipHeight int32) {
	requireHeight := tipHeight
	if requireHeight > 6 {
		requireHeight -= 6
	}
	s.lp.RequirePeerHeight(requireHeight)
}

// Run synchronizes the wallet, returning when synchronization fails or the
// context is cancelled.
func (s *Syncer) Run(ctx context.Context) error {
	tipHash, tipHeight := s.wallet.MainChainTip(ctx)
	s.setRequiredHeight(tipHeight)
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

	s.lp.AddrManager().Start()
	defer func() {
		err := s.lp.AddrManager().Stop()
		if err != nil {
			log.Errorf("Failed to cleanly stop address manager: %v", err)
		}
	}()

	// Seed peers over DNS when not disabled by persistent peers.
	if len(s.persistentPeers) == 0 {
		s.lp.SeedPeers(ctx, wire.SFNodeNetwork)
	}

	// Start background handlers to read received messages from remote peers
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.receiveGetData(ctx) })
	g.Go(func() error { return s.receiveInv(ctx) })
	g.Go(func() error { return s.receiveHeadersAnnouncements(ctx) })
	g.Go(func() error { return s.receiveMixMsgs(ctx) })
	s.lp.AddHandledMessages(p2p.MaskGetData | p2p.MaskInv)

	if len(s.persistentPeers) != 0 {
		for i := range s.persistentPeers {
			raddr := s.persistentPeers[i]
			g.Go(func() error { return s.connectToPersistent(ctx, raddr) })
		}
	} else {
		g.Go(func() error { return s.connectToCandidates(ctx) })
	}

	g.Go(func() error { return s.handleMempool(ctx) })

	s.wallet.SetNetworkBackend(s)
	defer s.wallet.SetNetworkBackend(nil)

	// Perform the initial startup sync.
	g.Go(func() error {
		// First step: fetch missing CFilters.
		progress := make(chan wallet.MissingCFilterProgress, 1)
		go s.wallet.FetchMissingCFiltersWithProgress(ctx, s, progress)

		log.Debugf("Fetching missing CFilters...")
		s.fetchMissingCfiltersStart()
		for p := range progress {
			if p.Err != nil {
				return p.Err
			}
			s.fetchMissingCfiltersProgress(p.BlockHeightStart, p.BlockHeightEnd)
		}
		s.fetchMissingCfiltersFinished()
		log.Debugf("Fetched all missing cfilters")

		// Next: fetch headers and cfilters up to mainchain tip.
		s.fetchHeadersStart()
		log.Debugf("Fetching headers and CFilters...")
		err = s.initialSyncHeaders(ctx)
		if err != nil {
			return err
		}
		s.fetchHeadersFinished()

		// Finally: Perform the initial rescan over the received blocks.
		err = s.initialSyncRescan(ctx)
		if err != nil {
			return err
		}

		// Signal that the initial sync has completed.
		close(s.initialSyncDone)
		return nil
	})

	// Run wallet background goroutines (currently, this just runs
	// mixclient).
	g.Go(func() error {
		return s.wallet.Run(ctx)
	})

	// Wait until cancellation or a handler errors.
	return g.Wait()
}

// peerCandidate returns a peer address that we shall attempt to connect to.
// Only peers not already remotes or in the process of connecting are returned.
// Any address returned is marked in s.connectingRemotes before returning.
func (s *Syncer) peerCandidate(svcs wire.ServiceFlag) (*addrmgr.NetAddress, error) {
	// Try to obtain peer candidates at random, decreasing the requirements
	// as more tries are performed.
	for tries := 0; tries < 100; tries++ {
		kaddr := s.lp.AddrManager().GetAddress()
		if kaddr == nil {
			break
		}
		na := kaddr.NetAddress()

		k := na.Key()
		s.remotesMu.Lock()
		_, isConnecting := s.connectingRemotes[k]
		_, isRemote := s.remotes[k]

		switch {
		// Skip peer if already connected, or in process of connecting
		// TODO: this should work with network blocks, not exact addresses.
		case isConnecting || isRemote:
			fallthrough
		// Only allow recent nodes (10mins) after we failed 30 times
		case tries < 30 && time.Since(kaddr.LastAttempt()) < 10*time.Minute:
			fallthrough
		// Skip peers without matching service flags for the first 50 tries.
		case tries < 50 && kaddr.NetAddress().Services&svcs != svcs:
			s.remotesMu.Unlock()
			continue
		}

		s.connectingRemotes[k] = struct{}{}
		s.remotesMu.Unlock()

		return na, nil
	}
	return nil, errors.New("no addresses")
}

var errBreaksMinVersionTarget = errors.New("peer uses too low version to satisify " +
	"minimum target version count")

// connectAndRunPeer connects to and runs the syncing process with the specified
// peer. It blocks until the peer disconnects and logs any errors.
func (s *Syncer) connectAndRunPeer(ctx context.Context, raddr string, persistent bool) {
	// Attempt connection to peer.
	rp, err := s.lp.ConnectOutbound(ctx, raddr, reqSvcs)
	if err != nil {
		// Remove from list of connecting remotes if it was a peer
		// candidate (as opposed to a persistent peer).
		s.remotesMu.Lock()
		delete(s.connectingRemotes, raddr)
		s.remotesMu.Unlock()
		if !errors.Is(err, context.Canceled) {
			log.Warnf("Peering attempt failed: %v", err)
		}
		return
	}

	// Track peer as running as opposed to attempting connection.
	s.remotesMu.Lock()
	delete(s.connectingRemotes, raddr)
	if !persistent && s.breaksMinVersionTarget(rp) {
		s.remotesMu.Unlock()
		log.Debugf("Disconnecting %v: %v", raddr, errBreaksMinVersionTarget)
		rp.Disconnect(errBreaksMinVersionTarget)
		return
	}
	s.remotes[raddr] = rp
	n := len(s.remotes)
	if s.remoteAvailable != nil {
		close(s.remoteAvailable)
		s.remoteAvailable = nil
	}
	s.remotesMu.Unlock()
	log.Infof("New peer %v %v version=%d %v", raddr, rp.UA(), rp.Pver(), rp.Services())
	s.peerConnected(n, raddr)

	// Alert disconnection once this peer is done.
	defer func() {
		s.remotesMu.Lock()
		delete(s.remotes, raddr)
		n = len(s.remotes)
		s.remotesMu.Unlock()
		s.peerDisconnected(n, raddr)
		if n == 0 {
			s.unsynced()
		}
	}()

	// Perform peer startup.
	err = s.peerStartup(ctx, rp)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Warnf("Unable to complete startup sync with peer %v: %v", raddr, err)
		} else {
			log.Infof("Lost peer %v", raddr)
		}
		rp.Disconnect(err)
		return
	}

	// Finally, block until the peer disconnects.
	err = rp.Err()
	if !errors.Is(err, context.Canceled) {
		log.Warnf("Lost peer %v: %v", raddr, err)
	} else {
		log.Infof("Lost peer %v", raddr)
	}
}

func (s *Syncer) breaksMinVersionTarget(rp *p2p.RemotePeer) bool {
	if rp.Pver() >= minVersion {
		return false
	}
	n := len(s.remotes)
	if n < targetPeerCount-minVersionTarget {
		return false
	}
	var meetsMin int
	for _, rp := range s.remotes {
		if rp.Pver() >= minVersion {
			meetsMin++
			if meetsMin == minVersionTarget {
				return false
			}
		}
	}
	return true
}

func (s *Syncer) connectToPersistent(ctx context.Context, raddr string) error {
	for {
		s.connectAndRunPeer(ctx, raddr, true)

		// Retry persistent peer after 5 seconds.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *Syncer) connectToCandidates(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	sem := make(chan struct{}, targetPeerCount)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		na, err := s.peerCandidate(reqSvcs)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				<-sem
				continue
			}
		}

		wg.Add(1)
		go func() {
			raddr := na.String()
			s.connectAndRunPeer(ctx, raddr, false)
			wg.Done()
			<-sem
		}()
	}
}

func (s *Syncer) forRemotes(f func(rp *p2p.RemotePeer) error) error {
	defer s.remotesMu.Unlock()
	s.remotesMu.Lock()
	if len(s.remotes) == 0 {
		return errors.E(errors.NoPeers)
	}
	for _, rp := range s.remotes {
		err := f(rp)
		if err != nil {
			return err
		}
	}
	return nil
}

// waitForAnyRemote blocks until there is one or more remote peers available or
// the context is canceled.
//
// The [isEligible] callback determines which peers are eligible for selection.
//
// This function may return a nil peer with nil error if there are peers
// available but none of them are eligible.
//
// If waitEligible is true, then this function will wait until at least one
// remote is eligible.  Otherwise, this function returns a nil peer with nil
// error when there are peers but none of them are eligible.
func (s *Syncer) waitForRemote(ctx context.Context, isEligible func(rp *p2p.RemotePeer) bool, waitEligible bool) (*p2p.RemotePeer, error) {
	for {
		s.remotesMu.Lock()
		if len(s.remotes) == 0 {
			c := s.remoteAvailable
			if c == nil {
				c = make(chan struct{})
				s.remoteAvailable = c
			}
			s.remotesMu.Unlock()

			// Wait until a peer is available.
			select {
			case <-c:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			for _, rp := range s.remotes {
				if isEligible(rp) {
					s.remotesMu.Unlock()
					return rp, nil
				}
			}

			s.remotesMu.Unlock()
			if !waitEligible {
				return nil, nil
			}
		}
	}
}

// receiveGetData handles all received getdata requests from peers.  An inv
// message declaring knowledge of the data must have been previously sent to the
// peer, or a notfound message reports the data as missing.  Only transactions
// may be queried by a peer.
func (s *Syncer) receiveGetData(ctx context.Context) error {
	var wg sync.WaitGroup
	for {
		rp, msg, err := s.lp.ReceiveGetData(ctx)
		if err != nil {
			wg.Wait()
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Ensure that the data was (recently) announced using an inv.
			var txHashes []*chainhash.Hash
			var mixHashes []*chainhash.Hash
			var notFound []*wire.InvVect
			for _, inv := range msg.InvList {
				if !rp.InvsSent().Contains(inv.Hash) {
					notFound = append(notFound, inv)
					continue
				}
				switch inv.Type {
				case wire.InvTypeTx:
					txHashes = append(txHashes, &inv.Hash)
				case wire.InvTypeMix:
					if !s.wallet.MixingEnabled() {
						notFound = append(notFound, inv)
						continue
					}
					mixHashes = append(mixHashes, &inv.Hash)
				default:
					notFound = append(notFound, inv)
				}
			}

			// Search for requested transactions
			var foundTxs []*wire.MsgTx
			if len(txHashes) != 0 {
				var missing []*wire.InvVect
				var err error
				foundTxs, missing, err = s.wallet.GetTransactionsByHashes(ctx, txHashes)
				if err != nil && !errors.Is(err, errors.NotExist) {
					log.Warnf("Failed to look up transactions for getdata reply to peer %v: %v",
						rp.RemoteAddr(), err)
					return
				}

				// For the missing ones, attempt to search in
				// the non-wallet-relevant syncer mempool.
				for _, miss := range missing {
					if v, ok := s.mempool.Load(miss.Hash); ok {
						tx := v.(*wire.MsgTx)
						foundTxs = append(foundTxs, tx)
						continue
					}
					notFound = append(notFound, miss)
				}
			}

			// Search for requested mix messages
			var foundMixMsgs []mixing.Message
			if len(mixHashes) != 0 {
				for _, hash := range mixHashes {
					msg, err := s.wallet.MixMessage(hash)
					if err != nil {
						invvect := wire.NewInvVect(wire.InvTypeMix, hash)
						notFound = append(notFound, invvect)
						continue
					}
					foundMixMsgs = append(foundMixMsgs, msg)
				}
			}

			// Send all found transactions
			for _, tx := range foundTxs {
				err := rp.SendMessage(ctx, tx)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					log.Warnf("Failed to send getdata reply to peer %v: %v",
						rp.RemoteAddr(), err)
				}
			}

			// Send all found mix messages
			for _, msg := range foundMixMsgs {
				err := rp.SendMessage(ctx, msg)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					log.Warnf("Failed to send getdata reply to peer %v: %v",
						rp.RemoteAddr(), err)
				}
			}

			// Send notfound message for all missing or unannounced data.
			if len(notFound) != 0 {
				err := rp.SendMessage(ctx, &wire.MsgNotFound{InvList: notFound})
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					log.Warnf("Failed to send notfound reply to peer %v: %v",
						rp.RemoteAddr(), err)
				}
			}
		}()
	}
}

// receiveInv receives all inv messages from peers and starts goroutines to
// handle block and tx announcements.
func (s *Syncer) receiveInv(ctx context.Context) error {
	var wg sync.WaitGroup
	for {
		rp, msg, err := s.lp.ReceiveInv(ctx)
		if err != nil {
			wg.Wait()
			return err
		}

		var blocks []*chainhash.Hash
		var txs []*chainhash.Hash
		var mixMsgs []*chainhash.Hash
		for _, inv := range msg.InvList {
			switch inv.Type {
			case wire.InvTypeBlock:
				blocks = append(blocks, &inv.Hash)
			case wire.InvTypeTx:
				txs = append(txs, &inv.Hash)
			case wire.InvTypeMix:
				if s.wallet.MixingEnabled() {
					mixMsgs = append(mixMsgs, &inv.Hash)
				}
			}
		}

		if len(blocks) != 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()

				err := s.handleBlockInvs(ctx, rp, blocks)
				if ctx.Err() != nil {
					return
				}
				if errors.Is(err, errors.Protocol) || errors.Is(err, errors.Consensus) {
					log.Warnf("Disconnecting peer %v: %v", rp, err)
					rp.Disconnect(err)
					return
				}
				if err != nil {
					log.Warnf("Failed to handle blocks inventoried by %v: %v", rp, err)
				}
			}()
		}
		if len(txs) != 0 {
			wg.Add(1)
			go func() {
				s.handleTxInvs(ctx, rp, txs)
				wg.Done()
			}()
		}
		if len(mixMsgs) != 0 {
			wg.Add(1)
			go func() {
				s.handleMixInvs(ctx, rp, mixMsgs, nil)
				wg.Done()
			}()
		}
	}
}

// verifyTSpendSignature verifies that the provided signature and public key
// were the ones that signed the provided message transaction.
func (s *Syncer) verifyTSpendSignature(msgTx *wire.MsgTx, signature, pubKey []byte) error {
	// Calculate signature hash.
	sigHash, err := txscript.CalcSignatureHash(nil,
		txscript.SigHashAll, msgTx, 0, nil)
	if err != nil {
		return errors.Errorf("CalcSignatureHash: %w", err)
	}

	// Lift Signature from bytes.
	sig, err := schnorr.ParseSignature(signature)
	if err != nil {
		return errors.Errorf("ParseSignature: %w", err)
	}

	// Lift public PI key from bytes.
	pk, err := schnorr.ParsePubKey(pubKey)
	if err != nil {
		return errors.Errorf("ParsePubKey: %w", err)
	}

	// Verify transaction was properly signed.
	if !sig.Verify(sigHash, pk) {
		return errors.Errorf("Verify failed")
	}

	return nil
}

func (s *Syncer) checkTSpend(ctx context.Context, tx *wire.MsgTx) bool {
	var (
		isTSpend          bool
		signature, pubKey []byte
		err               error
	)
	signature, pubKey, err = stake.CheckTSpend(tx)
	isTSpend = err == nil

	if !isTSpend {
		return false
	}

	_, height := s.wallet.MainChainTip(ctx)
	if uint32(height) > tx.Expiry {
		return false
	}

	// If we have a TSpend verify the signature.
	// Check if this is a sanctioned PI key.
	if !s.wallet.ChainParams().PiKeyExists(pubKey) {
		return false
	}

	// Verify that the signature is valid and corresponds to the
	// provided public key.
	err = s.verifyTSpendSignature(tx, signature, pubKey)
	if err != nil {
		return false
	}

	return true
}

// GetInitState requests the init state, then using the tspend hashes requests
// all unseen tspend txs, validates them, and adds them to the tspends cache.
func (s *Syncer) GetInitState(ctx context.Context, rp *p2p.RemotePeer) error {
	msg := wire.NewMsgGetInitState()
	msg.AddTypes(wire.InitStateTSpends)

	initState, err := rp.GetInitState(ctx, msg)
	if err != nil {
		return err
	}

	unseenTSpends := make([]*chainhash.Hash, 0)
	for h := range initState.TSpendHashes {
		if !s.wallet.IsTSpendCached(&initState.TSpendHashes[h]) {
			unseenTSpends = append(unseenTSpends, &initState.TSpendHashes[h])
		}
	}

	if len(unseenTSpends) == 0 {
		return nil
	}

	tspendTxs, err := rp.Transactions(ctx, unseenTSpends)
	if errors.Is(err, errors.NotExist) {
		err = nil
		// Remove notfound txs.
		prevTxs := tspendTxs
		tspendTxs = tspendTxs[:0]
		for _, tx := range prevTxs {
			if tx != nil {
				tspendTxs = append(tspendTxs, tx)
			}
		}
	}
	if err != nil {
		return nil
	}

	for _, v := range tspendTxs {
		if s.checkTSpend(ctx, v) {
			s.wallet.AddTSpend(*v)
		}
	}
	return nil
}

func (s *Syncer) handleBlockInvs(ctx context.Context, rp *p2p.RemotePeer, hashes []*chainhash.Hash) error {
	const opf = "spv.handleBlockInvs(%v)"

	// We send a sendheaders msg at the end of our startup stage. Ignore
	// any invs sent before that happens, since we'll still be performing
	// an initial sync with the peer.
	if !rp.SendHeadersSent() {
		log.Debugf("Ignoring block invs from %v before "+
			"sendheaders is sent", rp)
		return nil
	}

	blocks, err := rp.Blocks(ctx, hashes)
	if err != nil {
		op := errors.Opf(opf, rp)
		return errors.E(op, err)
	}
	headers := make([]*wire.BlockHeader, len(blocks))
	bmap := make(map[chainhash.Hash]*wire.MsgBlock)
	for i, block := range blocks {
		bmap[block.BlockHash()] = block
		h := block.Header
		headers[i] = &h
	}

	return s.handleBlockAnnouncements(ctx, rp, headers, bmap)
}

// handleTxInvs responds to the inv message created by rp by fetching
// all unseen transactions announced by the peer.  Any transactions
// that are relevant to the wallet are saved as unconfirmed
// transactions.  Transaction invs are ignored when a rescan is
// necessary or ongoing.
func (s *Syncer) handleTxInvs(ctx context.Context, rp *p2p.RemotePeer, hashes []*chainhash.Hash) {
	const opf = "spv.handleTxInvs(%v)"

	rpt, err := s.wallet.RescanPoint(ctx)
	if err != nil {
		op := errors.Opf(opf, rp.RemoteAddr())
		log.Warn(errors.E(op, err))
		return
	}
	if rpt != nil {
		return
	}

	// Ignore already-processed transactions
	unseen := hashes[:0]
	for _, h := range hashes {
		if !s.seenTxs.Contains(*h) {
			unseen = append(unseen, h)
		}
	}
	if len(unseen) == 0 {
		return
	}

	txs, err := rp.Transactions(ctx, unseen)
	if errors.Is(err, errors.NotExist) {
		err = nil
		// Remove notfound txs.
		prevTxs, prevUnseen := txs, unseen
		txs, unseen = txs[:0], unseen[:0]
		for i, tx := range prevTxs {
			if tx != nil {
				txs = append(txs, tx)
				unseen = append(unseen, prevUnseen[i])
			}
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			op := errors.Opf(opf, rp.RemoteAddr())
			err := errors.E(op, err)
			log.Warn(err)
		}
		return
	}

	// Mark transactions as processed so they are not queried from other nodes
	// who announce them in the future.
	for _, h := range unseen {
		s.seenTxs.Add(*h)
	}

	for _, tx := range txs {
		if s.checkTSpend(ctx, tx) {
			s.wallet.AddTSpend(*tx)
		}
	}

	// Save any relevant transaction.
	relevant := s.filterRelevant(txs)
	for _, tx := range relevant {
		if s.wallet.ManualTickets() && stake.IsSStx(tx) {
			continue
		}
		err := s.wallet.AddTransaction(ctx, tx, nil)
		if err != nil {
			op := errors.Opf(opf, rp.RemoteAddr())
			log.Warn(errors.E(op, err))
		}
	}
	s.mempoolTxs(relevant)
}

// handleMixInvs responds to the inv message created by rp by fetching all
// unseen mix messages announced by the peer.  Any messages not known to the
// wallet's mixpool are requested from the announcing peer.
func (s *Syncer) handleMixInvs(ctx context.Context, rp *p2p.RemotePeer, hashes []*chainhash.Hash,
	onlyByID map[[33]byte]struct{}) {

	const opf = "spv.handleMixInvs(%v)"

	// Ignore already-processed messages
	unseen := hashes[:0]
	for _, h := range hashes {
		if !s.seenMixMsgs.Contains(*h) {
			unseen = append(unseen, h)
		}
	}
	if len(unseen) == 0 {
		return
	}

	msgs, err := rp.MixMessages(ctx, unseen)
	if errors.Is(err, errors.NotExist) {
		err = nil
		// Remove notfound txs.
		prevMsgs, prevUnseen := msgs, unseen
		msgs, unseen = msgs[:0], unseen[:0]
		for i, msg := range prevMsgs {
			if msg != nil {
				msgs = append(msgs, msg)
				unseen = append(unseen, prevUnseen[i])
			}
		}
	}
	if err != nil {
		if ctx.Err() == nil {
			op := errors.Opf(opf, rp.RemoteAddr())
			err := errors.E(op, err)
			log.Warn(err)
		}
		return
	}

	// Mark messages as processed so they are not queried from other nodes
	// who announce them in the future.
	for _, h := range unseen {
		s.seenMixMsgs.Add(*h)
	}

	requestUnknownPRs := make(map[chainhash.Hash]struct{})
	unknownPRIDs := make(map[[33]byte]struct{})

	// Accept mix messages to the wallet's mixpool.  If any KE was an
	// orphan and does not reference its own PR, request the previous
	// messages as well.
	for _, msg := range msgs {
		if len(onlyByID) != 0 {
			if _, ok := onlyByID[[33]byte(msg.Pub())]; !ok {
				continue
			}
		}

		err := s.wallet.AcceptMixMessage(msg)
		var missingPRErr *mixpool.MissingOwnPRError
		if errors.As(err, &missingPRErr) {
			ke := msg.(*wire.MsgMixKeyExchange)
			log.Debugf("will request unknown PR from %x", ke.Identity[:])
			requestUnknownPRs[missingPRErr.MissingPR] = struct{}{}
			unknownPRIDs[ke.Identity] = struct{}{}
		} else if err != nil {
			op := errors.Opf(opf, rp.RemoteAddr())
			log.Warn(errors.E(op, err))
		}
	}

	if len(requestUnknownPRs) > 0 {
		requestUnknownPRs := make(map[chainhash.Hash]struct{})
		unknownPRs := make([]*chainhash.Hash, 0, len(requestUnknownPRs))
		for hash := range requestUnknownPRs {
			hash := hash
			unknownPRs = append(unknownPRs, &hash)
		}
		s.handleMixInvs(ctx, rp, unknownPRs, unknownPRIDs)
	}
}

// receiveHeaderAnnouncements receives all block announcements through pushed
// headers messages messages from peers and starts goroutines to handle the
// announced header.
func (s *Syncer) receiveHeadersAnnouncements(ctx context.Context) error {
	for {
		rp, headers, err := s.lp.ReceiveHeadersAnnouncement(ctx)
		if err != nil {
			return err
		}

		go func() {
			err := s.handleBlockAnnouncements(ctx, rp, headers, nil)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				if errors.Is(err, errors.Protocol) || errors.Is(err, errors.Consensus) {
					log.Warnf("Disconnecting peer %v: %v", rp, err)
					rp.Disconnect(err)
					return
				}

				log.Warnf("Failed to handle headers announced by %v: %v", rp, err)
			}
		}()
	}
}

// receiveMixMsgs receives all mixing messages from peers and starts goroutines
// to handle the message acceptance.
func (s *Syncer) receiveMixMsgs(ctx context.Context) error {
	for {
		rp, msg, err := s.lp.ReceiveMixMessage(ctx)
		if err != nil {
			return err
		}

		go func() {
			err := s.handleMixMessage(ctx, rp, msg)
			if err != nil {
				if ctx.Err() != nil {
					return
				}

				if errors.Is(err, errors.Protocol) || errors.Is(err, errors.Consensus) {
					log.Warnf("Disconnecting peer %v: %v", rp, err)
					rp.Disconnect(err)
					return
				}

				log.Warnf("Failed to handle mix message announced by %v: %v", rp, err)
			}
		}()
	}
}

// scanChain checks for matching filters of chain and returns a map of
// relevant wallet transactions keyed by block hash.  bmap is queried
// for the block first with fallback to querying rp using getdata.
func (s *Syncer) scanChain(ctx context.Context, rp *p2p.RemotePeer, chain []*wallet.BlockNode,
	bmap map[chainhash.Hash]*wire.MsgBlock) (map[chainhash.Hash][]*wire.MsgTx, error) {

	found := make(map[chainhash.Hash][]*wire.MsgTx)

	s.filterMu.Lock()
	filterData := s.filterData
	s.filterMu.Unlock()

	fetched := make([]*wire.MsgBlock, len(chain))
	if bmap != nil {
		for i := range chain {
			if b, ok := bmap[*chain[i].Hash]; ok {
				fetched[i] = b
			}
		}
	}

	var fmatches []*chainhash.Hash
	var fmatchidx []int
	var fmatchMu sync.Mutex

	// Scan filters with up to ncpu workers
	c := make(chan int)
	var wg sync.WaitGroup
	worker := func() {
		for i := range c {
			n := chain[i]
			f := n.FilterV2
			k := blockcf2.Key(&n.Header.MerkleRoot)
			if f.N() != 0 && f.MatchAny(k, filterData) {
				fmatchMu.Lock()
				fmatches = append(fmatches, n.Hash)
				fmatchidx = append(fmatchidx, i)
				fmatchMu.Unlock()
			}
		}
		wg.Done()
	}
	nworkers := 0
	for i := 0; i < len(chain); i++ {
		if fetched[i] != nil {
			continue // Already have block
		}
		select {
		case c <- i:
		default:
			if nworkers < runtime.NumCPU() {
				nworkers++
				wg.Add(1)
				go worker()
			}
			c <- i
		}
	}
	close(c)
	wg.Wait()

	if len(fmatches) != 0 {
		blocks, err := rp.Blocks(ctx, fmatches)
		if err != nil {
			return nil, err
		}
		for j, b := range blocks {
			i := fmatchidx[j]

			// Perform context-free validation on the block.
			// Disconnect peer when invalid.
			err := validate.MerkleRoots(b)
			if err != nil {
				err = validate.DCP0005MerkleRoot(b)
			}
			if err != nil {
				rp.Disconnect(err)
				return nil, err
			}

			fetched[i] = b
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for i := 0; i < len(chain); i++ {
		b := fetched[i]
		if b == nil {
			continue
		}
		matches := s.rescanBlock(b)
		found[*chain[i].Hash] = matches
	}
	return found, nil
}

// handleBlockAnnouncements handles blocks announced through block invs or
// headers messages by rp.  bmap should contain the full blocks of any
// inventoried blocks, but may be nil in case the blocks were announced through
// headers.
func (s *Syncer) handleBlockAnnouncements(ctx context.Context, rp *p2p.RemotePeer, headers []*wire.BlockHeader,
	bmap map[chainhash.Hash]*wire.MsgBlock) (err error) {

	const opf = "spv.handleBlockAnnouncements(%v)"
	defer func() {
		if err != nil && ctx.Err() == nil {
			op := errors.Opf(opf, rp.RemoteAddr())
			err = errors.E(op, err)
		}
	}()

	if len(headers) == 0 {
		return nil
	}

	firstHeader := headers[0]

	// Disconnect if the peer announced a header that is significantly
	// behind our main chain height.
	const maxAnnHeaderTipDelta = int32(256)
	_, tipHeight := s.wallet.MainChainTip(ctx)
	if int32(firstHeader.Height) < tipHeight && tipHeight-int32(firstHeader.Height) > maxAnnHeaderTipDelta {
		err = errors.E(errors.Protocol, "peer announced old header")
		return err
	}

	newBlocks := make([]*wallet.BlockNode, 0, len(headers))
	var bestChain []*wallet.BlockNode
	var matchingTxs map[chainhash.Hash][]*wire.MsgTx
	cnet := s.wallet.ChainParams().Net
	err = func() error {
		defer s.sidechainMu.Unlock()
		s.sidechainMu.Lock()

		// Determine if the peer sent a header that connects to an
		// unknown sidechain (i.e. an orphan chain). In that case,
		// re-request headers to hopefully find the missing ones.
		//
		// The header is an orphan if its parent block is not in the
		// mainchain nor on a previously known side chain.
		prevInMainChain, _, err := s.wallet.BlockInMainChain(ctx, &firstHeader.PrevBlock)
		if err != nil {
			return err
		}
		if !prevInMainChain && !s.sidechains.HasSideChainBlock(&firstHeader.PrevBlock) {
			if err := rp.ReceivedOrphanHeader(); err != nil {
				return err
			}

			locators, _, err := s.wallet.BlockLocators(ctx, nil)
			if err != nil {
				return err
			}
			if err := rp.HeadersAsync(ctx, locators, &hashStop); err != nil {
				return err
			}

			// We requested async headers, so return early and wait
			// for the next headers msg.
			//
			// newBlocks and bestChain are empty at this point, so
			// the rest of this function continues without
			// producing side effects.
			return nil
		}

		for i := range headers {
			hash := headers[i].BlockHash()

			// Skip the first blocks sent if they are already in
			// the mainchain or on a known side chain. We only skip
			// those at the start of the list to ensure every block
			// in newBlocks still connects in sequence.
			if len(newBlocks) == 0 {
				haveBlock, _, err := s.wallet.BlockInMainChain(ctx, &hash)
				if err != nil {
					return err
				}

				if haveBlock || s.sidechains.HasSideChainBlock(&hash) {
					continue
				}
			}

			n := wallet.NewBlockNode(headers[i], &hash, nil)
			newBlocks = append(newBlocks, n)
		}

		if len(newBlocks) == 0 {
			// Peer did not send any headers we didn't already
			// have.
			return nil
		}

		fullsc, err := s.sidechains.FullSideChain(newBlocks)
		if err != nil {
			return err
		}
		_, err = s.wallet.ValidateHeaderChainDifficulties(ctx, fullsc, 0)
		if err != nil {
			return err
		}

		for _, n := range newBlocks {
			s.sidechains.AddBlockNode(n)
		}

		bestChain, err = s.wallet.EvaluateBestChain(ctx, &s.sidechains)
		if err != nil {
			return err
		}

		if len(bestChain) == 0 {
			return nil
		}

		bestChainHashes := make([]*chainhash.Hash, len(bestChain))
		for i, n := range bestChain {
			bestChainHashes[i] = n.Hash
		}

		filters, err := rp.CFiltersV2(ctx, bestChainHashes)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}

		for i, cf := range filters {
			filter, proofIndex, proof := cf.Filter, cf.ProofIndex, cf.Proof

			err = validate.CFilterV2HeaderCommitment(cnet,
				bestChain[i].Header, filter, proofIndex, proof)
			if err != nil {
				return err
			}

			bestChain[i].FilterV2 = filter
		}

		rpt, err := s.wallet.RescanPoint(ctx)
		if err != nil {
			return err
		}
		if rpt == nil {
			matchingTxs, err = s.scanChain(ctx, rp, bestChain, bmap)
			if err != nil {
				return err
			}
		}

		prevChain, err := s.wallet.ChainSwitch(ctx, &s.sidechains, bestChain, matchingTxs)
		if err != nil {
			return err
		}
		if len(prevChain) != 0 {
			log.Infof("Reorganize from %v to %v (total %d block(s) reorged)",
				prevChain[len(prevChain)-1].Hash, bestChain[len(bestChain)-1].Hash, len(prevChain))
			for _, n := range prevChain {
				s.sidechains.AddBlockNode(n)
			}
		}
		tipHeader := bestChain[len(bestChain)-1].Header
		s.setRequiredHeight(int32(tipHeader.Height))
		s.disconnectStragglers(int32(tipHeader.Height))
		s.tipChanged(tipHeader, int32(len(prevChain)), matchingTxs)

		return nil
	}()
	if err != nil {
		return err
	}

	// Log connected blocks.
	for _, n := range bestChain {
		log.Infof("Connected block %v, height %d, %d wallet transaction(s)",
			n.Hash, n.Header.Height, len(matchingTxs[*n.Hash]))
	}
	// Announced blocks not in the main chain are logged as sidechain or orphan
	// blocks.
	for _, n := range newBlocks {
		haveBlock, _, err := s.wallet.BlockInMainChain(ctx, n.Hash)
		if err != nil {
			return err
		}
		if haveBlock {
			continue
		}
		log.Infof("Received sidechain or orphan block %v, height %v",
			n.Hash, n.Header.Height)
	}

	return nil
}

// disconnectStragglers disconnects from any peers that have fallen too much
// behind the passed tip height.
func (s *Syncer) disconnectStragglers(height int32) {
	const stragglerLimit = 6 // How many blocks behind.
	s.forRemotes(func(rp *p2p.RemotePeer) error {
		// Use the higher of InitialHeight and LastHeight. During
		// initial sync, InitialHeight will be higher, after initial
		// sync and when sendheaders was sent, we'll keep receiving
		// new headers, which updates LastHeight().
		peerHeight, initHeight := rp.LastHeight(), rp.InitialHeight()
		if initHeight > peerHeight {
			peerHeight = initHeight
		}
		if height-peerHeight > stragglerLimit {
			errMsg := fmt.Sprintf("disconnecting from straggler peer (peer height %d, tip height %d)",
				peerHeight, height)
			err := errors.E(errors.Policy, errMsg)
			rp.Disconnect(err)
		}
		return nil
	})
}

// hashStop is a zero value stop hash for fetching all possible data using
// locators.
var hashStop chainhash.Hash

// initialSyncHeaders fetches headers and cfilters from peers until the wallet
// is up to date with all connected peers.  This is part of the startup sync
// process.
func (s *Syncer) initialSyncHeaders(ctx context.Context) error {
	// The strategy for fetching initial headers is to split each batch of
	// headers into 3 stages that are run in parallel, with each batch
	// flowing through the pipeline:
	//
	// 1. Fetch headers
	// 2. Fetch cfilters
	// 3. Chain switch
	//
	// In the positive path during IBD, this allows processing the chain
	// much faster, because the bottleneck is generally the latency for
	// completing steps 1 and 2, and thus running them in parallel for
	// each batch speeds up the overall sync time.
	//
	// The pipelining is accomplished by assuming that the next batch of
	// headers will be a successor of the most recently fetched batch and
	// requesting this next batch using locators derived from this (not yet
	// completely processed) batch.  For all but the most recent blocks in
	// the main chain, this assumption will be true.
	//
	// Occasionally, in case of misbehaving peers, the sync process might be
	// entirely reset and started from scratch using only the wallet derived
	// block locators.
	//
	// The three stages are run as goroutines in the following g.
	g, ctx := errgroup.WithContext(ctx)

	// invalidateBatchChan is closed when one of the later stages (cfilter
	// fetching or chain switch) fails and the header fetching stage should
	// restart without using the cached chain.
	invalidateBatchChan := make(chan struct{})
	var invalidateBatchMu sync.Mutex
	invalidateBatch := func() {
		invalidateBatchMu.Lock()
		select {
		case <-invalidateBatchChan:
		default:
			close(invalidateBatchChan)
			invalidateBatchChan = make(chan struct{})
		}
		invalidateBatchMu.Unlock()
	}
	nextInvalidateBatchChan := func() chan struct{} {
		invalidateBatchMu.Lock()
		res := invalidateBatchChan
		invalidateBatchMu.Unlock()
		return res
	}

	// Stage 1: fetch headers.
	headersChan := make(chan *headersBatch)
	g.Go(func() error {
		var batch *headersBatch
		var err error
		invalidateChan := nextInvalidateBatchChan()
		for {
			// If we have a previous batch, the next batch is
			// likely to be a successor to it.
			var likelyBestChain []*wallet.BlockNode
			if batch != nil {
				likelyBestChain = batch.bestChain
			}

			// Fetch a batch of headers.
			batch, err = s.getHeaders(ctx, likelyBestChain)
			if err != nil {
				return err
			}

			// Before sending to the next stage, check if the
			// last one has already been invalidated while we
			// were waiting for a getHeaders response.
			select {
			case <-invalidateChan:
				invalidateChan = nextInvalidateBatchChan()
				batch = nil
				continue
			default:
			}

			// Otherwise, send to next stage.
			select {
			case headersChan <- batch:
			case <-invalidateChan:
				invalidateChan = nextInvalidateBatchChan()
				batch = nil
				continue
			case <-ctx.Done():
				return ctx.Err()
			}

			if batch.done {
				return nil
			}
		}
	})

	// Stage 2: fetch cfilters.
	cfiltersChan := make(chan *headersBatch)
	g.Go(func() error {
		var batch *headersBatch
		var err error
		for {
			// Wait for a batch of headers.
			select {
			case batch = <-headersChan:
			case <-ctx.Done():
				return ctx.Err()
			}

			// Once done, send the last batch forward and return.
			if batch.done {
				select {
				case cfiltersChan <- batch:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}

			// Determine which nodes don't have cfilters yet.
			s.sidechainMu.Lock()
			var missingCfilter []*wallet.BlockNode
			for i := range batch.bestChain {
				if batch.bestChain[i].FilterV2 == nil {
					missingCfilter = batch.bestChain[i:]
					break
				}
			}
			s.sidechainMu.Unlock()

			// Fetch Missing CFilters.
			err = s.cfiltersV2FromNodes(ctx, missingCfilter)
			if errors.Is(err, errCfilterWatchdogTriggered) {
				log.Debugf("Unable to fetch missing cfilters from %v: %v",
					batch.rp, err)
				invalidateBatch()
				continue
			}
			if err != nil {
				return err
			}

			if len(missingCfilter) > 0 {
				lastHeight := missingCfilter[len(missingCfilter)-1].Header.Height
				log.Debugf("Fetched %d new cfilters(s) ending at height %d",
					len(missingCfilter), lastHeight)
			}

			// Pass the batch to the next stage.
			select {
			case cfiltersChan <- batch:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	// Stage 3: chain switch.
	g.Go(func() error {
		startTime := time.Now()
		for ctx.Err() == nil {
			// Fetch a batch with cfilters filled in.
			var batch *headersBatch
			select {
			case batch = <-cfiltersChan:
			case <-ctx.Done():
				return ctx.Err()
			}

			if batch.done {
				// All done.
				log.Debugf("Initial sync completed in %s",
					time.Since(startTime).Round(time.Second))
				return nil
			}

			// Switch the best chain, now that all cfilters have been
			// fetched for it.
			s.sidechainMu.Lock()
			bestChain := batch.bestChain

			// When the first N blocks of bestChain have already been added
			// to mainchain tip, they don't have to be added again.  This
			// happens when requesting batches ahead of time.
			tipHash, tipHeight := s.wallet.MainChainTip(ctx)
			tipIndex := tipHeight - int32(bestChain[0].Header.Height)
			if tipIndex > 0 && tipIndex < int32(len(bestChain)-1) && *bestChain[tipIndex].Hash == tipHash {
				log.Tracef("Updating bestChain to tipIndex %d from %d to %d",
					tipIndex, bestChain[0].Header.Height,
					bestChain[tipIndex+1].Header.Height)
				bestChain = bestChain[tipIndex+1:]
			}

			// Switch to the new main chain.
			prevChain, err := s.wallet.ChainSwitch(ctx, &s.sidechains, bestChain, nil)
			if err != nil {
				s.sidechainMu.Unlock()
				batch.rp.Disconnect(err)
				invalidateBatch()
				continue
			}

			if len(prevChain) != 0 {
				log.Infof("Reorganize from %v to %v (total %d block(s) reorged)",
					prevChain[len(prevChain)-1].Hash, bestChain[len(bestChain)-1].Hash, len(prevChain))
				for _, n := range prevChain {
					s.sidechains.AddBlockNode(n)
				}
			}
			tip := bestChain[len(bestChain)-1]
			if len(bestChain) == 1 {
				log.Infof("Connected block %v, height %d", tip.Hash, tip.Header.Height)
			} else {
				log.Infof("Connected %d blocks, new tip %v, height %d, date %v",
					len(bestChain), tip.Hash, tip.Header.Height, tip.Header.Timestamp)
			}
			s.fetchHeadersProgress(tip.Header)

			s.sidechainMu.Unlock()

			// Peers should not be significantly behind the new tip.
			s.setRequiredHeight(int32(tip.Header.Height))
			s.disconnectStragglers(int32(tip.Header.Height))
		}

		return ctx.Err()
	})

	return g.Wait()
}

// initialSyncRescan performs account and address discovery and rescans blocks
// during the initial syncer operation.
func (s *Syncer) initialSyncRescan(ctx context.Context) error {
	rescanPoint, err := s.wallet.RescanPoint(ctx)
	if err != nil {
		return err
	}
	if rescanPoint == nil {
		// The wallet is already up to date with transactions in all
		// blocks. Load the data filters to check for transactions in
		// future blocks received via sendheaders.
		log.Debugf("Skipping rescanning due to rescanPoint == nil")
		err = s.wallet.LoadActiveDataFilters(ctx, s, true)
		if err != nil {
			return err
		}

		s.synced()
		return nil
	}

	// Perform address/account discovery.
	gapLimit := s.wallet.GapLimit()
	log.Debugf("Starting address discovery (discoverAccounts=%v, gapLimit=%d, rescanPoint=%v)",
		s.discoverAccounts, gapLimit, rescanPoint)
	s.discoverAddressesStart()
	err = s.wallet.DiscoverActiveAddresses(ctx, s, rescanPoint, s.discoverAccounts, gapLimit)
	if err != nil {
		return err
	}
	s.discoverAddressesFinished()

	// Prepare the filters with the list of addresses to watch for.
	err = s.wallet.LoadActiveDataFilters(ctx, s, true)
	if err != nil {
		return err
	}

	// Start the rescan asynchronously.
	rescanBlock, err := s.wallet.BlockHeader(ctx, rescanPoint)
	if err != nil {
		return err
	}
	progress := make(chan wallet.RescanProgress, 1)
	go s.wallet.RescanProgressFromHeight(ctx, s, int32(rescanBlock.Height), progress)

	// Read the rescan progress.
	s.rescanStart()
	for p := range progress {
		if p.Err != nil {
			return p.Err
		}
		s.rescanProgress(p.ScannedThrough)
	}
	s.rescanFinished()

	// Wallet is now synced.
	log.Debugf("Wallet considered synced")
	s.synced()
	return nil
}

// peerStartup performs initial startup operations with a recently connected
// peer.
func (s *Syncer) peerStartup(ctx context.Context, rp *p2p.RemotePeer) error {
	// Only continue with peer startup after the initial sync process
	// has completed.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-rp.Done():
		return rp.Err()
	case <-s.initialSyncDone:
	}

	if rp.Pver() >= wire.InitStateVersion {
		err := s.GetInitState(ctx, rp)
		if err != nil {
			log.Errorf("Failed to get init state: %v", err)
		}
	}

	unminedTxs, err := s.wallet.UnminedTransactions(ctx)
	if err != nil {
		log.Errorf("Cannot load unmined transactions for resending: %v", err)
		return nil
	}
	if len(unminedTxs) > 0 {
		err = rp.PublishTransactions(ctx, unminedTxs...)
		if err != nil {
			// TODO: Transactions should be removed if this is a double spend.
			log.Errorf("Failed to resent one or more unmined transactions: %v", err)
		}
	}

	// Ask peer to send any new headers.
	if err := rp.SendHeaders(ctx); err != nil {
		return err
	}

	// Request any updated headers from the peer.
	log.Debugf("Requesting updated headers from peer %v", rp)
	locators, _, err := s.wallet.BlockLocators(ctx, nil)
	if err != nil {
		return err
	}
	if err := rp.HeadersAsync(ctx, locators, &hashStop); err != nil {
		return err
	}

	return nil
}

// handleMempool handles eviction from the local mempool of non-wallet-backed
// transactions. It MUST be run as a goroutine.
func (s *Syncer) handleMempool(ctx context.Context) error {
	const mempoolEvictionTimeout = 60 * time.Minute

	for {
		select {
		case txHash := <-s.mempoolAdds:
			go func() {
				select {
				case <-ctx.Done():
				case <-time.After(mempoolEvictionTimeout):
					s.mempool.Delete(*txHash)
				}
			}()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// handleMixMessage handles mixing messages announced by peers.  It adds the
// messages to the wallet's mixing pool, thereby allowing the wallet to perform
// the mixing protocol.
func (s *Syncer) handleMixMessage(ctx context.Context, rp *p2p.RemotePeer, msg mixing.Message) (err error) {
	err = s.wallet.AcceptMixMessage(msg)
	if err != nil {
		return err
	}

	return
}
