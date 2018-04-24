// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

const maxBlocksPerRescan = 2000

// RescannedBlock models the relevant data returned during a rescan from a
// single block.
type RescannedBlock struct {
	BlockHash    chainhash.Hash
	Transactions [][]byte
}

// TODO: track whether a rescan is already in progress, and cancel either it or
// this new rescan, keeping the one that still has the most blocks to scan.

// rescan synchronously scans over all blocks on the main chain starting at
// startHash and height up through the recorded main chain tip block.  The
// progress channel, if non-nil, is sent non-error progress notifications with
// the heights the rescan has completed through, starting with the start height.
func (w *Wallet) rescan(ctx context.Context, n NetworkBackend,
	startHash *chainhash.Hash, height int32, p chan<- RescanProgress) error {

	blockHashStorage := make([]chainhash.Hash, maxBlocksPerRescan)
	rescanFrom := *startHash
	inclusive := true
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var rescanBlocks []chainhash.Hash
		err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
			txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
			var err error
			rescanBlocks, err = w.TxStore.GetMainChainBlockHashes(txmgrNs,
				&rescanFrom, inclusive, blockHashStorage)
			return err
		})
		if err != nil {
			return err
		}
		if len(rescanBlocks) == 0 {
			return nil
		}

		scanningThrough := height + int32(len(rescanBlocks)) - 1
		log.Infof("Rescanning blocks %v-%v...", height,
			scanningThrough)
		rescanResults, err := n.Rescan(ctx, rescanBlocks)
		if err != nil {
			return err
		}
		var rawBlockHeader udb.RawBlockHeader
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
			for _, r := range rescanResults {
				blockMeta, err := w.TxStore.GetBlockMetaForHash(txmgrNs, &r.BlockHash)
				if err != nil {
					return err
				}
				serHeader, err := w.TxStore.GetSerializedBlockHeader(txmgrNs,
					&r.BlockHash)
				if err != nil {
					return err
				}
				err = copyHeaderSliceToArray(&rawBlockHeader, serHeader)
				if err != nil {
					return err
				}

				for _, tx := range r.Transactions {
					rec, err := udb.NewTxRecord(tx, time.Now())
					if err != nil {
						return err
					}
					err = w.processTransaction(dbtx, rec, &rawBlockHeader, &blockMeta)
					if err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if p != nil {
			p <- RescanProgress{ScannedThrough: scanningThrough}
		}
		rescanFrom = rescanBlocks[len(rescanBlocks)-1]
		height += int32(len(rescanBlocks))
		inclusive = false
	}
}

// Rescan starts a rescan of the wallet for all blocks on the main chain
// beginning at startHash.  This function blocks until the rescan completes.
func (w *Wallet) Rescan(ctx context.Context, n NetworkBackend, startHash *chainhash.Hash) error {
	const op errors.Op = "wallet.Rescan"

	var startHeight int32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		header, err := w.TxStore.GetSerializedBlockHeader(txmgrNs, startHash)
		if err != nil {
			return err
		}
		startHeight = udb.ExtractBlockHeaderHeight(header)
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}

	err = w.rescan(ctx, n, startHash, startHeight, nil)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// RescanFromHeight is an alternative to Rescan that takes a block height
// instead of a hash.  See Rescan for more details.
func (w *Wallet) RescanFromHeight(ctx context.Context, n NetworkBackend, startHeight int32) error {
	const op errors.Op = "wallet.RescanFromHeight"

	var startHash chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		startHash, err = w.TxStore.GetMainChainBlockHashForHeight(
			txmgrNs, startHeight)
		return err
	})
	if err != nil {
		return errors.E(op, err)
	}

	err = w.rescan(ctx, n, &startHash, startHeight, nil)
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// RescanProgress records the height the rescan has completed through and any
// errors during processing of the rescan.
type RescanProgress struct {
	Err            error
	ScannedThrough int32
}

// RescanProgressFromHeight rescans for relevant transactions in all blocks in
// the main chain starting at startHeight.  Progress notifications and any
// errors are sent to the channel p.  This function blocks until the rescan
// completes or ends in an error.  p is closed before returning.
func (w *Wallet) RescanProgressFromHeight(ctx context.Context, n NetworkBackend,
	startHeight int32, p chan<- RescanProgress) {

	const op errors.Op = "wallet.RescanProgressFromHeight"

	defer close(p)

	var startHash chainhash.Hash
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		startHash, err = w.TxStore.GetMainChainBlockHashForHeight(
			txmgrNs, startHeight)
		return err
	})
	if err != nil {
		p <- RescanProgress{Err: errors.E(op, err)}
		return
	}

	err = w.rescan(ctx, n, &startHash, startHeight, p)
	if err != nil {
		p <- RescanProgress{Err: errors.E(op, err)}
	}
}
