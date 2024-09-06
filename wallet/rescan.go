// Copyright (c) 2013-2014 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"time"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/udb"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/crypto/ripemd160"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

const maxBlocksPerRescan = 2000

// RescanFilter implements a precise filter intended to hold all watched wallet
// data in memory such as addresses and unspent outputs.  The zero value is not
// valid, and filters must be created using NewRescanFilter.  RescanFilter is
// not safe for concurrent access.
type RescanFilter struct {
	// Implemented fast paths for address lookup.
	pubKeyHashes      map[[ripemd160.Size]byte]struct{}
	scriptHashes      map[[ripemd160.Size]byte]struct{}
	compressedPubKeys map[[33]byte]struct{}

	// A fallback address lookup map in case a fast path doesn't exist.
	// Only exists for completeness.  If using this shows up in a profile,
	// there's a good chance a fast path should be added.
	otherAddresses map[string]struct{}

	// Outpoints of unspent outputs.
	unspent map[wire.OutPoint]struct{}
}

// NewRescanFilter creates and initializes a RescanFilter containing each passed
// address and outpoint.
func NewRescanFilter(addresses []stdaddr.Address, unspentOutPoints []*wire.OutPoint) *RescanFilter {
	filter := &RescanFilter{
		pubKeyHashes:      map[[ripemd160.Size]byte]struct{}{},
		scriptHashes:      map[[ripemd160.Size]byte]struct{}{},
		compressedPubKeys: map[[33]byte]struct{}{},
		otherAddresses:    map[string]struct{}{},
		unspent:           make(map[wire.OutPoint]struct{}, len(unspentOutPoints)),
	}

	for _, s := range addresses {
		filter.AddAddress(s)
	}
	for _, op := range unspentOutPoints {
		filter.AddUnspentOutPoint(op)
	}

	return filter
}

// AddAddress adds an address to the filter if it does not already exist.
func (f *RescanFilter) AddAddress(a stdaddr.Address) {
	switch a := a.(type) {
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
		f.pubKeyHashes[*a.Hash160()] = struct{}{}
	case *stdaddr.AddressScriptHashV0:
		f.scriptHashes[*a.Hash160()] = struct{}{}
	case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
		serializedPubKey := a.SerializedPubKey()
		switch len(serializedPubKey) {
		case 33: // compressed
			var compressedPubKey [33]byte
			copy(compressedPubKey[:], serializedPubKey)
			f.compressedPubKeys[compressedPubKey] = struct{}{}
		default:
			f.otherAddresses[a.String()] = struct{}{}
		}
	default:
		f.otherAddresses[a.String()] = struct{}{}
	}
}

// ExistsAddress returns whether an address is contained in the filter.
func (f *RescanFilter) ExistsAddress(a stdaddr.Address) (ok bool) {
	switch a := a.(type) {
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
		_, ok = f.pubKeyHashes[*a.Hash160()]
	case *stdaddr.AddressScriptHashV0:
		_, ok = f.scriptHashes[*a.Hash160()]
	case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
		serializedPubKey := a.SerializedPubKey()
		switch len(serializedPubKey) {
		case 33: // compressed
			var compressedPubKey [33]byte
			copy(compressedPubKey[:], serializedPubKey)
			_, ok = f.compressedPubKeys[compressedPubKey]
			if !ok {
				a := a.AddressPubKeyHash().(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0)
				_, ok = f.pubKeyHashes[*a.Hash160()]
			}
		default:
			_, ok = f.otherAddresses[a.String()]
		}
	default:
		_, ok = f.otherAddresses[a.String()]
	}
	return
}

// RemoveAddress removes an address from the filter if it exists.
func (f *RescanFilter) RemoveAddress(a stdaddr.Address) {
	switch a := a.(type) {
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0:
		delete(f.pubKeyHashes, *a.Hash160())
	case *stdaddr.AddressScriptHashV0:
		delete(f.scriptHashes, *a.Hash160())
	case *stdaddr.AddressPubKeyEcdsaSecp256k1V0:
		serializedPubKey := a.SerializedPubKey()
		switch len(serializedPubKey) {
		case 33: // compressed
			var compressedPubKey [33]byte
			copy(compressedPubKey[:], serializedPubKey)
			delete(f.compressedPubKeys, compressedPubKey)
		default:
			delete(f.otherAddresses, a.String())
		}
	default:
		delete(f.otherAddresses, a.String())
	}
}

// AddUnspentOutPoint adds an outpoint to the filter if it does not already
// exist.
func (f *RescanFilter) AddUnspentOutPoint(op *wire.OutPoint) {
	f.unspent[*op] = struct{}{}
}

