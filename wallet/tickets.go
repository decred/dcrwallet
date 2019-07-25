// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/rpc/client/dcrd"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"golang.org/x/sync/errgroup"
)

// GenerateVoteTx creates a vote transaction for a chosen ticket purchase hash
// using the provided votebits.  The ticket purchase transaction must be stored
// by the wallet.
func (w *Wallet) GenerateVoteTx(blockHash *chainhash.Hash, height int32, ticketHash *chainhash.Hash, voteBits stake.VoteBits) (*wire.MsgTx, error) {
	const op errors.Op = "wallet.GenerateVoteTx"

	var vote *wire.MsgTx
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		ticketPurchase, err := w.TxStore.Tx(txmgrNs, ticketHash)
		if err != nil {
			return err
		}
		vote, err = createUnsignedVote(ticketHash, ticketPurchase,
			height, blockHash, voteBits, w.subsidyCache, w.chainParams)
		if err != nil {
			return errors.E(op, err)
		}
		err = w.signVote(addrmgrNs, ticketPurchase, vote)
		if err != nil {
			return errors.E(op, err)
		}
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return vote, nil
}

// LiveTicketHashes returns the hashes of live tickets that the wallet has
// purchased or has voting authority for.
func (w *Wallet) LiveTicketHashes(ctx context.Context, rpcCaller Caller, includeImmature bool) ([]chainhash.Hash, error) {
	const op errors.Op = "wallet.LiveTicketHashes"

	var ticketHashes []chainhash.Hash
	var maybeLive []*chainhash.Hash

	extraTickets := w.StakeMgr.DumpSStxHashes()

	var tipHeight int32 // Assigned in view below.

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Remove tickets from the extraTickets slice if they will appear in the
		// ticket iteration below.
		hashes := extraTickets
		extraTickets = hashes[:0]
		for i := range hashes {
			h := &hashes[i]
			if !w.TxStore.ExistsTx(txmgrNs, h) {
				extraTickets = append(extraTickets, *h)
			}
		}

		_, tipHeight = w.TxStore.MainChainTip(txmgrNs)

		it := w.TxStore.IterateTickets(dbtx)
		defer it.Close()
		for it.Next() {
			// Tickets that are mined at a height beyond the expiry height can
			// not be live.
			if ticketExpired(w.chainParams, it.Block.Height, tipHeight) {
				continue
			}

			// Tickets that have not reached ticket maturity are immature.
			// Exclude them unless the caller requested to include immature
			// tickets.
			if !ticketMatured(w.chainParams, it.Block.Height, tipHeight) {
				if includeImmature {
					ticketHashes = append(ticketHashes, it.Hash)
				}
				continue
			}

			// The ticket may be live.  Because the selected state of tickets is
			// not yet known by the wallet, this must be queried over RPC.  Add
			// this hash to a slice of ticket purchase hashes to check later.
			hash := it.Hash
			maybeLive = append(maybeLive, &hash)
		}
		return it.Err()
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Determine if the extra tickets are immature or possibly live.  Because
	// these transactions are not part of the wallet's transaction history, dcrd
	// must be queried for their blockchain height.  This functionality requires
	// the dcrd transaction index to be enabled.
	var g errgroup.Group
	type extraTicketResult struct {
		valid  bool // unspent with known height
		height int32
	}
	extraTicketResults := make([]extraTicketResult, len(extraTickets))
	for i := range extraTickets {
		i := i
		g.Go(func() error {
			// gettxout is used first as an optimization to check that output 0
			// of the ticket is unspent.
			var txOut *dcrdtypes.GetTxOutResult
			err := rpcCaller.Call(ctx, "gettxout", &txOut, extraTickets[i].String(), 0)
			if err != nil || txOut == nil {
				return nil
			}
			var grt struct {
				BlockHeight int32 `json:"blockheight"`
			}
			err = rpcCaller.Call(ctx, "getrawtransaction", &grt, extraTickets[i].String(), 1)
			if err != nil {
				return nil
			}
			extraTicketResults[i] = extraTicketResult{true, grt.BlockHeight}
			return nil
		})
	}
	err = g.Wait()
	if err != nil {
		return nil, err
	}
	for i := range extraTickets {
		r := &extraTicketResults[i]
		if !r.valid {
			continue
		}
		// Same checks as above in the db view.
		if ticketExpired(w.chainParams, r.height, tipHeight) {
			continue
		}
		if !ticketMatured(w.chainParams, r.height, tipHeight) {
			if includeImmature {
				ticketHashes = append(ticketHashes, extraTickets[i])
			}
			continue
		}
		maybeLive = append(maybeLive, &extraTickets[i])
	}

	// If there are no possibly live tickets to check, ticketHashes contains all
	// of the results.
	if len(maybeLive) == 0 {
		return ticketHashes, nil
	}

	// Use RPC to query which of the possibly-live tickets are really live.
	rpc := dcrd.New(rpcCaller)
	live, err := rpc.ExistsLiveTickets(ctx, maybeLive)
	if err != nil {
		return nil, errors.E(op, err)
	}
	for i, h := range maybeLive {
		if live.Get(i) {
			ticketHashes = append(ticketHashes, *h)
		}
	}

	return ticketHashes, nil
}

