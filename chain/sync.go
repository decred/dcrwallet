// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"bytes"
	"context"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"golang.org/x/sync/errgroup"
)

// RPCSyncer implements wallet synchronization services by processing
// notifications from a dcrd JSON-RPC server.
type RPCSyncer struct {
	wallet    *wallet.Wallet
	rpcClient *RPCClient

	discoverAccts bool
	mu            sync.Mutex
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
				var txs []*wire.MsgTx
				rpt, err := s.wallet.RescanPoint()
				if err != nil {
					break
				}
				if rpt == nil {
					txs = make([]*wire.MsgTx, 0, len(n.transactions))
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

				f, err := getCfilter.Receive()
				if err != nil {
					break
				}

				blockNode := wallet.NewBlockNode(header, &blockHash, f)
				sidechains.AddBlockNode(blockNode)

				bestChain, err := s.wallet.EvaluateBestChain(sidechains)
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
					relevantTxs = nil

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
	err := s.wallet.FetchMissingCFilters(ctx, n)
	if err != nil {
		return err
	}

	// Request notifications for connected and disconnected blocks.
	err = s.rpcClient.NotifyBlocks()
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
				err := header.Deserialize(newHexReader(h))
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
				err := ctxdo(ctx, "dcrd.jsonrpc.getcfilter", func() error {
					var err error
					filter, err = s.rpcClient.GetCFilter(&hash, wire.GCSFilterRegular)
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

		log.Infof("Fetched %d new header(s) ending at height %d from %v",
			added, nodes[len(nodes)-1].Header.Height, s.rpcClient)
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

	rescanPoint, err := s.wallet.RescanPoint()
	if err != nil {
		return err
	}
	if rescanPoint != nil {
		s.mu.Lock()
		discoverAccts := s.discoverAccts
		s.mu.Unlock()
		err = s.wallet.DiscoverActiveAddresses(ctx, n, rescanPoint, discoverAccts)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.discoverAccts = false
		s.mu.Unlock()
		err = s.wallet.LoadActiveDataFilters(ctx, n, true)
		if err != nil {
			return err
		}
		err = s.wallet.Rescan(ctx, n, rescanPoint)
		if err != nil {
			return err
		}
	} else {
		err = s.wallet.LoadActiveDataFilters(ctx, n, true)
		if err != nil {
			return err
		}
	}

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
