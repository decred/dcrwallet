// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v2"
	"golang.org/x/sync/errgroup"
)

// RPCSyncer implements wallet synchronization services by processing
// notifications from a dcrd JSON-RPC server.
type RPCSyncer struct {
	atomicWalletSynced uint32 // CAS (synced=1) when wallet syncing complete

	wallet    *wallet.Wallet
	rpcClient *RPCClient

	discoverAccts bool
	mu            sync.Mutex

	// Holds all potential callbacks used to notify clients
	notifications *Notifications
}

// NewRPCSyncer creates an RPCSyncer that will sync the wallet using the RPC
// client.
func NewRPCSyncer(w *wallet.Wallet, rpcClient *RPCClient) *RPCSyncer {
	return &RPCSyncer{
		wallet:        w,
		rpcClient:     rpcClient,
		discoverAccts: !w.Locked(),
	}
}

// Notifications struct to contain all of the upcoming callbacks that will
// be used to update the rpc streams for syncing.
type Notifications struct {
	Synced                       func(sync bool)
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

// SetNotifications sets the possible various callbacks that are used
// to notify interested parties to the syncing progress.
func (s *RPCSyncer) SetNotifications(ntfns *Notifications) {
	s.notifications = ntfns
}

// synced checks the atomic that controls wallet syncness and if previously
// unsynced, updates to synced and notifies the callback, if set.
func (s *RPCSyncer) synced() {
	if atomic.CompareAndSwapUint32(&s.atomicWalletSynced, 0, 1) &&
		s.notifications != nil &&
		s.notifications.Synced != nil {
		s.notifications.Synced(true)
	}
}

// unsynced checks the atomic that controls wallet syncness and if previously
// synced, updates to unsynced and notifies the callback, if set.
func (s *RPCSyncer) unsynced() {
	if atomic.CompareAndSwapUint32(&s.atomicWalletSynced, 1, 0) &&
		s.notifications != nil &&
		s.notifications.Synced != nil {
		s.notifications.Synced(false)
	}
}

func (s *RPCSyncer) fetchMissingCfiltersStart() {
	if s.notifications != nil && s.notifications.FetchMissingCFiltersStarted != nil {
		s.notifications.FetchMissingCFiltersStarted()
	}
}

func (s *RPCSyncer) fetchMissingCfiltersProgress(startMissingCFilterHeight, endMissinCFilterHeight int32) {
	if s.notifications != nil && s.notifications.FetchMissingCFiltersProgress != nil {
		s.notifications.FetchMissingCFiltersProgress(startMissingCFilterHeight, endMissinCFilterHeight)
	}
}

func (s *RPCSyncer) fetchMissingCfiltersFinished() {
	if s.notifications != nil && s.notifications.FetchMissingCFiltersFinished != nil {
		s.notifications.FetchMissingCFiltersFinished()
	}
}

func (s *RPCSyncer) fetchHeadersStart() {
	if s.notifications != nil && s.notifications.FetchHeadersStarted != nil {
		s.notifications.FetchHeadersStarted()
	}
}

func (s *RPCSyncer) fetchHeadersProgress(fetchedHeadersCount int32, lastHeaderTime int64) {
	if s.notifications != nil && s.notifications.FetchHeadersProgress != nil {
		s.notifications.FetchHeadersProgress(fetchedHeadersCount, lastHeaderTime)
	}
}

func (s *RPCSyncer) fetchHeadersFinished() {
	if s.notifications != nil && s.notifications.FetchHeadersFinished != nil {
		s.notifications.FetchHeadersFinished()
	}
}
func (s *RPCSyncer) discoverAddressesStart() {
	if s.notifications != nil && s.notifications.DiscoverAddressesStarted != nil {
		s.notifications.DiscoverAddressesStarted()
	}
}

func (s *RPCSyncer) discoverAddressesFinished() {
	if s.notifications != nil && s.notifications.DiscoverAddressesFinished != nil {
		s.notifications.DiscoverAddressesFinished()
	}
}

func (s *RPCSyncer) rescanStart() {
	if s.notifications != nil && s.notifications.RescanStarted != nil {
		s.notifications.RescanStarted()
	}
}

func (s *RPCSyncer) rescanProgress(rescannedThrough int32) {
	if s.notifications != nil && s.notifications.RescanProgress != nil {
		s.notifications.RescanProgress(rescannedThrough)
	}
}

func (s *RPCSyncer) rescanFinished() {
	if s.notifications != nil && s.notifications.RescanFinished != nil {
		s.notifications.RescanFinished()
	}
}

// Run synchronizes the wallet, returning when synchronization fails or the
// context is cancelled.  If startupSync is true, all synchronization tasks
// needed to fully register the wallet for notifications and synchronize it with
// the dcrd server are performed.  Otherwise, it will listen for notifications
// but not register for any updates.
func (s *RPCSyncer) Run(ctx context.Context, startupSync bool) error {
	const op errors.Op = "rpcsyncer.Run"

	tipHash, tipHeight := s.wallet.MainChainTip()
	rescanPoint, err := s.wallet.RescanPoint()
	if err != nil {
		return errors.E(op, err)
	}
	log.Infof("Headers synced through block %v height %d", &tipHash, tipHeight)
	if rescanPoint != nil {
		h, err := s.wallet.BlockHeader(rescanPoint)
		if err != nil {
			return errors.E(op, err)
		}
		// The rescan point is the first block that does not have synced
		// transactions, so we are synced with the parent.
		log.Infof("Transactions synced through block %v height %d", &h.PrevBlock, h.Height-1)
	} else {
		log.Infof("Transactions synced through block %v height %d", &tipHash, tipHeight)
	}

	// TODO: handling of voting notifications should be done sequentially with
	// every other notification (voters must know the blocks they are voting
	// on).  Until then, a couple notification processing goroutines must be
	// started and errors merged.
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if startupSync {
			err := s.startupSync(ctx)
			if err != nil {
				return err
			}
		}
		return s.handleNotifications(ctx)
	})
	g.Go(func() error {
		return s.handleVoteNotifications(ctx)
	})
	g.Go(func() error {
		err := s.rpcClient.NotifySpentAndMissedTickets()
		if err != nil {
			const op errors.Op = "dcrd.jsonrpc.notifyspentandmissedtickets"
			return errors.E(op, err)
		}

		if s.wallet.VotingEnabled() {
			// Request notifications for winning tickets.
			err := s.rpcClient.NotifyWinningTickets()
			if err != nil {
				const op errors.Op = "dcrd.jsonrpc.notifywinningtickets"
				return errors.E(op, err)
			}

			vb := s.wallet.VoteBits()
			log.Infof("Wallet voting enabled: vote bits = %#04x, "+
				"extended vote bits = %x", vb.Bits, vb.ExtendedBits)
			log.Infof("Please ensure your wallet remains unlocked so it may vote")
		}

		return nil
	})
	err = g.Wait()
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