// TicketHashesForVotingAddress returns the hashes of all tickets with voting
// rights delegated to votingAddr.  This function does not return the hashes of
// pruned tickets.
func (w *Wallet) TicketHashesForVotingAddress(votingAddr dcrutil.Address) ([]chainhash.Hash, error) {
	const op errors.Op = "wallet.TicketHashesForVotingAddress"

	var ticketHashes []chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		dump, err := w.StakeMgr.DumpSStxHashesForAddress(
			stakemgrNs, votingAddr)
		if err != nil {
			return err
		}

		// Exclude hashes for unsaved transactions.
		ticketHashes = dump[:0]
		for i := range dump {
			h := &dump[i]
			if w.TxStore.ExistsTx(txmgrNs, h) {
				ticketHashes = append(ticketHashes, *h)
			}
		}

		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return ticketHashes, nil
}

// AddTicket adds a ticket transaction to the stake manager.  It is not added to
// the transaction manager because it is unknown where the transaction belongs
// on the blockchain.  It will be used to create votes.
func (w *Wallet) AddTicket(ticket *wire.MsgTx) error {
	const op errors.Op = "wallet.AddTicket"

	err := stake.CheckSStx(ticket)
	if err != nil {
		txHash := ticket.TxHash()
		return errors.E(op, errors.Invalid, errors.Errorf("%v is not a ticket", &txHash))
	}

	err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		stakemgrNs := tx.ReadWriteBucket(wstakemgrNamespaceKey)

		// Insert the ticket to be tracked and voted.
		err := w.StakeMgr.InsertSStx(stakemgrNs, dcrutil.NewTx(ticket))
		if err != nil {
			return err
		}

		if w.stakePoolEnabled {
			// Pluck the ticketaddress to identify the stakepool user.
			pkVersion := ticket.TxOut[0].Version
			pkScript := ticket.TxOut[0].PkScript
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkVersion,
				pkScript, w.ChainParams())
			if err != nil {
				return err
			}

			ticketHash := ticket.TxHash()

			// Update the pool ticket stake. This will include removing it from the
			// invalid slice and adding a ImmatureOrLive ticket to the valid ones.
			err = w.StakeMgr.RemoveStakePoolUserInvalTickets(stakemgrNs, addrs[0], &ticketHash)
			if err != nil {
				return err
			}
			poolTicket := &udb.PoolTicket{
				Ticket: ticketHash,
				Status: udb.TSImmatureOrLive,
			}
			err = w.StakeMgr.UpdateStakePoolUserTickets(stakemgrNs, addrs[0], poolTicket)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// RevokeTickets creates and sends revocation transactions for any unrevoked
// missed and expired tickets.  The wallet must be unlocked to generate any
// revocations.
func (w *Wallet) RevokeTickets(ctx context.Context, rpcCaller Caller) error {
	const op errors.Op = "wallet.RevokeTickets"

	var ticketHashes []chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		_, tipHeight := w.TxStore.MainChainTip(ns)
		ticketHashes, err = w.TxStore.UnspentTickets(tx, tipHeight, false)
		return err
	})
	if err != nil {
		return errors.E(op, err)
	}

	ticketHashPtrs := make([]*chainhash.Hash, len(ticketHashes))
	for i := range ticketHashes {
		ticketHashPtrs[i] = &ticketHashes[i]
	}
	rpc := dcrd.New(rpcCaller)
	expired, missed, err := rpc.ExistsExpiredMissedTickets(ctx, ticketHashPtrs)
	if err != nil {
		return errors.E(op, err)
	}
	revokableTickets := make([]*chainhash.Hash, 0, len(ticketHashes))
	for i, p := range ticketHashPtrs {
		if expired.Get(i) || missed.Get(i) {
			revokableTickets = append(revokableTickets, p)
		}
	}
	feePerKb := w.RelayFee()
	revocations := make([]*wire.MsgTx, 0, len(revokableTickets))
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		for _, ticketHash := range revokableTickets {
			addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
			txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
			ticketPurchase, err := w.TxStore.Tx(txmgrNs, ticketHash)
			if err != nil {
				return err
			}

			// Don't create revocations when this wallet doesn't have voting
			// authority.
			owned, err := w.hasVotingAuthority(addrmgrNs, ticketPurchase)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}

			revocation, err := createUnsignedRevocation(ticketHash,
				ticketPurchase, feePerKb)
			if err != nil {
				return err
			}
			err = w.signRevocation(addrmgrNs, ticketPurchase, revocation)
			if err != nil {
				return err
			}
			revocations = append(revocations, revocation)
		}
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}

	for i, revocation := range revocations {
		rec, err := udb.NewTxRecordFromMsgTx(revocation, time.Now())
		if err != nil {
			return errors.E(op, err)
		}
		var watch []wire.OutPoint
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			// Could be more efficient by avoiding processTransaction, as we
			// know it is a revocation.
			watch, err = w.processTransactionRecord(dbtx, rec, nil, nil)
			if err != nil {
				return errors.E(op, err)
			}
			return rpc.PublishTransaction(ctx, revocation)
		})
		if err != nil {
			return errors.E(op, err)
		}
		log.Infof("Revoked ticket %v with revocation %v", revokableTickets[i],
			&rec.Hash)
		err = rpc.LoadTxFilter(ctx, false, nil, watch)
		if err != nil {
			log.Errorf("Failed to watch outpoints: %v", err)
		}
	}

	return nil
}

