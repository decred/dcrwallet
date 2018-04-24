// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"context"
	"encoding/hex"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet"
	"github.com/jrick/bitset"
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

func (b *rpcBackend) GetHeaders(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([][]byte, error) {
	const op errors.Op = "dcrd.jsonrpc.getheaders"

	r, err := b.rpcClient.GetHeaders(blockLocators, hashStop)
	if err != nil {
		return nil, errors.E(op, err)
	}
	headers := make([][]byte, 0, len(r.Headers))
	for _, hexHeader := range r.Headers {
		header, err := hex.DecodeString(hexHeader)
		if err != nil {
			return nil, errors.E(op, errors.Encoding, err)
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func (b *rpcBackend) LoadTxFilter(ctx context.Context, reload bool, addrs []dcrutil.Address, outpoints []wire.OutPoint) error {
	const op errors.Op = "dcrd.jsonrpc.loadtxfilter"

	err := b.rpcClient.LoadTxFilter(reload, addrs, outpoints)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

func (b *rpcBackend) PublishTransaction(ctx context.Context, tx *wire.MsgTx) error {
	const op errors.Op = "dcrd.jsonrpc.sendrawtransaction"

	// High fees are hardcoded and allowed here since transactions created by
	// the wallet perform their own high fee check if high fees are disabled.
	// This matches the lack of any high fee checking when publishing
	// transactions over the wire protocol.
	_, err := b.rpcClient.SendRawTransaction(tx, true)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

func (b *rpcBackend) AddressesUsed(ctx context.Context, addrs []dcrutil.Address) (bitset.Bytes, error) {
	const op errors.Op = "dcrd.jsonrpc.existsaddresses"

	hexBitSet, err := b.rpcClient.ExistsAddresses(addrs)
	if err != nil {
		return nil, errors.E(op, err)
	}
	bitset, err := hex.DecodeString(hexBitSet)
	if err != nil {
		return nil, errors.E(op, errors.Encoding, err)
	}
	return bitset, nil
}

func (b *rpcBackend) Rescan(ctx context.Context, blocks []chainhash.Hash) ([]*wallet.RescannedBlock, error) {
	const op errors.Op = "dcrd.jsonrpc.rescan"

	r, err := b.rpcClient.Rescan(blocks)
	if err != nil {
		return nil, errors.E(op, err)
	}
	discoveredData := make([]*wallet.RescannedBlock, 0, len(r.DiscoveredData))
	for _, d := range r.DiscoveredData {
		blockHash, err := chainhash.NewHashFromStr(d.Hash)
		if err != nil {
			return nil, errors.E(op, errors.Encoding, err)
		}
		txs := make([][]byte, 0, len(d.Transactions))
		for _, txHex := range d.Transactions {
			tx, err := hex.DecodeString(txHex)
			if err != nil {
				return nil, errors.E(op, errors.Encoding, err)
			}
			txs = append(txs, tx)
		}
		rescannedBlock := &wallet.RescannedBlock{
			BlockHash:    *blockHash,
			Transactions: txs,
		}
		discoveredData = append(discoveredData, rescannedBlock)
	}
	return discoveredData, nil
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

func (b *rpcBackend) GetBlockHash(ctx context.Context, height int32) (*chainhash.Hash, error) {
	const op errors.Op = "dcrd.jsonrpc.getblockhash"

	hash, err := b.rpcClient.GetBlockHash(int64(height))
	if err != nil {
		return nil, errors.E(op, err)
	}
	return hash, nil
}
