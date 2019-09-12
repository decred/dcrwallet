// Copyright (c) 2017-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
)

// Peer provides wallets with a subset of Decred network functionality available
// to a single peer.
type Peer interface {
	Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error)
	CFilters(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error)
	Headers(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error)
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
	Rescan(ctx context.Context, blocks []chainhash.Hash, save func(block *chainhash.Hash, txs []*wire.MsgTx) error) error

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

// Caller provides a client interface to perform remote procedure calls.
// Serialization and calling conventions are implementation-specific.
type Caller interface {
	// Call performs the remote procedure call defined by method and
	// waits for a response or a broken client connection.
	// Args provides positional parameters for the call.
	// Res must be a pointer to a struct, slice, or map type to unmarshal
	// a result (if any), or nil if no result is needed.
	Call(ctx context.Context, method string, res interface{}, args ...interface{}) error
}