// RevokeExpiredTickets revokes any unspent tickets that cannot be live due to
// being past expiry.  It is similar to RevokeTickets but is able to be used
// with any Peer implementation as it will not query the consensus RPC server
// for missed tickets.
func (w *Wallet) RevokeExpiredTickets(ctx context.Context, p Peer) (err error) {
	const opf = "wallet.RevokeExpiredTickets(%v)"
	defer func() {
		if err != nil {
			op := errors.Opf(opf, p)
			err = errors.E(op, err)
		}
	}()

	var expired []chainhash.Hash
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(ns)

		it := w.TxStore.IterateTickets(dbtx)
		defer it.Close()
		for it.Next() {
			// Spent tickets are excluded
			if it.SpenderHash != (chainhash.Hash{}) {
				continue
			}

			// Include ticket hash when it has reached expiry confirmations.
			if ticketExpired(w.chainParams, it.Block.Height, tipHeight) {
				expired = append(expired, it.TxRecord.Hash)
			}
		}
		return it.Err()
	})
	if err != nil {
		return err
	}

	if len(expired) == 0 {
		return nil
	}

	feePerKb := w.RelayFee()
	revocations := make([]*wire.MsgTx, 0, len(expired))
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		for i := range expired {
			ticketHash := &expired[i]
			addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
			txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
			ticketPurchase, err := w.TxStore.Tx(txmgrNs, ticketHash)
			if err != nil {
				return err
			}

			// Don't create revocations when this wallet doesn't have voting
			// authority.
			owned, err := w.hasVotingAuthority(addrmgrNs, ticketPurchase)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}

			revocation, err := createUnsignedRevocation(ticketHash,
				ticketPurchase, feePerKb)
			if err != nil {
				return err
			}
			err = w.signRevocation(addrmgrNs, ticketPurchase, revocation)
			if err != nil {
				return err
			}
			revocations = append(revocations, revocation)
		}
		return nil
	})
	if err != nil {
		return err
	}

	var watchOutPoints []wire.OutPoint
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		for i, revocation := range revocations {
			rec, err := udb.NewTxRecordFromMsgTx(revocation, time.Now())
			if err != nil {
				return err
			}

			log.Infof("Revoking ticket %v with revocation %v", &expired[i],
				&rec.Hash)

			watch, err := w.processTransactionRecord(dbtx, rec, nil, nil)
			if err != nil {
				return err
			}
			watchOutPoints = append(watchOutPoints, watch...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	err = p.PublishTransactions(ctx, revocations...)
	if err != nil {
		return err
	}

	if n, err := w.NetworkBackend(); err == nil && len(watchOutPoints) > 0 {
		err := n.LoadTxFilter(ctx, false, nil, watchOutPoints)
		if err != nil {
			log.Errorf("Failed to watch outpoints: %v", err)
		}
	}

	return nil
}
