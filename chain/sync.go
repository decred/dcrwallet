// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"context"
	"fmt"

	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/wallet"
	"golang.org/x/sync/errgroup"
)

// RPCSyncer implements wallet synchronization services by processing
// notifications from a dcrd JSON-RPC server.
type RPCSyncer struct {
	wallet    *wallet.Wallet
	rpcClient *RPCClient
}

// NewRPCSyncer creates an RPCSyncer that will sync the wallet using the RPC
// client.
func NewRPCSyncer(w *wallet.Wallet, rpcClient *RPCClient) *RPCSyncer {
	return &RPCSyncer{w, rpcClient}
}

// Run synchronizes the wallet, returning when synchronization fails or the
// context is cancelled.  If startupSync is true, all synchronization tasks
// needed to fully register the wallet for notifications and synchronize it with
// the dcrd server are performed.  Otherwise, it will listen for notifications
// but not register for any updates.
func (s *RPCSyncer) Run(ctx context.Context, startupSync bool) error {
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
			return fmt.Errorf("failed to register for spentandmissedtickets "+
				"notifications: %v", err)
		}

		if s.wallet.VotingEnabled() {
			// Request notifications for winning tickets.
			err := s.rpcClient.NotifyWinningTickets()
			if err != nil {
				return fmt.Errorf("failed to register for winningtickets "+
					"notifications: %v", err)
			}

			vb := s.wallet.VoteBits()
			log.Infof("Wallet voting enabled: vote bits = %#04x, "+
				"extended vote bits = %x", vb.Bits, vb.ExtendedBits)
			log.Infof("Please ensure your wallet remains unlocked so it may vote")
		}

		return nil
	})
	return g.Wait()
}

func (s *RPCSyncer) handleNotifications(ctx context.Context) error {
	// connectingBlocks keeps track of whether any blocks have been successfully
	// attached to the main chain.  Once any blocks have attached, if a future
	// block fails to attach, the error is fatal.  Otherwise, errors are logged.
	connectingBlocks := false

	c := s.rpcClient.notifications()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case n, ok := <-c:
			if !ok {
				return apperrors.New(apperrors.ErrDisconnected, "RPC client disconnected")
			}

			var notificationName string
			var err error
			nonFatal := false
			switch n := n.(type) {
			case blockConnected:
				notificationName = "blockconnected"
				err = s.wallet.ConnectBlock(n.blockHeader, n.transactions)
				if err == nil {
					connectingBlocks = true
				}
				nonFatal = !connectingBlocks

			case blockDisconnected:
				continue // These notifications are ignored

			case reorganization:
				notificationName = "reorganizing"
				err = s.wallet.StartReorganize(n.oldHash, n.newHash, n.oldHeight, n.newHeight)

			case relevantTxAccepted:
				notificationName = "relevanttxaccepted"
				err = s.wallet.AcceptMempoolTx(n.transaction)
				nonFatal = true

			case missedTickets:
				notificationName = "spentandmissedtickets"
				err = s.wallet.RevokeOwnedTickets(n.tickets)
				nonFatal = true

			default:
				log.Warnf("Notification handler received unknown notification type %T", n)
				continue
			}

			switch {
			case err == nil:
			case nonFatal:
				log.Errorf("Failed to process consensus server notification "+
					"(name: `%s`, detail: `%v`)", notificationName, err)
			default:
				return fmt.Errorf("failed to process consensus server notification "+
					"(name: `%s`, detail: `%v`)", notificationName, err)
			}
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
				return apperrors.New(apperrors.ErrDisconnected, "RPC client disconnected")
			}

			var notificationName string
			var err error
			switch n := n.(type) {
			case winningTickets:
				notificationName = "winningtickets"
				err = s.wallet.VoteOnOwnedTickets(n.tickets, n.blockHash, int32(n.blockHeight))
			default:
				log.Warnf("Voting handler received unknown notification type %T", n)
			}
			if err != nil {
				log.Errorf("Failed to handle %s notification: %v", notificationName, err)
			}
		}
	}
}

// startupSync brings the wallet up to date with the current chain server
// connection.  It creates a rescan request and blocks until the rescan has
// finished.
func (s *RPCSyncer) startupSync(ctx context.Context) error {
	// Request notifications for connected and disconnected blocks.
	err := s.rpcClient.NotifyBlocks()
	if err != nil {
		return err
	}

	n := BackendFromRPCClient(s.rpcClient.Client)

	// Discover any addresses for this wallet that have not yet been created.
	err = s.wallet.DiscoverActiveAddresses(n, !s.wallet.Locked())
	if err != nil {
		return err
	}

	// Load transaction filters with all active addresses and watched outpoints.
	err = s.wallet.LoadActiveDataFilters(n)
	if err != nil {
		return err
	}

	// Fetch headers for unseen blocks in the main chain, determine whether a
	// rescan is necessary, and when to begin it.
	fetchedHeaderCount, rescanStart, _, _, _, err := s.wallet.FetchHeaders(n)
	if err != nil {
		return err
	}

	// Rescan when necessary.
	if fetchedHeaderCount != 0 {
		err := s.wallet.Rescan(ctx, n, &rescanStart)
		if err != nil {
			return err
		}
	}

	unminedTxs, err := s.wallet.UnminedTransactions()
	if err != nil {
		log.Errorf("Cannot load unmined transactions for resending: %v", err)
		unminedTxs = nil
	}
	for _, tx := range unminedTxs {
		txHash := tx.TxHash()
		err := n.PublishTransaction(ctx, tx)
		if err != nil {
			// TODO: Transactions should be removed if this is a double spend.
			log.Tracef("Could not resend transaction %v: %v", &txHash, err)
			continue
		}
		log.Tracef("Resent unmined transaction %v", &txHash)
	}

	_, err = s.rpcClient.RawRequest("rebroadcastwinners", nil)
	if err != nil {
		return err
	}
	_, err = s.rpcClient.RawRequest("rebroadcastmissed", nil)
	if err != nil {
		return err
	}

	log.Infof("Blockchain sync completed, wallet ready for general usage.")

	return nil
}