func (s *RPCSyncer) handleNotifications(ctx context.Context) error {
	// connectingBlocks keeps track of whether any blocks have been successfully
	// attached to the main chain.  Once any blocks have attached, if a future
	// block fails to attach, the error is fatal.  Otherwise, errors are logged.
	connectingBlocks := false

	sidechains := new(wallet.SidechainForest)
	var relevantTxs map[chainhash.Hash][]*wire.MsgTx

	c := s.rpcClient.notifications()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case n, ok := <-c:
			if !ok {
				return errors.E(errors.NoPeers, "RPC client disconnected")
			}

			var op errors.Op
			var err error
			nonFatal := false
			switch n := n.(type) {
			case blockConnected:
				op = "dcrd.jsonrpc.blockconnected"
				header := new(wire.BlockHeader)
				err = header.Deserialize(bytes.NewReader(n.blockHeader))
				if err != nil {
					break
				}
				blockHash := header.BlockHash()
				getCfilter := s.rpcClient.GetCFilterAsync(&blockHash, wire.GCSFilterRegular)
				var rpt *chainhash.Hash
				rpt, err = s.wallet.RescanPoint()
				if err != nil {
					break
				}
				if rpt == nil {
					txs := make([]*wire.MsgTx, 0, len(n.transactions))
					for _, tx := range n.transactions {
						msgTx := new(wire.MsgTx)
						err = msgTx.Deserialize(bytes.NewReader(tx))
						if err != nil {
							break
						}
						txs = append(txs, msgTx)
					}
					if relevantTxs == nil {
						relevantTxs = make(map[chainhash.Hash][]*wire.MsgTx)
					}
					relevantTxs[blockHash] = txs
				}

				var f *gcs.Filter
				f, err = getCfilter.Receive()
				if err != nil {
					break
				}

				blockNode := wallet.NewBlockNode(header, &blockHash, f)
				sidechains.AddBlockNode(blockNode)

				var bestChain []*wallet.BlockNode
				bestChain, err = s.wallet.EvaluateBestChain(sidechains)
				if err != nil {
					break
				}
				if len(bestChain) != 0 {
					var prevChain []*wallet.BlockNode
					prevChain, err = s.wallet.ChainSwitch(sidechains, bestChain, relevantTxs)
					if err == nil {
						connectingBlocks = true
					}
					nonFatal = !connectingBlocks
					if err != nil {
						break
					}

					if len(prevChain) != 0 {
						log.Infof("Reorganize from %v to %v (total %d block(s) reorged)",
							prevChain[len(prevChain)-1].Hash, bestChain[len(bestChain)-1].Hash, len(prevChain))
						for _, n := range prevChain {
							sidechains.AddBlockNode(n)
						}
					}
					for _, n := range bestChain {
						log.Infof("Connected block %v, height %d, %d wallet transaction(s)",
							n.Hash, n.Header.Height, len(relevantTxs[*n.Hash]))
					}

					relevantTxs = nil
				}

			case blockDisconnected, reorganization:
				continue // These notifications are ignored

			case relevantTxAccepted:
				op = "dcrd.jsonrpc.relevanttxaccepted"
				nonFatal = true
				var rpt *chainhash.Hash
				rpt, err = s.wallet.RescanPoint()
				if err != nil || rpt != nil {
					break
				}
				err = s.wallet.AcceptMempoolTx(n.transaction)

			case missedTickets:
				op = "dcrd.jsonrpc.spentandmissedtickets"
				err = s.wallet.RevokeOwnedTickets(n.tickets)
				nonFatal = true

			default:
				log.Warnf("Notification handler received unknown notification type %T", n)
				continue
			}

			if err == nil {
				continue
			}

			err = errors.E(op, err)
			if nonFatal {
				log.Errorf("Failed to process consensus server notification: %v", err)
				continue
			}

			return err
		}
	}
}

