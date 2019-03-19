// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/v3/txauthor"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"golang.org/x/crypto/ripemd160"
)

const opNonstake = txscript.OP_NOP10

// Block contains the minimum amount of data to uniquely identify any block on
// either the best or side chain.
type Block struct {
	Hash   chainhash.Hash
	Height int32
}

// BlockMeta contains the unique identification for a block and any metadata
// pertaining to the block.  At the moment, this additional metadata only
// includes the block time from the block header.
type BlockMeta struct {
	Block
	Time     time.Time
	VoteBits uint16
}

// blockRecord is an in-memory representation of the block record saved in the
// database.
type blockRecord struct {
	Block
	Time         time.Time
	VoteBits     uint16
	transactions []chainhash.Hash
}

// incidence records the block hash and blockchain height of a mined transaction.
// Since a transaction hash alone is not enough to uniquely identify a mined
// transaction (duplicate transaction hashes are allowed), the incidence is used
// instead.
type incidence struct {
	txHash chainhash.Hash
	block  Block
}

// indexedIncidence records the transaction incidence and an input or output
// index.
type indexedIncidence struct {
	incidence
	index uint32
}

// credit describes a transaction output which was or is spendable by wallet.
type credit struct {
	outPoint   wire.OutPoint
	block      Block
	amount     dcrutil.Amount
	change     bool
	spentBy    indexedIncidence // Index == ^uint32(0) if unspent
	opCode     uint8
	isCoinbase bool
	hasExpiry  bool
}

// TxRecord represents a transaction managed by the Store.
type TxRecord struct {
	MsgTx        wire.MsgTx
	Hash         chainhash.Hash
	Received     time.Time
	SerializedTx []byte // Optional: may be nil
	TxType       stake.TxType
}

// NewTxRecord creates a new transaction record that may be inserted into the
// store.  It uses memoization to save the transaction hash and the serialized
// transaction.
func NewTxRecord(serializedTx []byte, received time.Time) (*TxRecord, error) {
	rec := &TxRecord{
		Received:     received,
		SerializedTx: serializedTx,
	}
	err := rec.MsgTx.Deserialize(bytes.NewReader(serializedTx))
	if err != nil {
		return nil, err
	}
	rec.TxType = stake.DetermineTxType(&rec.MsgTx)
	hash := rec.MsgTx.TxHash()
	copy(rec.Hash[:], hash[:])
	return rec, nil
}

// NewTxRecordFromMsgTx creates a new transaction record that may be inserted
// into the store.
func NewTxRecordFromMsgTx(msgTx *wire.MsgTx, received time.Time) (*TxRecord, error) {
	var buf bytes.Buffer
	buf.Grow(msgTx.SerializeSize())
	err := msgTx.Serialize(&buf)
	if err != nil {
		return nil, err
	}
	rec := &TxRecord{
		MsgTx:        *msgTx,
		Received:     received,
		SerializedTx: buf.Bytes(),
	}
	rec.TxType = stake.DetermineTxType(&rec.MsgTx)
	hash := rec.MsgTx.TxHash()
	copy(rec.Hash[:], hash[:])
	return rec, nil
}

// MultisigOut represents a spendable multisignature outpoint contain
// a script hash.
type MultisigOut struct {
	OutPoint     *wire.OutPoint
	Tree         int8
	ScriptHash   [ripemd160.Size]byte
	M            uint8
	N            uint8
	TxHash       chainhash.Hash
	BlockHash    chainhash.Hash
	BlockHeight  uint32
	Amount       dcrutil.Amount
	Spent        bool
	SpentBy      chainhash.Hash
	SpentByIndex uint32
}

// Credit is the type representing a transaction output which was spent or
// is still spendable by wallet.  A UTXO is an unspent Credit, but not all
// Credits are UTXOs.
type Credit struct {
	wire.OutPoint
	BlockMeta
	Amount       dcrutil.Amount
	PkScript     []byte
	Received     time.Time
	FromCoinBase bool
	HasExpiry    bool
}

// Store implements a transaction store for storing and managing wallet
// transactions.
type Store struct {
	chainParams    *chaincfg.Params
	acctLookupFunc func(walletdb.ReadBucket, dcrutil.Address) (uint32, error)
}

// MainChainTip returns the hash and height of the currently marked tip-most
// block of the main chain.
func (s *Store) MainChainTip(ns walletdb.ReadBucket) (chainhash.Hash, int32) {
	var hash chainhash.Hash
	tipHash := ns.Get(rootTipBlock)
	copy(hash[:], tipHash)

	header := ns.NestedReadBucket(bucketHeaders).Get(hash[:])
	height := extractBlockHeaderHeight(header)

	return hash, height
}

// ExtendMainChain inserts a block header and compact filter into the database.
// It must connect to the existing tip block.
//
// If the block is already inserted and part of the main chain, an errors.Exist
// error is returned.
//
// The main chain tip may not be extended unless compact filters have been saved
// for all existing main chain blocks.
func (s *Store) ExtendMainChain(ns walletdb.ReadWriteBucket, header *wire.BlockHeader, f *gcs.Filter) error {
	height := int32(header.Height)
	if height < 1 {
		return errors.E(errors.Invalid, "block 0 cannot be added")
	}
	v := ns.Get(rootHaveCFilters)
	if len(v) != 1 || v[0] != 1 {
		return errors.E(errors.Invalid, "main chain may not be extended without first saving all previous cfilters")
	}

	headerBucket := ns.NestedReadWriteBucket(bucketHeaders)

	blockHash := header.BlockHash()

	currentTipHash := ns.Get(rootTipBlock)
	if !bytes.Equal(header.PrevBlock[:], currentTipHash) {
		// Return a special error if it is a duplicate of an existing block in
		// the main chain (NOT the headers bucket, since headers are never
		// pruned).
		_, v := existsBlockRecord(ns, height)
		if v != nil && bytes.Equal(extractRawBlockRecordHash(v), blockHash[:]) {
			return errors.E(errors.Exist, "block already recorded in main chain")
		}
		return errors.E(errors.Invalid, "not direct child of current tip")
	}
	// Also check that the height is one more than the current height
	// recorded by the current tip.
	currentTipHeader := headerBucket.Get(currentTipHash)
	currentTipHeight := extractBlockHeaderHeight(currentTipHeader)
	if currentTipHeight+1 != height {
		return errors.E(errors.Invalid, "invalid height for next block")
	}

	var err error
	if approvesParent(header.VoteBits) {
		err = stakeValidate(ns, currentTipHeight)
	} else {
		err = stakeInvalidate(ns, currentTipHeight)
	}
	if err != nil {
		return err
	}

	// Add the header
	var headerBuffer bytes.Buffer
	err = header.Serialize(&headerBuffer)
	if err != nil {
		return err
	}
	err = headerBucket.Put(blockHash[:], headerBuffer.Bytes())
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Update the tip block
	err = ns.Put(rootTipBlock, blockHash[:])
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Add an empty block record
	blockKey := keyBlockRecord(height)
	blockVal := valueBlockRecordEmptyFromHeader(blockHash[:], headerBuffer.Bytes())
	err = putRawBlockRecord(ns, blockKey, blockVal)
	if err != nil {
		return err
	}

	// Save the compact filter
	return putRawCFilter(ns, blockHash[:], f.NBytes())
}

// ProcessedTxsBlockMarker returns the hash of the block which records the last
// block after the genesis block which has been recorded as being processed for
// relevant transactions.
func (s *Store) ProcessedTxsBlockMarker(dbtx walletdb.ReadTx) *chainhash.Hash {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	var h chainhash.Hash
	copy(h[:], ns.Get(rootLastTxsBlock))
	return &h
}

// UpdateProcessedTxsBlockMarker updates the hash of the block recording the
// final block since the genesis block for which all transactions have been
// processed.  Hash must describe a main chain block.  This does not modify the
// database if hash has a lower block height than the main chain fork point of
// the existing marker.
func (s *Store) UpdateProcessedTxsBlockMarker(dbtx walletdb.ReadWriteTx, hash *chainhash.Hash) error {
	ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)
	prev := s.ProcessedTxsBlockMarker(dbtx)
	for {
		mainChain, _ := s.BlockInMainChain(dbtx, prev)
		if mainChain {
			break
		}
		h, err := s.GetBlockHeader(dbtx, prev)
		if err != nil {
			return err
		}
		prev = &h.PrevBlock
	}
	prevHeader, err := s.GetBlockHeader(dbtx, prev)
	if err != nil {
		return err
	}
	if mainChain, _ := s.BlockInMainChain(dbtx, hash); !mainChain {
		return errors.E(errors.Invalid, errors.Errorf("%v is not a main chain block", hash))
	}
	header, err := s.GetBlockHeader(dbtx, hash)
	if err != nil {
		return err
	}
	if header.Height > prevHeader.Height {
		err := ns.Put(rootLastTxsBlock, hash[:])
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}
	return nil
}

// IsMissingMainChainCFilters returns whether all compact filters for main chain
// blocks have been recorded to the database after the upgrade which began to
// require them to extend the main chain.  If compact filters are missing, they
// must be added using InsertMissingCFilters.
func (s *Store) IsMissingMainChainCFilters(dbtx walletdb.ReadTx) bool {
	v := dbtx.ReadBucket(wtxmgrBucketKey).Get(rootHaveCFilters)
	return len(v) != 1 || v[0] == 0
}

// MissingCFiltersHeight returns the first main chain block height
// with a missing cfilter.  Errors with NotExist when all main chain
// blocks record cfilters.
func (s *Store) MissingCFiltersHeight(dbtx walletdb.ReadTx) (int32, error) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	c := ns.NestedReadBucket(bucketBlocks).ReadCursor()
	defer c.Close()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		hash := extractRawBlockRecordHash(v)
		_, err := fetchRawCFilter(ns, hash)
		if errors.Is(err, errors.NotExist) {
			height := int32(byteOrder.Uint32(k))
			return height, nil
		}
	}
	return 0, errors.E(errors.NotExist)
}

// InsertMissingCFilters records compact filters for each main chain block
// specified by blockHashes.  This is used to add the additional required
// cfilters after upgrading a database to version TODO as recording cfilters
// becomes a required part of extending the main chain.  This method may be
// called incrementally to record all main chain block cfilters.  When all
// cfilters of the main chain are recorded, extending the main chain becomes
// possible again.
func (s *Store) InsertMissingCFilters(dbtx walletdb.ReadWriteTx, blockHashes []*chainhash.Hash, filters []*gcs.Filter) error {
	ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)
	v := ns.Get(rootHaveCFilters)
	if len(v) == 1 && v[0] != 0 {
		return errors.E(errors.Invalid, "all cfilters for main chain blocks are already recorded")
	}

	if len(blockHashes) != len(filters) {
		return errors.E(errors.Invalid, "slices must have equal len")
	}
	if len(blockHashes) == 0 {
		return nil
	}

	for i, blockHash := range blockHashes {
		// Ensure that blockHashes are ordered and that all previous cfilters in the
		// main chain are known.
		ok := i == 0 && *blockHash == s.chainParams.GenesisHash
		if !ok {
			header := existsBlockHeader(ns, blockHash[:])
			if header == nil {
				return errors.E(errors.NotExist, errors.Errorf("missing header for block %v", blockHash))
			}
			parentHash := extractBlockHeaderParentHash(header)
			if i == 0 {
				_, err := fetchRawCFilter(ns, parentHash)
				ok = err == nil
			} else {
				ok = bytes.Equal(parentHash, blockHashes[i-1][:])
			}
		}
		if !ok {
			return errors.E(errors.Invalid, "block hashes are not ordered or previous cfilters are missing")
		}

		// Record cfilter for this block
		err := putRawCFilter(ns, blockHash[:], filters[i].NBytes())
		if err != nil {
			return err
		}
	}

	// Mark all main chain cfilters as saved if the last block hash is the main
	// chain tip.
	tip, _ := s.MainChainTip(ns)
	if bytes.Equal(tip[:], blockHashes[len(blockHashes)-1][:]) {
		err := ns.Put(rootHaveCFilters, []byte{1})
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}

// BlockCFilter is a compact filter for a Decred block.
type BlockCFilter struct {
	BlockHash chainhash.Hash
	Filter    *gcs.Filter
}

// GetMainChainCFilters returns compact filters from the main chain, starting at
// startHash, copying as many as possible into the storage slice and returning a
// subslice for the total number of results.  If the start hash is not in the
// main chain, this function errors.  If inclusive is true, the startHash is
// included in the results, otherwise only blocks after the startHash are
// included.
func (s *Store) GetMainChainCFilters(dbtx walletdb.ReadTx, startHash *chainhash.Hash, inclusive bool, storage []*BlockCFilter) ([]*BlockCFilter, error) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	header := ns.NestedReadBucket(bucketHeaders).Get(startHash[:])
	if header == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("starting block %v not found", startHash))
	}
	height := extractBlockHeaderHeight(header)
	if !inclusive {
		height++
	}

	blockRecords := ns.NestedReadBucket(bucketBlocks)

	storageUsed := 0
	for storageUsed < len(storage) {
		v := blockRecords.Get(keyBlockRecord(height))
		if v == nil {
			break
		}
		blockHash := extractRawBlockRecordHash(v)
		rawFilter, err := fetchRawCFilter(ns, blockHash)
		if err != nil {
			return nil, err
		}
		fCopy := make([]byte, len(rawFilter))
		copy(fCopy, rawFilter)
		f, err := gcs.FromNBytes(blockcf.P, fCopy)
		if err != nil {
			return nil, err
		}
		bf := &BlockCFilter{Filter: f}
		copy(bf.BlockHash[:], blockHash)
		storage[storageUsed] = bf

		height++
		storageUsed++
	}
	return storage[:storageUsed], nil
}

func extractBlockHeaderParentHash(header []byte) []byte {
	const parentOffset = 4
	return header[parentOffset : parentOffset+chainhash.HashSize]
}

// ExtractBlockHeaderParentHash subslices the header to return the bytes of the
// parent block's hash.  Must only be called on known good input.
//
// TODO: This really should not be exported by this package.
func ExtractBlockHeaderParentHash(header []byte) []byte {
	return extractBlockHeaderParentHash(header)
}

func extractBlockHeaderVoteBits(header []byte) uint16 {
	const voteBitsOffset = 100
	return binary.LittleEndian.Uint16(header[voteBitsOffset:])
}

func extractBlockHeaderHeight(header []byte) int32 {
	const heightOffset = 128
	return int32(binary.LittleEndian.Uint32(header[heightOffset:]))
}

