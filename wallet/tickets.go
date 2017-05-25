// Copyright (c) 2016-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"encoding/hex"

	"github.com/decred/bitset"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

// GenerateVoteTx returns generated vote transaction for a chosen ticket
// purchase hash using the provided votebits.
func (w *Wallet) GenerateVoteTx(blockHash *chainhash.Hash, height int64, sstxHash *chainhash.Hash, voteBits stake.VoteBits) (*wire.MsgTx, error) {
	var voteTx *wire.MsgTx
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)

		var err error
		voteTx, err = w.StakeMgr.GenerateVoteTx(stakemgrNs, addrmgrNs, blockHash, height, sstxHash, voteBits)
		return err
	})
	return voteTx, err
}

// LiveTicketHashes returns the hashes of live tickets that have been purchased
// by the wallet.
func (w *Wallet) LiveTicketHashes(rpcClient *chain.RPCClient, includeImmature bool) ([]chainhash.Hash, error) {
	// This was mostly copied from an older version of the legacy RPC server
	// implementation, hence the overall weirdness, inefficiencies, and the
	// direct dependency on the consensus server RPC client.
	type promiseGetTxOut struct {
		result dcrrpcclient.FutureGetTxOutResult
		ticket *chainhash.Hash
	}

	type promiseGetRawTransaction struct {
		result dcrrpcclient.FutureGetRawTransactionVerboseResult
		ticket *chainhash.Hash
	}

	var tipHeight int32
	var ticketHashes []chainhash.Hash
	ticketMap := make(map[chainhash.Hash]struct{})
	var stakeMgrTickets []chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight = w.TxStore.MainChainTip(txmgrNs)

		// UnspentTickets collects all the tickets that pay out to a
		// public key hash for a public key owned by this wallet.
		var err error
		ticketHashes, err = w.TxStore.UnspentTickets(txmgrNs, tipHeight,
			includeImmature)
		if err != nil {
			return err
		}

		// Access the stake manager and see if there are any extra tickets
		// there. Likely they were either pruned because they failed to get
		// into the blockchain or they are P2SH for some script we own.
		stakeMgrTickets, err = w.StakeMgr.DumpSStxHashes()
		return err
	})
	if err != nil {
		return nil, err
	}

	for _, h := range ticketHashes {
		ticketMap[h] = struct{}{}
	}

	promisesGetTxOut := make([]promiseGetTxOut, 0, len(stakeMgrTickets))
	promisesGetRawTransaction := make([]promiseGetRawTransaction, 0, len(stakeMgrTickets))

	// Get the raw transaction information from daemon and add
	// any relevant tickets. The ticket output is always the
	// zeroth output.
	for i, h := range stakeMgrTickets {
		_, exists := ticketMap[h]
		if exists {
			continue
		}

		promisesGetTxOut = append(promisesGetTxOut, promiseGetTxOut{
			result: rpcClient.GetTxOutAsync(&stakeMgrTickets[i], 0, true),
			ticket: &stakeMgrTickets[i],
		})
	}

	for _, p := range promisesGetTxOut {
		spent, err := p.result.Receive()
		if err != nil {
			continue
		}
		// This returns nil if the output is spent.
		if spent == nil {
			continue
		}

		promisesGetRawTransaction = append(promisesGetRawTransaction, promiseGetRawTransaction{
			result: rpcClient.GetRawTransactionVerboseAsync(p.ticket),
			ticket: p.ticket,
		})

	}

	for _, p := range promisesGetRawTransaction {
		ticketTx, err := p.result.Receive()
		if err != nil {
			continue
		}

		txHeight := ticketTx.BlockHeight
		unconfirmed := (txHeight == 0)
		immature := (tipHeight-int32(txHeight) <
			int32(w.ChainParams().TicketMaturity))
		if includeImmature {
			ticketHashes = append(ticketHashes, *p.ticket)
		} else {
			if !(unconfirmed || immature) {
				ticketHashes = append(ticketHashes, *p.ticket)
			}
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
func (w *Wallet) updateStakePoolInvalidTicket(stakemgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket,
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

	return w.StakeMgr.UpdateStakePoolUserTickets(stakemgrNs, addrmgrNs, addr, poolTicket)
}

// AddTicket adds a ticket transaction to the wallet.
func (w *Wallet) AddTicket(ticket *dcrutil.Tx) error {
	return walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		stakemgrNs := tx.ReadWriteBucket(wstakemgrNamespaceKey)

		// Insert the ticket to be tracked and voted.
		err := w.StakeMgr.InsertSStx(stakemgrNs, ticket)
		if err != nil {
			return err
		}

		if w.stakePoolEnabled {
			addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)

			// Pluck the ticketaddress to identify the stakepool user.
			pkVersion := ticket.MsgTx().TxOut[0].Version
			pkScript := ticket.MsgTx().TxOut[0].PkScript
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkVersion,
				pkScript, w.ChainParams())
			if err != nil {
				return err
			}

			ticketHash := ticket.MsgTx().TxHash()

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
			err = w.updateStakePoolInvalidTicket(stakemgrNs, addrmgrNs, addrs[0], &ticketHash, rawTx.BlockHeight)
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
func (w *Wallet) RevokeTickets(chainClient *chain.RPCClient) error {
	var ticketHashes []chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		_, tipHeight := w.TxStore.MainChainTip(ns)
		ticketHashes, err = w.TxStore.UnspentTickets(ns, tipHeight, false)
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
	allowHighFees := w.AllowHighFees
	for i := range revokableTickets {
		err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
			addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
			stakemgrNs := tx.ReadWriteBucket(wstakemgrNamespaceKey)
			txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

			tipHash, tipHeight := w.TxStore.MainChainTip(txmgrNs)

			// There is so much wrong with this method but it is the only way
			// revocations can currently be created:
			//
			//   - It takes several tickets but only one revocation should be
			//     performed per update, because...
			//   - This method sends the transaction
			//   - The transaction is also not added to txmgr and instead relies
			//     on a notification to add it.
			//   - Therefore, rapid calls to this method will cause double spend
			//     errors.
			_, err := w.StakeMgr.HandleMissedTicketsNtfn(stakemgrNs, addrmgrNs,
				&tipHash, int64(tipHeight), revokableTickets[i:i+1], feePerKb,
				allowHighFees)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
