// Copyright (c) 2018-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package spv

import (
	"context"
	"net"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/addrmgr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/lru"
	"github.com/decred/dcrwallet/p2p/v2"
	"github.com/decred/dcrwallet/validate"
	"github.com/decred/dcrwallet/wallet/v3"
	"golang.org/x/sync/errgroup"
)

// reqSvcs defines the services that must be supported by outbounded peers.
// After fetching more addresses (if needed), peers are disconnected from if
// they do not provide each of these services.
const reqSvcs = wire.SFNodeNetwork | wire.SFNodeCF

// Syncer implements wallet synchronization services by over the Decred wire
// protocol using Simplified Payment Verification (SPV) with compact filters.
type Syncer struct {
	// atomics
	atomicCatchUpTryLock uint32 // CAS (entered=1) to perform discovery/rescan
	atomicWalletSynced   uint32 // CAS (synced=1) when wallet syncing complete

	wallet *wallet.Wallet
	lp     *p2p.LocalPeer

	// Protected by atomicCatchUpTryLock
	discoverAccounts bool
	loadedFilters    bool

	persistentPeers []string

	connectingRemotes map[string]struct{}
	remotes           map[string]*p2p.RemotePeer
	remotesMu         sync.Mutex

	// Data filters
	//
	// TODO: Replace precise rescan filter with wallet db accesses to avoid
	// needing to keep all relevant data in memory.
	rescanFilter *wallet.RescanFilter
	filterData   blockcf.Entries
	filterMu     sync.Mutex

	// seenTxs records hashes of received inventoried transactions.  Once a
	// transaction is fetched and processed from one peer, the hash is added to
	// this cache to avoid fetching it again from other peers that announce the
	// transaction.
	seenTxs lru.Cache

	// Sidechain management
	sidechains  wallet.SidechainForest
	sidechainMu sync.Mutex

	currentLocators   []*chainhash.Hash
	locatorGeneration uint
	locatorMu         sync.Mutex

	// Holds all potential callbacks used to notify clients
	notifications *Notifications
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
		seenTxs:           lru.NewCache(2000),
		lp:                lp,
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

// synced checks the atomic that controls wallet syncness and if previously
// unsynced, updates to synced and notifies the callback, if set.
func (s *Syncer) synced() {
	if atomic.CompareAndSwapUint32(&s.atomicWalletSynced, 0, 1) &&
		s.notifications != nil &&
		s.notifications.Synced != nil {
		s.notifications.Synced(true)
	}
}

// unsynced checks the atomic that controls wallet syncness and if previously
// synced, updates to unsynced and notifies the callback, if set.
func (s *Syncer) unsynced() {
	if atomic.CompareAndSwapUint32(&s.atomicWalletSynced, 1, 0) &&
		s.notifications != nil &&
		s.notifications.Synced != nil {
		s.notifications.Synced(false)
	}
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

// Run synchronizes the wallet, returning when synchronization fails or the
// context is cancelled.
func (s *Syncer) Run(ctx context.Context) error {
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

	locators, err := s.wallet.BlockLocators(ctx, nil)
	if err != nil {
		return err
	}
	s.currentLocators = locators

	s.lp.AddrManager().Start()
	defer func() {
		err := s.lp.AddrManager().Stop()
		if err != nil {
			log.Errorf("Failed to cleanly stop address manager: %v", err)
		}
	}()

	// Seed peers over DNS when not disabled by persistent peers.
	if len(s.persistentPeers) == 0 {
		s.lp.DNSSeed(wire.SFNodeNetwork | wire.SFNodeCF)
	}

	// Start background handlers to read received messages from remote peers
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return s.receiveGetData(ctx) })
	g.Go(func() error { return s.receiveInv(ctx) })
	g.Go(func() error { return s.receiveHeadersAnnouncements(ctx) })
	s.lp.AddHandledMessages(p2p.MaskGetData | p2p.MaskInv)

	if len(s.persistentPeers) != 0 {
		for i := range s.persistentPeers {
			raddr := s.persistentPeers[i]
			g.Go(func() error { return s.connectToPersistent(ctx, raddr) })
		}
	} else {
		g.Go(func() error { return s.connectToCandidates(ctx) })
	}

	s.wallet.SetNetworkBackend(s)
	defer s.wallet.SetNetworkBackend(nil)

	// Wait until cancellation or a handler errors.
	return g.Wait()
}