// ExtractBlockHeaderHeight returns the height field that is encoded in the
// header.  Must only be called on known good input.
//
// TODO: This really should not be exported by this package.
func ExtractBlockHeaderHeight(header []byte) int32 {
	return extractBlockHeaderHeight(header)
}

func extractBlockHeaderUnixTime(header []byte) uint32 {
	const timestampOffset = 136
	return binary.LittleEndian.Uint32(header[timestampOffset:])
}

// ExtractBlockHeaderTime returns the unix timestamp that is encoded in the
// header.  Must only be called on known good input.  Header timestamps are only
// 4 bytes and this value is actually limited to a maximum unix time of 2^32-1.
//
// TODO: This really should not be exported by this package.
func ExtractBlockHeaderTime(header []byte) int64 {
	return int64(extractBlockHeaderUnixTime(header))
}

func blockMetaFromHeader(blockHash *chainhash.Hash, header []byte) BlockMeta {
	return BlockMeta{
		Block: Block{
			Hash:   *blockHash,
			Height: extractBlockHeaderHeight(header),
		},
		Time:     time.Unix(int64(extractBlockHeaderUnixTime(header)), 0),
		VoteBits: extractBlockHeaderVoteBits(header),
	}
}

// RawBlockHeader is a 180 byte block header (always true for version 0 blocks).
type RawBlockHeader [180]byte

// Height extracts the height encoded in a block header.
func (h *RawBlockHeader) Height() int32 {
	return extractBlockHeaderHeight(h[:])
}

// BlockHeaderData contains the block hashes and serialized blocks headers that
// are inserted into the database.  At time of writing this only supports wire
// protocol version 0 blocks and changes will need to be made if the block
// header size changes.
type BlockHeaderData struct {
	BlockHash        chainhash.Hash
	SerializedHeader RawBlockHeader
}

// stakeValidate validates regular tree transactions from the main chain block
// at some height.  This does not perform any changes when the block is not
// marked invalid.  When currently invalidated, the invalidated byte is set to 0
// to mark the block as stake validated and the mined balance is incremented for
// all credits of this block.
//
// Stake validation or invalidation should only occur for the block at height
// tip-1.
func stakeValidate(ns walletdb.ReadWriteBucket, height int32) error {
	k, v := existsBlockRecord(ns, height)
	if v == nil {
		return errors.E(errors.IO, errors.Errorf("missing block record for height %v", height))
	}
	if !extractRawBlockRecordStakeInvalid(v) {
		return nil
	}

	minedBalance, err := fetchMinedBalance(ns)
	if err != nil {
		return err
	}

	var blockRec blockRecord
	err = readRawBlockRecord(k, v, &blockRec)
	if err != nil {
		return err
	}

	// Rewrite the block record, marking the regular tree as stake validated.
	err = putRawBlockRecord(ns, k, valueBlockRecordStakeValidated(v))
	if err != nil {
		return err
	}

	for i := range blockRec.transactions {
		txHash := &blockRec.transactions[i]

		_, txv := existsTxRecord(ns, txHash, &blockRec.Block)
		if txv == nil {
			return errors.E(errors.IO, errors.Errorf("missing transaction record for tx %v block %v",
				txHash, &blockRec.Block.Hash))
		}
		var txRec TxRecord
		err = readRawTxRecord(txHash, txv, &txRec)
		if err != nil {
			return err
		}

		// Only regular tree transactions must be considered.
		if txRec.TxType != stake.TxTypeRegular {
			continue
		}

		// Move all credits from this tx to the non-invalidated credits bucket.
		// Add an unspent output for each validated credit.
		// The mined balance is incremented for all moved credit outputs.
		creditOutPoint := wire.OutPoint{Hash: txRec.Hash} // Index set in loop
		for i, output := range txRec.MsgTx.TxOut {
			k, v := existsInvalidatedCredit(ns, &txRec.Hash, uint32(i), &blockRec.Block)
			if v == nil {
				continue
			}

			err = ns.NestedReadWriteBucket(bucketStakeInvalidatedCredits).
				Delete(k)
			if err != nil {
				return errors.E(errors.IO, err)
			}
			err = putRawCredit(ns, k, v)
			if err != nil {
				return err
			}

			creditOutPoint.Index = uint32(i)
			err = putUnspent(ns, &creditOutPoint, &blockRec.Block)
			if err != nil {
				return err
			}

			minedBalance += dcrutil.Amount(output.Value)
		}

		// Move all debits from this tx to the non-invalidated debits bucket,
		// and spend any previous credits spent by the stake validated tx.
		// Remove utxos for all spent previous credits.  The mined balance is
		// decremented for all previous credits that are no longer spendable.
		debitIncidence := indexedIncidence{
			incidence: incidence{txHash: txRec.Hash, block: blockRec.Block},
			// index set for each rec input below.
		}
		for i := range txRec.MsgTx.TxIn {
			debKey, credKey, err := existsInvalidatedDebit(ns, &txRec.Hash, uint32(i),
				&blockRec.Block)
			if err != nil {
				return err
			}
			if debKey == nil {
				continue
			}
			debitIncidence.index = uint32(i)

			debVal := ns.NestedReadBucket(bucketStakeInvalidatedDebits).
				Get(debKey)
			debitAmount := extractRawDebitAmount(debVal)

			_, err = spendCredit(ns, credKey, &debitIncidence)
			if err != nil {
				return err
			}

			prevOut := &txRec.MsgTx.TxIn[i].PreviousOutPoint
			unspentKey := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
			err = deleteRawUnspent(ns, unspentKey)
			if err != nil {
				return err
			}

			err = ns.NestedReadWriteBucket(bucketStakeInvalidatedDebits).
				Delete(debKey)
			if err != nil {
				return errors.E(errors.IO, err)
			}
			err = putRawDebit(ns, debKey, debVal)
			if err != nil {
				return err
			}

			minedBalance -= debitAmount
		}
	}

	return putMinedBalance(ns, minedBalance)
}

// stakeInvalidate invalidates regular tree transactions from the main chain
// block at some height.  This does not perform any changes when the block is
// not marked invalid.  When not marked invalid, the invalidated byte is set to
// 1 to mark the block as stake invalidated and the mined balance is decremented
// for all credits.
//
// Stake validation or invalidation should only occur for the block at height
// tip-1.
//
// See stakeValidate (which performs the reverse operation) for more details.
func stakeInvalidate(ns walletdb.ReadWriteBucket, height int32) error {
	k, v := existsBlockRecord(ns, height)
	if v == nil {
		return errors.E(errors.IO, errors.Errorf("missing block record for height %v", height))
	}
	if extractRawBlockRecordStakeInvalid(v) {
		return nil
	}

	minedBalance, err := fetchMinedBalance(ns)
	if err != nil {
		return err
	}

	var blockRec blockRecord
	err = readRawBlockRecord(k, v, &blockRec)
	if err != nil {
		return err
	}

	// Rewrite the block record, marking the regular tree as stake invalidated.
	err = putRawBlockRecord(ns, k, valueBlockRecordStakeInvalidated(v))
	if err != nil {
		return err
	}

	for i := range blockRec.transactions {
		txHash := &blockRec.transactions[i]

		_, txv := existsTxRecord(ns, txHash, &blockRec.Block)
		if txv == nil {
			return errors.E(errors.IO, errors.Errorf("missing transaction record for tx %v block %v",
				txHash, &blockRec.Block.Hash))
		}
		var txRec TxRecord
		err = readRawTxRecord(txHash, txv, &txRec)
		if err != nil {
			return err
		}

		// Only regular tree transactions must be considered.
		if txRec.TxType != stake.TxTypeRegular {
			continue
		}

		// Move all credits from this tx to the invalidated credits bucket.
		// Remove the unspent output for each invalidated credit.
		// The mined balance is decremented for all moved credit outputs.
		for i, output := range txRec.MsgTx.TxOut {
			k, v := existsCredit(ns, &txRec.Hash, uint32(i), &blockRec.Block)
			if v == nil {
				continue
			}

			err = deleteRawCredit(ns, k)
			if err != nil {
				return err
			}
			err = ns.NestedReadWriteBucket(bucketStakeInvalidatedCredits).
				Put(k, v)
			if err != nil {
				return errors.E(errors.IO, err)
			}

			unspentKey := canonicalOutPoint(txHash, uint32(i))
			err = deleteRawUnspent(ns, unspentKey)
			if err != nil {
				return err
			}

			minedBalance -= dcrutil.Amount(output.Value)
		}

		// Move all debits from this tx to the invalidated debits bucket, and
		// unspend any credit spents by the stake invalidated tx.  The mined
		// balance is incremented for all previous credits that are spendable
		// again.
		for i := range txRec.MsgTx.TxIn {
			debKey, credKey, err := existsDebit(ns, &txRec.Hash, uint32(i),
				&blockRec.Block)
			if err != nil {
				return err
			}
			if debKey == nil {
				continue
			}

			debVal := ns.NestedReadBucket(bucketDebits).Get(debKey)
			debitAmount := extractRawDebitAmount(debVal)

			_, err = unspendRawCredit(ns, credKey)
			if err != nil {
				return err
			}

			err = deleteRawDebit(ns, debKey)
			if err != nil {
				return err
			}
			err = ns.NestedReadWriteBucket(bucketStakeInvalidatedDebits).
				Put(debKey, debVal)
			if err != nil {
				return errors.E(errors.IO, err)
			}

			prevOut := &txRec.MsgTx.TxIn[i].PreviousOutPoint
			unspentKey := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
			unspentVal := extractRawDebitUnspentValue(debVal)
			err = putRawUnspent(ns, unspentKey, unspentVal)
			if err != nil {
				return err
			}

			minedBalance += debitAmount
		}
	}

	return putMinedBalance(ns, minedBalance)
}

// GetMainChainBlockHashForHeight returns the block hash of the block on the
// main chain at a given height.
func (s *Store) GetMainChainBlockHashForHeight(ns walletdb.ReadBucket, height int32) (chainhash.Hash, error) {
	_, v := existsBlockRecord(ns, height)
	if v == nil {
		err := errors.E(errors.NotExist, errors.Errorf("no block at height %v in main chain", height))
		return chainhash.Hash{}, err
	}
	var hash chainhash.Hash
	copy(hash[:], extractRawBlockRecordHash(v))
	return hash, nil
}

// GetSerializedBlockHeader returns the bytes of the serialized header for the
// block specified by its hash.  These bytes are a copy of the value returned
// from the DB and are usable outside of the transaction.
func (s *Store) GetSerializedBlockHeader(ns walletdb.ReadBucket, blockHash *chainhash.Hash) ([]byte, error) {
	return fetchRawBlockHeader(ns, keyBlockHeader(blockHash))
}

// GetBlockHeader returns the block header for the block specified by its hash.
func (s *Store) GetBlockHeader(dbtx walletdb.ReadTx, blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	serialized, err := fetchRawBlockHeader(ns, keyBlockHeader(blockHash))
	if err != nil {
		return nil, err
	}
	header := new(wire.BlockHeader)
	err = header.Deserialize(bytes.NewReader(serialized))
	if err != nil {
		return nil, errors.E(errors.IO, err)
	}
	return header, nil
}

// BlockInMainChain returns whether a block identified by its hash is in the
// current main chain and if so, whether it has been stake invalidated by the
// next main chain block.
func (s *Store) BlockInMainChain(dbtx walletdb.ReadTx, blockHash *chainhash.Hash) (inMainChain bool, invalidated bool) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	header := existsBlockHeader(ns, keyBlockHeader(blockHash))
	if header == nil {
		return false, false
	}

	_, v := existsBlockRecord(ns, extractBlockHeaderHeight(header))
	if v == nil {
		return false, false
	}
	if !bytes.Equal(extractRawBlockRecordHash(v), blockHash[:]) {
		return false, false
	}
	return true, extractRawBlockRecordStakeInvalid(v)
}

// GetBlockMetaForHash returns the BlockMeta for a block specified by its hash.
//
// TODO: This is legacy code now that headers are saved.  BlockMeta can be removed.
func (s *Store) GetBlockMetaForHash(ns walletdb.ReadBucket, blockHash *chainhash.Hash) (BlockMeta, error) {
	header := ns.NestedReadBucket(bucketHeaders).Get(blockHash[:])
	if header == nil {
		err := errors.E(errors.NotExist, errors.Errorf("block %v header not found", blockHash))
		return BlockMeta{}, err
	}
	return BlockMeta{
		Block: Block{
			Hash:   *blockHash,
			Height: extractBlockHeaderHeight(header),
		},
		Time:     time.Unix(int64(extractBlockHeaderUnixTime(header)), 0),
		VoteBits: extractBlockHeaderVoteBits(header),
	}, nil
}

// GetMainChainBlockHashes returns block hashes from the main chain, starting at
// startHash, copying as many as possible into the storage slice and returning a
// subslice for the total number of results.  If the start hash is not in the
// main chain, this function errors.  If inclusive is true, the startHash is
// included in the results, otherwise only blocks after the startHash are
// included.
func (s *Store) GetMainChainBlockHashes(ns walletdb.ReadBucket, startHash *chainhash.Hash,
	inclusive bool, storage []chainhash.Hash) ([]chainhash.Hash, error) {

	header := ns.NestedReadBucket(bucketHeaders).Get(startHash[:])
	if header == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("starting block %v not found", startHash))
	}
	height := extractBlockHeaderHeight(header)

	// Check that the hash of the recorded main chain block at height is equal
	// to startHash.
	blockRecords := ns.NestedReadBucket(bucketBlocks)
	blockVal := blockRecords.Get(keyBlockRecord(height))
	if !bytes.Equal(extractRawBlockRecordHash(blockVal), startHash[:]) {
		return nil, errors.E(errors.Invalid, errors.Errorf("block %v not in main chain", startHash))
	}

	if !inclusive {
		height++
	}

	i := 0
	for i < len(storage) {
		v := blockRecords.Get(keyBlockRecord(height))
		if v == nil {
			break
		}
		copy(storage[i][:], extractRawBlockRecordHash(v))
		height++
		i++
	}
	return storage[:i], nil
}