func (s *RPCSyncer) handleVoteNotifications(ctx context.Context) error {
	c := s.rpcClient.notificationsVoting()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case n, ok := <-c:
			if !ok {
				return errors.E(errors.NoPeers, "RPC client disconnected")
			}

			var op errors.Op
			var err error
			switch n := n.(type) {
			case winningTickets:
				op = "dcrd.jsonrpc.winningtickets"
				err = s.wallet.VoteOnOwnedTickets(n.tickets, n.blockHash, int32(n.blockHeight))
			default:
				log.Warnf("Voting handler received unknown notification type %T", n)
			}
			if err != nil {
				err = errors.E(op, err)
				log.Errorf("Failed to process consensus server notification: %v", err)
			}
		}
	}
}

// ctxdo executes f, returning the earliest of f's returned error or a context
// error.  ctxdo adds early returns for operations which do not understand
// context, but the operation is not canceled and will continue executing in
// the background.  If f returns non-nil before the context errors, the error is
// wrapped with op.
func ctxdo(ctx context.Context, op errors.Op, f func() error) error {
	e := make(chan error, 1)
	go func() { e <- f() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-e:
		if err != nil {
			return errors.E(op, err)
		}
		return nil
	}
}

// hashStop is a zero value stop hash for fetching all possible data using
// locators.
var hashStop chainhash.Hash

