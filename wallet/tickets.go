// Copyright (c) 2016-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/rpc/client/dcrd"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"golang.org/x/sync/errgroup"
)

// LiveTicketHashes returns the hashes of live tickets that the wallet has
// purchased or has voting authority for. rpcCaller can be nil if this is an
// SPV wallet.
func (w *Wallet) LiveTicketHashes(ctx context.Context, rpcCaller Caller, includeImmature bool) ([]chainhash.Hash, error) {
	const op errors.Op = "wallet.LiveTicketHashes"

	var ticketHashes []chainhash.Hash
	var maybeLive []*chainhash.Hash

	extraTickets := w.stakeMgr.DumpSStxHashes()

	var tipHeight int32 // Assigned in view below.

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Remove tickets from the extraTickets slice if they will appear in the
		// ticket iteration below.
		hashes := extraTickets
		extraTickets = hashes[:0]
		for i := range hashes {
			h := &hashes[i]
			if !w.txStore.ExistsTx(txmgrNs, h) {
				extraTickets = append(extraTickets, *h)
			}
		}

		_, tipHeight = w.txStore.MainChainTip(dbtx)

		it := w.txStore.IterateTickets(dbtx)
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

	// SPV wallet can't evaluate extraTickets.
	if rpcCaller == nil {
		return ticketHashes, nil
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
			const index = 0
			const tree = 1
			err := rpcCaller.Call(ctx, "gettxout", &txOut, extraTickets[i].String(), index, tree)
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
func (w *Wallet) TicketHashesForVotingAddress(ctx context.Context, votingAddr stdaddr.Address) ([]chainhash.Hash, error) {
	const op errors.Op = "wallet.TicketHashesForVotingAddress"

	var ticketHashes []chainhash.Hash
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		dump, err := w.stakeMgr.DumpSStxHashesForAddress(
			stakemgrNs, votingAddr)
		if err != nil {
			return err
		}

		// Exclude hashes for unsaved transactions.
		ticketHashes = dump[:0]
		for i := range dump {
			h := &dump[i]
			if w.txStore.ExistsTx(txmgrNs, h) {
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