// fetchAccountForPkScript fetches an account for a given pkScript given a
// credit value, the script, and an account lookup function. It does this
// to maintain compatibility with older versions of the database.
func (s *Store) fetchAccountForPkScript(addrmgrNs walletdb.ReadBucket,
	credVal []byte, unminedCredVal []byte, pkScript []byte) (uint32, error) {

	// Attempt to get the account from the mined credit. If the account was
	// never stored, we can ignore the error and fall through to do the lookup
	// with the acctLookupFunc.
	//
	// TODO: upgrade the database to actually store the account for every credit
	// to avoid this nonsensical error handling.  The upgrade was not done
	// correctly in the past and only began recording accounts for newly
	// inserted credits without modifying existing ones.
	if credVal != nil {
		acct, err := fetchRawCreditAccount(credVal)
		if err == nil {
			return acct, nil
		}
	}
	if unminedCredVal != nil {
		acct, err := fetchRawUnminedCreditAccount(unminedCredVal)
		if err == nil {
			return acct, nil
		}
	}

	// Neither credVal or unminedCredVal were passed, or if they were, they
	// didn't have the account set. Figure out the account from the pkScript the
	// expensive way.
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(0, pkScript, s.chainParams)
	if err != nil {
		return 0, err
	}

	// Only look at the first address returned. This does not handle
	// multisignature or other custom pkScripts in the correct way, which
	// requires multiple account tracking.
	return s.acctLookupFunc(addrmgrNs, addrs[0])
}

// moveMinedTx moves a transaction record from the unmined buckets to block
// buckets.
func (s *Store) moveMinedTx(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket, rec *TxRecord, recKey, recVal []byte, block *BlockMeta) error {
	log.Debugf("Marking unconfirmed transaction %v mined in block %d",
		&rec.Hash, block.Height)

	// Add transaction to block record.
	blockKey, blockVal := existsBlockRecord(ns, block.Height)
	blockVal, err := appendRawBlockRecord(blockVal, &rec.Hash)
	if err != nil {
		return err
	}
	err = putRawBlockRecord(ns, blockKey, blockVal)
	if err != nil {
		return err
	}

	err = putRawTxRecord(ns, recKey, recVal)
	if err != nil {
		return err
	}
	minedBalance, err := fetchMinedBalance(ns)
	if err != nil {
		return err
	}

	// For all transaction inputs, remove the previous output marker from the
	// unmined inputs bucket.  For any mined transactions with unspent credits
	// spent by this transaction, mark each spent, remove from the unspents map,
	// and insert a debit record for the spent credit.
	debitIncidence := indexedIncidence{
		incidence: incidence{txHash: rec.Hash, block: block.Block},
		// index set for each rec input below.
	}
	for i, input := range rec.MsgTx.TxIn {
		unspentKey, credKey := existsUnspent(ns, &input.PreviousOutPoint)

		err = deleteRawUnminedInput(ns, unspentKey)
		if err != nil {
			return err
		}

		if credKey == nil {
			continue
		}

		debitIncidence.index = uint32(i)
		amt, err := spendCredit(ns, credKey, &debitIncidence)
		if err != nil {
			return err
		}

		credVal := existsRawCredit(ns, credKey)
		if credVal == nil {
			return errors.E(errors.IO, errors.Errorf("missing credit "+
				"%v, key %x, spent by %v", &input.PreviousOutPoint, credKey, &rec.Hash))
		}
		creditOpCode := fetchRawCreditTagOpCode(credVal)

		// Do not decrement ticket amounts.
		if !(creditOpCode == txscript.OP_SSTX) {
			minedBalance -= amt
		}
		err = deleteRawUnspent(ns, unspentKey)
		if err != nil {
			return err
		}

		err = putDebit(ns, &rec.Hash, uint32(i), amt, &block.Block, credKey)
		if err != nil {
			return err
		}
	}

	// For each output of the record that is marked as a credit, if the
	// output is marked as a credit by the unconfirmed store, remove the
	// marker and mark the output as a mined credit in the db.
	//
	// Moved credits are added as unspents, even if there is another
	// unconfirmed transaction which spends them.
	cred := credit{
		outPoint: wire.OutPoint{Hash: rec.Hash},
		block:    block.Block,
		spentBy:  indexedIncidence{index: ^uint32(0)},
	}
	for i := uint32(0); i < uint32(len(rec.MsgTx.TxOut)); i++ {
		k := canonicalOutPoint(&rec.Hash, i)
		v := existsRawUnminedCredit(ns, k)
		if v == nil {
			continue
		}

		// TODO: This should use the raw apis.  The credit value (it.cv)
		// can be moved from unmined directly to the credits bucket.
		// The key needs a modification to include the block
		// height/hash.
		amount, change, err := fetchRawUnminedCreditAmountChange(v)
		if err != nil {
			return err
		}
		cred.outPoint.Index = i
		cred.amount = amount
		cred.change = change
		cred.opCode = fetchRawUnminedCreditTagOpcode(v)
		cred.isCoinbase = fetchRawUnminedCreditTagIsCoinbase(v)

		// Legacy credit output values may be of the wrong
		// size.
		scrType := fetchRawUnminedCreditScriptType(v)
		scrPos := fetchRawUnminedCreditScriptOffset(v)
		scrLen := fetchRawUnminedCreditScriptLength(v)

		// Grab the pkScript quickly.
		pkScript, err := fetchRawTxRecordPkScript(recKey, recVal,
			cred.outPoint.Index, scrPos, scrLen)
		if err != nil {
			return err
		}

		acct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
		if err != nil {
			return err
		}

		err = deleteRawUnminedCredit(ns, k)
		if err != nil {
			return err
		}
		err = putUnspentCredit(ns, &cred, scrType, scrPos, scrLen, acct, DBVersion)
		if err != nil {
			return err
		}
		err = putUnspent(ns, &cred.outPoint, &block.Block)
		if err != nil {
			return err
		}

		// Do not increment ticket credits.
		if !(cred.opCode == txscript.OP_SSTX) {
			minedBalance += amount
		}
	}

	err = putMinedBalance(ns, minedBalance)
	if err != nil {
		return err
	}

	return deleteRawUnmined(ns, rec.Hash[:])
}

// InsertMinedTx inserts a new transaction record for a mined transaction into
// the database.  The block header must have been previously saved.  If the
// exact transaction is already saved as an unmined transaction, it is moved to
// a block.  Other unmined transactions which become double spends are removed.
func (s *Store) InsertMinedTx(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket, rec *TxRecord, blockHash *chainhash.Hash) error {
	// Ensure block is in the main chain before proceeding.
	blockHeader := existsBlockHeader(ns, blockHash[:])
	if blockHeader == nil {
		return errors.E(errors.Invalid, "block header must be recorded")
	}
	height := extractBlockHeaderHeight(blockHeader)
	blockVal := ns.NestedReadBucket(bucketBlocks).Get(keyBlockRecord(height))
	if !bytes.Equal(extractRawBlockRecordHash(blockVal), blockHash[:]) {
		return errors.E(errors.Invalid, "mined transactions must be added to main chain blocks")
	}

	// Fetch the mined balance in case we need to update it.
	minedBalance, err := fetchMinedBalance(ns)
	if err != nil {
		return err
	}

	// Add a debit record for each unspent credit spent by this tx.
	block := blockMetaFromHeader(blockHash, blockHeader)
	spender := indexedIncidence{
		incidence: incidence{
			txHash: rec.Hash,
			block:  block.Block,
		},
		// index set for each iteration below
	}
	txType := stake.DetermineTxType(&rec.MsgTx)

	invalidated := false
	if txType == stake.TxTypeRegular {
		height := extractBlockHeaderHeight(blockHeader)
		_, rawBlockRecVal := existsBlockRecord(ns, height)
		invalidated = extractRawBlockRecordStakeInvalid(rawBlockRecVal)
	}

	for i, input := range rec.MsgTx.TxIn {
		unspentKey, credKey := existsUnspent(ns, &input.PreviousOutPoint)
		if credKey == nil {
			// Debits for unmined transactions are not explicitly
			// tracked.  Instead, all previous outputs spent by any
			// unmined transaction are added to a map for quick
			// lookups when it must be checked whether a mined
			// output is unspent or not.
			//
			// Tracking individual debits for unmined transactions
			// could be added later to simplify (and increase
			// performance of) determining some details that need
			// the previous outputs (e.g. determining a fee), but at
			// the moment that is not done (and a db lookup is used
			// for those cases instead).  There is also a good
			// chance that all unmined transaction handling will
			// move entirely to the db rather than being handled in
			// memory for atomicity reasons, so the simplist
			// implementation is currently used.
			continue
		}

		if invalidated {
			// Add an invalidated debit but do not spend the previous credit,
			// remove it from the utxo set, or decrement the mined balance.
			debKey := keyDebit(&rec.Hash, uint32(i), &block.Block)
			credVal := existsRawCredit(ns, credKey)
			credAmt, err := fetchRawCreditAmount(credVal)
			if err != nil {
				return err
			}
			debVal := valueDebit(credAmt, credKey)
			err = ns.NestedReadWriteBucket(bucketStakeInvalidatedDebits).
				Put(debKey, debVal)
			if err != nil {
				return errors.E(errors.IO, err)
			}
		} else {
			spender.index = uint32(i)
			amt, err := spendCredit(ns, credKey, &spender)
			if err != nil {
				return err
			}
			err = putDebit(ns, &rec.Hash, uint32(i), amt, &block.Block,
				credKey)
			if err != nil {
				return err
			}

			// Don't decrement spent ticket amounts.
			isTicketInput := (txType == stake.TxTypeSSGen && i == 1) ||
				(txType == stake.TxTypeSSRtx && i == 0)
			if !isTicketInput {
				minedBalance -= amt
			}

			err = deleteRawUnspent(ns, unspentKey)
			if err != nil {
				return err
			}
		}
	}

	// TODO only update if we actually modified the
	// mined balance.
	err = putMinedBalance(ns, minedBalance)
	if err != nil {
		return err
	}

	// If a transaction record for this tx hash and block already exist,
	// there is nothing left to do.
	k, v := existsTxRecord(ns, &rec.Hash, &block.Block)
	if v != nil {
		return nil
	}

	// If the transaction is a ticket purchase, record it in the ticket
	// purchases bucket.
	if txType == stake.TxTypeSStx {
		tk := rec.Hash[:]
		tv := existsRawTicketRecord(ns, tk)
		if tv == nil {
			tv = valueTicketRecord(-1)
			err := putRawTicketRecord(ns, tk, tv)
			if err != nil {
				return err
			}
		}
	}

	// If the exact tx (not a double spend) is already included but
	// unconfirmed, move it to a block.
	v = existsRawUnmined(ns, rec.Hash[:])
	if v != nil {
		if invalidated {
			panic(fmt.Sprintf("unimplemented: moveMinedTx called on a stake-invalidated tx: block %v height %v tx %v", &block.Hash, block.Height, &rec.Hash))
		}
		return s.moveMinedTx(ns, addrmgrNs, rec, k, v, &block)
	}

	// As there may be unconfirmed transactions that are invalidated by this
	// transaction (either being duplicates, or double spends), remove them
	// from the unconfirmed set.  This also handles removing unconfirmed
	// transaction spend chains if any other unconfirmed transactions spend
	// outputs of the removed double spend.
	err = s.removeDoubleSpends(ns, rec)
	if err != nil {
		return err
	}

	// Adding this transaction hash to the set of transactions from this block.
	blockKey, blockValue := existsBlockRecord(ns, block.Height)
	blockValue, err = appendRawBlockRecord(blockValue, &rec.Hash)
	if err != nil {
		return err
	}
	err = putRawBlockRecord(ns, blockKey, blockValue)
	if err != nil {
		return err
	}

	return putTxRecord(ns, rec, &block.Block)
}

// AddCredit marks a transaction record as containing a transaction output
// spendable by wallet.  The output is added unspent, and is marked spent
// when a new transaction spending the output is inserted into the store.
//
// TODO(jrick): This should not be necessary.  Instead, pass the indexes
// that are known to contain credits when a transaction or merkleblock is
// inserted into the store.
func (s *Store) AddCredit(ns walletdb.ReadWriteBucket, rec *TxRecord, block *BlockMeta,
	index uint32, change bool, account uint32) error {

	if int(index) >= len(rec.MsgTx.TxOut) {
		return errors.E(errors.Invalid, "transaction output index for credit does not exist")
	}

	invalidated := false
	if rec.TxType == stake.TxTypeRegular && block != nil {
		blockHeader := existsBlockHeader(ns, block.Hash[:])
		height := extractBlockHeaderHeight(blockHeader)
		_, rawBlockRecVal := existsBlockRecord(ns, height)
		invalidated = extractRawBlockRecordStakeInvalid(rawBlockRecVal)
	}
	if invalidated {
		// Write an invalidated credit.  Do not create a utxo for the output,
		// and do not increment the mined balance.
		pkScript := rec.MsgTx.TxOut[index].PkScript
		k := keyCredit(&rec.Hash, index, &block.Block)
		cred := credit{
			outPoint: wire.OutPoint{
				Hash:  rec.Hash,
				Index: index,
			},
			block:      block.Block,
			amount:     dcrutil.Amount(rec.MsgTx.TxOut[index].Value),
			change:     change,
			spentBy:    indexedIncidence{index: ^uint32(0)},
			opCode:     getP2PKHOpCode(pkScript),
			isCoinbase: blockchain.IsCoinBaseTx(&rec.MsgTx),
			hasExpiry:  rec.MsgTx.Expiry != 0,
		}
		scTy := pkScriptType(pkScript)
		scLoc := uint32(rec.MsgTx.PkScriptLocs()[index])
		v := valueUnspentCredit(&cred, scTy, scLoc, uint32(len(pkScript)),
			account, DBVersion)
		err := ns.NestedReadWriteBucket(bucketStakeInvalidatedCredits).Put(k, v)
		if err != nil {
			return errors.E(errors.IO, err)
		}
		return nil
	}

	_, err := s.addCredit(ns, rec, block, index, change, account)
	return err
}

// getP2PKHOpCode returns opNonstake for non-stake transactions, or
// the stake op code tag for stake transactions.
func getP2PKHOpCode(pkScript []byte) uint8 {
	class := txscript.GetScriptClass(0, pkScript)
	switch {
	case class == txscript.StakeSubmissionTy:
		return txscript.OP_SSTX
	case class == txscript.StakeGenTy:
		return txscript.OP_SSGEN
	case class == txscript.StakeRevocationTy:
		return txscript.OP_SSRTX
	case class == txscript.StakeSubChangeTy:
		return txscript.OP_SSTXCHANGE
	}

	return opNonstake
}

