// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"context"
	"encoding/hex"
	"strings"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"golang.org/x/sync/errgroup"
)

type rpcBackend struct {
	rpcClient *rpcclient.Client
}

var _ wallet.NetworkBackend = (*rpcBackend)(nil)

// BackendFromRPCClient creates a wallet network backend from an RPC client.
func BackendFromRPCClient(rpcClient *rpcclient.Client) wallet.NetworkBackend {
	return &rpcBackend{rpcClient}
}

// RPCClientFromBackend returns the RPC client used to create a wallet network
// backend.  This errors if the backend was not created using
// BackendFromRPCClient.
func RPCClientFromBackend(n wallet.NetworkBackend) (*rpcclient.Client, error) {
	const op errors.Op = "chain.RPCClientFromBackend"

	b, ok := n.(*rpcBackend)
	if !ok {
		return nil, errors.E(op, errors.Invalid, "this operation requires "+
			"the network backend to be the consensus RPC server")
	}
	return b.rpcClient, nil
}

func (b *rpcBackend) GetBlocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	const op errors.Op = "dcrd.jsonrpc.getblock"

	blocks := make([]*wire.MsgBlock, len(blockHashes))
	var g errgroup.Group
	for i := range blockHashes {
		i := i
		g.Go(func() error {
			block, err := b.rpcClient.GetBlock(blockHashes[i])
			blocks[i] = block
			return err
		})
	}
	err := g.Wait()
	if err != nil {
		return nil, errors.E(op, err)
	}
	return blocks, nil
}

func (b *rpcBackend) GetCFilters(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error) {
	const op errors.Op = "dcrd.jsonrpc.getcfilter"

	// TODO: this is spammy and would be better implemented with a single RPC.
	filters := make([]*gcs.Filter, len(blockHashes))
	var g errgroup.Group
	for i := range blockHashes {
		i := i
		g.Go(func() error {
			f, err := b.rpcClient.GetCFilter(blockHashes[i], wire.GCSFilterRegular)
			filters[i] = f
			return err
		})
	}
	err := g.Wait()
	if err != nil {
		return nil, errors.E(op, err)
	}
	return filters, nil
}

func (b *rpcBackend) GetHeaders(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error) {
	const op errors.Op = "dcrd.jsonrpc.getheaders"

	r, err := b.rpcClient.GetHeaders(blockLocators, hashStop)
	if err != nil {
		return nil, errors.E(op, err)
	}
	headers := make([]*wire.BlockHeader, 0, len(r.Headers))
	for _, hexHeader := range r.Headers {
		header := new(wire.BlockHeader)
		err := header.Deserialize(hex.NewDecoder(strings.NewReader(hexHeader)))
		if err != nil {
			return nil, errors.E(op, errors.Encoding, err)
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func (b *rpcBackend) String() string {
	return b.rpcClient.String()
}

func (b *rpcBackend) LoadTxFilter(ctx context.Context, reload bool, addrs []dcrutil.Address, outpoints []wire.OutPoint) error {
	const op errors.Op = "dcrd.jsonrpc.loadtxfilter"

	err := b.rpcClient.LoadTxFilter(reload, addrs, outpoints)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

func (b *rpcBackend) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	const op errors.Op = "dcrd.jsonrpc.sendrawtransaction"

	// sendrawtransaction does not allow orphans, so we can not concurrently or
	// asynchronously send transactions.  All transaction sends are attempted,
	// and the first non-nil error is returned.
	var firstErr error
	for _, tx := range txs {
		// High fees are hardcoded and allowed here since transactions created by
		// the wallet perform their own high fee check if high fees are disabled.
		// This matches the lack of any high fee checking when publishing
		// transactions over the wire protocol.
		_, err := b.rpcClient.SendRawTransaction(tx, true)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return errors.E(op, firstErr)
	}
	return nil
}

func (b *rpcBackend) Rescan(ctx context.Context, blocks []chainhash.Hash, r wallet.RescanSaver) error {
	const op errors.Op = "dcrd.jsonrpc.rescan"

	res, err := b.rpcClient.Rescan(blocks)
	if err != nil {
		return errors.E(op, err)
	}
	for _, d := range res.DiscoveredData {
		blockHash, err := chainhash.NewHashFromStr(d.Hash)
		if err != nil {
			return errors.E(op, errors.Encoding, err)
		}
		txs := make([]*wire.MsgTx, 0, len(d.Transactions))
		for _, txHex := range d.Transactions {
			tx := new(wire.MsgTx)
			err := tx.Deserialize(hex.NewDecoder(strings.NewReader(txHex)))
			if err != nil {
				return errors.E(op, errors.Encoding, err)
			}
			txs = append(txs, tx)
		}
		err = r.SaveRescanned(blockHash, txs)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *rpcBackend) StakeDifficulty(ctx context.Context) (dcrutil.Amount, error) {
	const op errors.Op = "dcrd.jsonrpc.getstakedifficulty"

	r, err := b.rpcClient.GetStakeDifficulty()
	if err != nil {
		return 0, errors.E(op, err)
	}
	amount, err := dcrutil.NewAmount(r.NextStakeDifficulty)
	if err != nil {
		return 0, errors.E(op, err)
	}
	return amount, nil
}

func (b *rpcBackend) RPCClient() *rpcclient.Client {
	return b.rpcClient
}