// ExistsUnspentOutPoint returns whether an outpoint is contained in the filter.
func (f *RescanFilter) ExistsUnspentOutPoint(op *wire.OutPoint) bool {
	_, ok := f.unspent[*op]
	return ok
}

// RemoveUnspentOutPoint removes an outpoint from the filter if it exists.
func (f *RescanFilter) RemoveUnspentOutPoint(op *wire.OutPoint) {
	delete(f.unspent, *op)
}

// logRescannedTx logs a newly-observed transaction that was added by the
// rescan or a transaction that was changed from unmined to mined.  It should
// only be called if the wallet field logRescannedTransactions is true, which
// will be set after the very first rescan during the process lifetime.
func (w *Wallet) logRescannedTx(txmgrNs walletdb.ReadBucket, height int32, tx *wire.MsgTx) {
	txHash := tx.TxHash()
	haveMined, haveUnmined := w.txStore.ExistsTxMinedOrUnmined(txmgrNs, &txHash)
	if haveUnmined {
		log.Infof("Rescan of block %d discovered previously unmined "+
			"transaction %v", height, &txHash)
	} else if !haveMined {
		log.Infof("Rescan of block %d discovered new transaction %v",
			height, &txHash)
	}
}

// saveRescanned records transactions from a rescanned block.  This
// does not update the network backend with data to watch for future
// relevant transactions as the rescanner is assumed to handle this
// task.
func (w *Wallet) saveRescanned(ctx context.Context, dbtx walletdb.ReadWriteTx,
	hash *chainhash.Hash, txs []*wire.MsgTx, logTxs bool) (err error) {

	const op errors.Op = "wallet.saveRescanned"
	defer func() {
		if err != nil {
			err = errors.E(op, err)
		}
	}()

	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
	blockMeta, err := w.txStore.GetBlockMetaForHash(txmgrNs, hash)
	if err != nil {
		return err
	}
	header, err := w.txStore.GetBlockHeader(dbtx, hash)
	if err != nil {
		return err
	}

	for _, tx := range txs {
		if logTxs {
			w.logRescannedTx(txmgrNs, blockMeta.Height, tx)
		}

		rec, err := udb.NewTxRecordFromMsgTx(tx, time.Now())
		if err != nil {
			return err
		}
		_, err = w.processTransactionRecord(ctx, dbtx, rec, header, &blockMeta)
		if err != nil {
			return err
		}
	}
	return nil
}

// rescan synchronously scans over all blocks on the main chain starting at
// startHash and height up through the recorded main chain tip block.  The
// progress channel, if non-nil, is sent non-error progress notifications with
// the heights the rescan has completed through, starting with the start height.
func (w *Wallet) rescan(ctx context.Context, n NetworkBackend,
	startHash *chainhash.Hash, height int32, p chan<- RescanProgress) error {

	w.logRescannedTransactionsMu.Lock()
	logTxs := w.logRescannedTransactions
	w.logRescannedTransactions = true
	w.logRescannedTransactionsMu.Unlock()

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
		err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
			txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
			var err error
			rescanBlocks, err = w.txStore.GetMainChainBlockHashes(txmgrNs,
				&rescanFrom, inclusive, blockHashStorage)
			return err
		})
		if err != nil {
			return err
		}
		if len(rescanBlocks) == 0 {
			break
		}

		through := height + int32(len(rescanBlocks)) - 1
		// Genesis block is not rescanned
		if height == 0 {
			rescanBlocks = rescanBlocks[1:]
			height = 1
			if len(rescanBlocks) == 0 {
				break
			}
		}
		log.Infof("Rescanning block range [%v, %v]...", height, through)

		// Helper func to save batches of matching transactions.
		saveRescanned := func(blocks []*chainhash.Hash, txs [][]*wire.MsgTx) error {
			if len(blocks) != len(txs) {
				return errors.E(errors.Bug, "len(blocks) must match len(txs)")
			}
			if len(blocks) == 0 {
				return nil
			}

			w.lockedOutpointMu.Lock()
			defer w.lockedOutpointMu.Unlock()

			return walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
				for i := range blocks {
					block := blocks[i]
					txs := txs[i]

					err := w.saveRescanned(ctx, dbtx, block, txs, logTxs)
					if err != nil {
						return err
					}
				}

				return nil
			})
		}

		// Use a background goroutine reading a channel of
		// transactions to process from the rescan.  This allows
		// grouping the updates instead of potentially performing a
		// single db update for every block in the rescanned range.
		type rescannedBlock struct {
			blockHash *chainhash.Hash
			txs       []*wire.MsgTx
			errc      chan error
		}
		ch := make(chan rescannedBlock, 1)
		blockHashes := make([]*chainhash.Hash, 0, maxBlocksPerRescan)
		txs := make([][]*wire.MsgTx, 0, maxBlocksPerRescan)
		lastBatchErr := make(chan error)
		go func() {
			numTxs := 0
			for item := range ch {
				errc := item.errc

				blockHashes = append(blockHashes, item.blockHash)
				txs = append(txs, item.txs)
				numTxs += len(item.txs)

				if numTxs >= 256 { // XXX: tune this
					err := saveRescanned(blockHashes, txs)
					if err != nil {
						errc <- err
						return
					}

					blockHashes = blockHashes[:0]
					txs = txs[:0]
					numTxs = 0
				}

				errc <- nil
			}

			lastBatchErr <- saveRescanned(blockHashes, txs)
		}()

		err = n.Rescan(ctx, rescanBlocks, func(blockHash *chainhash.Hash, txs []*wire.MsgTx) error {
			errc := make(chan error)
			ch <- rescannedBlock{
				blockHash: blockHash,
				txs:       txs,
				errc:      errc,
			}
			return <-errc
		})
		close(ch) // End the background worker
		if err != nil {
			return err
		}
		err = <-lastBatchErr
		if err != nil {
			return err
		}
		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			return w.txStore.UpdateProcessedTxsBlockMarker(dbtx, &rescanBlocks[len(rescanBlocks)-1])
		})
		if p != nil {
			p <- RescanProgress{ScannedThrough: through}
		}
		rescanFrom = rescanBlocks[len(rescanBlocks)-1]
		height += int32(len(rescanBlocks))
		inclusive = false
	}

	log.Infof("Rescan complete")
	return nil
}

