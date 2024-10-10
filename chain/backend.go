// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chain

import (
	"context"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v4"
	"github.com/decred/dcrd/mixing"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/jrick/bitset"
)

// Blocks is part of the wallet.NetworkBackend interface.
func (s *Syncer) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	return s.rpc.Blocks(ctx, blockHashes)
}

type filterProof = struct {
	Filter     *gcs.FilterV2
	ProofIndex uint32
	Proof      []chainhash.Hash
}

// CFiltersV2 is part of the wallet.NetworkBackend interface.
func (s *Syncer) CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]filterProof, error) {
	return s.rpc.CFiltersV2(ctx, blockHashes)
}

// PublishTransactions is part of the wallet.NetworkBackend interface.
func (s *Syncer) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	return s.rpc.PublishTransactions(ctx, txs...)
}

// PublishMixMessages submits each mixing message to the dcrd mixpool for acceptance.
// If accepted, the messages are published to other peers.
func (s *Syncer) PublishMixMessages(ctx context.Context, msgs ...mixing.Message) error {
	return s.rpc.PublishMixMessages(ctx, msgs...)
}

// LoadTxFilter is part of the wallet.NetworkBackend interface.
func (s *Syncer) LoadTxFilter(ctx context.Context, reload bool, addrs []stdaddr.Address, outpoints []wire.OutPoint) error {
	return s.rpc.LoadTxFilter(ctx, reload, addrs, outpoints)
}

// Rescan is part of the wallet.NetworkBackend interface.
func (s *Syncer) Rescan(ctx context.Context, blocks []chainhash.Hash, save func(block *chainhash.Hash, txs []*wire.MsgTx) error) error {
	return s.rpc.Rescan(ctx, blocks, save)
}

// StakeDifficulty is part of the wallet.NetworkBackend interface.
func (s *Syncer) StakeDifficulty(ctx context.Context) (dcrutil.Amount, error) {
	return s.rpc.StakeDifficulty(ctx)
}

// Deployments fulfills the DeploymentQuerier interface.
func (s *Syncer) Deployments(ctx context.Context) (map[string]dcrdtypes.AgendaInfo, error) {
	info, err := s.rpc.GetBlockchainInfo(ctx)
	if err != nil {
		return nil, err
	}
	return info.Deployments, nil
}

// GetTxOut fulfills the LiveTicketQuerier interface.
func (s *Syncer) GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, tree int8, includeMempool bool) (*dcrdtypes.GetTxOutResult, error) {
	return s.rpc.GetTxOut(ctx, txHash, index, tree, includeMempool)
}

// GetConfirmationHeight fulfills the LiveTicketQuerier interface.
func (s *Syncer) GetConfirmationHeight(ctx context.Context, txHash *chainhash.Hash) (int32, error) {
	return s.rpc.GetConfirmationHeight(ctx, txHash)
}

// ExistsLiveTickets fulfills the LiveTicketQuerier interface.
func (s *Syncer) ExistsLiveTickets(ctx context.Context, tickets []*chainhash.Hash) (bitset.Bytes, error) {
	return s.rpc.ExistsLiveTickets(ctx, tickets)
}

// UsedAddresses fulfills the usedAddressesQuerier interface.
func (s *Syncer) UsedAddresses(ctx context.Context, addrs []stdaddr.Address) (bitset.Bytes, error) {
	return s.rpc.UsedAddresses(ctx, addrs)
}

func (s *Syncer) Done() <-chan struct{} {
	s.doneMu.Lock()
	c := s.done
	s.doneMu.Unlock()
	return c
}

func (s *Syncer) Err() error {
	s.doneMu.Lock()
	c := s.done
	err := s.err
	s.doneMu.Unlock()

	select {
	case <-c:
		return err
	default:
		return nil
	}
}