// startupSync brings the wallet up to date with the current chain server
// connection.  It creates a rescan request and blocks until the rescan has
// finished.
func (s *RPCSyncer) startupSync(ctx context.Context) error {
	n := BackendFromRPCClient(s.rpcClient.Client)

	// Fetch any missing main chain compact filters.
	s.fetchMissingCfiltersStart()
	progress := make(chan wallet.MissingCFilterProgress, 1)
	go s.wallet.FetchMissingCFiltersWithProgress(ctx, n, progress)

	for p := range progress {
		if p.Err != nil {
			return p.Err
		}
		s.fetchMissingCfiltersProgress(p.BlockHeightStart, p.BlockHeightEnd)
	}
	s.fetchMissingCfiltersFinished()

	// Request notifications for connected and disconnected blocks.
	err := s.rpcClient.NotifyBlocks()
	if err != nil {
		const op errors.Op = "dcrd.jsonrpc.notifyblocks"
		return errors.E(op, err)
	}

	// Fetch new headers and cfilters from the server.
	var sidechains wallet.SidechainForest
	locators, err := s.wallet.BlockLocators(nil)
	if err != nil {
		return err
	}

	s.fetchHeadersStart()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var headers []*wire.BlockHeader
		err := ctxdo(ctx, "dcrd.jsonrpc.getheaders", func() error {
			headersMsg, err := s.rpcClient.GetHeaders(locators, &hashStop)
			if err != nil {
				return err
			}
			headers = make([]*wire.BlockHeader, 0, len(headersMsg.Headers))
			for _, h := range headersMsg.Headers {
				header := new(wire.BlockHeader)
				err := header.Deserialize(hex.NewDecoder(strings.NewReader(h)))
				if err != nil {
					return err
				}
				headers = append(headers, header)
			}
			return nil
		})
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
				var filter *gcs.Filter
				err := ctxdo(ctx, "", func() error {
					const opf = "dcrd.jsonrpc.getcfilter(%v)"
					var err error
					filter, err = s.rpcClient.GetCFilter(&hash, wire.GCSFilterRegular)
					if err != nil {
						op := errors.Opf(opf, &hash)
						err = errors.E(op, err)
					}
					return err
				})
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
			haveBlock, _, _ := s.wallet.BlockInMainChain(n.Hash)
			if haveBlock {
				continue
			}
			if sidechains.AddBlockNode(n) {
				added++
			}
		}

		s.fetchHeadersProgress(int32(added), headers[len(headers)-1].Timestamp.Unix())

		log.Infof("Fetched %d new header(s) ending at height %d from %v",
			added, nodes[len(nodes)-1].Header.Height, s.rpcClient)

		// Stop fetching headers when no new blocks are returned.
		// Because getheaders did return located blocks, this indicates
		// that the server is not as far synced as the wallet.  Blocks
		// the server has not processed are not reorged out of the
		// wallet at this time, but a reorg will switch to a better
		// chain later if one is discovered.
		if added == 0 {
			break
		}

		bestChain, err := s.wallet.EvaluateBestChain(&sidechains)
		if err != nil {
			return err
		}
		if len(bestChain) == 0 {
			continue
		}

		_, err = s.wallet.ValidateHeaderChainDifficulties(bestChain, 0)
		if err != nil {
			return err
		}

		prevChain, err := s.wallet.ChainSwitch(&sidechains, bestChain, nil)
		if err != nil {
			return err
		}

		if len(prevChain) != 0 {
			log.Infof("Reorganize from %v to %v (total %d block(s) reorged)",
				prevChain[len(prevChain)-1].Hash, bestChain[len(bestChain)-1].Hash, len(prevChain))
			for _, n := range prevChain {
				sidechains.AddBlockNode(n)
			}
		}
		tip := bestChain[len(bestChain)-1]
		if len(bestChain) == 1 {
			log.Infof("Connected block %v, height %d", tip.Hash, tip.Header.Height)
		} else {
			log.Infof("Connected %d blocks, new tip block %v, height %d, date %v",
				len(bestChain), tip.Hash, tip.Header.Height, tip.Header.Timestamp)
		}

		locators, err = s.wallet.BlockLocators(nil)
		if err != nil {
			return err
		}
	}
	s.fetchHeadersFinished()

	rescanPoint, err := s.wallet.RescanPoint()
	if err != nil {
		return err
	}
	if rescanPoint != nil {
		s.mu.Lock()
		discoverAccts := s.discoverAccts
		s.mu.Unlock()
		s.discoverAddressesStart()
		err = s.wallet.DiscoverActiveAddresses(ctx, n, rescanPoint, discoverAccts)
		if err != nil {
			return err
		}
		s.discoverAddressesFinished()
		s.mu.Lock()
		s.discoverAccts = false
		s.mu.Unlock()
		err = s.wallet.LoadActiveDataFilters(ctx, n, true)
		if err != nil {
			return err
		}

		s.rescanStart()
		rescanBlock, err := s.wallet.BlockHeader(rescanPoint)
		if err != nil {
			return err
		}
		progress := make(chan wallet.RescanProgress, 1)
		go s.wallet.RescanProgressFromHeight(ctx, n, int32(rescanBlock.Height), progress)

		for p := range progress {
			if p.Err != nil {
				return p.Err
			}
			s.rescanProgress(p.ScannedThrough)
		}
		s.rescanFinished()

	} else {
		err = s.wallet.LoadActiveDataFilters(ctx, n, true)
		if err != nil {
			return err
		}
	}
	s.synced()

	// Rebroadcast unmined transactions
	err = s.wallet.PublishUnminedTransactions(ctx, n)
	if err != nil {
		// Returning this error would end and (likely) restart sync in
		// an endless loop.  It's possible a transaction should be
		// removed, but this is difficult to reliably detect over RPC.
		log.Warnf("Could not publish one or more unmined transactions: %v", err)
	}

	_, err = s.rpcClient.RawRequest("rebroadcastwinners", nil)
	if err != nil {
		const op errors.Op = "dcrd.jsonrpc.rebroadcastwinners"
		return errors.E(op, err)
	}
	_, err = s.rpcClient.RawRequest("rebroadcastmissed", nil)
	if err != nil {
		const op errors.Op = "dcrd.jsonrpc.rebroadcastmissed"
		return errors.E(op, err)
	}

	log.Infof("Blockchain sync completed, wallet ready for general usage.")

	return nil
}
