// Copyright (c) 2018-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package spv

import (
	"context"
	"runtime"
	"sync"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/p2p"
	"decred.org/dcrwallet/v4/validate"
	"decred.org/dcrwallet/v4/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

var _ wallet.NetworkBackend = (*Syncer)(nil)

// TODO: When using the Syncer as a NetworkBackend, keep track of in-flight
// blocks and cfilters.  If one is already incoming, wait on that response.  If
// that peer is lost, try a different peer.  Optionally keep a cache of fetched
// data so it can be immediately returned without another call.

func pickAny(*p2p.RemotePeer) bool { return true }

// pickForGetHeaders returns a function to use in waitForRemotes which selects
// peers that may have headers that are more recent than the passed tipHeight.
func pickForGetHeaders(tipHeight int32) func(rp *p2p.RemotePeer) bool {
	return func(rp *p2p.RemotePeer) bool {
		// We are interested in this peer's headers if they announced a
		// height greater than the current tip height and if we haven't
		// yet fetched all the headers that it announced.
		return rp.InitialHeight() > tipHeight && rp.LastHeight() < rp.InitialHeight()
	}
}

// Blocks implements the Blocks method of the wallet.Peer interface.
func (s *Syncer) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.waitForRemote(ctx, pickAny, true)
		if err != nil {
			return nil, err
		}
		blocks, err := rp.Blocks(ctx, blockHashes)
		if err != nil {
			log.Debugf("unable to fetch blocks from %v: %v", rp, err)
			continue
		}
		return blocks, nil
	}
}

// filterProof is an alias to the same anonymous struct as wallet package's
// FilterProof struct.
type filterProof = struct {
	Filter     *gcs.FilterV2
	ProofIndex uint32
	Proof      []chainhash.Hash
}

// CFiltersV2 implements the CFiltersV2 method of the wallet.Peer interface.
// This function blocks until a valid peer in the syncer returns the
// appropriate CFilters.
func (s *Syncer) CFiltersV2(ctx context.Context, blockHashes []*chainhash.Hash) ([]filterProof, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.waitForRemote(ctx, pickAny, true)
		if err != nil {
			return nil, err
		}
		fs, err := rp.CFiltersV2(ctx, blockHashes)
		if err != nil {
			log.Debugf("Error while fetching cfilters from %v: %v",
				rp, err)
			continue
		}
		return fs, nil
	}
}

// Headers implements the Headers method of the wallet.Peer interface.
func (s *Syncer) Headers(ctx context.Context, blockLocators []*chainhash.Hash, hashStop *chainhash.Hash) ([]*wire.BlockHeader, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rp, err := s.pickRemote(pickAny)
		if err != nil {
			return nil, err
		}
		hs, err := rp.Headers(ctx, blockLocators, hashStop)
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
//
// NOTE: due to blockcf2 *not* including the spent outpoints in the block, the
// addrs[] slice MUST include the addresses corresponding to the respective
// outpoints, otherwise they will not be returned during the rescan.
func (s *Syncer) LoadTxFilter(ctx context.Context, reload bool, addrs []stdaddr.Address, outpoints []wire.OutPoint) error {
	s.filterMu.Lock()
	if reload || s.rescanFilter == nil {
		s.rescanFilter = wallet.NewRescanFilter(nil, nil)
		s.filterData = nil
	}
	for _, addr := range addrs {
		_, pkScript := addr.PaymentScript()
		s.rescanFilter.AddAddress(addr)
		s.filterData.AddRegularPkScript(pkScript)
	}
	for i := range outpoints {
		s.rescanFilter.AddUnspentOutPoint(&outpoints[i])
	}
	s.filterMu.Unlock()
	return nil
}

// PublishTransactions implements the PublishTransaction method of the
// wallet.Peer interface.
func (s *Syncer) PublishTransactions(ctx context.Context, txs ...*wire.MsgTx) error {
	// Figure out transactions that are not stored by the wallet and create
	// an aux map so we can choose which need to be stored in the syncer's
	// mempool.
	walletBacked := make(map[chainhash.Hash]bool, len(txs))
	relevant, _, err := s.wallet.DetermineRelevantTxs(ctx, txs...)
	if err != nil {
		return err
	}
	for _, tx := range relevant {
		walletBacked[tx.TxHash()] = true
	}

	msg := wire.NewMsgInvSizeHint(uint(len(txs)))
	for _, tx := range txs {
		txHash := tx.TxHash()
		if !walletBacked[txHash] {
			// Load into the mempool and let the mempool handler
			// know of it.
			if _, loaded := s.mempool.LoadOrStore(txHash, tx); !loaded {
				select {
				case s.mempoolAdds <- &txHash:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
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
func (s *Syncer) Rescan(ctx context.Context, blockHashes []chainhash.Hash, save func(*chainhash.Hash, []*wire.MsgTx) error) error {
	const op errors.Op = "spv.Rescan"

	cfilters := make([]*gcs.FilterV2, 0, len(blockHashes))
	cfilterKeys := make([][gcs.KeySize]byte, 0, len(blockHashes))
	for i := 0; i < len(blockHashes); i++ {
		k, f, err := s.wallet.CFilterV2(ctx, &blockHashes[i])
		if err != nil {
			return err
		}
		cfilters = append(cfilters, f)
		cfilterKeys = append(cfilterKeys, k)
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
					key := cfilterKeys[i]
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
				if err := ctx.Err(); err != nil {
					return err
				}
				if rp == nil {
					var err error
					rp, err = s.pickRemote(pickAny)
					if err != nil {
						return err
					}
				}

				blocks, err := rp.Blocks(ctx, fmatches)
				if err != nil {
					rp = nil
					continue PickPeer
				}

				for j, b := range blocks {
					// Validate fetched blocks before rescanning transactions.  PoW
					// and PoS difficulties have already been validated since the
					// header is saved by the wallet, and modifications to these in
					// the downloaded block would result in a different block hash
					// and failure to fetch the block.
					//
					// Block filters were also validated
					// against the header (assuming dcp0005
					// was activated).
					err = validate.MerkleRoots(b)
					if err != nil {
						err = validate.DCP0005MerkleRoot(b)
					}
					if err != nil {
						err := errors.E(op, err)
						rp.Disconnect(err)
						rp = nil
						continue PickPeer
					}

					i := fmatchidx[j]
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
				err := save(&blockHashes[i], matchedTxs)
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