// Rescan starts a rescan of the wallet for all blocks on the main chain
// beginning at startHash.  This function blocks until the rescan completes.
func (w *Wallet) Rescan(ctx context.Context, n NetworkBackend, startHash *chainhash.Hash) error {
	const op errors.Op = "wallet.Rescan"

	var startHeight int32
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		header, err := w.txStore.GetSerializedBlockHeader(txmgrNs, startHash)
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
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		startHash, err = w.txStore.GetMainChainBlockHashForHeight(
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
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		startHash, err = w.txStore.GetMainChainBlockHashForHeight(
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

func (w *Wallet) mainChainAncestor(dbtx walletdb.ReadTx, hash *chainhash.Hash) (*chainhash.Hash, error) {
	for {
		mainChain, _ := w.txStore.BlockInMainChain(dbtx, hash)
		if mainChain {
			break
		}
		h, err := w.txStore.GetBlockHeader(dbtx, hash)
		if err != nil {
			return nil, err
		}
		hash = &h.PrevBlock
	}
	return hash, nil
}

// RescanPoint returns the block hash at which a rescan should begin
// (inclusive), or nil when no rescan is necessary.
func (w *Wallet) RescanPoint(ctx context.Context) (*chainhash.Hash, error) {
	const op errors.Op = "wallet.RescanPoint"
	var rp *chainhash.Hash
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		rp, err = w.rescanPoint(dbtx)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return rp, nil
}

func (w *Wallet) rescanPoint(dbtx walletdb.ReadTx) (*chainhash.Hash, error) {
	ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
	r := w.txStore.ProcessedTxsBlockMarker(dbtx)
	r, err := w.mainChainAncestor(dbtx, r) // Walk back to the main chain ancestor
	if err != nil {
		return nil, err
	}
	if tipHash, _ := w.txStore.MainChainTip(dbtx); *r == tipHash {
		return nil, nil
	}
	// r is not the tip, so a child block must exist in the main chain.
	h, err := w.txStore.GetBlockHeader(dbtx, r)
	if err != nil {
		log.Info(err)
		return nil, err
	}
	rescanPoint, err := w.txStore.GetMainChainBlockHashForHeight(ns, int32(h.Height)+1)
	if err != nil {
		log.Info(err)
		return nil, err
	}
	return &rescanPoint, nil
}

// SetBirthState sets the birthday state in the database.
func (w *Wallet) SetBirthState(ctx context.Context, bs *udb.BirthdayState) error {
	const op errors.Op = "wallet.SetBirthState"
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		return udb.SetBirthState(dbtx, bs)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// BirthState returns the birthday state.
func (w *Wallet) BirthState(ctx context.Context) (bs *udb.BirthdayState, err error) {
	const op errors.Op = "wallet.BirthState"
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		bs = udb.BirthState(dbtx)
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return bs, nil
}