// pkScriptType determines the general type of pkScript for the purposes of
// fast extraction of pkScript data from a raw transaction record.
func pkScriptType(pkScript []byte) scriptType {
	class := txscript.GetScriptClass(0, pkScript)
	switch class {
	case txscript.PubKeyHashTy:
		return scriptTypeP2PKH
	case txscript.PubKeyTy:
		return scriptTypeP2PK
	case txscript.ScriptHashTy:
		return scriptTypeP2SH
	case txscript.PubkeyHashAltTy:
		return scriptTypeP2PKHAlt
	case txscript.PubkeyAltTy:
		return scriptTypeP2PKAlt
	case txscript.StakeSubmissionTy:
		fallthrough
	case txscript.StakeGenTy:
		fallthrough
	case txscript.StakeRevocationTy:
		fallthrough
	case txscript.StakeSubChangeTy:
		subClass, err := txscript.GetStakeOutSubclass(pkScript)
		if err != nil {
			return scriptTypeUnspecified
		}
		switch subClass {
		case txscript.PubKeyHashTy:
			return scriptTypeSP2PKH
		case txscript.ScriptHashTy:
			return scriptTypeSP2SH
		}
	}

	return scriptTypeUnspecified
}

func (s *Store) addCredit(ns walletdb.ReadWriteBucket, rec *TxRecord, block *BlockMeta,
	index uint32, change bool, account uint32) (bool, error) {

	opCode := getP2PKHOpCode(rec.MsgTx.TxOut[index].PkScript)
	isCoinbase := blockchain.IsCoinBaseTx(&rec.MsgTx)
	hasExpiry := rec.MsgTx.Expiry != wire.NoExpiryValue

	if block == nil {
		// Unmined tx must have been already added for the credit to be added.
		if existsRawUnmined(ns, rec.Hash[:]) == nil {
			panic("attempted to add credit for unmined tx, but unmined tx with same hash does not exist")
		}

		k := canonicalOutPoint(&rec.Hash, index)
		if existsRawUnminedCredit(ns, k) != nil {
			return false, nil
		}
		scrType := pkScriptType(rec.MsgTx.TxOut[index].PkScript)
		pkScrLocs := rec.MsgTx.PkScriptLocs()
		scrLoc := pkScrLocs[index]
		scrLen := len(rec.MsgTx.TxOut[index].PkScript)

		v := valueUnminedCredit(dcrutil.Amount(rec.MsgTx.TxOut[index].Value),
			change, opCode, isCoinbase, hasExpiry, scrType, uint32(scrLoc),
			uint32(scrLen), account, DBVersion)
		return true, putRawUnminedCredit(ns, k, v)
	}

	k, v := existsCredit(ns, &rec.Hash, index, &block.Block)
	if v != nil {
		return false, nil
	}

	txOutAmt := dcrutil.Amount(rec.MsgTx.TxOut[index].Value)
	log.Debugf("Marking transaction %v output %d (%v) spendable",
		rec.Hash, index, txOutAmt)

	cred := credit{
		outPoint: wire.OutPoint{
			Hash:  rec.Hash,
			Index: index,
		},
		block:      block.Block,
		amount:     txOutAmt,
		change:     change,
		spentBy:    indexedIncidence{index: ^uint32(0)},
		opCode:     opCode,
		isCoinbase: isCoinbase,
		hasExpiry:  rec.MsgTx.Expiry != wire.NoExpiryValue,
	}
	scrType := pkScriptType(rec.MsgTx.TxOut[index].PkScript)
	pkScrLocs := rec.MsgTx.PkScriptLocs()
	scrLoc := pkScrLocs[index]
	scrLen := len(rec.MsgTx.TxOut[index].PkScript)

	v = valueUnspentCredit(&cred, scrType, uint32(scrLoc), uint32(scrLen),
		account, DBVersion)
	err := putRawCredit(ns, k, v)
	if err != nil {
		return false, err
	}

	minedBalance, err := fetchMinedBalance(ns)
	if err != nil {
		return false, err
	}
	// Update the balance so long as it's not a ticket output.
	if !(opCode == txscript.OP_SSTX) {
		err = putMinedBalance(ns, minedBalance+txOutAmt)
		if err != nil {
			return false, err
		}
	}

	return true, putUnspent(ns, &cred.outPoint, &block.Block)
}

// AddTicketCommitment adds the given output of a transaction as a ticket
// commitment originating from a wallet account.
//
// The transaction record MUST correspond to a ticket (sstx) transaction, the
// index MUST be from a commitment output and the account MUST be from a
// wallet-controlled account, otherwise the database will be put in an undefined
// state.
func (s *Store) AddTicketCommitment(ns walletdb.ReadWriteBucket, rec *TxRecord,
	index, account uint32) error {

	k := keyTicketCommitment(rec.Hash, index)
	v := existsRawTicketCommitment(ns, k)
	if v != nil {
		// If we have already recorded this ticket commitment, there's nothing
		// else to do. Note that this means unspent ticket commitment entries
		// are added to the index only once, at the very first time the ticket
		// commitment is seen.
		return nil
	}

	if index >= uint32(len(rec.MsgTx.TxOut)) {
		return errors.E(errors.Invalid, "index should be of an existing output")
	}

	if index%2 != 1 {
		return errors.E(errors.Invalid, "index should be of a ticket commitment")
	}

	if rec.TxType != stake.TxTypeSStx {
		return errors.E(errors.Invalid, "transaction record should be of a ticket")
	}

	txOut := rec.MsgTx.TxOut[index]
	txOutAmt, err := stake.AmountFromSStxPkScrCommitment(txOut.PkScript)
	if err != nil {
		return err
	}

	log.Debugf("Accounting for ticket commitment %v:%d (%v) from the wallet",
		rec.Hash, index, txOutAmt)

	v = valueTicketCommitment(txOutAmt, account)
	err = putRawTicketCommitment(ns, k, v)
	if err != nil {
		return err
	}

	v = valueUnspentTicketCommitment(false)
	return putRawUnspentTicketCommitment(ns, k, v)
}

// originalTicketInfo returns the transaction hash and output count for the
// ticket spent by the given transaction.
//
// spenderType MUST be either TxTypeSSGen or TxTypeSSRtx and MUST by the type of
// the provided spenderTx, otherwise the result is undefined.
func originalTicketInfo(spenderType stake.TxType, spenderTx *wire.MsgTx) (chainhash.Hash, uint32) {
	if spenderType == stake.TxTypeSSGen {
		// Votes have an additional input (stakebase) and two additional outputs
		// (previous block hash and vote bits) so account for those.
		return spenderTx.TxIn[1].PreviousOutPoint.Hash,
			uint32((len(spenderTx.TxOut)-2)*2 + 1)
	}

	return spenderTx.TxIn[0].PreviousOutPoint.Hash,
		uint32(len(spenderTx.TxOut)*2 + 1)
}

// removeUnspentTicketCommitments deletes any outstanding commitments that are
// controlled by this wallet and have been redeemed by the given transaction.
//
// rec MUST be either a vote or revocation transaction, otherwise the results
// are undefined.
func (s *Store) removeUnspentTicketCommitments(ns walletdb.ReadWriteBucket,
	txType stake.TxType, tx *wire.MsgTx) error {

	ticketHash, ticketOutCount := originalTicketInfo(txType, tx)
	for i := uint32(1); i < ticketOutCount; i += 2 {
		k := keyTicketCommitment(ticketHash, i)
		if existsRawTicketCommitment(ns, k) == nil {
			// This commitment was not tracked by the wallet, so ignore it.
			continue
		}

		log.Debugf("Removing unspent ticket commitment %v:%d from the wallet",
			ticketHash, i)

		err := deleteRawUnspentTicketCommitment(ns, k)
		if err != nil {
			return err
		}
	}

	return nil
}

// replaceTicketCommitmentUnminedSpent replaces the unminedSpent flag of all
// unspent commitments spent by the given transaction to the given value.
//
// txType MUST be either a TxTypeSSGen or TxTypeSSRtx and MUST be the type for
// the corresponding tx parameter, otherwise the result is undefined.
func (s *Store) replaceTicketCommitmentUnminedSpent(ns walletdb.ReadWriteBucket,
	txType stake.TxType, tx *wire.MsgTx, value bool) error {

	ticketHash, ticketOutCount := originalTicketInfo(txType, tx)

	// Loop over the indices of possible ticket commitments, checking if they
	// are controlled by the wallet. If they are, replace the corresponding
	// unminedSpent flag.
	for i := uint32(1); i < ticketOutCount; i += 2 {
		k := keyTicketCommitment(ticketHash, i)
		if existsRawTicketCommitment(ns, k) == nil {
			// This commitment was not tracked by the wallet, so ignore it.
			continue
		}

		log.Debugf("Marking ticket commitment %v:%d unmined spent as %v",
			ticketHash, i, value)
		v := valueUnspentTicketCommitment(value)
		err := putRawUnspentTicketCommitment(ns, k, v)
		if err != nil {
			return err
		}
	}

	return nil
}

// RedeemTicketCommitments redeems the commitments of the given vote or
// revocation transaction by marking the commitments unminedSpent or removing
// them altogether.
//
// rec MUST be either a vote (ssgen) or revocation (ssrtx) or this method fails.
func (s *Store) RedeemTicketCommitments(ns walletdb.ReadWriteBucket, rec *TxRecord,
	block *BlockMeta) error {

	if (rec.TxType != stake.TxTypeSSGen) && (rec.TxType != stake.TxTypeSSRtx) {
		return errors.E(errors.Invalid, "rec must be a vote or revocation tx")
	}

	if block == nil {
		// When the tx is unmined, we only mark the commitment as spent by an
		// unmined tx.
		return s.replaceTicketCommitmentUnminedSpent(ns, rec.TxType,
			&rec.MsgTx, true)
	}

	// When the tx is mined we completely remove the commitment from the unspent
	// commitments index.
	return s.removeUnspentTicketCommitments(ns, rec.TxType, &rec.MsgTx)
}

// copied from txscript/v1
func getScriptHashFromP2SHScript(pkScript []byte) ([]byte, error) {
	// Scan through the opcodes until the first HASH160 opcode is found.
	const scriptVersion = 0
	tokenizer := txscript.MakeScriptTokenizer(scriptVersion, pkScript)
	for tokenizer.Next() {
		if tokenizer.Opcode() == txscript.OP_HASH160 {
			break
		}
	}

	// Attempt to extract the script hash if the script is not already fully
	// parsed and didn't already fail during parsing above.
	//
	// Note that this means a script without a data push for the script hash or
	// where OP_HASH160 wasn't found will result in nil data being returned.
	// This was done to preserve existing behavior, although it is questionable
	// since a p2sh script has a specific form and if there is no push here,
	// it's not really a p2sh script and thus should probably have returned an
	// appropriate error for calling the function incorrectly.
	if tokenizer.Next() {
		return tokenizer.Data(), nil
	}
	return nil, tokenizer.Err()
}

// AddMultisigOut adds a P2SH multisignature spendable output into the
// transaction manager. In the event that the output already existed but
// was not mined, the output is updated so its value reflects the block
// it was included in.
func (s *Store) AddMultisigOut(ns walletdb.ReadWriteBucket, rec *TxRecord, block *BlockMeta, index uint32) error {
	if int(index) >= len(rec.MsgTx.TxOut) {
		return errors.E(errors.Invalid, "transaction output does not exist")
	}

	empty := &chainhash.Hash{}

	// Check to see if the output already exists and is now being
	// mined into a block. If it does, update the record and return.
	key := keyMultisigOut(rec.Hash, index)
	val := existsMultisigOutCopy(ns, key)
	if val != nil && block != nil {
		blockHashV, _ := fetchMultisigOutMined(val)
		if blockHashV.IsEqual(empty) {
			setMultisigOutMined(val, block.Block.Hash,
				uint32(block.Block.Height))
			return putMultisigOutRawValues(ns, key, val)
		}
		return errors.E(errors.Invalid, "multisig credit is mined")
	}
	// The multisignature output already exists in the database
	// as an unmined, unspent output and something is trying to
	// add it in duplicate. Return.
	if val != nil && block == nil {
		blockHashV, _ := fetchMultisigOutMined(val)
		if blockHashV.IsEqual(empty) {
			return nil
		}
	}

	// Dummy block for created transactions.
	if block == nil {
		block = &BlockMeta{Block{*empty, 0},
			rec.Received,
			0}
	}

	// Otherwise create a full record and insert it.
	p2shScript := rec.MsgTx.TxOut[index].PkScript
	class, _, _, err := txscript.ExtractPkScriptAddrs(
		rec.MsgTx.TxOut[index].Version, p2shScript, s.chainParams)
	if err != nil {
		return err
	}
	tree := wire.TxTreeRegular
	isStakeType := class == txscript.StakeSubmissionTy ||
		class == txscript.StakeSubChangeTy ||
		class == txscript.StakeGenTy ||
		class == txscript.StakeRevocationTy
	if isStakeType {
		class, err = txscript.GetStakeOutSubclass(p2shScript)
		if err != nil {
			return errors.E(errors.Bug, err)
		}
		tree = wire.TxTreeStake
	}
	if class != txscript.ScriptHashTy {
		return errors.E(errors.Invalid, "multisig output must be P2SH")
	}
	scriptHash, err := getScriptHashFromP2SHScript(p2shScript)
	if err != nil {
		return err
	}
	multisigScript := existsTxScript(ns, scriptHash)
	if multisigScript == nil {
		return errors.E(errors.Invalid, "no recorded redeem script for multisig output")
	}
	numPubKeys, requiredSigs, err := txscript.CalcMultiSigStats(multisigScript)
	if err != nil {
		return errors.E(errors.IO, "invalid m-of-n multisig script")
	}
	var p2shScriptHash [ripemd160.Size]byte
	copy(p2shScriptHash[:], scriptHash)
	val = valueMultisigOut(p2shScriptHash,
		uint8(requiredSigs),
		uint8(numPubKeys),
		false,
		tree,
		block.Block.Hash,
		uint32(block.Block.Height),
		dcrutil.Amount(rec.MsgTx.TxOut[index].Value),
		*empty,     // Unspent
		0xFFFFFFFF, // Unspent
		rec.Hash)

	// Write the output, and insert the unspent key.
	err = putMultisigOutRawValues(ns, key, val)
	if err != nil {
		return err
	}
	return putMultisigOutUS(ns, key)
}

