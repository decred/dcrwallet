// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/wire"
)

// peerFuncs implements Peer with custom implementations of each individual method.
// Functions may be left nil (unimplemented) if they will not be called by the test code.
type peerFuncs struct {
	blocks              func(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error)
	cfilters            func(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error)
	headers             func(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error)
	publishTransactions func(ctx context.Context, txs ...*wire.MsgTx) error
}

func (p *peerFuncs) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	return p.blocks(ctx, blockHashes)
}
func (p *peerFuncs) CFilters(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error) {
	return p.cfilters(ctx, blockHashes)
}
func (p *peerFuncs) Headers(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error) {
	return p.headers(ctx, blockLocators, hashStop)
}
func (p *peerFuncs) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	return p.publishTransactions(ctx, txs...)
}