func (s *Syncer) peerCandidate(svcs wire.ServiceFlag) (*wire.NetAddress, error) {
	// Try to obtain peer candidates at random, decreasing the requirements
	// as more tries are performed.
	for tries := 0; tries < 100; tries++ {
		kaddr := s.lp.AddrManager().GetAddress()
		if kaddr == nil {
			break
		}
		na := kaddr.NetAddress()

		// Skip peer if already connected
		// TODO: this should work with network blocks, not exact addresses.
		k := addrmgr.NetAddressKey(na)
		s.remotesMu.Lock()
		_, isConnecting := s.connectingRemotes[k]
		_, isRemote := s.remotes[k]
		s.remotesMu.Unlock()
		if isConnecting || isRemote {
			continue
		}

		// Only allow recent nodes (10mins) after we failed 30 times
		if tries < 30 && time.Since(kaddr.LastAttempt()) < 10*time.Minute {
			continue
		}

		// Skip peers without matching service flags for the first 50 tries.
		if tries < 50 && kaddr.NetAddress().Services&svcs != svcs {
			continue
		}

		return na, nil
	}
	return nil, errors.New("no addresses")
}

func (s *Syncer) connectToPersistent(ctx context.Context, raddr string) error {
	for {
		func() {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			rp, err := s.lp.ConnectOutbound(ctx, raddr, reqSvcs)
			if err != nil {
				if ctx.Err() == nil {
					log.Errorf("Peering attempt failed: %v", err)
				}
				return
			}
			log.Infof("New peer %v %v %v", raddr, rp.UA(), rp.Services())

			k := addrmgr.NetAddressKey(rp.NA())
			s.remotesMu.Lock()
			s.remotes[k] = rp
			n := len(s.remotes)
			s.remotesMu.Unlock()
			s.peerConnected(n, k)

			wait := make(chan struct{})
			go func() {
				err := s.startupSync(ctx, rp)
				if err != nil {
					rp.Disconnect(err)
				}
				wait <- struct{}{}
			}()

			err = rp.Err()
			s.remotesMu.Lock()
			delete(s.remotes, k)
			n = len(s.remotes)
			s.remotesMu.Unlock()
			s.peerDisconnected(n, k)
			<-wait
			if ctx.Err() != nil {
				return
			}
			log.Warnf("Lost peer %v: %v", raddr, err)
		}()

		if err := ctx.Err(); err != nil {
			return err
		}

		time.Sleep(5 * time.Second)
	}
}