// SpendMultisigOut spends a multisignature output by making it spent in
// the general bucket and removing it from the unspent bucket.
func (s *Store) SpendMultisigOut(ns walletdb.ReadWriteBucket, op *wire.OutPoint, spendHash chainhash.Hash, spendIndex uint32) error {
	// Mark the output spent.
	key := keyMultisigOut(op.Hash, op.Index)
	val := existsMultisigOutCopy(ns, key)
	if val == nil {
		return errors.E(errors.NotExist, errors.Errorf("no multisig output for outpoint %v", op))
	}
	// Attempting to double spend an outpoint is an error.
	if fetchMultisigOutSpent(val) {
		_, foundSpendHash, foundSpendIndex := fetchMultisigOutSpentVerbose(val)
		// It's not technically an error to try to respend
		// the output with exactly the same transaction.
		// However, there's no need to set it again. Just return.
		if spendHash == foundSpendHash && foundSpendIndex == spendIndex {
			return nil
		}
		return errors.E(errors.DoubleSpend, errors.Errorf("outpoint %v spent by %v", op, &foundSpendHash))
	}
	setMultisigOutSpent(val, spendHash, spendIndex)

	// Check to see that it's in the unspent bucket.
	existsUnspent := existsMultisigOutUS(ns, key)
	if !existsUnspent {
		return errors.E(errors.IO, "missing unspent multisig record")
	}

	// Write the updated output, and delete the unspent key.
	err := putMultisigOutRawValues(ns, key, val)
	if err != nil {
		return err
	}
	return deleteMultisigOutUS(ns, key)
}

// Rollback removes all blocks at height onwards, moving any transactions within
// each block to the unconfirmed pool.
func (s *Store) Rollback(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket, height int32) error {
	return s.rollback(ns, addrmgrNs, height)
}

func approvesParent(voteBits uint16) bool {
	return dcrutil.IsFlagSet16(voteBits, dcrutil.BlockValid)
}

// Note: does not stake validate the parent block at height-1.  Assumes the
// rollback is being done to add more blocks starting at height, and stake
// validation will occur when that block is attached.
func (s *Store) rollback(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket, height int32) error {
	if height == 0 {
		return errors.E(errors.Invalid, "cannot rollback the genesis block")
	}

	minedBalance, err := fetchMinedBalance(ns)
	if err != nil {
		return err
	}

	// Keep track of all credits that were removed from coinbase
	// transactions.  After detaching all blocks, if any transaction record
	// exists in unmined that spends these outputs, remove them and their
	// spend chains.
	//
	// It is necessary to keep these in memory and fix the unmined
	// transactions later since blocks are removed in increasing order.
	var coinBaseCredits []wire.OutPoint

	var heightsToRemove []int32

	it := makeReverseBlockIterator(ns)
	defer it.close()
	for it.prev() {
		b := &it.elem
		if it.elem.Height < height {
			break
		}

		heightsToRemove = append(heightsToRemove, it.elem.Height)

		log.Debugf("Rolling back transactions from block %v height %d",
			b.Hash, b.Height)

		// cache the values of removed credits so they can be inspected even
		// after removal from the db.
		removedCredits := make(map[string][]byte)

		for i := range b.transactions {
			txHash := &b.transactions[i]

			recKey := keyTxRecord(txHash, &b.Block)
			recVal := existsRawTxRecord(ns, recKey)
			var rec TxRecord
			err = readRawTxRecord(txHash, recVal, &rec)
			if err != nil {
				return err
			}

			err = deleteTxRecord(ns, txHash, &b.Block)
			if err != nil {
				return err
			}

			// Handle coinbase transactions specially since they are
			// not moved to the unconfirmed store.  A coinbase cannot
			// contain any debits, but all credits should be removed
			// and the mined balance decremented.
			if blockchain.IsCoinBaseTx(&rec.MsgTx) {
				for i, output := range rec.MsgTx.TxOut {
					k, v := existsCredit(ns, &rec.Hash,
						uint32(i), &b.Block)
					if v == nil {
						continue
					}

					coinBaseCredits = append(coinBaseCredits, wire.OutPoint{
						Hash:  rec.Hash,
						Index: uint32(i),
						Tree:  wire.TxTreeRegular,
					})

					outPointKey := canonicalOutPoint(&rec.Hash, uint32(i))
					credKey := existsRawUnspent(ns, outPointKey)
					if credKey != nil {
						minedBalance -= dcrutil.Amount(output.Value)
						err = deleteRawUnspent(ns, outPointKey)
						if err != nil {
							return err
						}
					}
					removedCredits[string(k)] = v
					err = deleteRawCredit(ns, k)
					if err != nil {
						return err
					}

					// Check if this output is a multisignature
					// P2SH output. If it is, access the value
					// for the key and mark it unmined.
					msKey := keyMultisigOut(*txHash, uint32(i))
					msVal := existsMultisigOutCopy(ns, msKey)
					if msVal != nil {
						setMultisigOutUnmined(msVal)
						err := putMultisigOutRawValues(ns, msKey, msVal)
						if err != nil {
							return err
						}
					}
				}

				continue
			}

			err = putRawUnmined(ns, txHash[:], recVal)
			if err != nil {
				return err
			}

			txType := rec.TxType

			// For each debit recorded for this transaction, mark
			// the credit it spends as unspent (as long as it still
			// exists) and delete the debit.  The previous output is
			// recorded in the unconfirmed store for every previous
			// output, not just debits.
			for i, input := range rec.MsgTx.TxIn {
				// Skip stakebases.
				if i == 0 && txType == stake.TxTypeSSGen {
					continue
				}

				prevOut := &input.PreviousOutPoint
				prevOutKey := canonicalOutPoint(&prevOut.Hash,
					prevOut.Index)
				err = putRawUnminedInput(ns, prevOutKey, rec.Hash[:])
				if err != nil {
					return err
				}

				// If this input is a debit, remove the debit
				// record and mark the credit that it spent as
				// unspent, incrementing the mined balance.
				debKey, credKey, err := existsDebit(ns,
					&rec.Hash, uint32(i), &b.Block)
				if err != nil {
					return err
				}
				if debKey == nil {
					continue
				}

				// Store the credit OP code for later use.  Since the credit may
				// already have been removed if it also appeared in this block,
				// a cache of removed credits is also checked.
				credVal := existsRawCredit(ns, credKey)
				if credVal == nil {
					credVal = removedCredits[string(credKey)]
				}
				if credVal == nil {
					return errors.E(errors.IO, errors.Errorf("missing credit "+
						"%v, key %x, spent by %v", prevOut, credKey, &rec.Hash))
				}
				creditOpCode := fetchRawCreditTagOpCode(credVal)

				// unspendRawCredit does not error in case the no credit exists
				// for this key, but this behavior is correct.  Since
				// transactions are removed in an unspecified order
				// (transactions in the blo	ck record are not sorted by
				// appearance in the block), this credit may have already been
				// removed.
				var amt dcrutil.Amount
				amt, err = unspendRawCredit(ns, credKey)
				if err != nil {
					return err
				}

				err = deleteRawDebit(ns, debKey)
				if err != nil {
					return err
				}

				// If the credit was previously removed in the
				// rollback, the credit amount is zero.  Only
				// mark the previously spent credit as unspent
				// if it still exists.
				if amt == 0 {
					continue
				}
				unspentVal, err := fetchRawCreditUnspentValue(credKey)
				if err != nil {
					return err
				}

				// Ticket output spends are never decremented, so no need
				// to add them back.
				if !(creditOpCode == txscript.OP_SSTX) {
					minedBalance += amt
				}

				err = putRawUnspent(ns, prevOutKey, unspentVal)
				if err != nil {
					return err
				}

				// Check if this input uses a multisignature P2SH
				// output. If it did, mark the output unspent
				// and create an entry in the unspent bucket.
				msVal := existsMultisigOutCopy(ns, prevOutKey)
				if msVal != nil {
					setMultisigOutUnSpent(msVal)
					err := putMultisigOutRawValues(ns, prevOutKey, msVal)
					if err != nil {
						return err
					}
					err = putMultisigOutUS(ns, prevOutKey)
					if err != nil {
						return err
					}
				}
			}

			// For each detached non-coinbase credit, move the
			// credit output to unmined.  If the credit is marked
			// unspent, it is removed from the utxo set and the
			// mined balance is decremented.
			//
			// TODO: use a credit iterator
			for i, output := range rec.MsgTx.TxOut {
				k, v := existsCredit(ns, &rec.Hash, uint32(i),
					&b.Block)
				if v == nil {
					continue
				}
				vcopy := make([]byte, len(v))
				copy(vcopy, v)
				removedCredits[string(k)] = vcopy

				amt, change, err := fetchRawCreditAmountChange(v)
				if err != nil {
					return err
				}
				opCode := fetchRawCreditTagOpCode(v)
				isCoinbase := fetchRawCreditIsCoinbase(v)
				hasExpiry := fetchRawCreditHasExpiry(v, DBVersion)

				scrType := pkScriptType(output.PkScript)
				scrLoc := rec.MsgTx.PkScriptLocs()[i]
				scrLen := len(rec.MsgTx.TxOut[i].PkScript)

				acct, err := s.fetchAccountForPkScript(addrmgrNs, v, nil, output.PkScript)
				if err != nil {
					return err
				}

				outPointKey := canonicalOutPoint(&rec.Hash, uint32(i))
				unminedCredVal := valueUnminedCredit(amt, change, opCode,
					isCoinbase, hasExpiry, scrType, uint32(scrLoc), uint32(scrLen),
					acct, DBVersion)
				err = putRawUnminedCredit(ns, outPointKey, unminedCredVal)
				if err != nil {
					return err
				}

				err = deleteRawCredit(ns, k)
				if err != nil {
					return err
				}

				credKey := existsRawUnspent(ns, outPointKey)
				if credKey != nil {
					// Ticket amounts were never added, so ignore them when
					// correcting the balance.
					isTicketOutput := (txType == stake.TxTypeSStx && i == 0)
					if !isTicketOutput {
						minedBalance -= dcrutil.Amount(output.Value)
					}
					err = deleteRawUnspent(ns, outPointKey)
					if err != nil {
						return err
					}
				}

				// Check if this output is a multisignature
				// P2SH output. If it is, access the value
				// for the key and mark it unmined.
				msKey := keyMultisigOut(*txHash, uint32(i))
				msVal := existsMultisigOutCopy(ns, msKey)
				if msVal != nil {
					setMultisigOutUnmined(msVal)
					err := putMultisigOutRawValues(ns, msKey, msVal)
					if err != nil {
						return err
					}
				}
			}

			// When rolling back votes and revocations, return unspent status
			// for tracked commitments.
			if (rec.TxType == stake.TxTypeSSGen) || (rec.TxType == stake.TxTypeSSRtx) {
				s.replaceTicketCommitmentUnminedSpent(ns, rec.TxType, &rec.MsgTx, true)
			}
		}

		// reposition cursor before deleting this k/v pair and advancing to the
		// previous.
		it.reposition(it.elem.Height)

		// Avoid cursor deletion until bolt issue #620 is resolved.
		// err = it.delete()
		// if err != nil {
		// 	return err
		// }
	}
	if it.err != nil {
		return it.err
	}

	// Delete the block records outside of the iteration since cursor deletion
	// is broken.
	for _, h := range heightsToRemove {
		err = deleteBlockRecord(ns, h)
		if err != nil {
			return err
		}
	}

	for _, op := range coinBaseCredits {
		opKey := canonicalOutPoint(&op.Hash, op.Index)
		unminedKey := existsRawUnminedInput(ns, opKey)
		if unminedKey != nil {
			unminedVal := existsRawUnmined(ns, unminedKey)
			var unminedRec TxRecord
			copy(unminedRec.Hash[:], unminedKey) // Silly but need an array
			err = readRawTxRecord(&unminedRec.Hash, unminedVal, &unminedRec)
			if err != nil {
				return err
			}

			log.Debugf("Transaction %v spends a removed coinbase "+
				"output -- removing as well", unminedRec.Hash)
			err = s.RemoveUnconfirmed(ns, &unminedRec.MsgTx, &unminedRec.Hash)
			if err != nil {
				return err
			}
		}
	}

	err = putMinedBalance(ns, minedBalance)
	if err != nil {
		return err
	}

	// Mark block hash for height-1 as the new main chain tip.
	_, newTipBlockRecord := existsBlockRecord(ns, height-1)
	newTipHash := extractRawBlockRecordHash(newTipBlockRecord)
	err = ns.Put(rootTipBlock, newTipHash)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

// outputCreditInfo fetches information about a credit from the database,
// fills out a credit struct, and returns it.
func (s *Store) outputCreditInfo(ns walletdb.ReadBucket, op wire.OutPoint, block *Block) (*Credit, error) {
	// It has to exists as a credit or an unmined credit.
	// Look both of these up. If it doesn't, throw an
	// error. Check unmined first, then mined.
	var minedCredV []byte
	unminedCredV := existsRawUnminedCredit(ns,
		canonicalOutPoint(&op.Hash, op.Index))
	if unminedCredV == nil {
		if block != nil {
			credK := keyCredit(&op.Hash, op.Index, block)
			minedCredV = existsRawCredit(ns, credK)
		}
	}
	if minedCredV == nil && unminedCredV == nil {
		return nil, errors.E(errors.IO, errors.Errorf("missing credit for outpoint %v", &op))
	}

	// Error for DB inconsistency if we find any.
	if minedCredV != nil && unminedCredV != nil {
		return nil, errors.E(errors.IO, errors.Errorf("inconsistency: credit %v is marked mined and unmined", &op))
	}

	var amt dcrutil.Amount
	var opCode uint8
	var isCoinbase bool
	var hasExpiry bool
	var mined bool
	var blockTime time.Time
	var pkScript []byte
	var receiveTime time.Time

	if unminedCredV != nil {
		var err error
		amt, err = fetchRawUnminedCreditAmount(unminedCredV)
		if err != nil {
			return nil, err
		}

		opCode = fetchRawUnminedCreditTagOpcode(unminedCredV)
		hasExpiry = fetchRawCreditHasExpiry(unminedCredV, DBVersion)

		v := existsRawUnmined(ns, op.Hash[:])
		received, err := fetchRawUnminedReceiveTime(v)
		if err != nil {
			return nil, err
		}
		receiveTime = time.Unix(received, 0)

		var tx wire.MsgTx
		err = tx.Deserialize(bytes.NewReader(extractRawUnminedTx(v)))
		if err != nil {
			return nil, errors.E(errors.IO, err)
		}
		if op.Index >= uint32(len(tx.TxOut)) {
			return nil, errors.E(errors.IO, errors.Errorf("no output %d for tx %v", op.Index, &op.Hash))
		}
		pkScript = tx.TxOut[op.Index].PkScript
	} else {
		mined = true

		var err error
		amt, err = fetchRawCreditAmount(minedCredV)
		if err != nil {
			return nil, err
		}

		opCode = fetchRawCreditTagOpCode(minedCredV)
		isCoinbase = fetchRawCreditIsCoinbase(minedCredV)
		hasExpiry = fetchRawCreditHasExpiry(minedCredV, DBVersion)

		scrLoc := fetchRawCreditScriptOffset(minedCredV)
		scrLen := fetchRawCreditScriptLength(minedCredV)

		recK, recV := existsTxRecord(ns, &op.Hash, block)
		receiveTime = fetchRawTxRecordReceived(recV)
		pkScript, err = fetchRawTxRecordPkScript(recK, recV, op.Index,
			scrLoc, scrLen)
		if err != nil {
			return nil, err
		}
	}

	op.Tree = wire.TxTreeRegular
	if opCode != opNonstake {
		op.Tree = wire.TxTreeStake
	}

	c := &Credit{
		OutPoint: op,
		BlockMeta: BlockMeta{
			Block: Block{Height: -1},
			Time:  blockTime,
		},
		Amount:       amt,
		PkScript:     pkScript,
		Received:     receiveTime,
		FromCoinBase: isCoinbase,
		HasExpiry:    hasExpiry,
	}
	if mined {
		c.BlockMeta.Block = *block
	}
	return c, nil
}

// UnspentOutputs returns all unspent received transaction outputs.
// The order is undefined.
func (s *Store) UnspentOutputs(ns walletdb.ReadBucket) ([]*Credit, error) {
	var unspent []*Credit

	var op wire.OutPoint
	var block Block
	c := ns.NestedReadBucket(bucketUnspent).ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			c.Close()
			return nil, err
		}
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip this k/v pair.
			continue
		}

		err = readUnspentBlock(v, &block)
		if err != nil {
			c.Close()
			return nil, err
		}

		cred, err := s.outputCreditInfo(ns, op, &block)
		if err != nil {
			c.Close()
			return nil, err
		}

		unspent = append(unspent, cred)
	}
	c.Close()

	c = ns.NestedReadBucket(bucketUnminedCredits).ReadCursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			continue
		}

		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			c.Close()
			return nil, err
		}

		cred, err := s.outputCreditInfo(ns, op, nil)
		if err != nil {
			c.Close()
			return nil, err
		}

		unspent = append(unspent, cred)
	}
	c.Close()

	log.Tracef("%v many utxos found in database", len(unspent))

	return unspent, nil
}

