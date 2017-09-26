// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"encoding/hex"
	"time"

	"github.com/decred/bitset"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
	"golang.org/x/sync/errgroup"
)

// GenerateVoteTx creates a vote transaction for a chosen ticket purchase hash
// using the provided votebits.  The ticket purchase transaction must be stored
// by the wallet.
func (w *Wallet) GenerateVoteTx(blockHash *chainhash.Hash, height int32, ticketHash *chainhash.Hash, voteBits stake.VoteBits) (*wire.MsgTx, error) {
	var vote *wire.MsgTx
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		ticketPurchase, err := w.TxStore.Tx(txmgrNs, ticketHash)
		if err != nil {
			return err
		}
		if ticketPurchase == nil {
			const str = "ticket purchase transaction not found"
			return apperrors.New(apperrors.ErrSStxNotFound, str)
		}
		vote, err = createUnsignedVote(ticketHash, ticketPurchase,
			height, blockHash, voteBits, w.subsidyCache, w.chainParams)
		if err != nil {
			return err
		}
		return w.signVote(addrmgrNs, ticketPurchase, vote)
	})
	return vote, err
}

// LiveTicketHashes returns the hashes of live tickets that the wallet has
// purchased or has voting authority for.
func (w *Wallet) LiveTicketHashes(chainClient *dcrrpcclient.Client, includeImmature bool) ([]chainhash.Hash, error) {
	var ticketHashes []chainhash.Hash
	var maybeLive []*chainhash.Hash

	extraTickets := w.StakeMgr.DumpSStxHashes()

	expiryConfs := int32(w.chainParams.TicketExpiry) +
		int32(w.chainParams.TicketMaturity) + 1

	var tipHeight int32 // Assigned in view below.

	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Remove tickets from the extraTickets slice if they will appear in the
		// ticket iteration below.
		for i := 0; i < len(extraTickets); {
			if !w.TxStore.ExistsTx(txmgrNs, &extraTickets[i]) {
				i++
				continue
			}
			extraTickets[i] = extraTickets[len(extraTickets)-1]
			extraTickets = extraTickets[:len(extraTickets)-1]
		}

		_, tipHeight = w.TxStore.MainChainTip(txmgrNs)

		it := w.TxStore.IterateTickets(dbtx)
		for it.Next() {
			// Tickets that are mined at a height beyond the expiry height can
			// not be live.
			if confirmed(expiryConfs, it.Block.Height, tipHeight) {
				continue
			}

			// Tickets that have not reached ticket maturity are immature.
			// Exclude them unless the caller requested to include immature
			// tickets.
			if !confirmed(int32(w.chainParams.TicketMaturity)+1, it.Block.Height,
				tipHeight) {
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
		return nil, err
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
			getTxOutResult, err := chainClient.GetTxOut(&extraTickets[i], 0, true)
			if err != nil || getTxOutResult == nil {
				return nil
			}
			r, err := chainClient.GetRawTransactionVerbose(&extraTickets[i])
			if err != nil {
				return nil
			}
			extraTicketResults[i] = extraTicketResult{true, int32(r.BlockHeight)}
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
		if confirmed(expiryConfs, r.height, tipHeight) {
			continue
		}
		if !confirmed(int32(w.chainParams.TicketMaturity)+1, r.height, tipHeight) {
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
	liveBitsetHex, err := chainClient.ExistsLiveTickets(maybeLive)
	if err != nil {
		return nil, err
	}
	liveBitset, err := hex.DecodeString(liveBitsetHex)
	if err != nil {
		return nil, err
	}
	for i, h := range maybeLive {
		if bitset.Bytes(liveBitset).Get(i) {
			ticketHashes = append(ticketHashes, *h)
		}
	}

	return ticketHashes, nil
}

// TicketHashesForVotingAddress returns the hashes of all tickets with voting
// rights delegated to votingAddr.  This function does not return the hashes of
// pruned tickets.
func (w *Wallet) TicketHashesForVotingAddress(votingAddr dcrutil.Address) ([]chainhash.Hash, error) {
	var ticketHashes []chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		var err error
		ticketHashes, err = w.StakeMgr.DumpSStxHashesForAddress(
			stakemgrNs, votingAddr)
		if err != nil {
			return err
		}

		// Exclude the hash if the transaction is not saved too.  No
		// promises of hash order are given (and at time of writing,
		// they are copies of iterators of a Go map in wstakemgr) so
		// when one must be removed, replace it with the last and
		// decrease the len.
		for i := 0; i < len(ticketHashes); {
			if w.TxStore.ExistsTx(txmgrNs, &ticketHashes[i]) {
				i++
				continue
			}

			ticketHashes[i] = ticketHashes[len(ticketHashes)-1]
			ticketHashes = ticketHashes[:len(ticketHashes)-1]
		}

		return nil
	})
	return ticketHashes, err
}

// updateStakePoolInvalidTicket properly updates a previously marked Invalid pool ticket,
// it then creates a new entry in the validly tracked pool ticket db.
func (w *Wallet) updateStakePoolInvalidTicket(stakemgrNs walletdb.ReadWriteBucket,
	addr dcrutil.Address, ticket *chainhash.Hash, ticketHeight int64) error {

	err := w.StakeMgr.RemoveStakePoolUserInvalTickets(stakemgrNs, addr, ticket)
	if err != nil {
		return err
	}
	poolTicket := &udb.PoolTicket{
		Ticket:       *ticket,
		HeightTicket: uint32(ticketHeight),
		Status:       udb.TSImmatureOrLive,
	}

	return w.StakeMgr.UpdateStakePoolUserTickets(stakemgrNs, addr, poolTicket)
}

// AddTicket adds a ticket transaction to the stake manager.  It is not added to
// the transaction manager because it is unknown where the transaction belongs
// on the blockchain.  It will be used to create votes.
func (w *Wallet) AddTicket(ticket *wire.MsgTx) error {
	_, err := stake.IsSStx(ticket)
	if err != nil {
		return err
	}

	return walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
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

			chainClient, err := w.requireChainClient()
			if err != nil {
				return err
			}
			rawTx, err := chainClient.GetRawTransactionVerbose(&ticketHash)
			if err != nil {
				return err
			}

			// Update the pool ticket stake. This will include removing it from the
			// invalid slice and adding a ImmatureOrLive ticket to the valid ones.
			err = w.updateStakePoolInvalidTicket(stakemgrNs, addrs[0], &ticketHash,
				rawTx.BlockHeight)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// RevokeTickets creates and sends revocation transactions for any unrevoked
// missed and expired tickets.  The wallet must be unlocked to generate any
// revocations.
func (w *Wallet) RevokeTickets(chainClient *dcrrpcclient.Client) error {
	var ticketHashes []chainhash.Hash
	var tipHash chainhash.Hash
	var tipHeight int32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		tipHash, tipHeight = w.TxStore.MainChainTip(ns)
		ticketHashes, err = w.TxStore.UnspentTickets(tx, tipHeight, false)
		return err
	})
	if err != nil {
		return err
	}

	ticketHashPtrs := make([]*chainhash.Hash, len(ticketHashes))
	for i := range ticketHashes {
		ticketHashPtrs[i] = &ticketHashes[i]
	}
	expiredFuture := chainClient.ExistsExpiredTicketsAsync(ticketHashPtrs)
	missedFuture := chainClient.ExistsMissedTicketsAsync(ticketHashPtrs)
	expiredBitsHex, err := expiredFuture.Receive()
	if err != nil {
		return err
	}
	missedBitsHex, err := missedFuture.Receive()
	if err != nil {
		return err
	}
	expiredBits, err := hex.DecodeString(expiredBitsHex)
	if err != nil {
		return err
	}
	missedBits, err := hex.DecodeString(missedBitsHex)
	if err != nil {
		return err
	}
	revokableTickets := make([]*chainhash.Hash, 0, len(ticketHashes))
	for i, p := range ticketHashPtrs {
		if bitset.Bytes(expiredBits).Get(i) || bitset.Bytes(missedBits).Get(i) {
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
		return err
	}

	for i, revocation := range revocations {
		rec, err := udb.NewTxRecordFromMsgTx(revocation, time.Now())
		if err != nil {
			return err
		}
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			err = w.StakeMgr.StoreRevocationInfo(dbtx, revokableTickets[i],
				&rec.Hash, &tipHash, tipHeight)
			if err != nil {
				return err
			}
			// Could be more efficient by avoiding processTransaction, as we
			// know it is a revocation.
			err = w.processTransactionRecord(dbtx, rec, nil, nil)
			if err != nil {
				return err
			}
			_, err = chainClient.SendRawTransaction(revocation, true)
			return err
		})
		if err != nil {
			return err
		}
		log.Infof("Revoked ticket %v with revocation %v", revokableTickets[i],
			&rec.Hash)
	}

	return nil
}