func (s *Syncer) connectToCandidates(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	sem := make(chan struct{}, 8)
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
			ctx, cancel := context.WithCancel(ctx)
			defer func() {
				cancel()
				wg.Done()
				<-sem
			}()

			// Make outbound connections to remote peers.
			port := strconv.FormatUint(uint64(na.Port), 10)
			raddr := net.JoinHostPort(na.IP.String(), port)
			k := addrmgr.NetAddressKey(na)

			s.remotesMu.Lock()
			s.connectingRemotes[k] = struct{}{}
			s.remotesMu.Unlock()

			rp, err := s.lp.ConnectOutbound(ctx, raddr, reqSvcs)
			if err != nil {
				s.remotesMu.Lock()
				delete(s.connectingRemotes, k)
				s.remotesMu.Unlock()
				if ctx.Err() == nil {
					log.Warnf("Peering attempt failed: %v", err)
				}
				return
			}
			log.Infof("New peer %v %v %v", raddr, rp.UA(), rp.Services())

			s.remotesMu.Lock()
			delete(s.connectingRemotes, k)
			s.remotes[k] = rp
			n := len(s.remotes)
			s.remotesMu.Unlock()
			s.peerConnected(n, k)

			wait := make(chan struct{})
			go func() {
				err := s.startupSync(ctx, rp)
				if err != nil {
					rp.Disconnect(err)
				}
				wait <- struct{}{}
			}()

			err = rp.Err()
			if ctx.Err() != context.Canceled {
				log.Warnf("Lost peer %v: %v", raddr, err)
			}

			<-wait
			s.remotesMu.Lock()
			delete(s.remotes, k)
			n = len(s.remotes)
			s.remotesMu.Unlock()
			s.peerDisconnected(n, k)
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

func (s *Syncer) pickRemote(pick func(*p2p.RemotePeer) bool) (*p2p.RemotePeer, error) {
	defer s.remotesMu.Unlock()
	s.remotesMu.Lock()

	for _, rp := range s.remotes {
		if pick(rp) {
			return rp, nil
		}
	}
	return nil, errors.E(errors.NoPeers)
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
			var notFound []*wire.InvVect
			for _, inv := range msg.InvList {
				if !rp.InvsSent().Contains(inv.Hash) {
					notFound = append(notFound, inv)
					continue
				}
				switch inv.Type {
				case wire.InvTypeTx:
					txHashes = append(txHashes, &inv.Hash)
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
				if len(missing) != 0 {
					notFound = append(notFound, missing...)
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

		wg.Add(1)
		go func() {
			defer wg.Done()

			var blocks []*chainhash.Hash
			var txs []*chainhash.Hash

			for _, inv := range msg.InvList {
				switch inv.Type {
				case wire.InvTypeBlock:
					blocks = append(blocks, &inv.Hash)
				case wire.InvTypeTx:
					txs = append(txs, &inv.Hash)
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
		}()
	}
}

func (s *Syncer) handleBlockInvs(ctx context.Context, rp *p2p.RemotePeer, hashes []*chainhash.Hash) error {
	const opf = "spv.handleBlockInvs(%v)"

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

	// Save any relevant transaction.
	relevant := s.filterRelevant(txs)
	for _, tx := range relevant {
		err := s.wallet.AcceptMempoolTx(ctx, tx)
		if err != nil {
			op := errors.Opf(opf, rp.RemoteAddr())
			log.Warn(errors.E(op, err))
		}
	}
	s.mempoolTxs(relevant)
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

	idx := 0
FilterLoop:
	for idx < len(chain) {
		var fmatches []*chainhash.Hash
		var fmatchidx []int
		var fmatchMu sync.Mutex

		// Scan remaining filters with up to ncpu workers
		c := make(chan int)
		var wg sync.WaitGroup
		worker := func() {
			for i := range c {
				n := chain[i]
				f := n.Filter
				k := blockcf.Key(n.Hash)
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
		for i := idx; i < len(chain); i++ {
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
				err = validate.RegularCFilter(b, chain[i].Filter)
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

		for i := idx; i < len(chain); i++ {
			b := fetched[i]
			if b == nil {
				continue
			}
			matches, fadded := s.rescanBlock(b)
			found[*chain[i].Hash] = matches
			if len(fadded) != 0 {
				idx = i + 1
				filterData = fadded
				continue FilterLoop
			}
		}
		return found, nil
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

	blockHashes := make([]*chainhash.Hash, 0, len(headers))
	for _, h := range headers {
		hash := h.BlockHash()
		blockHashes = append(blockHashes, &hash)
	}
	filters, err := rp.CFilters(ctx, blockHashes)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	newBlocks := make([]*wallet.BlockNode, 0, len(headers))
	var bestChain []*wallet.BlockNode
	var matchingTxs map[chainhash.Hash][]*wire.MsgTx
	err = func() error {
		defer s.sidechainMu.Unlock()
		s.sidechainMu.Lock()

		for i := range headers {
			haveBlock, _, err := s.wallet.BlockInMainChain(ctx, blockHashes[i])
			if err != nil {
				return err
			}
			if haveBlock {
				continue
			}
			n := wallet.NewBlockNode(headers[i], blockHashes[i], filters[i])
			if s.sidechains.AddBlockNode(n) {
				newBlocks = append(newBlocks, n)
			}
		}

		bestChain, err = s.wallet.EvaluateBestChain(ctx, &s.sidechains)
		if err != nil {
			return err
		}

		if len(bestChain) == 0 {
			return nil
		}

		_, err = s.wallet.ValidateHeaderChainDifficulties(ctx, bestChain, 0)
		if err != nil {
			return err
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
		s.tipChanged(bestChain[len(bestChain)-1].Header, int32(len(prevChain)), matchingTxs)

		return nil
	}()
	if err != nil {
		return err
	}

	if len(bestChain) != 0 {
		s.locatorMu.Lock()
		s.currentLocators = nil
		s.locatorGeneration++
		s.locatorMu.Unlock()
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
		log.Infof("Received sidechain or orphan block %v, height %v", n.Hash, n.Header.Height)
	}

	return nil
}

// hashStop is a zero value stop hash for fetching all possible data using
// locators.
var hashStop chainhash.Hash

// getHeaders iteratively fetches headers from rp using the latest locators.
// Returns when no more headers are available.  A sendheaders message is pushed
// to the peer when there are no more headers to fetch.
func (s *Syncer) getHeaders(ctx context.Context, rp *p2p.RemotePeer) error {
	var locators []*chainhash.Hash
	var generation uint
	var err error
	s.locatorMu.Lock()
	locators = s.currentLocators
	generation = s.locatorGeneration
	if len(locators) == 0 {
		locators, err = s.wallet.BlockLocators(ctx, nil)
		if err != nil {
			s.locatorMu.Unlock()
			return err
		}
		s.currentLocators = locators
		s.locatorGeneration++
	}
	s.locatorMu.Unlock()

	var lastHeight int32

	for {
		headers, err := rp.Headers(ctx, locators, &hashStop)
		if err != nil {
			return err
		}

		if len(headers) == 0 {
			// Ensure that the peer provided headers through the height
			// advertised during handshake.
			if lastHeight < rp.InitialHeight() {
				// Peer may not have provided any headers if our own locators
				// were up to date.  Compare the best locator hash with the
				// advertised height.
				h, err := s.wallet.BlockHeader(ctx, locators[0])
				if err == nil && int32(h.Height) < rp.InitialHeight() {
					return errors.E(errors.Protocol, "peer did not provide "+
						"headers through advertised height")
				}
			}

			return rp.SendHeaders(ctx)
		}

		lastHeight = int32(headers[len(headers)-1].Height)

		nodes := make([]*wallet.BlockNode, len(headers))
		g, ctx := errgroup.WithContext(ctx)
		for i := range headers {
			i := i
			g.Go(func() error {
				header := headers[i]
				hash := header.BlockHash()
				filter, err := rp.CFilter(ctx, &hash)
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
		s.sidechainMu.Lock()
		for _, n := range nodes {
			haveBlock, _, _ := s.wallet.BlockInMainChain(ctx, n.Hash)
			if haveBlock {
				continue
			}
			if s.sidechains.AddBlockNode(n) {
				added++
			}
		}
		if added == 0 {
			s.sidechainMu.Unlock()

			s.locatorMu.Lock()
			if s.locatorGeneration > generation {
				locators = s.currentLocators
			}
			if len(locators) == 0 {
				locators, err = s.wallet.BlockLocators(ctx, nil)
				if err != nil {
					s.locatorMu.Unlock()
					return err
				}
				s.currentLocators = locators
				s.locatorGeneration++
				generation = s.locatorGeneration
			}
			s.locatorMu.Unlock()
			continue
		}
		s.fetchHeadersProgress(headers[len(headers)-1])
		log.Debugf("Fetched %d new header(s) ending at height %d from %v",
			added, nodes[len(nodes)-1].Header.Height, rp)

		bestChain, err := s.wallet.EvaluateBestChain(ctx, &s.sidechains)
		if err != nil {
			s.sidechainMu.Unlock()
			return err
		}
		if len(bestChain) == 0 {
			s.sidechainMu.Unlock()
			continue
		}

		_, err = s.wallet.ValidateHeaderChainDifficulties(ctx, bestChain, 0)
		if err != nil {
			s.sidechainMu.Unlock()
			return err
		}

		prevChain, err := s.wallet.ChainSwitch(ctx, &s.sidechains, bestChain, nil)
		if err != nil {
			s.sidechainMu.Unlock()
			return err
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

		s.sidechainMu.Unlock()

		// Generate new locators
		s.locatorMu.Lock()
		locators, err = s.wallet.BlockLocators(ctx, nil)
		if err != nil {
			s.locatorMu.Unlock()
			return err
		}
		s.currentLocators = locators
		s.locatorGeneration++
		s.locatorMu.Unlock()
	}
}

func (s *Syncer) startupSync(ctx context.Context, rp *p2p.RemotePeer) error {
	// Disconnect from the peer if their advertised block height is
	// significantly behind the wallet's.
	_, tipHeight := s.wallet.MainChainTip(ctx)
	if rp.InitialHeight() < tipHeight-6 {
		return errors.E("peer is not synced")
	}
	s.fetchMissingCfiltersStart()
	progress := make(chan wallet.MissingCFilterProgress, 1)
	go s.wallet.FetchMissingCFiltersWithProgress(ctx, rp, progress)

	for p := range progress {
		if p.Err != nil {
			return p.Err
		}
		s.fetchMissingCfiltersProgress(p.BlockHeightStart, p.BlockHeightEnd)
	}
	s.fetchMissingCfiltersFinished()

	// Fetch any unseen headers from the peer.
	s.fetchHeadersStart()
	log.Debugf("Fetching headers from %v", rp.RemoteAddr())
	err := s.getHeaders(ctx, rp)
	if err != nil {
		return err
	}
	s.fetchHeadersFinished()

	if atomic.CompareAndSwapUint32(&s.atomicCatchUpTryLock, 0, 1) {
		err = func() error {
			rescanPoint, err := s.wallet.RescanPoint(ctx)
			if err != nil {
				return err
			}
			if rescanPoint == nil {
				if !s.loadedFilters {
					err = s.wallet.LoadActiveDataFilters(ctx, s, true)
					if err != nil {
						return err
					}
					s.loadedFilters = true
				}

				s.synced()

				return nil
			}
			// RescanPoint is != nil so we are not synced to the peer and
			// check to see if it was previously synced
			s.unsynced()

			s.discoverAddressesStart()
			err = s.wallet.DiscoverActiveAddresses(ctx, rp, rescanPoint, s.discoverAccounts)
			if err != nil {
				return err
			}
			s.discoverAddressesFinished()
			s.discoverAccounts = false

			err = s.wallet.LoadActiveDataFilters(ctx, s, true)
			if err != nil {
				return err
			}
			s.loadedFilters = true

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

			s.synced()

			return nil
		}()
		atomic.StoreUint32(&s.atomicCatchUpTryLock, 0)
		if err != nil {
			return err
		}
	}

	unminedTxs, err := s.wallet.UnminedTransactions(ctx)
	if err != nil {
		log.Errorf("Cannot load unmined transactions for resending: %v", err)
		return nil
	}
	if len(unminedTxs) == 0 {
		return nil
	}
	err = rp.PublishTransactions(ctx, unminedTxs...)
	if err != nil {
		// TODO: Transactions should be removed if this is a double spend.
		log.Errorf("Failed to resent one or more unmined transactions: %v", err)
	}
	return nil
}