// ForEachUnspentOutpoint calls f on each UTXO outpoint.
// The order is undefined.
func (s *Store) ForEachUnspentOutpoint(dbtx walletdb.ReadTx, f func(*wire.OutPoint) error) error {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	c := ns.NestedReadBucket(bucketUnspent).ReadCursor()
	defer func() {
		c.Close()
	}()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var op wire.OutPoint
		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			return err
		}
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip this k/v pair.
			continue
		}

		block := new(Block)
		err = readUnspentBlock(v, block)
		if err != nil {
			return err
		}

		kC := keyCredit(&op.Hash, op.Index, block)
		vC := existsRawCredit(ns, kC)
		opCode := fetchRawCreditTagOpCode(vC)
		op.Tree = wire.TxTreeRegular
		if opCode != opNonstake {
			op.Tree = wire.TxTreeStake
		}

		if err := f(&op); err != nil {
			return err
		}
	}

	c.Close()
	c = ns.NestedReadBucket(bucketUnminedCredits).ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			continue
		}

		var op wire.OutPoint
		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			return err
		}

		opCode := fetchRawUnminedCreditTagOpcode(v)
		op.Tree = wire.TxTreeRegular
		if opCode != opNonstake {
			op.Tree = wire.TxTreeStake
		}

		if err := f(&op); err != nil {
			return err
		}
	}

	return nil
}

// IsUnspentOutpoint returns whether the outpoint is recorded as a wallet UTXO.
func (s *Store) IsUnspentOutpoint(dbtx walletdb.ReadTx, op *wire.OutPoint) bool {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	k := canonicalOutPoint(&op.Hash, op.Index)
	if v := ns.NestedReadBucket(bucketUnspent); v != nil {
		// Output is mined and not spent by any other mined tx, but may be spent
		// by an unmined transaction.
		return existsRawUnminedInput(ns, k) == nil
	}
	if v := existsRawUnminedCredit(ns, k); v != nil {
		// Output is in an unmined transaction, but may be spent by another
		// unmined transaction.
		return existsRawUnminedInput(ns, k) == nil
	}
	return false
}

// UnspentTickets returns all unspent tickets that are known for this wallet.
// Tickets that have been spent by an unmined vote that is not a vote on the tip
// block are also considered unspent and are returned.  The order of the hashes
// is undefined.
func (s *Store) UnspentTickets(dbtx walletdb.ReadTx, syncHeight int32, includeImmature bool) ([]chainhash.Hash, error) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	tipBlock, _ := s.MainChainTip(ns)
	var tickets []chainhash.Hash
	c := ns.NestedReadBucket(bucketTickets).ReadCursor()
	defer c.Close()
	var hash chainhash.Hash
	for ticketHash, _ := c.First(); ticketHash != nil; ticketHash, _ = c.Next() {
		copy(hash[:], ticketHash)

		// Skip over tickets that are spent by votes or revocations.  As long as
		// the ticket is relevant to the wallet, output zero is recorded as a
		// credit.  Use the credit's spent tracking to determine if the ticket
		// is spent or not.
		opKey := canonicalOutPoint(&hash, 0)
		if existsRawUnspent(ns, opKey) == nil {
			// No unspent record indicates the output was spent by a mined
			// transaction.
			continue
		}
		if spenderHash := existsRawUnminedInput(ns, opKey); spenderHash != nil {
			// A non-nil record for the outpoint indicates that there exists an
			// unmined transaction that spends the output.  Determine if the
			// spender is a vote, and append the hash if the vote is not for the
			// tip block height.  Otherwise continue to the next ticket.
			serializedSpender := extractRawUnminedTx(existsRawUnmined(ns, spenderHash))
			if serializedSpender == nil {
				continue
			}
			var spender wire.MsgTx
			err := spender.Deserialize(bytes.NewReader(serializedSpender))
			if err != nil {
				return nil, errors.E(errors.IO, err)
			}
			if stake.IsSSGen(&spender) {
				voteBlock, _ := stake.SSGenBlockVotedOn(&spender)
				if voteBlock != tipBlock {
					goto Include
				}
			}

			continue
		}

		// When configured to exclude immature tickets, skip the transaction if
		// is unmined or has not reached ticket maturity yet.
		if !includeImmature {
			txRecKey, _ := latestTxRecord(ns, ticketHash)
			if txRecKey == nil {
				continue
			}
			var height int32
			err := readRawTxRecordBlockHeight(txRecKey, &height)
			if err != nil {
				return nil, err
			}
			if !ticketMatured(s.chainParams, height, syncHeight) {
				continue
			}
		}

	Include:
		tickets = append(tickets, hash)
	}
	return tickets, nil
}

// OwnTicket returns whether ticketHash is the hash of a ticket purchase
// transaction managed by the wallet.
func (s *Store) OwnTicket(dbtx walletdb.ReadTx, ticketHash *chainhash.Hash) bool {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	v := existsRawTicketRecord(ns, ticketHash[:])
	return v != nil
}

// Ticket embeds a TxRecord for a ticket purchase transaction, the block it is
// mined in (if any), and the transaction hash of the vote or revocation
// transaction that spends the ticket (if any).
type Ticket struct {
	TxRecord
	Block       Block          // Height -1 if unmined
	SpenderHash chainhash.Hash // Zero value if unspent
}

// TicketIterator is used to iterate over all ticket purchase transactions.
type TicketIterator struct {
	Ticket
	ns     walletdb.ReadBucket
	c      walletdb.ReadCursor
	ck, cv []byte
	err    error
}

// IterateTickets returns an object used to iterate over all ticket purchase
// transactions.
func (s *Store) IterateTickets(dbtx walletdb.ReadTx) *TicketIterator {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	c := ns.NestedReadBucket(bucketTickets).ReadCursor()
	ck, cv := c.First()
	return &TicketIterator{ns: ns, c: c, ck: ck, cv: cv}
}

// Next reads the next Ticket from the database, writing it to the iterator's
// embedded Ticket member.  Returns false after all tickets have been iterated
// over or an error occurs.
func (it *TicketIterator) Next() bool {
	if it.err != nil {
		return false
	}

	// Some tickets may be recorded in the tickets bucket but the transaction
	// records for them are missing because they were double spent and removed.
	// Add a label here so that the code below can branch back here to skip to
	// the next ticket.  This could also be implemented using a for loop, but at
	// the loss of an indent.
CheckNext:

	// The cursor value will be nil when all items in the bucket have been
	// iterated over.
	if it.cv == nil {
		return false
	}

	// Determine whether there is a mined transaction record for the ticket
	// purchase, an unmined transaction record, or no recorded transaction at
	// all.
	var ticketHash chainhash.Hash
	copy(ticketHash[:], it.ck)
	if k, v := latestTxRecord(it.ns, it.ck); v != nil {
		// Ticket is recorded mined
		err := readRawTxRecordBlock(k, &it.Block)
		if err != nil {
			it.err = err
			return false
		}
		err = readRawTxRecord(&ticketHash, v, &it.TxRecord)
		if err != nil {
			it.err = err
			return false
		}

		// Check if the ticket is spent or not.  Look up the credit for output 0
		// and check if either a debit is recorded or the output is spent by an
		// unmined transaction.
		_, credVal := existsCredit(it.ns, &ticketHash, 0, &it.Block)
		if credVal != nil {
			if extractRawCreditIsSpent(credVal) {
				debKey := extractRawCreditSpenderDebitKey(credVal)
				debHash := extractRawDebitHash(debKey)
				copy(it.SpenderHash[:], debHash)
			} else {
				it.SpenderHash = chainhash.Hash{}
			}
		} else {
			opKey := canonicalOutPoint(&ticketHash, 0)
			spenderVal := existsRawUnminedInput(it.ns, opKey)
			if spenderVal != nil {
				copy(it.SpenderHash[:], spenderVal)
			} else {
				it.SpenderHash = chainhash.Hash{}
			}
		}
	} else if v := existsRawUnmined(it.ns, ticketHash[:]); v != nil {
		// Ticket is recorded unmined
		it.Block = Block{Height: -1}
		// Unmined tickets cannot be spent
		it.SpenderHash = chainhash.Hash{}
		err := readRawTxRecord(&ticketHash, v, &it.TxRecord)
		if err != nil {
			it.err = err
			return false
		}
	} else {
		// Transaction was removed, skip to next
		it.ck, it.cv = it.c.Next()
		goto CheckNext
	}

	// Advance the cursor to the next item before returning.  Next expects the
	// cursor key and value to be set to the next item to read.
	it.ck, it.cv = it.c.Next()

	return true
}

// Err returns the final error state of the iterator.  It should be checked
// after iteration completes when Next returns false.
func (it *TicketIterator) Err() error { return it.err }

func (it *TicketIterator) Close() {
	if it.c != nil {
		it.c.Close()
	}
}

// MultisigCredit is a redeemable P2SH multisignature credit.
type MultisigCredit struct {
	OutPoint   *wire.OutPoint
	ScriptHash [ripemd160.Size]byte
	MSScript   []byte
	M          uint8
	N          uint8
	Amount     dcrutil.Amount
}

// GetMultisigOutput takes an outpoint and returns multisignature
// credit data stored about it.
func (s *Store) GetMultisigOutput(ns walletdb.ReadBucket, op *wire.OutPoint) (*MultisigOut, error) {
	key := canonicalOutPoint(&op.Hash, op.Index)
	val := existsMultisigOutCopy(ns, key)
	if val == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no multisig output for outpoint %v", op))
	}

	return fetchMultisigOut(key, val)
}

// UnspentMultisigCreditsForAddress returns all unspent multisignature P2SH
// credits in the wallet for some specified address.
func (s *Store) UnspentMultisigCreditsForAddress(ns walletdb.ReadBucket, addr dcrutil.Address) ([]*MultisigCredit, error) {
	p2shAddr, ok := addr.(*dcrutil.AddressScriptHash)
	if !ok {
		return nil, errors.E(errors.Invalid, "address must be P2SH")
	}
	addrScrHash := p2shAddr.Hash160()

	var mscs []*MultisigCredit
	c := ns.NestedReadBucket(bucketMultisigUsp).ReadCursor()
	defer c.Close()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		val := existsMultisigOutCopy(ns, k)
		if val == nil {
			return nil, errors.E(errors.IO, "missing multisig credit")
		}

		// Skip everything that's unrelated to the address
		// we're concerned about.
		scriptHash := fetchMultisigOutScrHash(val)
		if scriptHash != *addrScrHash {
			continue
		}

		var op wire.OutPoint
		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			return nil, err
		}

		multisigScript := existsTxScript(ns, scriptHash[:])
		if multisigScript == nil {
			return nil, errors.E(errors.IO, "missing multisig redeem script")
		}
		m, n := fetchMultisigOutMN(val)
		amount := fetchMultisigOutAmount(val)
		op.Tree = fetchMultisigOutTree(val)

		msc := &MultisigCredit{
			&op,
			scriptHash,
			multisigScript,
			m,
			n,
			amount,
		}
		mscs = append(mscs, msc)
	}

	return mscs, nil
}

type minimalCredit struct {
	txRecordKey []byte
	index       uint32
	Amount      int64
	tree        int8
	unmined     bool
}

// byUtxoAmount defines the methods needed to satisify sort.Interface to
// sort a slice of Utxos by their amount.
type byUtxoAmount []*minimalCredit

func (u byUtxoAmount) Len() int           { return len(u) }
func (u byUtxoAmount) Less(i, j int) bool { return u[i].Amount < u[j].Amount }
func (u byUtxoAmount) Swap(i, j int)      { u[i], u[j] = u[j], u[i] }

// confirmed checks whether a transaction at height txHeight has met minConf
// confirmations for a blockchain at height curHeight.
func confirmed(minConf, txHeight, curHeight int32) bool {
	return confirms(txHeight, curHeight) >= minConf
}

// confirms returns the number of confirmations for a transaction in a block at
// height txHeight (or -1 for an unconfirmed tx) given the chain height
// curHeight.
func confirms(txHeight, curHeight int32) int32 {
	switch {
	case txHeight == -1, txHeight > curHeight:
		return 0
	default:
		return curHeight - txHeight + 1
	}
}

// coinbaseMatured returns whether a transaction mined at txHeight has
// reached coinbase maturity in a chain with tip height curHeight.
func coinbaseMatured(params *chaincfg.Params, txHeight, curHeight int32) bool {
	return txHeight >= 0 && curHeight-txHeight+1 > int32(params.CoinbaseMaturity)
}

