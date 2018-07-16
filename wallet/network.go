// Copyright (c) 2017-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
)

// Peer provides wallets with a subset of Decred network functionality available
// to a single peer.
type Peer interface {
	GetBlocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error)
	GetCFilters(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error)
	GetHeaders(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error)
	PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error
}

// NetworkBackend provides wallets with Decred network functionality.  Some
// wallet operations require the wallet to be associated with a network backend
// to complete.
//
// NetworkBackend expands on the Peer interface to provide additional
// functionality for rescanning and filtering.
type NetworkBackend interface {
	Peer
	LoadTxFilter(ctx context.Context, reload bool, addrs []dcrutil.Address, outpoints []wire.OutPoint) error
	Rescan(ctx context.Context, blocks []chainhash.Hash, r RescanSaver) error

	// This is impossible to determine over the wire protocol, and will always
	// error.  Use Wallet.NextStakeDifficulty to calculate the next ticket price
	// when the DCP0001 deployment is known to be active.
	StakeDifficulty(ctx context.Context) (dcrutil.Amount, error)
}

// NetworkBackend returns the currently associated network backend of the
// wallet, or an error if the no backend is currently set.
func (w *Wallet) NetworkBackend() (NetworkBackend, error) {
	const op errors.Op = "wallet.NetworkBackend"

	w.networkBackendMu.Lock()
	n := w.networkBackend
	w.networkBackendMu.Unlock()
	if n == nil {
		return nil, errors.E(op, errors.NoPeers)
	}
	return n, nil
}

// SetNetworkBackend sets the network backend used by various functions of the
// wallet.
func (w *Wallet) SetNetworkBackend(n NetworkBackend) {
	w.networkBackendMu.Lock()
	w.networkBackend = n
	w.networkBackendMu.Unlock()
}
