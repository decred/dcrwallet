// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package spv

import (
	"context"
	"runtime"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/p2p"
	"github.com/decred/dcrwallet/validate"
	"github.com/decred/dcrwallet/wallet"
)

var _ wallet.NetworkBackend = (*Syncer)(nil)

// TODO: When using the Syncer as a NetworkBackend, keep track of in-flight
// blocks and cfilters.  If one is already incoming, wait on that response.  If
// that peer is lost, try a different peer.  Optionally keep a cache of fetched
// data so it can be immediately returned without another call.

func pickAny(*p2p.RemotePeer) bool { return true }

// GetBlocks implements the GetBlocks method of the wallet.Peer interface.
func (s *Syncer) GetBlocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.pickRemote(pickAny)
		if err != nil {
			return nil, err
		}
		blocks, err := rp.GetBlocks(ctx, blockHashes)
		if err != nil {
			continue
		}
		return blocks, nil
	}
}

// GetCFilters implements the GetCFilters method of the wallet.Peer interface.
func (s *Syncer) GetCFilters(ctx context.Context, blockHashes []*chainhash.Hash) ([]*gcs.Filter, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.pickRemote(pickAny)
		if err != nil {
			return nil, err
		}
		fs, err := rp.GetCFilters(ctx, blockHashes)
		if err != nil {
			continue
		}
		return fs, nil
	}
}

// GetHeaders implements the GetHeaders method of the wallet.Peer interface.
func (s *Syncer) GetHeaders(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.pickRemote(pickAny)
		if err != nil {
			return nil, err
		}
		hs, err := rp.GetHeaders(ctx, blockLocators, hashStop)
		if err != nil {
			continue
		}
		return hs, nil
	}
}

func (s *Syncer) String() string {
	// This method is part of the wallet.Peer interface and will typically
	// specify the remote address of the peer.  Since the syncer can encompass
	// multiple peers, just use the qualified type as the string.
	return "spv.Syncer"
}

// LoadTxFilter implements the LoadTxFilter method of the wallet.NetworkBackend
// interface.
func (s *Syncer) LoadTxFilter(ctx context.Context, reload bool, addrs []dcrutil.Address, outpoints []wire.OutPoint) error {
	s.filterMu.Lock()
	if reload || s.rescanFilter == nil {
		s.rescanFilter = wallet.NewRescanFilter(nil, nil)
		s.filterData = nil
	}
	for _, addr := range addrs {
		pkScript, err := txscript.PayToAddrScript(addr)
		if err == nil {
			s.rescanFilter.AddAddress(addr)
			s.filterData.AddRegularPkScript(pkScript)
		}
	}
	for i := range outpoints {
		s.rescanFilter.AddUnspentOutPoint(&outpoints[i])
		s.filterData.AddOutPoint(&outpoints[i])
	}
	s.filterMu.Unlock()
	return nil
}

// PublishTransactions implements the PublishTransaction method of the
// wallet.Peer interface.
func (s *Syncer) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	msg := wire.NewMsgInvSizeHint(uint(len(txs)))
	for _, tx := range txs {
		txHash := tx.TxHash()
		err := msg.AddInvVect(wire.NewInvVect(wire.InvTypeTx, &txHash))
		if err != nil {
			return errors.E(errors.Protocol, err)
		}
	}
	return s.forRemotes(func(rp *p2p.RemotePeer) error {
		for _, inv := range msg.InvList {
			rp.InvsSent().Add(inv.Hash)
		}
		return rp.SendMessage(ctx, msg)
	})
}