// ticketChangeMatured returns whether a ticket change mined at
// txHeight has reached ticket maturity in a chain with a tip height
// curHeight.
func ticketChangeMatured(params *chaincfg.Params, txHeight, curHeight int32) bool {
	return txHeight >= 0 && curHeight-txHeight+1 > int32(params.SStxChangeMaturity)
}

// ticketMatured returns whether a ticket mined at txHeight has
// reached ticket maturity in a chain with a tip height curHeight.
func ticketMatured(params *chaincfg.Params, txHeight, curHeight int32) bool {
	// dcrd has an off-by-one in the calculation of the ticket
	// maturity, which results in maturity being one block higher
	// than the params would indicate.
	return txHeight >= 0 && curHeight-txHeight > int32(params.TicketMaturity)
}

// ticketExpired returns whether a ticket mined at txHeight has
// reached ticket expiry in a chain with a tip height curHeight.
func ticketExpired(params *chaincfg.Params, txHeight, curHeight int32) bool {
	// Ticket maturity off-by-one extends to the expiry depth as well.
	return txHeight >= 0 && curHeight-txHeight > int32(params.TicketMaturity)+int32(params.TicketExpiry)
}

func (s *Store) fastCreditPkScriptLookup(ns walletdb.ReadBucket, credKey []byte, unminedCredKey []byte) ([]byte, error) {
	// It has to exists as a credit or an unmined credit.
	// Look both of these up. If it doesn't, throw an
	// error. Check unmined first, then mined.
	var minedCredV []byte
	unminedCredV := existsRawUnminedCredit(ns, unminedCredKey)
	if unminedCredV == nil {
		minedCredV = existsRawCredit(ns, credKey)
	}
	if minedCredV == nil && unminedCredV == nil {
		return nil, errors.E(errors.IO, "missing mined and unmined credit")
	}

	if unminedCredV != nil { // unmined
		var op wire.OutPoint
		err := readCanonicalOutPoint(unminedCredKey, &op)
		if err != nil {
			return nil, err
		}
		k := op.Hash[:]
		v := existsRawUnmined(ns, k)
		var tx wire.MsgTx
		err = tx.Deserialize(bytes.NewReader(extractRawUnminedTx(v)))
		if err != nil {
			return nil, errors.E(errors.IO, err)
		}
		if op.Index >= uint32(len(tx.TxOut)) {
			return nil, errors.E(errors.IO, errors.Errorf("no output %d for tx %v", op.Index, &op.Hash))
		}
		return tx.TxOut[op.Index].PkScript, nil
	}

	scrLoc := fetchRawCreditScriptOffset(minedCredV)
	scrLen := fetchRawCreditScriptLength(minedCredV)

	k := extractRawCreditTxRecordKey(credKey)
	v := existsRawTxRecord(ns, k)
	idx := extractRawCreditIndex(credKey)
	return fetchRawTxRecordPkScript(k, v, idx, scrLoc, scrLen)
}

// minimalCreditToCredit looks up a minimal credit's data and prepares a Credit
// from this data.
func (s *Store) minimalCreditToCredit(ns walletdb.ReadBucket, mc *minimalCredit) (*Credit, error) {
	var cred *Credit

	switch mc.unmined {
	case false: // Mined transactions.
		opHash, err := chainhash.NewHash(mc.txRecordKey[0:32])
		if err != nil {
			return nil, err
		}

		var block Block
		err = readUnspentBlock(mc.txRecordKey[32:68], &block)
		if err != nil {
			return nil, err
		}

		var op wire.OutPoint
		op.Hash = *opHash
		op.Index = mc.index

		cred, err = s.outputCreditInfo(ns, op, &block)
		if err != nil {
			return nil, err
		}

	case true: // Unmined transactions.
		opHash, err := chainhash.NewHash(mc.txRecordKey[0:32])
		if err != nil {
			return nil, err
		}

		var op wire.OutPoint
		op.Hash = *opHash
		op.Index = mc.index

		cred, err = s.outputCreditInfo(ns, op, nil)
		if err != nil {
			return nil, err
		}
	}

	return cred, nil
}

// UnspentOutputsForAmount returns all non-stake outputs that sum up to the
// amount passed. If not enough funds are found, a nil pointer is returned
// without error.
func (s *Store) UnspentOutputsForAmount(ns, addrmgrNs walletdb.ReadBucket, needed dcrutil.Amount, syncHeight int32, minConf int32, all bool, account uint32) ([]*Credit, error) {
	var eligible []*minimalCredit
	var toUse []*minimalCredit
	var unspent []*Credit
	found := dcrutil.Amount(0)

	c := ns.NestedReadBucket(bucketUnspent).ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if found >= needed {
			break
		}

		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			continue
		}

		cKey := make([]byte, 72)
		copy(cKey[0:32], k[0:32])   // Tx hash
		copy(cKey[32:36], v[0:4])   // Block height
		copy(cKey[36:68], v[4:36])  // Block hash
		copy(cKey[68:72], k[32:36]) // Output index

		cVal := existsRawCredit(ns, cKey)
		if cVal == nil {
			continue
		}

		if !all {
			// Check the account first.
			pkScript, err := s.fastCreditPkScriptLookup(ns, cKey, nil)
			if err != nil {
				c.Close()
				return nil, err
			}
			thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, cVal, nil, pkScript)
			if err != nil {
				c.Close()
				return nil, err
			}
			if account != thisAcct {
				continue
			}
		}

		amt, spent, err := fetchRawCreditAmountSpent(cVal)
		if err != nil {
			c.Close()
			return nil, err
		}

		// This should never happen since this is already in bucket
		// unspent, but let's be careful anyway.
		if spent {
			continue
		}
		// Skip ticket outputs, as only SSGen can spend these.
		opcode := fetchRawCreditTagOpCode(cVal)
		if opcode == txscript.OP_SSTX {
			continue
		}

		// Only include this output if it meets the required number of
		// confirmations.  Coinbase transactions must have have reached
		// maturity before their outputs may be spent.
		txHeight := extractRawCreditHeight(cKey)
		if !confirmed(minConf, txHeight, syncHeight) {
			continue
		}

		// Skip outputs that are not mature.
		if fetchRawCreditIsCoinbase(cVal) {
			if !coinbaseMatured(s.chainParams, txHeight, syncHeight) {
				continue
			}
		}
		// Skip outputs that have an expiry but have not yet reached
		// coinbase maturity .
		if fetchRawCreditHasExpiry(cVal, DBVersion) {
			if !coinbaseMatured(s.chainParams, txHeight, syncHeight) {
				continue
			}
		}
		if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
			if !coinbaseMatured(s.chainParams, txHeight, syncHeight) {
				continue
			}
		}
		if opcode == txscript.OP_SSTXCHANGE {
			if !ticketChangeMatured(s.chainParams, txHeight, syncHeight) {
				continue
			}
		}

		// Determine the txtree for the outpoint by whether or not it's
		// using stake tagged outputs.
		tree := wire.TxTreeRegular
		if opcode != opNonstake {
			tree = wire.TxTreeStake
		}

		mc := &minimalCredit{
			extractRawCreditTxRecordKey(cKey),
			extractRawCreditIndex(cKey),
			int64(amt),
			tree,
			false,
		}

		eligible = append(eligible, mc)
		found += amt
	}
	c.Close()

	// Unconfirmed transaction output handling.
	if minConf == 0 {
		c = ns.NestedReadBucket(bucketUnminedCredits).ReadCursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if found >= needed {
				break
			}

			// Make sure this output was not spent by an unmined transaction.
			// If it was, skip this credit.
			if existsRawUnminedInput(ns, k) != nil {
				continue
			}

			// Check the account first.
			if !all {
				pkScript, err := s.fastCreditPkScriptLookup(ns, nil, k)
				if err != nil {
					c.Close()
					return nil, err
				}
				thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
				if err != nil {
					c.Close()
					return nil, err
				}
				if account != thisAcct {
					continue
				}
			}

			amt, err := fetchRawUnminedCreditAmount(v)
			if err != nil {
				c.Close()
				return nil, err
			}

			// Skip ticket outputs, as only SSGen can spend these.
			opcode := fetchRawUnminedCreditTagOpcode(v)
			if opcode == txscript.OP_SSTX {
				continue
			}

			// Skip outputs that are not mature.
			switch opcode {
			case txscript.OP_SSGEN, txscript.OP_SSRTX, txscript.OP_SSTXCHANGE:
				continue
			}

			// Determine the txtree for the outpoint by whether or not it's
			// using stake tagged outputs.
			tree := wire.TxTreeRegular
			if opcode != opNonstake {
				tree = wire.TxTreeStake
			}

			localOp := new(wire.OutPoint)
			err = readCanonicalOutPoint(k, localOp)
			if err != nil {
				c.Close()
				return nil, err
			}

			mc := &minimalCredit{
				localOp.Hash[:],
				localOp.Index,
				int64(amt),
				tree,
				true,
			}

			eligible = append(eligible, mc)
			found += amt
		}
		c.Close()
	}

	// Sort by amount, descending.
	sort.Sort(sort.Reverse(byUtxoAmount(eligible)))

	sum := int64(0)
	for _, mc := range eligible {
		toUse = append(toUse, mc)
		sum += mc.Amount

		// Exit the loop if we have enough outputs.
		if sum >= int64(needed) {
			break
		}
	}

	// We couldn't find enough utxos to possibly generate an output
	// of needed, so just return.
	if sum < int64(needed) {
		return nil, nil
	}

	// Look up the Credit data we need for our utxo and store it.
	for _, mc := range toUse {
		credit, err := s.minimalCreditToCredit(ns, mc)
		if err != nil {
			return nil, err
		}
		unspent = append(unspent, credit)
	}

	return unspent, nil
}

// InputSource provides a method (SelectInputs) to incrementally select unspent
// outputs to use as transaction inputs.
type InputSource struct {
	source func(dcrutil.Amount) (*txauthor.InputDetail, error)
}

// SelectInputs selects transaction inputs to redeem unspent outputs stored in
// the database.  It may be called multiple times with increasing target amounts
// to return additional inputs for a higher target amount.  It returns the total
// input amount referenced by the previous transaction outputs, a slice of
// transaction inputs referencing these outputs, and a slice of previous output
// scripts from each previous output referenced by the corresponding input.
func (s *InputSource) SelectInputs(target dcrutil.Amount) (*txauthor.InputDetail, error) {
	return s.source(target)
}

// MakeInputSource creates an InputSource to redeem unspent outputs from an
// account.  The minConf and syncHeight parameters are used to filter outputs
// based on some spendable policy.
//
// Deprecated: Use MakeIgnoredInputSource.
func (s *Store) MakeInputSource(ns, addrmgrNs walletdb.ReadBucket, account uint32, minConf, syncHeight int32) InputSource {
	return s.MakeIgnoredInputSource(ns, addrmgrNs, account, minConf, syncHeight, nil)
}

