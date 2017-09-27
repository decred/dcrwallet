// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/jrick/bitset"
)

// NetworkBackend provides wallets with Decred network functionality.  Some
// wallet operations require the wallet to be associated with a network backend
// to complete.
type NetworkBackend interface {
	// Should be no issue for spv
	GetHeaders(ctx context.Context, blockLocators []chainhash.Hash, hashStop *chainhash.Hash) ([][]byte, error)
	LoadTxFilter(ctx context.Context, reload bool, addrs []dcrutil.Address, outpoints []wire.OutPoint) error
	PublishTransaction(ctx context.Context, tx *wire.MsgTx) error

	// Tricky but not impossible for spv
	AddressesUsed(ctx context.Context, addrs []dcrutil.Address) (bitset.Bytes, error)
	Rescan(ctx context.Context, blocks []chainhash.Hash) ([]*RescannedBlock, error)

	// TODO: these should be known directly by the wallet.
	StakeDifficulty(ctx context.Context) (dcrutil.Amount, error)

	// TODO: only used to work around a hack for broken getheaders json-rpc
	GetBlockHash(ctx context.Context, height int32) (*chainhash.Hash, error)
}

// NetworkBackend returns the currently associated network backend of the
// wallet, or an error if the no backend is currently set.
func (w *Wallet) NetworkBackend() (NetworkBackend, error) {
	w.networkBackendMu.Lock()
	n := w.networkBackend
	w.networkBackendMu.Unlock()
	if n == nil {
		return nil, apperrors.New(apperrors.ErrDisconnected, "no network backend set")
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
