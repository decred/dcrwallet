// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
	"golang.org/x/crypto/ripemd160"
)

func storeError(code apperrors.Code, str string, err error) error {
	return apperrors.E{ErrorCode: code, Description: str, Err: err}
}

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
		str := "failed to deserialize transaction"
		return nil, storeError(apperrors.ErrInput, str, err)
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
		str := "failed to serialize transaction"
		return nil, storeError(apperrors.ErrInput, str, err)
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

// ExtendMainChain inserts a block header into the database.  It must connect to
// the existing tip block.
//
// If the block is already inserted and part of the main chain, an error with
// code ErrDuplicate is returned.
func (s *Store) ExtendMainChain(ns walletdb.ReadWriteBucket, header *BlockHeaderData) error {
	height := header.SerializedHeader.Height()
	if height < 1 {
		const str = "can not extend main chain with genesis block 0"
		return storeError(apperrors.ErrInput, str, nil)
	}

	headerBucket := ns.NestedReadWriteBucket(bucketHeaders)

	headerParent := extractBlockHeaderParentHash(header.SerializedHeader[:])
	currentTipHash := ns.Get(rootTipBlock)
	if !bytes.Equal(headerParent, currentTipHash) {
		// Return a special error if it is a duplicate of an existing block in
		// the main chain (NOT the headers bucket, since headers are never
		// pruned).
		_, v := existsBlockRecord(ns, height)
		if v != nil && bytes.Equal(extractRawBlockRecordHash(v), header.BlockHash[:]) {
			const str = "block already recorded in the main chain"
			return storeError(apperrors.ErrDuplicate, str, nil)
		}
		const str = "header is not a direct child of the current tip block"
		return storeError(apperrors.ErrInput, str, nil)
	}
	// Also check that the height is one more than the current height
	// recorded by the current tip.
	currentTipHeader := headerBucket.Get(currentTipHash)
	currentTipHeight := extractBlockHeaderHeight(currentTipHeader)
	if currentTipHeight+1 != height {
		const str = "header height is not one more than the current tip height"
		return storeError(apperrors.ErrInput, str, nil)
	}

	vb := extractBlockHeaderVoteBits(header.SerializedHeader[:])
	var err error
	if approvesParent(vb) {
		err = stakeValidate(ns, currentTipHeight)
	} else {
		err = stakeInvalidate(ns, currentTipHeight)
	}
	if err != nil {
		return err
	}

	// Add the header
	err = headerBucket.Put(header.BlockHash[:], header.SerializedHeader[:])
	if err != nil {
		const str = "failed to write header"
		return storeError(apperrors.ErrDatabase, str, err)
	}

	// Update the tip block
	err = ns.Put(rootTipBlock, header.BlockHash[:])
	if err != nil {
		const str = "failed to write new tip block hash"
		return storeError(apperrors.ErrDatabase, str, err)
	}

	// Add an empty block record
	blockKey := keyBlockRecord(height)
	blockVal := valueBlockRecordEmptyFromHeader(&header.BlockHash, &header.SerializedHeader)
	return putRawBlockRecord(ns, blockKey, blockVal)
}

// log2 calculates an integer approximation of log2(x).  This is used to
// approximate the cap to use when allocating memory for the block locators.
func log2(x int) int {
	res := 0
	for x != 0 {
		x /= 2
		res++
	}
	return res
}