// MakeIgnoredInputSource is identical to MakeInputSource but allows an optional
// function to be checked to ignore including an input in the results.
func (s *Store) MakeIgnoredInputSource(ns, addrmgrNs walletdb.ReadBucket, account uint32, minConf,
	syncHeight int32, ignore func(*wire.OutPoint) bool) InputSource {

	// Cursors to iterate over the (mined) unspent and unmined credit
	// buckets.  These are closed over by the returned input source and
	// reused across multiple calls.
	//
	// These cursors are initialized to nil and are set to a valid cursor
	// when first needed.  This is done since cursors are not positioned
	// when created, and positioning a cursor also returns a key/value pair.
	// The simplest way to handle this is to branch to either cursor.First
	// or cursor.Next depending on whether the cursor has already been
	// created or not.
	var bucketUnspentCursor, bucketUnminedCreditsCursor walletdb.ReadCursor

	defer func() {
		if bucketUnspentCursor != nil {
			bucketUnspentCursor.Close()
			bucketUnspentCursor = nil
		}

		if bucketUnminedCreditsCursor != nil {
			bucketUnminedCreditsCursor.Close()
			bucketUnminedCreditsCursor = nil
		}
	}()

	// Current inputs and their total value.  These are closed over by the
	// returned input source and reused across multiple calls.
	var (
		currentTotal      dcrutil.Amount
		currentInputs     []*wire.TxIn
		currentScripts    [][]byte
		redeemScriptSizes []int
	)

	f := func(target dcrutil.Amount) (*txauthor.InputDetail, error) {
		for currentTotal < target || target == 0 {
			var k, v []byte
			if bucketUnspentCursor == nil {
				b := ns.NestedReadBucket(bucketUnspent)
				bucketUnspentCursor = b.ReadCursor()
				k, v = bucketUnspentCursor.First()
			} else {
				k, v = bucketUnspentCursor.Next()
			}
			if k == nil || v == nil {
				break
			}
			if existsRawUnminedInput(ns, k) != nil {
				// Output is spent by an unmined transaction.
				// Skip to next unmined credit.
				continue
			}

			cKey := make([]byte, 72)
			copy(cKey[0:32], k[0:32])   // Tx hash
			copy(cKey[32:36], v[0:4])   // Block height
			copy(cKey[36:68], v[4:36])  // Block hash
			copy(cKey[68:72], k[32:36]) // Output index

			cVal := existsRawCredit(ns, cKey)

			// Check the account first.
			pkScript, err := s.fastCreditPkScriptLookup(ns, cKey, nil)
			if err != nil {
				return nil, err
			}
			thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, cVal, nil, pkScript)
			if err != nil {
				return nil, err
			}
			if account != thisAcct {
				continue
			}

			amt, spent, err := fetchRawCreditAmountSpent(cVal)
			if err != nil {
				return nil, err
			}

			// This should never happen since this is already in bucket
			// unspent, but let's be careful anyway.
			if spent {
				continue
			}

			// Skip zero value outputs.
			if amt == 0 {
				continue
			}

			// Skip ticket outputs, as only SSGen can spend these.
			opcode := fetchRawCreditTagOpCode(cVal)
			if opcode == txscript.OP_SSTX {
				continue
			}

			// Only include this output if it meets the required number of
			// confirmations.  Coinbase transactions must have have reached
			// maturity before their outputs may be spent.
			txHeight := extractRawCreditHeight(cKey)
			if !confirmed(minConf, txHeight, syncHeight) {
				continue
			}

			// Skip outputs that are not mature.
			if opcode == opNonstake && fetchRawCreditIsCoinbase(cVal) {
				if !coinbaseMatured(s.chainParams, txHeight, syncHeight) {
					continue
				}
			}
			if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
				if !coinbaseMatured(s.chainParams, txHeight, syncHeight) {
					continue
				}
			}
			if opcode == txscript.OP_SSTXCHANGE {
				if !ticketChangeMatured(s.chainParams, txHeight, syncHeight) {
					continue
				}
			}

			// Determine the txtree for the outpoint by whether or not it's
			// using stake tagged outputs.
			tree := wire.TxTreeRegular
			if opcode != opNonstake {
				tree = wire.TxTreeStake
			}

			var op wire.OutPoint
			err = readCanonicalOutPoint(k, &op)
			if err != nil {
				return nil, err
			}
			op.Tree = tree

			if ignore != nil && ignore(&op) {
				continue
			}

			input := wire.NewTxIn(&op, int64(amt), nil)
			var scriptSize int

			// Unspent credits are currently expected to be either P2PKH or
			// P2PK, P2PKH/P2SH nested in a revocation/stakechange/vote output.
			scriptClass := txscript.GetScriptClass(0, pkScript)

			switch scriptClass {
			case txscript.PubKeyHashTy:
				scriptSize = txsizes.RedeemP2PKHSigScriptSize
			case txscript.PubKeyTy:
				scriptSize = txsizes.RedeemP2PKSigScriptSize
			case txscript.StakeRevocationTy, txscript.StakeSubChangeTy, txscript.StakeGenTy:
				scriptClass, err = txscript.GetStakeOutSubclass(pkScript)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to extract nested script in stake output: %v",
						err)
				}

				// For stake transactions we expect P2PKH and P2SH script class
				// types only but ignore P2SH script type since it can pay
				// to any script which the wallet may not recognize.
				if scriptClass != txscript.PubKeyHashTy {
					log.Errorf("unexpected nested script class for credit: %v",
						scriptClass)
					continue
				}

				scriptSize = txsizes.RedeemP2PKHSigScriptSize
			default:
				log.Errorf("unexpected script class for credit: %v",
					scriptClass)
				continue
			}

			currentTotal += amt
			currentInputs = append(currentInputs, input)
			currentScripts = append(currentScripts, pkScript)
			redeemScriptSizes = append(redeemScriptSizes, scriptSize)
		}

		// Return the current results if the target was specified and met
		// or unspent unmined credits can not be included.
		if (target != 0 && currentTotal >= target) || minConf != 0 {
			inputDetail := &txauthor.InputDetail{
				Amount:            currentTotal,
				Inputs:            currentInputs,
				Scripts:           currentScripts,
				RedeemScriptSizes: redeemScriptSizes,
			}

			return inputDetail, nil
		}

		// Iterate through unspent unmined credits.
		for currentTotal < target || target == 0 {
			var k, v []byte
			if bucketUnminedCreditsCursor == nil {
				b := ns.NestedReadBucket(bucketUnminedCredits)
				bucketUnminedCreditsCursor = b.ReadCursor()
				k, v = bucketUnminedCreditsCursor.First()
			} else {
				k, v = bucketUnminedCreditsCursor.Next()
			}
			if k == nil || v == nil {
				break
			}

			// Make sure this output was not spent by an unmined transaction.
			// If it was, skip this credit.
			if existsRawUnminedInput(ns, k) != nil {
				continue
			}

			// Check the account first.
			pkScript, err := s.fastCreditPkScriptLookup(ns, nil, k)
			if err != nil {
				return nil, err
			}
			thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
			if err != nil {
				return nil, err
			}
			if account != thisAcct {
				continue
			}

			amt, err := fetchRawUnminedCreditAmount(v)
			if err != nil {
				return nil, err
			}

			// Skip ticket outputs, as only SSGen can spend these.
			opcode := fetchRawUnminedCreditTagOpcode(v)
			if opcode == txscript.OP_SSTX {
				continue
			}

			// Skip outputs that are not mature.
			if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
				continue
			}
			if opcode == txscript.OP_SSTXCHANGE {
				continue
			}

			// Determine the txtree for the outpoint by whether or not it's
			// using stake tagged outputs.
			tree := wire.TxTreeRegular
			if opcode != opNonstake {
				tree = wire.TxTreeStake
			}

			var op wire.OutPoint
			err = readCanonicalOutPoint(k, &op)
			if err != nil {
				return nil, err
			}

			op.Tree = tree
			input := wire.NewTxIn(&op, int64(amt), nil)
			var scriptSize int

			// Unspent credits are currently expected to be either P2PKH or
			// P2PK, P2PKH/P2SH nested in a revocation/stakechange/vote output.
			scriptClass := txscript.GetScriptClass(0, pkScript)

			switch scriptClass {
			case txscript.PubKeyHashTy:
				scriptSize = txsizes.RedeemP2PKHSigScriptSize
			case txscript.PubKeyTy:
				scriptSize = txsizes.RedeemP2PKSigScriptSize
			case txscript.StakeRevocationTy, txscript.StakeSubChangeTy,
				txscript.StakeGenTy:
				scriptClass, err = txscript.GetStakeOutSubclass(pkScript)
				if err != nil {
					return nil, fmt.Errorf(
						"failed to extract nested script in stake output: %v",
						err)
				}

				// For stake transactions we expect P2PKH and P2SH script class
				// types only but ignore P2SH script type since it can pay
				// to any script which the wallet may not recognize.
				if scriptClass != txscript.PubKeyHashTy {
					log.Errorf("unexpected nested script class for credit: %v",
						scriptClass)
					continue
				}

				scriptSize = txsizes.RedeemP2PKHSigScriptSize
			default:
				log.Errorf("unexpected script class for credit: %v",
					scriptClass)
				continue
			}

			currentTotal += amt
			currentInputs = append(currentInputs, input)
			currentScripts = append(currentScripts, pkScript)
			redeemScriptSizes = append(redeemScriptSizes, scriptSize)
		}

		inputDetail := &txauthor.InputDetail{
			Amount:            currentTotal,
			Inputs:            currentInputs,
			Scripts:           currentScripts,
			RedeemScriptSizes: redeemScriptSizes,
		}

		return inputDetail, nil
	}

	return InputSource{source: f}
}

// balanceFullScan does a fullscan of the UTXO set to get the current balance.
// It is less efficient than the other balance functions, but works fine for
// accounts.
func (s *Store) balanceFullScan(ns, addrmgrNs walletdb.ReadBucket, minConf int32, syncHeight int32) (map[uint32]*Balances, error) {
	accountBalances := make(map[uint32]*Balances)
	c := ns.NestedReadBucket(bucketUnspent).ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			continue
		}

		cKey := make([]byte, 72)
		copy(cKey[0:32], k[0:32])   // Tx hash
		copy(cKey[32:36], v[0:4])   // Block height
		copy(cKey[36:68], v[4:36])  // Block hash
		copy(cKey[68:72], k[32:36]) // Output index

		cVal := existsRawCredit(ns, cKey)
		if cVal == nil {
			c.Close()
			return nil, errors.E(errors.IO, "missing credit for unspent output")
		}

		// Check the account first.
		pkScript, err := s.fastCreditPkScriptLookup(ns, cKey, nil)
		if err != nil {
			c.Close()
			return nil, err
		}
		thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, cVal, nil, pkScript)
		if err != nil {
			c.Close()
			return nil, err
		}

		utxoAmt, err := fetchRawCreditAmount(cVal)
		if err != nil {
			c.Close()
			return nil, err
		}

		height := extractRawCreditHeight(cKey)
		opcode := fetchRawCreditTagOpCode(cVal)

		ab, ok := accountBalances[thisAcct]
		if !ok {
			ab = &Balances{
				Account: thisAcct,
			}
			accountBalances[thisAcct] = ab
		}

		switch opcode {
		case opNonstake:
			isConfirmed := confirmed(minConf, height, syncHeight)
			creditFromCoinbase := fetchRawCreditIsCoinbase(cVal)
			matureCoinbase := (creditFromCoinbase &&
				coinbaseMatured(s.chainParams, height, syncHeight))

			if (isConfirmed && !creditFromCoinbase) ||
				matureCoinbase {
				ab.Spendable += utxoAmt
			} else if creditFromCoinbase && !matureCoinbase {
				ab.ImmatureCoinbaseRewards += utxoAmt
			}

			ab.Total += utxoAmt
		case txscript.OP_SSTX:
			ab.VotingAuthority += utxoAmt
		case txscript.OP_SSGEN:
			fallthrough
		case txscript.OP_SSRTX:
			if coinbaseMatured(s.chainParams, height, syncHeight) {
				ab.Spendable += utxoAmt
			} else {
				ab.ImmatureStakeGeneration += utxoAmt
			}

			ab.Total += utxoAmt
		case txscript.OP_SSTXCHANGE:
			if ticketChangeMatured(s.chainParams, height, syncHeight) {
				ab.Spendable += utxoAmt
			}

			ab.Total += utxoAmt
		default:
			log.Warnf("Unhandled opcode: %v", opcode)
		}
	}

	c.Close()

	// Unconfirmed transaction output handling.
	c = ns.NestedReadBucket(bucketUnminedCredits).ReadCursor()
	defer c.Close()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		// Make sure this output was not spent by an unmined transaction.
		// If it was, skip this credit.
		if existsRawUnminedInput(ns, k) != nil {
			continue
		}

		// Check the account first.
		pkScript, err := s.fastCreditPkScriptLookup(ns, nil, k)
		if err != nil {
			return nil, err
		}
		thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
		if err != nil {
			return nil, err
		}

		utxoAmt, err := fetchRawUnminedCreditAmount(v)
		if err != nil {
			return nil, err
		}

		ab, ok := accountBalances[thisAcct]
		if !ok {
			ab = &Balances{
				Account: thisAcct,
			}
			accountBalances[thisAcct] = ab
		}
		// Skip ticket outputs, as only SSGen can spend these.
		opcode := fetchRawUnminedCreditTagOpcode(v)

		switch opcode {
		case opNonstake:
			if minConf == 0 {
				ab.Spendable += utxoAmt
			} else if !fetchRawCreditIsCoinbase(v) {
				ab.Unconfirmed += utxoAmt
			}
			ab.Total += utxoAmt
		case txscript.OP_SSTX:
			ab.VotingAuthority += utxoAmt
		case txscript.OP_SSGEN:
			fallthrough
		case txscript.OP_SSRTX:
			ab.ImmatureStakeGeneration += utxoAmt
			ab.Total += utxoAmt
		case txscript.OP_SSTXCHANGE:
			ab.Total += utxoAmt
			continue
		default:
			log.Warnf("Unhandled unconfirmed opcode %v: %v", opcode, v)
		}
	}

	// Account for ticket commitments by iterating over the unspent commitments
	// index.
	it := makeUnspentTicketCommitsIterator(ns)
	for it.next() {
		if it.err != nil {
			return nil, it.err
		}

		if it.unminedSpent {
			// Some unmined tx is redeeming this commitment, so ignore it for
			// balance purposes.
			continue
		}

		ab, ok := accountBalances[it.account]
		if !ok {
			ab = &Balances{
				Account: it.account,
			}
			accountBalances[it.account] = ab
		}

		ab.LockedByTickets += it.amount
		ab.Total += it.amount
	}
	it.close()

	return accountBalances, nil
}

// Balances is an convenience type.
type Balances struct {
	Account                 uint32
	ImmatureCoinbaseRewards dcrutil.Amount
	ImmatureStakeGeneration dcrutil.Amount
	LockedByTickets         dcrutil.Amount
	Spendable               dcrutil.Amount
	Total                   dcrutil.Amount
	VotingAuthority         dcrutil.Amount
	Unconfirmed             dcrutil.Amount
}

// AccountBalance returns a Balances struct for some given account at
// syncHeight block height with all UTXOS that have minConf manyn confirms.
func (s *Store) AccountBalance(ns, addrmgrNs walletdb.ReadBucket, minConf int32, account uint32) (Balances, error) {
	balances, err := s.AccountBalances(ns, addrmgrNs, minConf)
	if err != nil {
		return Balances{}, err
	}

	balance, ok := balances[account]
	if !ok {
		// No balance for the account was found so must be zero.
		return Balances{
			Account: account,
		}, nil
	}

	return *balance, nil
}

// AccountBalances returns a map of all account balances at syncHeight block
// height with all UTXOs that have minConf many confirms.
func (s *Store) AccountBalances(ns, addrmgrNs walletdb.ReadBucket, minConf int32) (map[uint32]*Balances, error) {
	_, syncHeight := s.MainChainTip(ns)
	return s.balanceFullScan(ns, addrmgrNs, minConf, syncHeight)
}

// InsertTxScript inserts a transaction script into the database.
func (s *Store) InsertTxScript(ns walletdb.ReadWriteBucket, script []byte) error {
	return putTxScript(ns, script)
}

// GetTxScript fetches a transaction script from the database using
// the RIPEMD160 hash as a key.
func (s *Store) GetTxScript(ns walletdb.ReadBucket, hash []byte) ([]byte, error) {
	v := existsTxScript(ns, hash)
	if v == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no script for hash %x", hash))
	}
	return v, nil
}

// StoredTxScripts returns a slice of byte slices containing all the transaction
// scripts currently stored in wallet.
func (s *Store) StoredTxScripts(ns walletdb.ReadBucket) [][]byte {
	var scripts [][]byte
	c := ns.NestedReadBucket(bucketScripts).ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		s := make([]byte, len(v))
		copy(s, v)
		scripts = append(scripts, s)
	}
	c.Close()
	return scripts
}

// TotalInput calculates the input value referenced by all transaction inputs.
// If this is not calculable, this returns 0.
func (s *Store) TotalInput(dbtx walletdb.ReadTx, tx *wire.MsgTx) (dcrutil.Amount, error) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)

	var total dcrutil.Amount
	for _, in := range tx.TxIn {
		var tx wire.MsgTx
		if v := existsRawUnmined(ns, in.PreviousOutPoint.Hash[:]); v != nil {
			err := tx.Deserialize(bytes.NewReader(extractRawUnminedTx(v)))
			if err != nil {
				return 0, errors.E(errors.IO, err)
			}
		} else if _, v := latestTxRecord(ns, in.PreviousOutPoint.Hash[:]); v != nil {
			err := readRawTxRecordMsgTx(&in.PreviousOutPoint.Hash, v, &tx)
			if err != nil {
				return 0, err
			}
		} else {
			return 0, nil
		}

		if in.PreviousOutPoint.Index >= uint32(len(tx.TxOut)) {
			idx := in.PreviousOutPoint.Index
			hash := &in.PreviousOutPoint.Hash
			return 0, errors.E(errors.Invalid, errors.Errorf("previous output index %d does not exist in transaction %v", idx, hash))
		}

		total += dcrutil.Amount(tx.TxOut[in.PreviousOutPoint.Index].Value)
	}

	return total, nil
}
