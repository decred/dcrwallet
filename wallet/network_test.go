// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/payments"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/wire"
)

// mockNetwork implements all methods of NetworkBackend, returning zero values
// without error.  It may be embedded in a struct to create another
// NetworkBackend which dispatches to particular implementations of the methods.
type mockNetwork struct{}

func (mockNetwork) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	return nil, nil
}
func (mockNetwork) CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]FilterProof, error) {
	return nil, nil
}
func (mockNetwork) Headers(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error) {
	return nil, nil
}
func (mockNetwork) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error { return nil }
func (mockNetwork) LoadTxFilter(ctx context.Context, reload bool, addrs []payments.Address, outpoints []wire.OutPoint) error {
	return nil
}
func (mockNetwork) Rescan(ctx context.Context, blocks []chainhash.Hash, save func(*chainhash.Hash, []*wire.MsgTx) error) error {
	return nil
}
func (mockNetwork) StakeDifficulty(ctx context.Context) (dcrutil.Amount, error) { return 0, nil }
