// Copyright (c) 2016-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/jrick/bitset"
)

// LiveTicketQuerier defines the functions required of a (trusted) network
// backend that provides information about the live ticket pool.
type LiveTicketQuerier interface {
	// ExistsLiveTickets relies on the node having the entire live ticket
	// pool available. May be removed if/when there is a header commitment
	// to the live ticket pool.
	ExistsLiveTickets(ctx context.Context, tickets []*chainhash.Hash) (bitset.Bytes, error)
}

// LiveTicketHashes returns the hashes of live tickets that the wallet has
// purchased or has voting authority for. rpc can be nil if this is an
// SPV wallet.
func (w *Wallet) LiveTicketHashes(ctx context.Context, rpc LiveTicketQuerier, includeImmature bool) ([]chainhash.Hash, error) {
	const op errors.Op = "wallet.LiveTicketHashes"

	var ticketHashes []chainhash.Hash
	var maybeLive []*chainhash.Hash

	var tipHeight int32 // Assigned in view below.

	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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

	// SPV wallet can't evaluate possibly live tickets.
	if rpc == nil {
		return ticketHashes, nil
	}

	// If there are no possibly live tickets to check, ticketHashes contains all
	// of the results.
	if len(maybeLive) == 0 {
		return ticketHashes, nil
	}

	// Use RPC to query which of the possibly-live tickets are really live.
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