// BlockLocators returns, in reversed order (newest blocks first), hashes of
// blocks believed to be on the main chain.  For memory and lookup efficiency,
// many older hashes are skipped, with increasing gaps between included hashes.
// This returns the block locators that should be included in a getheaders wire
// message or RPC request.
func (s *Store) BlockLocators(ns walletdb.ReadBucket) []chainhash.Hash {
	headerBucket := ns.NestedReadBucket(bucketHeaders)
	hash := ns.Get(rootTipBlock)
	height := extractBlockHeaderHeight(headerBucket.Get(hash))

	locators := make([]chainhash.Hash, 1, 10+log2(int(height)))
	copy(locators[0][:], hash)
	var skip, skips int32 = 0, 1
	for height >= 0 {
		if skip != 0 {
			height -= skip
			skip = 0
			continue
		}

		_, blockVal := existsBlockRecord(ns, height)
		hash = extractRawBlockRecordHash(blockVal)

		var locator chainhash.Hash
		copy(locator[:], hash)
		locators = append(locators, locator)

		if len(locators) >= 10 {
			skips *= 2
			skip = skips
		}
	}
	return locators
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
		str := fmt.Sprintf("no block record for height %v", height)
		return storeError(apperrors.ErrValueNoExists, str, nil)
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
			str := fmt.Sprintf("missing transaction record for tx %v block %v",
				txHash, &blockRec.Block.Hash)
			return storeError(apperrors.ErrData, str, err)
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
				const str = "failed to remove stake invalidated credit"
				return storeError(apperrors.ErrDatabase, str, err)
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
				const str = "failed to remove stake invalidated debit"
				return storeError(apperrors.ErrDatabase, str, err)
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
		str := fmt.Sprintf("no block record for height %v", height)
		return storeError(apperrors.ErrValueNoExists, str, nil)
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
			str := fmt.Sprintf("missing transaction record for tx %v block %v",
				txHash, &blockRec.Block.Hash)
			return storeError(apperrors.ErrData, str, err)
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
				const str = "failed to write stake invalidated credit"
				return storeError(apperrors.ErrDatabase, str, err)
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
				const str = "failed to write stake invalidated debit"
				return storeError(apperrors.ErrDatabase, str, err)
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

// InsertMainChainHeaders permanently saves block headers.  Headers should be in
// order of increasing heights and there should not be any gaps between blocks.
// After inserting headers, if the existing recorded tip block is behind the
// last main chain block header that was inserted, a chain switch occurs and the
// new tip block is recorded.
func (s *Store) InsertMainChainHeaders(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket,
	headers []BlockHeaderData) error {

	if len(headers) == 0 {
		return nil
	}

	// If the first header is not yet saved, make sure the parent is.
	if existsBlockHeader(ns, keyBlockHeader(&headers[0].BlockHash)) == nil {
		parentHash := extractBlockHeaderParentHash(headers[0].SerializedHeader[:])
		if existsBlockHeader(ns, parentHash) == nil {
			const str = "missing parent of first header"
			return storeError(apperrors.ErrValueNoExists, str, nil)
		}
	}

	candidateTip := &headers[len(headers)-1]
	candidateTipHeight := extractBlockHeaderHeight(candidateTip.SerializedHeader[:])
	currentTipHash := ns.Get(rootTipBlock)
	currentTipHeader := ns.NestedReadBucket(bucketHeaders).Get(currentTipHash)
	currentTipHeight := extractBlockHeaderHeight(currentTipHeader)
	if candidateTipHeight >= currentTipHeight {
		err := ns.Put(rootTipBlock, candidateTip.BlockHash[:])
		if err != nil {
			const str = "failed to write new tip block"
			return storeError(apperrors.ErrDatabase, str, err)
		}
	}

	for i := range headers {
		hash := &headers[i].BlockHash
		header := &headers[i].SerializedHeader

		// Any blocks known to not exist on this main chain are to be removed.
		height := extractBlockHeaderHeight(header[:])
		rk, rv := existsBlockRecord(ns, height)
		if rv != nil {
			recHash := extractRawBlockRecordHash(rv)
			if !bytes.Equal(hash[:], recHash) {
				err := s.rollback(ns, addrmgrNs, height)
				if err != nil {
					return err
				}
				blockVal := valueBlockRecordEmptyFromHeader(hash, header)
				err = putRawBlockRecord(ns, rk, blockVal)
				if err != nil {
					return err
				}
			}
		} else {
			blockVal := valueBlockRecordEmptyFromHeader(hash, header)
			err := putRawBlockRecord(ns, rk, blockVal)
			if err != nil {
				return err
			}
		}

		// Insert the header.
		k := keyBlockHeader(hash)
		v := header[:]
		err := putRawBlockHeader(ns, k, v)
		if err != nil {
			return err
		}

		// Handle stake validation of the parent block.
		parentHeight := header.Height() - 1
		vb := extractBlockHeaderVoteBits(header[:])
		if approvesParent(vb) {
			err = stakeValidate(ns, parentHeight)
		} else {
			err = stakeInvalidate(ns, parentHeight)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// GetMainChainBlockHashForHeight returns the block hash of the block on the
// main chain at a given height.
func (s *Store) GetMainChainBlockHashForHeight(ns walletdb.ReadBucket, height int32) (chainhash.Hash, error) {
	_, v := existsBlockRecord(ns, height)
	if v == nil {
		const str = "No main chain block for height"
		return chainhash.Hash{}, storeError(apperrors.ErrValueNoExists, str, nil)
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
		const str = "block header not found"
		return BlockMeta{}, storeError(apperrors.ErrValueNoExists, str, nil)
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
		const str = "starting block not found"
		return nil, storeError(apperrors.ErrValueNoExists, str, nil)
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
		copy(storage[storageUsed][:], extractRawBlockRecordHash(v))
		height++
		storageUsed++
	}
	return storage[:storageUsed], nil
}

// fetchAccountForPkScript fetches an account for a given pkScript given a
// credit value, the script, and an account lookup function. It does this
// to maintain compatibility with older versions of the database.
func (s *Store) fetchAccountForPkScript(addrmgrNs walletdb.ReadBucket,
	credVal []byte, unminedCredVal []byte, pkScript []byte) (uint32, error) {

	// Attempt to get the account from the mined credit. If the
	// account was never stored, we can ignore the error and
	// fall through to do the lookup with the acctLookupFunc.
	if credVal != nil {
		acct, err := fetchRawCreditAccount(credVal)
		if err == nil {
			return acct, nil
		}
		storeErr, ok := err.(apperrors.E)
		if !ok {
			return 0, err
		}
		switch storeErr.ErrorCode {
		case apperrors.ErrValueNoExists:
		case apperrors.ErrData:
		default:
			return 0, err
		}
	}
	if unminedCredVal != nil {
		acct, err := fetchRawUnminedCreditAccount(unminedCredVal)
		if err == nil {
			return acct, nil
		}
		storeErr, ok := err.(apperrors.E)
		if !ok {
			return 0, err
		}
		switch storeErr.ErrorCode {
		case apperrors.ErrValueNoExists:
		case apperrors.ErrData:
		default:
			return 0, err
		}
	}

	// Neither credVal or unminedCredVal were passed, or if they
	// were, they didn't have the account set. Figure out the
	// account from the pkScript the expensive way.
	_, addrs, _, err :=
		txscript.ExtractPkScriptAddrs(txscript.DefaultScriptVersion,
			pkScript, s.chainParams)
	if err != nil {
		return 0, err
	}

	// Only look at the first address returned. This does not
	// handle multisignature or other custom pkScripts in the
	// correct way, which requires multiple account tracking.
	acct, err := s.acctLookupFunc(addrmgrNs, addrs[0])
	if err != nil {
		return 0, err
	}

	return acct, nil
}

// moveMinedTx moves a transaction record from the unmined buckets to block
// buckets.
func (s *Store) moveMinedTx(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket, rec *TxRecord, recKey,
	recVal []byte, block *BlockMeta) error {
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
			return fmt.Errorf("missing credit value")
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
func (s *Store) InsertMinedTx(ns walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket, rec *TxRecord,
	blockHash *chainhash.Hash) error {

	blockHeader := existsBlockHeader(ns, blockHash[:])
	if blockHeader == nil {
		const str = "block header has not yet been recorded"
		return storeError(apperrors.ErrInput, str, nil)
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
				const str = "failed to put invalidated debit"
				return storeError(apperrors.ErrDatabase, str, err)
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
		str := "transaction output does not exist"
		return storeError(apperrors.ErrInput, str, nil)
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
		err := ns.NestedReadWriteBucket(bucketStakeInvalidatedCredits).
			Put(k, v)
		if err != nil {
			const str = "failed to write invalidated credit"
			return storeError(apperrors.ErrDatabase, str, err)
		}
		return nil
	}

	_, err := s.addCredit(ns, rec, block, index, change, account)
	return err
}

// getP2PKHOpCode returns opNonstake for non-stake transactions, or
// the stake op code tag for stake transactions.
func getP2PKHOpCode(pkScript []byte) uint8 {
	class := txscript.GetScriptClass(txscript.DefaultScriptVersion, pkScript)
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
	class := txscript.GetScriptClass(txscript.DefaultScriptVersion, pkScript)
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

// AddMultisigOut adds a P2SH multisignature spendable output into the
// transaction manager. In the event that the output already existed but
// was not mined, the output is updated so its value reflects the block
// it was included in.
//
func (s *Store) AddMultisigOut(ns walletdb.ReadWriteBucket, rec *TxRecord, block *BlockMeta,
	index uint32) error {

	if int(index) >= len(rec.MsgTx.TxOut) {
		str := "transaction output does not exist"
		return storeError(apperrors.ErrInput, str, nil)
	}

	return s.addMultisigOut(ns, rec, block, index)
}

func (s *Store) addMultisigOut(ns walletdb.ReadWriteBucket, rec *TxRecord,
	block *BlockMeta, index uint32) error {
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
		str := "tried to update a mined multisig out's mined information"
		return storeError(apperrors.ErrDatabase, str, nil)
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
			str := "unknown stake output subclass encountered"
			return storeError(apperrors.ErrInput, str, nil)
		}
		tree = wire.TxTreeStake
	}
	if class != txscript.ScriptHashTy {
		str := "transaction output is wrong type (not p2sh)"
		return storeError(apperrors.ErrInput, str, nil)
	}
	scriptHash, err :=
		txscript.GetScriptHashFromP2SHScript(p2shScript)
	if err != nil {
		return err
	}
	multisigScript := existsTxScript(ns, scriptHash)
	if multisigScript == nil {
		str := "failed to insert multisig out: transaction multisig " +
			"script does not exist in script bucket"
		return storeError(apperrors.ErrValueNoExists, str, nil)
	}
	m, n, err := txscript.GetMultisigMandN(multisigScript)
	if err != nil {
		return storeError(apperrors.ErrInput, err.Error(), nil)
	}
	var p2shScriptHash [ripemd160.Size]byte
	copy(p2shScriptHash[:], scriptHash)
	val = valueMultisigOut(p2shScriptHash,
		m,
		n,
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
		return storeError(apperrors.ErrDatabase, err.Error(), nil)
	}
	return putMultisigOutUS(ns, key)
}

// SpendMultisigOut spends a multisignature output by making it spent in
// the general bucket and removing it from the unspent bucket.
func (s *Store) SpendMultisigOut(ns walletdb.ReadWriteBucket, op *wire.OutPoint,
	spendHash chainhash.Hash, spendIndex uint32) error {

	return s.spendMultisigOut(ns, op, spendHash, spendIndex)
}

func (s *Store) spendMultisigOut(ns walletdb.ReadWriteBucket, op *wire.OutPoint,
	spendHash chainhash.Hash, spendIndex uint32) error {
	// Mark the output spent.
	key := keyMultisigOut(op.Hash, op.Index)
	val := existsMultisigOutCopy(ns, key)
	if val == nil {
		str := "tried to spend multisig output that doesn't exist"
		return storeError(apperrors.ErrValueNoExists, str, nil)
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
		str := fmt.Sprintf("transaction %v tried to doublespend multisig "+
			"output %v already spent by %v", &spendHash, op, &foundSpendHash)
		return storeError(apperrors.ErrDoubleSpend, str, nil)
	}
	setMultisigOutSpent(val, spendHash, spendIndex)

	// Check to see that it's in the unspent bucket.
	existsUnspent := existsMultisigOutUS(ns, key)
	if !existsUnspent {
		str := "unspent multisig outpoint is missing from the unspent bucket"
		return storeError(apperrors.ErrInput, str, nil)
	}

	// Write the updated output, and delete the unspent key.
	err := putMultisigOutRawValues(ns, key, val)
	if err != nil {
		return storeError(apperrors.ErrDatabase, err.Error(), nil)
	}
	err = deleteMultisigOutUS(ns, key)
	if err != nil {
		return storeError(apperrors.ErrDatabase, err.Error(), nil)
	}

	return nil
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
		const str = "cannot remove the genesis block"
		return storeError(apperrors.ErrInput, str, nil)
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

			txType := stake.DetermineTxType(&rec.MsgTx)

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
					return fmt.Errorf("missing credit value")
				}
				creditOpCode := fetchRawCreditTagOpCode(credVal)

				// unspendRawCredit does not error in case the no credit exists
				// for this key, but this behavior is correct.  Since
				// transactions are removed in an unspecified order
				// (transactions in the block record are not sorted by
				// appearence in the block), this credit may have already been
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

				amt, change, err := fetchRawCreditAmountChange(v)
				if err != nil {
					return err
				}
				opCode := fetchRawCreditTagOpCode(v)
				isCoinbase := fetchRawCreditIsCoinbase(v)
				hasExpiry := fetchRawCreditHasExpiry(v)

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
			err = s.removeUnconfirmed(ns, &unminedRec.MsgTx, &unminedRec.Hash)
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
		const str = "cannot update tip block"
		return storeError(apperrors.ErrDatabase, str, err)
	}

	return nil
}

// UnspentOutputs returns all unspent received transaction outputs.
// The order is undefined.
func (s *Store) UnspentOutputs(ns walletdb.ReadBucket) ([]*Credit, error) {
	return s.unspentOutputs(ns)
}

// outputCreditInfo fetches information about a credit from the database,
// fills out a credit struct, and returns it.
func (s *Store) outputCreditInfo(ns walletdb.ReadBucket, op wire.OutPoint,
	block *Block) (*Credit, error) {
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
		errStr := fmt.Errorf("missing utxo %x, %v", op.Hash, op.Index)
		return nil, storeError(apperrors.ErrValueNoExists, "couldn't find relevant credit "+
			"for unspent output", errStr)
	}

	// Throw an inconsistency error if we find one.
	if minedCredV != nil && unminedCredV != nil {
		errStr := fmt.Errorf("duplicated utxo %x, %v", op.Hash, op.Index)
		return nil, storeError(apperrors.ErrDatabase, "credit exists in mined and unmined "+
			"utxo set in duplicate", errStr)
	}

	var err error
	var amt dcrutil.Amount
	var opCode uint8
	var isCoinbase bool
	var hasExpiry bool
	var scrLoc, scrLen uint32

	mined := false
	if unminedCredV != nil {
		amt, err = fetchRawUnminedCreditAmount(unminedCredV)
		if err != nil {
			return nil, err
		}

		opCode = fetchRawUnminedCreditTagOpcode(unminedCredV)
		isCoinbase = fetchRawCreditIsCoinbase(unminedCredV)
		hasExpiry = fetchRawCreditHasExpiry(unminedCredV)

		// These errors are skipped because they may throw incorrectly
		// on values recorded in older versions of the wallet. 0-offset
		// script locs will cause raw extraction from the deserialized
		// transactions. See extractRawTxRecordPkScript.
		scrLoc = fetchRawUnminedCreditScriptOffset(unminedCredV)
		scrLen = fetchRawUnminedCreditScriptLength(unminedCredV)
	}
	if minedCredV != nil {
		mined = true
		amt, err = fetchRawCreditAmount(minedCredV)
		if err != nil {
			return nil, err
		}

		opCode = fetchRawCreditTagOpCode(minedCredV)
		isCoinbase = fetchRawCreditIsCoinbase(minedCredV)
		hasExpiry = fetchRawCreditHasExpiry(minedCredV)

		// Same error caveat as above.
		scrLoc = fetchRawCreditScriptOffset(minedCredV)
		scrLen = fetchRawCreditScriptLength(minedCredV)
	}

	var recK, recV, pkScript []byte
	if !mined {
		recK = op.Hash[:]
		recV = existsRawUnmined(ns, recK)
		pkScript, err = fetchRawTxRecordPkScript(recK, recV, op.Index,
			scrLoc, scrLen)
		if err != nil {
			return nil, err
		}
	} else {
		recK, recV = existsTxRecord(ns, &op.Hash, block)
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

	var blockTime time.Time
	if mined {
		blockTime, err = fetchBlockTime(ns, block.Height)
		if err != nil {
			return nil, err
		}
	}

	var c *Credit
	if !mined {
		c = &Credit{
			OutPoint: op,
			BlockMeta: BlockMeta{
				Block: Block{Height: -1},
			},
			Amount:       amt,
			PkScript:     pkScript,
			Received:     fetchRawTxRecordReceived(recV),
			FromCoinBase: isCoinbase,
			HasExpiry:    hasExpiry,
		}
	} else {
		c = &Credit{
			OutPoint: op,
			BlockMeta: BlockMeta{
				Block: *block,
				Time:  blockTime,
			},
			Amount:       amt,
			PkScript:     pkScript,
			Received:     fetchRawTxRecordReceived(recV),
			FromCoinBase: isCoinbase,
			HasExpiry:    hasExpiry,
		}
	}

	return c, nil
}

func (s *Store) unspentOutputs(ns walletdb.ReadBucket) ([]*Credit, error) {
	var unspent []*Credit
	numUtxos := 0

	var op wire.OutPoint
	var block Block
	err := ns.NestedReadBucket(bucketUnspent).ForEach(func(k, v []byte) error {
		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			return err
		}
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip this k/v pair.
			return nil
		}

		err = readUnspentBlock(v, &block)
		if err != nil {
			return err
		}

		cred, err := s.outputCreditInfo(ns, op, &block)
		if err != nil {
			return err
		}

		unspent = append(unspent, cred)
		numUtxos++

		return nil
	})
	if err != nil {
		if _, ok := err.(apperrors.E); ok {
			return nil, err
		}
		str := "failed iterating unspent bucket"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}

	err = ns.NestedReadBucket(bucketUnminedCredits).ForEach(func(k, v []byte) error {
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			return nil
		}

		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			return err
		}

		cred, err := s.outputCreditInfo(ns, op, nil)
		if err != nil {
			return err
		}

		unspent = append(unspent, cred)
		numUtxos++

		return nil
	})
	if err != nil {
		if _, ok := err.(apperrors.E); ok {
			return nil, err
		}
		str := "failed iterating unmined credits bucket"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}

	log.Tracef("%v many utxos found in database", numUtxos)

	return unspent, nil
}