// Rescan implements the Rescan method of the wallet.NetworkBackend interface.
func (s *Syncer) Rescan(ctx context.Context, blockHashes []chainhash.Hash, r wallet.RescanSaver) error {
	const op errors.Op = "spv.Rescan"

	cfilters := make([]*gcs.Filter, 0, len(blockHashes))
	for i := 0; i < len(blockHashes); i++ {
		f, err := s.wallet.CFilter(&blockHashes[i])
		if err != nil {
			return err
		}
		cfilters = append(cfilters, f)
	}

	blockMatches := make([]*wire.MsgBlock, len(blockHashes)) // Block assigned to slice once fetched

	// Read current filter data.  filterData is reassinged to new data matches
	// for subsequent filter checks, which improves filter matching performance
	// by checking for less data.
	s.filterMu.Lock()
	filterData := s.filterData
	s.filterMu.Unlock()

	idx := 0
FilterLoop:
	for idx < len(blockHashes) {
		var fmatches []*chainhash.Hash
		var fmatchidx []int
		var fmatchMu sync.Mutex

		// Spawn ncpu workers to check filter matches
		ncpu := runtime.NumCPU()
		c := make(chan int, ncpu)
		var wg sync.WaitGroup
		wg.Add(ncpu)
		for i := 0; i < ncpu; i++ {
			go func() {
				for i := range c {
					blockHash := &blockHashes[i]
					key := blockcf.Key(blockHash)
					f := cfilters[i]
					if f.MatchAny(key, filterData) {
						fmatchMu.Lock()
						fmatches = append(fmatches, blockHash)
						fmatchidx = append(fmatchidx, i)
						fmatchMu.Unlock()
					}
				}
				wg.Done()
			}()
		}
		for i := idx; i < len(blockHashes); i++ {
			if blockMatches[i] != nil {
				// Already fetched this block
				continue
			}
			c <- i
		}
		close(c)
		wg.Wait()

		if len(fmatches) != 0 {
			var rp *p2p.RemotePeer
		PickPeer:
			for {
				if rp == nil {
					var err error
					rp, err = s.pickRemote(pickAny)
					if err != nil {
						return err
					}
				}

				blocks, err := rp.GetBlocks(ctx, fmatches)
				if err != nil {
					rp = nil
					continue PickPeer
				}

				for j, b := range blocks {
					// Validate fetched blocks before rescanning transactions.  PoW
					// and PoS difficulties have already been valdiated since the
					// header is saved by the wallet, and modifications to these in
					// the downloaded block would result in a different block hash
					// and failure to fetch the block.
					i := fmatchidx[j]
					err = validate.MerkleRoots(b)
					if err != nil {
						err := errors.E(op, err)
						rp.Disconnect(err)
						rp = nil
						continue PickPeer
					}
					err = validate.RegularCFilter(b, cfilters[i])
					if err != nil {
						err := errors.E(op, err)
						rp.Disconnect(err)
						rp = nil
						continue PickPeer
					}

					blockMatches[i] = b
				}
				break
			}
		}

		for i := idx; i < len(blockMatches); i++ {
			b := blockMatches[i]
			if b == nil {
				// No filter match, skip block
				continue
			}

			if err := ctx.Err(); err != nil {
				return err
			}

			matchedTxs, fadded := s.rescanBlock(b)
			if len(matchedTxs) != 0 {
				err := r.SaveRescanned(&blockHashes[i], matchedTxs)
				if err != nil {
					return err
				}

				// Check for more matched blocks using updated filters,
				// starting at the next block.
				if len(fadded) != 0 {
					idx = i + 1
					filterData = fadded
					continue FilterLoop
				}
			}
		}
		return nil
	}

	return nil
}

// StakeDifficulty implements the StakeDifficulty method of the
// wallet.NetworkBackend interface.
//
// This implementation of the method will always error as the stake difficulty
// is not queryable over wire protocol, and when the next stake difficulty is
// available in a header commitment, the wallet will be able to determine this
// itself without requiring the NetworkBackend.
func (s *Syncer) StakeDifficulty(ctx context.Context) (dcrutil.Amount, error) {
	return 0, errors.E(errors.Invalid, "stake difficulty is not queryable over wire protocol")
}
