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
	"github.com/decred/dcrwallet/apperrors"
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
	b, ok := n.(*rpcBackend)
	if !ok {
		return nil, apperrors.New(apperrors.ErrUnsupported,
			"this operation requires the network backend to be the consensus RPC server")
	}
	return b.rpcClient, nil
}

func (b *rpcBackend) GetHeaders(ctx context.Context, blockLocators []chainhash.Hash, hashStop *chainhash.Hash) ([][]byte, error) {
	r, err := b.rpcClient.GetHeaders(blockLocators, hashStop)
	if err != nil {
		return nil, err
	}
	headers := make([][]byte, 0, len(r.Headers))
	for _, hexHeader := range r.Headers {
		header, err := hex.DecodeString(hexHeader)
		if err != nil {
			return nil, err
		}
		headers = append(headers, header)
	}
	return headers, nil
}

func (b *rpcBackend) LoadTxFilter(ctx context.Context, reload bool, addrs []dcrutil.Address, outpoints []wire.OutPoint) error {
	return b.rpcClient.LoadTxFilter(reload, addrs, outpoints)
}

func (b *rpcBackend) PublishTransaction(ctx context.Context, tx *wire.MsgTx) error {
	// High fees are hardcoded and allowed here since transactions created by
	// the wallet perform their own high fee check if high fees are disabled.
	// This matches the lack of any high fee checking when publishing
	// transactions over the wire protocol.
	_, err := b.rpcClient.SendRawTransaction(tx, true)
	return err
}

func (b *rpcBackend) AddressesUsed(ctx context.Context, addrs []dcrutil.Address) (bitset.Bytes, error) {
	hexBitSet, err := b.rpcClient.ExistsAddresses(addrs)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(hexBitSet)
}

func (b *rpcBackend) Rescan(ctx context.Context, blocks []chainhash.Hash) ([]*wallet.RescannedBlock, error) {
	r, err := b.rpcClient.Rescan(blocks)
	if err != nil {
		return nil, err
	}
	discoveredData := make([]*wallet.RescannedBlock, 0, len(r.DiscoveredData))
	for _, d := range r.DiscoveredData {
		blockHash, err := chainhash.NewHashFromStr(d.Hash)
		if err != nil {
			return nil, err
		}
		txs := make([][]byte, 0, len(d.Transactions))
		for _, txHex := range d.Transactions {
			tx, err := hex.DecodeString(txHex)
			if err != nil {
				return nil, err
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
	r, err := b.rpcClient.GetStakeDifficulty()
	if err != nil {
		return 0, err
	}
	return dcrutil.NewAmount(r.NextStakeDifficulty)
}

func (b *rpcBackend) GetBlockHash(ctx context.Context, height int32) (*chainhash.Hash, error) {
	return b.rpcClient.GetBlockHash(int64(height))
}