// UnspentOutpoints returns all unspent received transaction outpoints.
// The order is undefined.
func (s *Store) UnspentOutpoints(ns walletdb.ReadBucket) ([]wire.OutPoint, error) {
	var unspent []wire.OutPoint
	numUtxos := 0

	err := ns.NestedReadBucket(bucketUnspent).ForEach(func(k, v []byte) error {
		var op wire.OutPoint
		err := readCanonicalOutPoint(k, &op)
		if err != nil {
			return err
		}
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip this k/v pair.
			return nil
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

		unspent = append(unspent, op)
		return nil
	})
	if err != nil {
		if _, ok := err.(apperrors.E); ok {
			return nil, err
		}
		str := "failed iterating unspent bucket"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}

	var unspentZC []wire.OutPoint
	err = ns.NestedReadBucket(bucketUnminedCredits).ForEach(func(k, v []byte) error {
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			return nil
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

		unspentZC = append(unspentZC, op)
		numUtxos++

		return nil
	})
	if err != nil {
		if _, ok := err.(apperrors.E); ok {
			return nil, err
		}
		str := "failed iterating unmined credits bucket"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}

	log.Tracef("%v many utxo outpoints found", numUtxos)

	return append(unspent, unspentZC...), nil
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
				const str = "unmined tx decode failed"
				return nil, apperrors.Wrap(err, apperrors.ErrData, str)
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
			if !confirmed(int32(s.chainParams.TicketMaturity)+1, height, syncHeight) {
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

// MultisigCredit is a redeemable P2SH multisignature credit.
type MultisigCredit struct {
	OutPoint   *wire.OutPoint
	ScriptHash [ripemd160.Size]byte
	MSScript   []byte
	M          uint8
	N          uint8
	Amount     dcrutil.Amount
}

// GetMultisigCredit takes an outpoint and returns multisignature
// credit data stored about it.
func (s *Store) GetMultisigCredit(ns walletdb.ReadBucket, op *wire.OutPoint) (*MultisigCredit, error) {
	return s.getMultisigCredit(ns, op)
}

func (s *Store) getMultisigCredit(ns walletdb.ReadBucket,
	op *wire.OutPoint) (*MultisigCredit, error) {
	if op == nil {
		str := fmt.Sprintf("missing input outpoint")
		return nil, storeError(apperrors.ErrInput, str, nil)
	}

	val := existsMultisigOutCopy(ns, canonicalOutPoint(&op.Hash, op.Index))
	if val == nil {
		str := fmt.Sprintf("missing multisignature output for outpoint "+
			"hash %v, index %v (while getting ms credit)", op.Hash, op.Index)
		return nil, storeError(apperrors.ErrValueNoExists, str, nil)
	}

	// Make sure it hasn't already been spent.
	spent, by, byIndex := fetchMultisigOutSpentVerbose(val)
	if spent {
		str := fmt.Sprintf("multisignature output %v index %v has already "+
			"been spent by transaction %v (input %v)", op.Hash, op.Index,
			by, byIndex)
		return nil, storeError(apperrors.ErrInput, str, nil)
	}

	// Script is contained in val above too, but I check this
	// to make sure the db has consistency.
	scriptHash := fetchMultisigOutScrHash(val)
	multisigScript := existsTxScript(ns, scriptHash[:])
	if multisigScript == nil {
		str := "couldn't get multisig credit: transaction multisig " +
			"script does not exist in script bucket"
		return nil, storeError(apperrors.ErrValueNoExists, str, nil)
	}
	m, n := fetchMultisigOutMN(val)
	amount := fetchMultisigOutAmount(val)
	op.Tree = fetchMultisigOutTree(val)

	msc := &MultisigCredit{
		op,
		scriptHash,
		multisigScript,
		m,
		n,
		amount,
	}

	return msc, nil
}

// GetMultisigOutput takes an outpoint and returns multisignature
// credit data stored about it.
func (s *Store) GetMultisigOutput(ns walletdb.ReadBucket, op *wire.OutPoint) (*MultisigOut, error) {
	return s.getMultisigOutput(ns, op)
}

func (s *Store) getMultisigOutput(ns walletdb.ReadBucket, op *wire.OutPoint) (*MultisigOut, error) {
	if op == nil {
		str := fmt.Sprintf("missing input outpoint")
		return nil, storeError(apperrors.ErrInput, str, nil)
	}

	key := canonicalOutPoint(&op.Hash, op.Index)
	val := existsMultisigOutCopy(ns, key)
	if val == nil {
		str := fmt.Sprintf("missing multisignature output for outpoint "+
			"hash %v, index %v", op.Hash, op.Index)
		return nil, storeError(apperrors.ErrValueNoExists, str, nil)
	}

	mso, err := fetchMultisigOut(key, val)
	if err != nil {
		str := fmt.Sprintf("failed to deserialized multisignature output "+
			"for outpoint hash %v, index %v", op.Hash, op.Index)
		return nil, storeError(apperrors.ErrValueNoExists, str, nil)
	}

	return mso, nil
}

// UnspentMultisigCredits returns all unspent multisignature P2SH credits in
// the wallet.
func (s *Store) UnspentMultisigCredits(ns walletdb.ReadBucket) ([]*MultisigCredit, error) {
	return s.unspentMultisigCredits(ns)
}

func (s *Store) unspentMultisigCredits(ns walletdb.ReadBucket) ([]*MultisigCredit,
	error) {
	var unspentKeys [][]byte

	err := ns.NestedReadBucket(bucketMultisigUsp).ForEach(func(k, v []byte) error {
		unspentKeys = append(unspentKeys, k)
		return nil
	})

	var mscs []*MultisigCredit
	for _, key := range unspentKeys {
		val := existsMultisigOutCopy(ns, key)
		if val == nil {
			str := "failed to get unspent multisig credits: " +
				"does not exist in bucket"
			return nil, storeError(apperrors.ErrValueNoExists, str, nil)
		}
		var op wire.OutPoint
		errRead := readCanonicalOutPoint(key, &op)
		if errRead != nil {
			return nil, storeError(apperrors.ErrInput, errRead.Error(), err)
		}

		scriptHash := fetchMultisigOutScrHash(val)
		multisigScript := existsTxScript(ns, scriptHash[:])
		if multisigScript == nil {
			str := "failed to get unspent multisig credits: " +
				"transaction multisig script does not exist " +
				"in script bucket"
			return nil, storeError(apperrors.ErrValueNoExists, str, nil)
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

// UnspentMultisigCreditsForAddress returns all unspent multisignature P2SH
// credits in the wallet for some specified address.
func (s *Store) UnspentMultisigCreditsForAddress(ns walletdb.ReadBucket,
	addr dcrutil.Address) ([]*MultisigCredit, error) {

	return s.unspentMultisigCreditsForAddress(ns, addr)
}

func (s *Store) unspentMultisigCreditsForAddress(ns walletdb.ReadBucket,
	addr dcrutil.Address) ([]*MultisigCredit, error) {
	// Make sure the address is P2SH, then get the
	// Hash160 for the script from the address.
	var addrScrHash []byte
	if sha, ok := addr.(*dcrutil.AddressScriptHash); ok {
		addrScrHash = sha.ScriptAddress()
	} else {
		str := "address passed was not a P2SH address"
		return nil, storeError(apperrors.ErrInput, str, nil)
	}

	var unspentKeys [][]byte
	err := ns.NestedReadBucket(bucketMultisigUsp).ForEach(func(k, v []byte) error {
		unspentKeys = append(unspentKeys, k)
		return nil
	})

	var mscs []*MultisigCredit
	for _, key := range unspentKeys {
		val := existsMultisigOutCopy(ns, key)
		if val == nil {
			str := "failed to get unspent multisig credits: " +
				"does not exist in bucket"
			return nil, storeError(apperrors.ErrValueNoExists, str, nil)
		}

		// Skip everything that's unrelated to the address
		// we're concerned about.
		scriptHash := fetchMultisigOutScrHash(val)
		if !bytes.Equal(scriptHash[:], addrScrHash) {
			continue
		}

		var op wire.OutPoint
		errRead := readCanonicalOutPoint(key, &op)
		if errRead != nil {
			return nil, storeError(apperrors.ErrInput, errRead.Error(), err)
		}

		multisigScript := existsTxScript(ns, scriptHash[:])
		if multisigScript == nil {
			str := "failed to get unspent multisig credits: " +
				"transaction multisig script does not exist " +
				"in script bucket"
			return nil, storeError(apperrors.ErrValueNoExists, str, nil)
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

// UnspentOutputsForAmount returns all non-stake outputs that sum up to the
// amount passed. If not enough funds are found, a nil pointer is returned
// without error.
func (s *Store) UnspentOutputsForAmount(ns, addrmgrNs walletdb.ReadBucket,
	amt dcrutil.Amount, height int32, minConf int32, all bool,
	account uint32) ([]*Credit, error) {

	return s.unspentOutputsForAmount(ns, addrmgrNs, amt, height, minConf, all, account)
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

// outputCreditInfo fetches information about a credit from the database,
// fills out a credit struct, and returns it.
func (s *Store) fastCreditPkScriptLookup(ns walletdb.ReadBucket, credKey []byte,
	unminedCredKey []byte) ([]byte, error) {
	// It has to exists as a credit or an unmined credit.
	// Look both of these up. If it doesn't, throw an
	// error. Check unmined first, then mined.
	var minedCredV []byte
	unminedCredV := existsRawUnminedCredit(ns, unminedCredKey)
	if unminedCredV == nil {
		minedCredV = existsRawCredit(ns, credKey)
	}
	if minedCredV == nil && unminedCredV == nil {
		errStr := fmt.Errorf("missing utxo during pkscript lookup")
		return nil, storeError(apperrors.ErrValueNoExists, "couldn't find relevant credit "+
			"for unspent output during pkscript look up", errStr)
	}

	var scrLoc, scrLen uint32

	mined := false
	if unminedCredV != nil {
		// These errors are skipped because they may throw incorrectly
		// on values recorded in older versions of the wallet. 0-offset
		// script locs will cause raw extraction from the deserialized
		// transactions. See extractRawTxRecordPkScript.
		scrLoc = fetchRawUnminedCreditScriptOffset(unminedCredV)
		scrLen = fetchRawUnminedCreditScriptLength(unminedCredV)
	}
	if minedCredV != nil {
		mined = true

		// Same error caveat as above.
		scrLoc = fetchRawCreditScriptOffset(minedCredV)
		scrLen = fetchRawCreditScriptLength(minedCredV)
	}

	var recK, recV, pkScript []byte
	var err error
	if !mined {
		var op wire.OutPoint
		err := readCanonicalOutPoint(unminedCredKey, &op)
		if err != nil {
			return nil, err
		}

		recK = op.Hash[:]
		recV = existsRawUnmined(ns, recK)
		pkScript, err = fetchRawTxRecordPkScript(recK, recV, op.Index,
			scrLoc, scrLen)
		if err != nil {
			return nil, err
		}
	} else {
		recK := extractRawCreditTxRecordKey(credKey)
		recV = existsRawTxRecord(ns, recK)
		idx := extractRawCreditIndex(credKey)

		pkScript, err = fetchRawTxRecordPkScript(recK, recV, idx,
			scrLoc, scrLen)
		if err != nil {
			return nil, err
		}
	}

	return pkScript, nil
}

// minimalCreditToCredit looks up a minimal credit's data and prepares a Credit
// from this data.
func (s *Store) minimalCreditToCredit(ns walletdb.ReadBucket,
	mc *minimalCredit) (*Credit, error) {
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

// errForEachBreakout is used to break out of a a wallet db ForEach loop.
var errForEachBreakout = errors.New("forEachBreakout")

func (s *Store) unspentOutputsForAmount(ns, addrmgrNs walletdb.ReadBucket, needed dcrutil.Amount,
	syncHeight int32, minConf int32, all bool, account uint32) ([]*Credit, error) {
	var eligible []*minimalCredit
	var toUse []*minimalCredit
	var unspent []*Credit
	found := dcrutil.Amount(0)

	err := ns.NestedReadBucket(bucketUnspent).ForEach(func(k, v []byte) error {
		if found >= needed {
			return errForEachBreakout
		}

		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			return nil
		}

		cKey := make([]byte, 72)
		copy(cKey[0:32], k[0:32])   // Tx hash
		copy(cKey[32:36], v[0:4])   // Block height
		copy(cKey[36:68], v[4:36])  // Block hash
		copy(cKey[68:72], k[32:36]) // Output index

		cVal := existsRawCredit(ns, cKey)
		if cVal == nil {
			return nil
		}

		if !all {
			// Check the account first.
			pkScript, err := s.fastCreditPkScriptLookup(ns, cKey, nil)
			if err != nil {
				return err
			}
			thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, cVal, nil, pkScript)
			if err != nil {
				return err
			}
			if account != thisAcct {
				return nil
			}
		}

		amt, spent, err := fetchRawCreditAmountSpent(cVal)
		if err != nil {
			return err
		}

		// This should never happen since this is already in bucket
		// unspent, but let's be careful anyway.
		if spent {
			return nil
		}
		// Skip ticket outputs, as only SSGen can spend these.
		opcode := fetchRawCreditTagOpCode(cVal)
		if opcode == txscript.OP_SSTX {
			return nil
		}

		// Only include this output if it meets the required number of
		// confirmations.  Coinbase transactions must have have reached
		// maturity before their outputs may be spent.
		txHeight := extractRawCreditHeight(cKey)
		if !confirmed(minConf, txHeight, syncHeight) {
			return nil
		}

		// Skip outputs that are not mature.
		if fetchRawCreditIsCoinbase(cVal) {
			if !confirmed(int32(s.chainParams.CoinbaseMaturity), txHeight,
				syncHeight) {
				return nil
			}
		}
		// Skip outputs that have an expiry but have not yet reached
		// coinbase maturity .
		if fetchRawCreditHasExpiry(cVal) {
			if !confirmed(int32(s.chainParams.CoinbaseMaturity), txHeight,
				syncHeight) {
				return nil
			}
		}
		if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
			if !confirmed(int32(s.chainParams.CoinbaseMaturity), txHeight,
				syncHeight) {
				return nil
			}
		}
		if opcode == txscript.OP_SSTXCHANGE {
			if !confirmed(int32(s.chainParams.SStxChangeMaturity), txHeight,
				syncHeight) {
				return nil
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

		return nil
	})
	if err != nil {
		if err != errForEachBreakout {
			if _, ok := err.(apperrors.E); ok {
				return nil, err
			}
			str := "failed iterating unspent bucket"
			return nil, storeError(apperrors.ErrDatabase, str, err)
		}
	}

	// Unconfirmed transaction output handling.
	if minConf == 0 {
		err = ns.NestedReadBucket(bucketUnminedCredits).ForEach(func(k, v []byte) error {
			if found >= needed {
				return errForEachBreakout
			}

			// Make sure this output was not spent by an unmined transaction.
			// If it was, skip this credit.
			if existsRawUnminedInput(ns, k) != nil {
				return nil
			}

			// Check the account first.
			if !all {
				pkScript, err := s.fastCreditPkScriptLookup(ns, nil, k)
				if err != nil {
					return err
				}
				thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
				if err != nil {
					return err
				}
				if account != thisAcct {
					return nil
				}
			}

			amt, err := fetchRawUnminedCreditAmount(v)
			if err != nil {
				return err
			}

			// Skip ticket outputs, as only SSGen can spend these.
			opcode := fetchRawUnminedCreditTagOpcode(v)
			if opcode == txscript.OP_SSTX {
				return nil
			}

			// Skip outputs that are not mature.
			if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
				return nil
			}
			if opcode == txscript.OP_SSTXCHANGE {
				return nil
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
				return err
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

			return nil
		})
	}
	if err != nil {
		if err != errForEachBreakout {
			if _, ok := err.(apperrors.E); ok {
				return nil, err
			}
			str := "failed iterating unmined credits bucket"
			return nil, storeError(apperrors.ErrDatabase, str, err)
		}
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
	source func(dcrutil.Amount) (dcrutil.Amount, []*wire.TxIn, [][]byte, error)
}

// SelectInputs selects transaction inputs to redeem unspent outputs stored in
// the database.  It may be called multiple times with increasing target amounts
// to return additional inputs for a higher target amount.  It returns the total
// input amount referenced by the previous transaction outputs, a slice of
// transaction inputs referencing these outputs, and a slice of previous output
// scripts from each previous output referenced by the corresponding input.
func (s *InputSource) SelectInputs(target dcrutil.Amount) (dcrutil.Amount, []*wire.TxIn, [][]byte, error) {
	return s.source(target)
}

// MakeInputSource creates an InputSource to redeem unspent outputs from an
// account.  The minConf and syncHeight parameters are used to filter outputs
// based on some spendable policy.
func (s *Store) MakeInputSource(ns, addrmgrNs walletdb.ReadBucket, account uint32, minConf, syncHeight int32) InputSource {
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

	// Current inputs and their total value.  These are closed over by the
	// returned input source and reused across multiple calls.
	var (
		currentTotal   dcrutil.Amount
		currentInputs  []*wire.TxIn
		currentScripts [][]byte
	)

	f := func(target dcrutil.Amount) (dcrutil.Amount, []*wire.TxIn, [][]byte, error) {
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
				return 0, nil, nil, err
			}
			thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, cVal, nil, pkScript)
			if err != nil {
				return 0, nil, nil, err
			}
			if account != thisAcct {
				continue
			}

			amt, spent, err := fetchRawCreditAmountSpent(cVal)
			if err != nil {
				return 0, nil, nil, err
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
				if !confirmed(int32(s.chainParams.CoinbaseMaturity), txHeight,
					syncHeight) {
					continue
				}
			}
			if opcode == txscript.OP_SSGEN || opcode == txscript.OP_SSRTX {
				if !confirmed(int32(s.chainParams.CoinbaseMaturity), txHeight,
					syncHeight) {
					continue
				}
			}
			if opcode == txscript.OP_SSTXCHANGE {
				if !confirmed(int32(s.chainParams.SStxChangeMaturity), txHeight,
					syncHeight) {
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
				return 0, nil, nil, err
			}
			op.Tree = tree

			input := wire.NewTxIn(&op, nil)

			currentTotal += amt
			currentInputs = append(currentInputs, input)
			currentScripts = append(currentScripts, pkScript)
		}

		// Return the current results if the target was specified and met
		// or unspent unmined credits can not be included.
		if (target != 0 && currentTotal >= target) || minConf != 0 {
			return currentTotal, currentInputs, currentScripts, nil
		}

		// Iterate through unspent unmined credits
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
				return 0, nil, nil, err
			}
			thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
			if err != nil {
				return 0, nil, nil, err
			}
			if account != thisAcct {
				continue
			}

			amt, err := fetchRawUnminedCreditAmount(v)
			if err != nil {
				return 0, nil, nil, err
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
				return 0, nil, nil, err
			}
			op.Tree = tree

			input := wire.NewTxIn(&op, nil)

			currentTotal += amt
			currentInputs = append(currentInputs, input)
			currentScripts = append(currentScripts, pkScript)
		}
		return currentTotal, currentInputs, currentScripts, nil
	}

	return InputSource{source: f}
}

// balanceFullScan does a fullscan of the UTXO set to get the current balance.
// It is less efficient than the other balance functions, but works fine for
// accounts.
func (s *Store) balanceFullScan(ns, addrmgrNs walletdb.ReadBucket, minConf int32,
	syncHeight int32) (map[uint32]*Balances, error) {

	accountBalances := make(map[uint32]*Balances)
	err := ns.NestedReadBucket(bucketUnspent).ForEach(func(k, v []byte) error {
		if existsRawUnminedInput(ns, k) != nil {
			// Output is spent by an unmined transaction.
			// Skip to next unmined credit.
			return nil
		}

		cKey := make([]byte, 72)
		copy(cKey[0:32], k[0:32])   // Tx hash
		copy(cKey[32:36], v[0:4])   // Block height
		copy(cKey[36:68], v[4:36])  // Block hash
		copy(cKey[68:72], k[32:36]) // Output index

		// Skip unmined credits.
		cVal := existsRawCredit(ns, cKey)
		if cVal == nil {
			return fmt.Errorf("couldn't find a credit for unspent txo")
		}

		// Check the account first.
		pkScript, err := s.fastCreditPkScriptLookup(ns, cKey, nil)
		if err != nil {
			return err
		}
		thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, cVal, nil, pkScript)
		if err != nil {
			return err
		}

		utxoAmt, err := fetchRawCreditAmount(cVal)
		if err != nil {
			return err
		}

		height := extractRawCreditHeight(cKey)
		opcode := fetchRawCreditTagOpCode(cVal)

		ab, ok := accountBalances[thisAcct]
		if !ok {
			ab = &Balances{
				Account: thisAcct,
				Total:   utxoAmt,
			}
			accountBalances[thisAcct] = ab
		} else {
			ab.Total += utxoAmt
		}

		switch opcode {
		case opNonstake:
			isConfirmed := confirmed(minConf, height, syncHeight)
			creditFromCoinbase := fetchRawCreditIsCoinbase(cVal)
			matureCoinbase := (creditFromCoinbase &&
				confirmed(int32(s.chainParams.CoinbaseMaturity),
					height,
					syncHeight))

			if (isConfirmed && !creditFromCoinbase) ||
				matureCoinbase {
				ab.Spendable += utxoAmt
			} else if creditFromCoinbase && !matureCoinbase {
				ab.ImmatureCoinbaseRewards += utxoAmt
			}

		case txscript.OP_SSTX:
			// Locked as stake ticket.
			txHash := extractRawCreditTxHash(k)

			blockRec, err := fetchBlockRecord(ns, height)
			if err != nil {
				return err
			}

			_, txv := existsTxRecord(ns, &txHash, &blockRec.Block)
			if txv == nil {
				str := fmt.Sprintf("missing transaction record for tx %v block %v",
					txHash, &blockRec.Block.Hash)
				return storeError(apperrors.ErrData, str, err)
			}

			var rec TxRecord
			err = readRawTxRecord(&txHash, txv, &rec)
			if err != nil {
				return err
			}
			votingAuthorityAmt := dcrutil.Amount(0)
			lockedByTicketsAmt := dcrutil.Amount(0)

			// Calculate total input amount which will allow a proper fee calculation.
			// Fee is needed to be removed from the stake.AmountFromSStxPkScrCommitment
			// due to the way tickets are constructed and rewards are calculated.
			totalInputAmount := dcrutil.Amount(0)
			for i := range rec.MsgTx.TxIn {
				_, credKey, err := existsDebit(ns,
					&txHash, uint32(i), &blockRec.Block)
				if err != nil {
					return err
				}
				if credKey == nil {
					continue
				}
				credVal := existsRawCredit(ns, credKey)
				if credVal == nil {
					return apperrors.New(apperrors.ErrData, "couldn't find a credit for unspent txo")
				}
				inputAmount, err := fetchRawCreditAmount(credVal)
				if err != nil {
					return err
				}
				totalInputAmount += inputAmount
			}
			for i, txout := range rec.MsgTx.TxOut {
				if i%2 != 0 {
					addr, err := stake.AddrFromSStxPkScrCommitment(txout.PkScript,
						s.chainParams)
					if err != nil {
						return err
					}
					amt, err := stake.AmountFromSStxPkScrCommitment(txout.PkScript)
					if err != nil {
						return err
					}
					votingAuthorityAmt += amt
					if _, err := s.acctLookupFunc(addrmgrNs, addr); err != nil {
						if apperrors.IsError(err, apperrors.ErrAddressNotFound) {
							continue
						}
						return err
					}
					lockedByTicketsAmt += amt
				}
			}
			// Calculate the fee here due to the commitamt in the OP_SSTX output script being the total
			// value in.
			fee := dcrutil.Amount(0)
			if totalInputAmount > 0 {
				fee = totalInputAmount - utxoAmt
			}
			if lockedByTicketsAmt > 0 {
				// Only calculate proper lockedbyticketstamt if > 0
				ab.LockedByTickets += lockedByTicketsAmt - fee
			}
			ab.VotingAuthority += votingAuthorityAmt - fee
		case txscript.OP_SSGEN:
			fallthrough
		case txscript.OP_SSRTX:
			if confirmed(int32(s.chainParams.CoinbaseMaturity),
				height, syncHeight) {
				ab.Spendable += utxoAmt
			} else {
				ab.ImmatureStakeGeneration += utxoAmt
			}

		case txscript.OP_SSTXCHANGE:
			if confirmed(int32(s.chainParams.SStxChangeMaturity),
				height, syncHeight) {
				ab.Spendable += utxoAmt
			}

		default:
			log.Warnf("Unhandled opcode: %v", opcode)
		}

		return nil
	})
	if err != nil {
		str := "failed iterating mined credits bucket for fullscan balance"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}

	// Unconfirmed transaction output handling.
	err = ns.NestedReadBucket(bucketUnminedCredits).ForEach(func(k, v []byte) error {
		// Make sure this output was not spent by an unmined transaction.
		// If it was, skip this credit.
		if existsRawUnminedInput(ns, k) != nil {
			return nil
		}

		// Check the account first.
		pkScript, err := s.fastCreditPkScriptLookup(ns, nil, k)
		if err != nil {
			return err
		}
		thisAcct, err := s.fetchAccountForPkScript(addrmgrNs, nil, v, pkScript)
		if err != nil {
			return err
		}

		utxoAmt, err := fetchRawUnminedCreditAmount(v)
		if err != nil {
			return err
		}

		ab, ok := accountBalances[thisAcct]
		if !ok {
			ab = &Balances{
				Account: thisAcct,
				Total:   utxoAmt,
			}
			accountBalances[thisAcct] = ab
		} else {
			ab.Total += utxoAmt
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
		case txscript.OP_SSTX:
			txHash := extractRawUnminedCreditTxHash(k)

			rawUnmined := existsRawUnmined(ns, txHash)
			if rawUnmined == nil {
				return nil
			}
			serializedTx := extractRawUnminedTx(rawUnmined)
			var rec TxRecord
			err = rec.MsgTx.Deserialize(bytes.NewReader(serializedTx))
			if err != nil {
				return err
			}
			votingAuthorityAmt := dcrutil.Amount(0)
			lockedByTicketsAmt := dcrutil.Amount(0)

			// Calculate total input amount which will allow a proper fee calculation.
			// Fee is needed to be removed from the stake.AmountFromSStxPkScrCommitment
			// due to the way tickets are constructed and rewards are calculated.
			totalInputAmount := dcrutil.Amount(0)
			for _, txin := range rec.MsgTx.TxIn {
				rawUnmined := existsRawUnmined(ns, txin.PreviousOutPoint.Hash[:])
				if rawUnmined != nil {
					serializedTx := extractRawUnminedTx(rawUnmined)
					var tx wire.MsgTx
					err = tx.Deserialize(bytes.NewReader(serializedTx))
					if err != nil {
						return err
					}
					if int(txin.PreviousOutPoint.Index) < len(tx.TxOut) {
						totalInputAmount += dcrutil.Amount(tx.TxOut[txin.PreviousOutPoint.Index].Value)
					}
				} else {
					_, txVal := latestTxRecord(ns, txin.PreviousOutPoint.Hash[:])
					if txVal == nil {
						continue
					}
					var tx wire.MsgTx
					err = readRawTxRecordMsgTx(&txin.PreviousOutPoint.Hash, txVal, &tx)
					if err != nil {
						return err
					}
					if int(txin.PreviousOutPoint.Index) < len(tx.TxOut) {
						totalInputAmount += dcrutil.Amount(tx.TxOut[txin.PreviousOutPoint.Index].Value)
					}
				}
			}
			for i, txout := range rec.MsgTx.TxOut {
				if i%2 != 0 {
					addr, err := stake.AddrFromSStxPkScrCommitment(txout.PkScript,
						s.chainParams)
					if err != nil {
						return err
					}
					amt, err := stake.AmountFromSStxPkScrCommitment(txout.PkScript)
					if err != nil {
						return err
					}
					votingAuthorityAmt += amt
					if _, err := s.acctLookupFunc(addrmgrNs, addr); err != nil {
						if apperrors.IsError(err, apperrors.ErrAddressNotFound) {
							continue
						}
						return err
					}
					lockedByTicketsAmt += amt
				}
			}
			// Calculate the fee here due to the commitamt in the OP_SSTX output script being the total
			// value in.
			fee := dcrutil.Amount(0)
			if totalInputAmount > 0 {
				fee = totalInputAmount - utxoAmt
			}
			if lockedByTicketsAmt > 0 {
				// Only calculate proper lockedbyticketstamt if > 0
				ab.LockedByTickets += lockedByTicketsAmt - fee
			}
			ab.VotingAuthority += votingAuthorityAmt - fee
		case txscript.OP_SSGEN:
			fallthrough
		case txscript.OP_SSRTX:
			ab.ImmatureStakeGeneration += utxoAmt
		case txscript.OP_SSTXCHANGE:
			return nil
		default:
			log.Warnf("Unhandled unconfirmed opcode %v: %v", opcode, v)
		}

		return nil
	})
	if err != nil {
		str := "failed iterating unmined credits bucket for fullscan balance"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}

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
func (s *Store) AccountBalance(ns, addrmgrNs walletdb.ReadBucket, minConf int32,
	account uint32) (Balances, error) {

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
func (s *Store) AccountBalances(ns, addrmgrNs walletdb.ReadBucket,
	minConf int32) (map[uint32]*Balances, error) {

	_, syncHeight := s.MainChainTip(ns)
	return s.balanceFullScan(ns, addrmgrNs, minConf, syncHeight)
}

// InsertTxScript is the exported version of insertTxScript.
func (s *Store) InsertTxScript(ns walletdb.ReadWriteBucket, script []byte) error {
	return s.insertTxScript(ns, script)
}

// insertTxScript inserts a transaction script into the database.
func (s *Store) insertTxScript(ns walletdb.ReadWriteBucket, script []byte) error {
	return putTxScript(ns, script)
}

// GetTxScript is the exported version of getTxScript.
func (s *Store) GetTxScript(ns walletdb.ReadBucket, hash []byte) ([]byte, error) {
	return s.getTxScript(ns, hash), nil
}

// getTxScript fetches a transaction script from the database using
// the RIPEMD160 hash as a key.
func (s *Store) getTxScript(ns walletdb.ReadBucket, hash []byte) []byte {
	return existsTxScript(ns, hash)
}

// StoredTxScripts is the exported version of storedTxScripts.
func (s *Store) StoredTxScripts(ns walletdb.ReadBucket) ([][]byte, error) {
	return s.storedTxScripts(ns)
}

// storedTxScripts returns a slice of byte slices containing all the transaction
// scripts currently stored in wallet.
func (s *Store) storedTxScripts(ns walletdb.ReadBucket) ([][]byte, error) {
	var scripts [][]byte
	err := ns.NestedReadBucket(bucketScripts).ForEach(func(k, v []byte) error {
		scripts = append(scripts, v)
		return nil
	})
	if err != nil {
		if _, ok := err.(apperrors.E); ok {
			return nil, err
		}
		str := "failed iterating scripts"
		return nil, storeError(apperrors.ErrDatabase, str, err)
	}
	return scripts, err
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
				desc := fmt.Sprintf("failed to deserialize unmined tx %v",
					&in.PreviousOutPoint.Hash)
				return 0, apperrors.Wrap(err, apperrors.ErrData, desc)
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
			desc := fmt.Sprintf("previous output index %d does not exist in "+
				"transaction %v", in.PreviousOutPoint.Index, &in.PreviousOutPoint.Hash)
			return 0, apperrors.New(apperrors.ErrInput, desc)
		}

		total += dcrutil.Amount(tx.TxOut[in.PreviousOutPoint.Index].Value)
	}

	return total, nil
}
