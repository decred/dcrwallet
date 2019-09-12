// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"encoding/binary"
	"runtime/debug"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"golang.org/x/crypto/ripemd160"
)

// Naming
//
// The following variables are commonly used in this file and given
// reserved names:
//
//   ns: The namespace bucket for this package
//   b:  The primary bucket being operated on
//   k:  A single bucket key
//   v:  A single bucket value
//   c:  A bucket cursor
//   ck: The current cursor key
//   cv: The current cursor value
//
// Functions use the naming scheme `Op[Raw]Type[Field]`, which performs the
// operation `Op` on the type `Type`, optionally dealing with raw keys and
// values if `Raw` is used.  Fetch and extract operations may only need to read
// some portion of a key or value, in which case `Field` describes the component
// being returned.  The following operations are used:
//
//   key:     return a db key for some data
//   value:   return a db value for some data
//   put:     insert or replace a value into a bucket
//   fetch:   read and return a value
//   read:    read a value into an out parameter
//   exists:  return the raw (nil if not found) value for some data
//   delete:  remove a k/v pair
//   extract: perform an unchecked slice to extract a key or value
//
// Other operations which are specific to the types being operated on
// should be explained in a comment.
//
// TODO Remove all magic numbers and replace them with cursors that are
//      incremented by constants. Comments need to be filled in. Only
//      about 1/2 of functions are properly commented.

const (
	// accountExistsMask is the bitmask for the accountExists bool in
	// the encoded scriptType for credits.
	accountExistsMask = uint8(0x80)
)

// scriptType indicates what type of script a pkScript is for the
// purposes of the database. In the future this can allow for very
// fast lookup of the 20-byte (or more) script/public key hash.
// waddrmgr currently takes addresses instead of 20-byte hashes
// for look up, so the script type is unused in favor of using
// txscript to extract the address from a pkScript.
type scriptType uint8

const (
	// scriptTypeNonexisting is the uint8 value representing an
	// unset script type.
	scriptTypeNonexisting = iota

	// scriptTypeUnspecified is the uint8 value representing an
	// unknown or unspecified type of script.
	scriptTypeUnspecified

	// scriptTypeP2PKH is the uint8 value representing a
	// pay-to-public-key-hash script for a regular transaction.
	scriptTypeP2PKH

	// scriptTypeP2PK is the uint8 value representing a
	// pay-to-public-key script for a regular transaction.
	scriptTypeP2PK

	// scriptTypeP2PKHAlt is the uint8 value representing a
	// pay-to-public-key-hash script for a regular transaction
	// with an alternative ECDSA.
	scriptTypeP2PKHAlt

	// scriptTypeP2PKAlt is the uint8 value representing a
	// pay-to-public-key script for a regular transaction with
	// an alternative ECDSA.
	scriptTypeP2PKAlt

	// scriptTypeP2SH is the uint8 value representing a
	// pay-to-script-hash script for a regular transaction.
	scriptTypeP2SH

	// scriptTypeSP2PKH is the uint8 value representing a
	// pay-to-public-key-hash script for a stake transaction.
	scriptTypeSP2PKH

	// scriptTypeP2SH is the uint8 value representing a
	// pay-to-script-hash script for a stake transaction.
	scriptTypeSP2SH
)

const (
	// scriptLocNotStored is the offset value indicating that
	// the output was stored as a legacy credit and that the
	// script location was not stored.
	scriptLocNotStored = 0
)

// Big endian is the preferred byte order, due to cursor scans over integer
// keys iterating in order.
var byteOrder = binary.BigEndian

// This package makes assumptions that the width of a chainhash.Hash is always 32
// bytes.  If this is ever changed (unlikely for bitcoin, possible for alts),
// offsets have to be rewritten.  Use a compile-time assertion that this
// assumption holds true.
var _ [32]byte = chainhash.Hash{}

// Bucket names
var (
	bucketBlocks                  = []byte("b")
	bucketHeaders                 = []byte("h")
	bucketTxRecords               = []byte("t")
	bucketCredits                 = []byte("c")
	bucketUnspent                 = []byte("u")
	bucketDebits                  = []byte("d")
	bucketUnmined                 = []byte("m")
	bucketUnminedCredits          = []byte("mc")
	bucketUnminedInputs           = []byte("mi")
	bucketTickets                 = []byte("tix")
	bucketScripts                 = []byte("sc")
	bucketMultisig                = []byte("ms")
	bucketMultisigUsp             = []byte("mu")
	bucketStakeInvalidatedCredits = []byte("ic")
	bucketStakeInvalidatedDebits  = []byte("id")
	bucketCFilters                = []byte("cf")
	bucketTicketCommitments       = []byte("cmt")
	bucketTicketCommitmentsUsp    = []byte("cmu")
)

// Root (namespace) bucket keys
var (
	rootCreateDate   = []byte("date")
	rootVersion      = []byte("vers")
	rootMinedBalance = []byte("bal")
	rootTipBlock     = []byte("tip")
	rootHaveCFilters = []byte("havecfilters")
	rootLastTxsBlock = []byte("lasttxsblock")
)

// The root bucket's mined balance k/v pair records the total balance for all
// unspent credits from mined transactions.  This includes immature outputs, and
// outputs spent by mempool transactions, which must be considered when
// returning the actual balance for a given number of block confirmations.  The
// value is the amount serialized as a uint64.
func fetchMinedBalance(ns walletdb.ReadBucket) (dcrutil.Amount, error) {
	v := ns.Get(rootMinedBalance)
	if len(v) != 8 {
		return 0, errors.E(errors.IO, errors.Errorf("mined balance len %d", len(v)))
	}
	return dcrutil.Amount(byteOrder.Uint64(v)), nil
}

func putMinedBalance(ns walletdb.ReadWriteBucket, amt dcrutil.Amount) error {
	v := make([]byte, 8)
	byteOrder.PutUint64(v, uint64(amt))
	err := ns.Put(rootMinedBalance, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// Several data structures are given canonical serialization formats as either
// keys or values.  These common formats allow keys and values to be reused
// across different buckets.
//
// The canonical outpoint serialization format is:
//
//   [0:32]  Trasaction hash (32 bytes)
//   [32:36] Output index (4 bytes)
//
// The canonical transaction hash serialization is simply the hash.

func canonicalOutPoint(txHash *chainhash.Hash, index uint32) []byte {
	k := make([]byte, 36)
	copy(k, txHash[:])
	byteOrder.PutUint32(k[32:36], index)
	return k
}

func readCanonicalOutPoint(k []byte, op *wire.OutPoint) error {
	if len(k) < 36 {
		return errors.E(errors.IO, errors.Errorf("outpoint len %d", len(k)))
	}
	copy(op.Hash[:], k)
	op.Index = byteOrder.Uint32(k[32:36])
	return nil
}

// Details regarding blocks are saved as k/v pairs in the blocks bucket.
// blockRecords are keyed by their height.  The value is serialized as such:
//
// TODO: Unix time and vote bits are redundant now that headers are saved and
// these can be removed from the block record in a future update.
//
//   [0:32]  Hash (32 bytes)
//   [32:40] Unix time (8 bytes)
//   [40:42] VoteBits (2 bytes/uint16)
//   [42:43] Whether regular transactions are stake invalidated (1 byte, 0==false)
//   [43:47] Number of transaction hashes (4 bytes)
//   [47:]   For each transaction hash:
//             Hash (32 bytes)

func keyBlockRecord(height int32) []byte {
	k := make([]byte, 4)
	byteOrder.PutUint32(k, uint32(height))
	return k
}

func valueBlockRecordEmptyFromHeader(blockHash []byte, header []byte) []byte {
	v := make([]byte, 47)
	copy(v, blockHash[:])
	byteOrder.PutUint64(v[32:40], uint64(extractBlockHeaderUnixTime(header[:])))
	byteOrder.PutUint16(v[40:42], extractBlockHeaderVoteBits(header[:]))
	byteOrder.PutUint32(v[43:47], 0)
	return v
}

// valueBlockRecordStakeValidated returns a copy of the block record value with
// stake validated byte set to zero.
func valueBlockRecordStakeValidated(v []byte) []byte {
	newv := make([]byte, len(v))
	copy(newv, v[:42])
	copy(newv[43:], v[43:])
	return newv
}

// valueBlockRecordStakeInvalidated returns a copy of the block record value
// with stake validated byte set to one.
func valueBlockRecordStakeInvalidated(v []byte) []byte {
	newv := make([]byte, len(v))
	copy(newv, v[:42])
	newv[42] = 1
	copy(newv[43:], v[43:])
	return newv
}

// appendRawBlockRecord returns a new block record value with a transaction
// hash appended to the end and an incremented number of transactions.
func appendRawBlockRecord(v []byte, txHash *chainhash.Hash) ([]byte, error) {
	if len(v) < 47 {
		return nil, errors.E(errors.IO, errors.Errorf("block record len %d", len(v)))
	}
	newv := append(v[:len(v):len(v)], txHash[:]...)
	n := byteOrder.Uint32(newv[43:47])
	byteOrder.PutUint32(newv[43:47], n+1)
	return newv, nil
}

func putRawBlockRecord(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketBlocks).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func fetchBlockTime(ns walletdb.ReadBucket, height int32) (time.Time, error) {
	k := keyBlockRecord(height)
	v := ns.NestedReadBucket(bucketBlocks).Get(k)
	if len(v) < 47 {
		return time.Time{}, errors.E(errors.IO, errors.Errorf("block record len %d", len(v)))
	}
	return time.Unix(int64(byteOrder.Uint64(v[32:40])), 0), nil
}

func fetchBlockRecord(ns walletdb.ReadBucket, height int32) (*blockRecord, error) {
	br := &blockRecord{}
	k := keyBlockRecord(height)
	v := ns.NestedReadBucket(bucketBlocks).Get(k)
	err := readRawBlockRecord(k, v, br)

	return br, err
}

func existsBlockRecord(ns walletdb.ReadBucket, height int32) (k, v []byte) {
	k = keyBlockRecord(height)
	v = ns.NestedReadBucket(bucketBlocks).Get(k)
	return
}

func readRawBlockRecord(k, v []byte, block *blockRecord) error {
	if len(k) < 4 {
		return errors.E(errors.IO, errors.Errorf("block key len %d", len(k)))
	}
	if len(v) < 47 {
		return errors.E(errors.IO, errors.Errorf("block record len %d", len(k)))
	}

	numTransactions := int(byteOrder.Uint32(v[43:47]))
	expectedLen := 47 + chainhash.HashSize*numTransactions
	if len(v) < expectedLen {
		return errors.E(errors.IO, errors.Errorf("%d tx block record len %d",
			numTransactions, len(v)))
	}

	block.Height = int32(byteOrder.Uint32(k))
	copy(block.Hash[:], v)
	block.Time = time.Unix(int64(byteOrder.Uint64(v[32:40])), 0)
	block.VoteBits = byteOrder.Uint16(v[40:42])
	block.transactions = make([]chainhash.Hash, numTransactions)
	off := 47
	for i := range block.transactions {
		copy(block.transactions[i][:], v[off:])
		off += chainhash.HashSize
	}

	return nil
}

func extractRawBlockRecordHash(v []byte) []byte {
	return v[:32]
}

func extractRawBlockRecordStakeInvalid(v []byte) bool {
	return v[42] != 0
}

type blockIterator struct {
	c    walletdb.ReadWriteCursor
	seek []byte
	ck   []byte
	cv   []byte
	elem blockRecord
	err  error
}

func makeReadBlockIterator(ns walletdb.ReadBucket, height int32) blockIterator {
	seek := make([]byte, 4)
	byteOrder.PutUint32(seek, uint32(height))
	c := ns.NestedReadBucket(bucketBlocks).ReadCursor()
	return blockIterator{c: readCursor{c}, seek: seek}
}

// Works just like makeBlockIterator but will initially position the cursor at
// the last k/v pair.  Use this with blockIterator.prev.
func makeReverseBlockIterator(ns walletdb.ReadWriteBucket) blockIterator {
	seek := make([]byte, 4)
	byteOrder.PutUint32(seek, ^uint32(0))
	c := ns.NestedReadWriteBucket(bucketBlocks).ReadWriteCursor()
	return blockIterator{c: c, seek: seek}
}

func (it *blockIterator) next() bool {
	if it.c == nil {
		return false
	}

	if it.ck == nil {
		it.ck, it.cv = it.c.Seek(it.seek)
	} else {
		it.ck, it.cv = it.c.Next()
	}
	if it.ck == nil {
		it.c.Close()
		it.c = nil
		return false
	}

	err := readRawBlockRecord(it.ck, it.cv, &it.elem)
	if err != nil {
		it.c = nil
		it.err = err
		return false
	}

	return true
}

func (it *blockIterator) prev() bool {
	if it.c == nil {
		return false
	}

	if it.ck == nil {
		it.ck, it.cv = it.c.Seek(it.seek)
		// Seek positions the cursor at the next k/v pair if one with
		// this prefix was not found.  If this happened (the prefixes
		// won't match in this case) move the cursor backward.
		//
		// This technically does not correct for multiple keys with
		// matching prefixes by moving the cursor to the last matching
		// key, but this doesn't need to be considered when dealing with
		// block records since the key (and seek prefix) is just the
		// block height.
		if !bytes.HasPrefix(it.ck, it.seek) {
			it.ck, it.cv = it.c.Prev()
		}
	} else {
		it.ck, it.cv = it.c.Prev()
	}
	if it.ck == nil {
		it.c.Close()
		it.c = nil
		return false
	}

	err := readRawBlockRecord(it.ck, it.cv, &it.elem)
	if err != nil {
		it.c.Close()
		it.c = nil
		it.err = err
		return false
	}

	return true
}

func (it *blockIterator) close() {
	if it.c == nil {
		return
	}
	it.c.Close()
}

// unavailable until https://github.com/boltdb/bolt/issues/620 is fixed.
// func (it *blockIterator) delete() error {
// 	err := it.c.Delete()
// 	if err != nil {
// 		return errors.E(errors.IO, err)
// 	}
// 	return nil
// }

func (it *blockIterator) reposition(height int32) {
	it.c.Seek(keyBlockRecord(height))
}

func deleteBlockRecord(ns walletdb.ReadWriteBucket, height int32) error {
	k := keyBlockRecord(height)
	return ns.NestedReadWriteBucket(bucketBlocks).Delete(k)
}

// Block headers are saved as k/v pairs in the headers bucket.  Block headers
// are keyed by their block hashes.  The value is the serialized block header.

func keyBlockHeader(blockHash *chainhash.Hash) []byte { return blockHash[:] }

func putRawBlockHeader(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketHeaders).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func fetchRawBlockHeader(ns walletdb.ReadBucket, k []byte) ([]byte, error) {
	v := ns.NestedReadBucket(bucketHeaders).Get(k)
	if v == nil {
		return nil, errors.E(errors.NotExist, "block header")
	}
	vcopy := make([]byte, len(v))
	copy(vcopy, v)
	return vcopy, nil
}

func existsBlockHeader(ns walletdb.ReadBucket, k []byte) []byte {
	return ns.NestedReadBucket(bucketHeaders).Get(k)
}

// Transaction records are keyed as such:
//
//   [0:32]  Transaction hash (32 bytes)
//   [32:36] Block height (4 bytes)
//   [36:68] Block hash (32 bytes)
//
// The leading transaction hash allows to prefix filter for all records with
// a matching hash.  The block height and hash records a particular incidence
// of the transaction in the blockchain.
//
// The record value is serialized as such:
//
//   [0:8]   Received time (8 bytes)
//   [8:]    Serialized transaction (varies)

func keyTxRecord(txHash *chainhash.Hash, block *Block) []byte {
	k := make([]byte, 68)
	copy(k, txHash[:])
	byteOrder.PutUint32(k[32:36], uint32(block.Height))
	copy(k[36:68], block.Hash[:])
	return k
}

func valueTxRecord(rec *TxRecord) ([]byte, error) {
	var v []byte
	if rec.SerializedTx == nil {
		txSize := rec.MsgTx.SerializeSize()
		v = make([]byte, 8, 8+txSize)
		err := rec.MsgTx.Serialize(bytes.NewBuffer(v[8:]))
		if err != nil {
			return nil, errors.E(errors.Invalid, err)
		}
		v = v[:cap(v)]
	} else {
		v = make([]byte, 8+len(rec.SerializedTx))
		copy(v[8:], rec.SerializedTx)
	}
	byteOrder.PutUint64(v, uint64(rec.Received.Unix()))
	return v, nil
}

func putTxRecord(ns walletdb.ReadWriteBucket, rec *TxRecord, block *Block) error {
	k := keyTxRecord(&rec.Hash, block)
	v, err := valueTxRecord(rec)
	if err != nil {
		return err
	}
	err = ns.NestedReadWriteBucket(bucketTxRecords).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func putRawTxRecord(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketTxRecords).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func readRawTxRecordMsgTx(txHash *chainhash.Hash, v []byte, msgTx *wire.MsgTx) error {
	if len(v) < 8 {
		return errors.E(errors.IO, errors.Errorf("tx record len %d", len(v)))
	}
	err := msgTx.Deserialize(bytes.NewReader(v[8:]))
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

func readRawTxRecord(txHash *chainhash.Hash, v []byte, rec *TxRecord) error {
	if len(v) < 8 {
		return errors.E(errors.IO, errors.Errorf("tx record len %d", len(v)))
	}
	rec.Hash = *txHash
	rec.Received = time.Unix(int64(byteOrder.Uint64(v)), 0)
	err := rec.MsgTx.Deserialize(bytes.NewReader(v[8:]))
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Calculate the stake TxType from the MsgTx.
	rec.TxType = stake.DetermineTxType(&rec.MsgTx)

	return nil
}

func readRawTxRecordHash(k []byte, hash *chainhash.Hash) error {
	if len(k) < 68 {
		debug.PrintStack()
		return errors.E(errors.IO, errors.Errorf("tx record key len %d", len(k)))
	}
	copy(hash[:], k[:32])
	return nil
}

func readRawTxRecordBlockHeight(k []byte, height *int32) error {
	if len(k) < 68 {
		debug.PrintStack()
		return errors.E(errors.IO, errors.Errorf("tx record key len %d", len(k)))
	}
	*height = int32(byteOrder.Uint32(k[32:36]))
	return nil
}

func readRawTxRecordBlock(k []byte, block *Block) error {
	if len(k) < 68 {
		debug.PrintStack()
		return errors.E(errors.IO, errors.Errorf("tx record key len %d", len(k)))
	}
	block.Height = int32(byteOrder.Uint32(k[32:36]))
	copy(block.Hash[:], k[36:68])
	return nil
}

func fetchRawTxRecordPkScript(k, v []byte, index uint32, scrLoc uint32, scrLen uint32) ([]byte, error) {
	var pkScript []byte

	var txHash chainhash.Hash
	err := readRawTxRecordHash(k, &txHash)
	if err != nil {
		return nil, err
	}

	// The script isn't stored (legacy credits). Deserialize the
	// entire transaction.
	if scrLoc == scriptLocNotStored {
		var rec TxRecord
		err = readRawTxRecord(&txHash, v, &rec)
		if err != nil {
			return nil, err
		}
		if int(index) >= len(rec.MsgTx.TxOut) {
			return nil, errors.E(errors.IO, "missing transaction output for credit index")
		}
		pkScript = rec.MsgTx.TxOut[index].PkScript
	} else {
		// We have the location and script length stored. Just
		// copy the script. Offset the script location for the
		// timestamp that prefixes it.
		scrLocInt := int(scrLoc) + 8
		scrLenInt := int(scrLen)

		// Check the bounds to make sure the we don't read beyond
		// the end of the serialized transaction value, which
		// would cause a panic.
		if scrLocInt > len(v)-1 || scrLocInt+scrLenInt > len(v) {
			return nil, errors.E(errors.IO, errors.Errorf(
				"invalid script offset %d for tx %v", scrLocInt, &txHash))
		}

		pkScript = make([]byte, scrLenInt)
		copy(pkScript, v[scrLocInt:scrLocInt+scrLenInt])
	}

	return pkScript, nil
}

func fetchRawTxRecordReceived(v []byte) time.Time {
	return time.Unix(int64(byteOrder.Uint64(v)), 0)
}

func existsTxRecord(ns walletdb.ReadBucket, txHash *chainhash.Hash, block *Block) (k, v []byte) {
	k = keyTxRecord(txHash, block)
	v = ns.NestedReadBucket(bucketTxRecords).Get(k)
	return
}

func existsRawTxRecord(ns walletdb.ReadBucket, k []byte) (v []byte) {
	return ns.NestedReadBucket(bucketTxRecords).Get(k)
}

func deleteTxRecord(ns walletdb.ReadWriteBucket, txHash *chainhash.Hash, block *Block) error {
	k := keyTxRecord(txHash, block)
	err := ns.NestedReadWriteBucket(bucketTxRecords).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// latestTxRecord searches for the newest recorded mined transaction record with
// a matching hash.  In case of a hash collision, the record from the newest
// block is returned.  Returns (nil, nil) if no matching transactions are found.
func latestTxRecord(ns walletdb.ReadBucket, txHash []byte) (k, v []byte) {
	c := ns.NestedReadBucket(bucketTxRecords).ReadCursor()
	ck, cv := c.Seek(txHash)
	var lastKey, lastVal []byte
	for bytes.HasPrefix(ck, txHash) {
		lastKey, lastVal = ck, cv
		ck, cv = c.Next()
	}
	c.Close()
	return lastKey, lastVal
}

// All transaction credits (outputs) are keyed as such:
//
//   [0:32]  Transaction hash (32 bytes)
//   [32:36] Block height (4 bytes)
//   [36:68] Block hash (32 bytes)
//   [68:72] Output index (4 bytes)
//
// The first 68 bytes match the key for the transaction record and may be used
// as a prefix filter to iterate through all credits in order.
//
// The credit value is serialized as such:
//
//   [0:8]   Amount (8 bytes)
//   [8]     Flags (1 byte)
//             0x01: Spent
//             0x02: Change
//             0x1c: P2PKH stake flag
//                 0x00: None (translates to OP_NOP10)
//                 0x04: OP_SSTX
//                 0x08: OP_SSGEN
//                 0x0c: OP_SSRTX
//                 0x10: OP_SSTXCHANGE
//             0x20: IsCoinbase
//             0x40: HasExpiry
//   [9:81]  OPTIONAL Debit bucket key (72 bytes)
//             [9:41]  Spender transaction hash (32 bytes)
//             [41:45] Spender block height (4 bytes)
//             [45:77] Spender block hash (32 bytes)
//             [77:81] Spender transaction input index (4 bytes)
//   [81:86] OPTIONAL scriptPk location in the transaction output (5 bytes)
//             [81] Script type (P2PKH, P2SH, etc) and accountExists
//             [82:86] Byte index (4 bytes, uint32)
//             [86:90] Length of script (4 bytes, uint32)
//             [90:94] Account (4 bytes, uint32)
//
// The optional debits key is only included if the credit is spent by another
// mined debit.

const (
	// creditKeySize is the total size of a credit key in bytes.
	creditKeySize = 72

	// creditValueSize is the total size of a credit value in bytes.
	creditValueSize = 94
)

func keyCredit(txHash *chainhash.Hash, index uint32, block *Block) []byte {
	k := make([]byte, creditKeySize)
	copy(k, txHash[:])
	byteOrder.PutUint32(k[32:36], uint32(block.Height))
	copy(k[36:68], block.Hash[:])
	byteOrder.PutUint32(k[68:72], index)
	return k
}

func condenseOpCode(opCode uint8) byte {
	return (opCode - 0xb9) << 2
}

// valueUnspentCredit creates a new credit value for an unspent credit.  All
// credits are created unspent, and are only marked spent later, so there is no
// value function to create either spent or unspent credits.
func valueUnspentCredit(cred *credit, scrType scriptType, scrLoc uint32,
	scrLen uint32, account uint32, dbVersion uint32) []byte {
	v := make([]byte, creditValueSize)
	byteOrder.PutUint64(v, uint64(cred.amount))
	v[8] = condenseOpCode(cred.opCode)
	if cred.change {
		v[8] |= 1 << 1
	}
	if cred.isCoinbase {
		v[8] |= 1 << 5
	}
	if cred.hasExpiry {
		switch {
		case dbVersion >= hasExpiryFixedVersion:
			v[8] |= 1 << 6
		case dbVersion >= hasExpiryVersion:
			v[8] |= 1 << 4
		}
	}

	v[81] = byte(scrType)
	v[81] |= accountExistsMask
	byteOrder.PutUint32(v[82:86], scrLoc)
	byteOrder.PutUint32(v[86:90], scrLen)
	byteOrder.PutUint32(v[90:94], account)

	return v
}

func putRawCredit(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketCredits).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putUnspentCredit puts a credit record for an unspent credit.  It may only be
// used when the credit is already know to be unspent, or spent by an
// unconfirmed transaction.
func putUnspentCredit(ns walletdb.ReadWriteBucket, cred *credit, scrType scriptType,
	scrLoc uint32, scrLen uint32, account uint32, dbVersion uint32) error {
	k := keyCredit(&cred.outPoint.Hash, cred.outPoint.Index, &cred.block)
	v := valueUnspentCredit(cred, scrType, scrLoc, scrLen, account, dbVersion)
	return putRawCredit(ns, k, v)
}

func extractRawCreditTxHash(k []byte) chainhash.Hash {
	hash, _ := chainhash.NewHash(k[0:32])
	return *hash
}

func extractRawCreditTxRecordKey(k []byte) []byte {
	return k[0:68]
}

func extractRawCreditHeight(k []byte) int32 {
	return int32(byteOrder.Uint32(k[32:36]))
}

func extractRawCreditIndex(k []byte) uint32 {
	return byteOrder.Uint32(k[68:72])
}

func extractRawUnminedCreditTxHash(k []byte) []byte {
	return k[:32]
}

func extractRawCreditIsSpent(k []byte) bool {
	return k[8]&1<<0 != 0
}

func extractRawCreditSpenderDebitKey(v []byte) []byte {
	return v[9:81]
}

// fetchRawCreditAmount returns the amount of the credit.
func fetchRawCreditAmount(v []byte) (dcrutil.Amount, error) {
	if len(v) < 9 {
		return 0, errors.E(errors.IO, errors.Errorf("credit len %d", len(v)))
	}
	return dcrutil.Amount(byteOrder.Uint64(v)), nil
}

// fetchRawCreditAmountSpent returns the amount of the credit and whether the
// credit is spent.
func fetchRawCreditAmountSpent(v []byte) (dcrutil.Amount, bool, error) {
	if len(v) < 9 {
		return 0, false, errors.E(errors.IO, errors.Errorf("credit len %d", len(v)))
	}
	return dcrutil.Amount(byteOrder.Uint64(v)), v[8]&(1<<0) != 0, nil
}

// fetchRawCreditAmountChange returns the amount of the credit and whether the
// credit is marked as change.
func fetchRawCreditAmountChange(v []byte) (dcrutil.Amount, bool, error) {
	if len(v) < 9 {
		return 0, false, errors.E(errors.IO, errors.Errorf("credit len %d", len(v)))
	}
	return dcrutil.Amount(byteOrder.Uint64(v)), v[8]&(1<<1) != 0, nil
}

// fetchRawCreditUnspentValue returns the unspent value for a raw credit key.
// This may be used to mark a credit as unspent.
func fetchRawCreditUnspentValue(k []byte) ([]byte, error) {
	if len(k) < 72 {
		return nil, errors.E(errors.IO, errors.Errorf("credit key len %d", len(k)))
	}
	return k[32:68], nil
}

// fetchRawCreditTagOpCode fetches the compressed OP code for a transaction.
func fetchRawCreditTagOpCode(v []byte) uint8 {
	return (((v[8] >> 2) & 0x07) + 0xb9)
}

// fetchRawCreditIsCoinbase returns whether or not the credit is a coinbase
// output or not.
func fetchRawCreditIsCoinbase(v []byte) bool {
	return v[8]&(1<<5) != 0
}

// fetchRawCreditHasExpiry returns whether or not the credit has an expiry
// set or not.
func fetchRawCreditHasExpiry(v []byte, dbVersion uint32) bool {
	switch {
	case dbVersion >= hasExpiryFixedVersion:
		return v[8]&(1<<6) != 0
	case dbVersion >= hasExpiryVersion:
		return v[8]&(1<<4) != 0
	default:
		return false
	}
}

// fetchRawCreditScriptOffset returns the ScriptOffset for the pkScript of this
// credit.
func fetchRawCreditScriptOffset(v []byte) uint32 {
	if len(v) < creditValueSize {
		return 0
	}
	return byteOrder.Uint32(v[82:86])
}

// fetchRawCreditScriptLength returns the ScriptOffset for the pkScript of this
// credit.
func fetchRawCreditScriptLength(v []byte) uint32 {
	if len(v) < creditValueSize {
		return 0
	}
	return byteOrder.Uint32(v[86:90])
}

// fetchRawCreditAccount returns the account for the pkScript of this
// credit.
func fetchRawCreditAccount(v []byte) (uint32, error) {
	if len(v) < creditValueSize {
		return 0, errors.E(errors.IO, errors.Errorf("credit len %d", len(v)))
	}

	// Was the account ever set?
	if v[81]&accountExistsMask != accountExistsMask {
		return 0, errors.E(errors.IO, "credit account unset")
	}

	return byteOrder.Uint32(v[90:94]), nil
}

// spendRawCredit marks the credit with a given key as mined at some particular
// block as spent by the input at some transaction incidence.  The debited
// amount is returned.
func spendCredit(ns walletdb.ReadWriteBucket, k []byte, spender *indexedIncidence) (dcrutil.Amount, error) {
	v := ns.NestedReadWriteBucket(bucketCredits).Get(k)
	newv := make([]byte, creditValueSize)
	copy(newv, v)
	v = newv
	v[8] |= 1 << 0
	copy(v[9:41], spender.txHash[:])
	byteOrder.PutUint32(v[41:45], uint32(spender.block.Height))
	copy(v[45:77], spender.block.Hash[:])
	byteOrder.PutUint32(v[77:81], spender.index)

	return dcrutil.Amount(byteOrder.Uint64(v[0:8])), putRawCredit(ns, k, v)
}

// unspendRawCredit rewrites the credit for the given key as unspent.  The
// output amount of the credit is returned.  It returns without error if no
// credit exists for the key.
func unspendRawCredit(ns walletdb.ReadWriteBucket, k []byte) (dcrutil.Amount, error) {
	b := ns.NestedReadWriteBucket(bucketCredits)
	v := b.Get(k)
	if v == nil {
		return 0, nil
	}
	newv := make([]byte, creditValueSize)
	copy(newv, v)
	newv[8] &^= 1 << 0

	err := b.Put(k, newv)
	if err != nil {
		return 0, errors.E(errors.IO, err)
	}
	return dcrutil.Amount(byteOrder.Uint64(v[0:8])), nil
}

func existsCredit(ns walletdb.ReadBucket, txHash *chainhash.Hash, index uint32, block *Block) (k, v []byte) {
	k = keyCredit(txHash, index, block)
	v = ns.NestedReadBucket(bucketCredits).Get(k)
	return
}

func existsRawCredit(ns walletdb.ReadBucket, k []byte) []byte {
	return ns.NestedReadBucket(bucketCredits).Get(k)
}

func existsInvalidatedCredit(ns walletdb.ReadBucket, txHash *chainhash.Hash, index uint32, block *Block) (k, v []byte) {
	k = keyCredit(txHash, index, block)
	v = ns.NestedReadBucket(bucketStakeInvalidatedCredits).Get(k)
	return
}

func deleteRawCredit(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketCredits).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// creditIterator allows for in-order iteration of all credit records for a
// mined transaction.
//
// Example usage:
//
//   prefix := keyTxRecord(txHash, block)
//   it := makeCreditIterator(ns, prefix)
//   for it.next() {
//           // Use it.elem
//           // If necessary, read additional details from it.ck, it.cv
//   }
//   if it.err != nil {
//           // Handle error
//   }
//
// The elem's Spent field is not set to true if the credit is spent by an
// unmined transaction.  To check for this case:
//
//   k := canonicalOutPoint(&txHash, it.elem.Index)
//   it.elem.Spent = existsRawUnminedInput(ns, k) != nil
type creditIterator struct {
	c         walletdb.ReadWriteCursor // Set to nil after final iteration
	dbVersion uint32
	prefix    []byte
	ck        []byte
	cv        []byte
	elem      CreditRecord
	err       error
}

func makeReadCreditIterator(ns walletdb.ReadBucket, prefix []byte, dbVersion uint32) creditIterator {
	c := ns.NestedReadBucket(bucketCredits).ReadCursor()
	return creditIterator{c: readCursor{c}, prefix: prefix, dbVersion: dbVersion}
}

func (it *creditIterator) readElem() error {
	if len(it.ck) < 72 {
		return errors.E(errors.IO, errors.Errorf("credit key len %d", len(it.ck)))
	}
	if len(it.cv) < 9 {
		return errors.E(errors.IO, errors.Errorf("credit len %d", len(it.cv)))
	}
	it.elem.Index = byteOrder.Uint32(it.ck[68:72])
	it.elem.Amount = dcrutil.Amount(byteOrder.Uint64(it.cv))
	it.elem.Spent = it.cv[8]&(1<<0) != 0
	it.elem.Change = it.cv[8]&(1<<1) != 0
	it.elem.OpCode = fetchRawCreditTagOpCode(it.cv)
	it.elem.IsCoinbase = fetchRawCreditIsCoinbase(it.cv)
	it.elem.HasExpiry = fetchRawCreditHasExpiry(it.cv, it.dbVersion)

	return nil
}

func (it *creditIterator) next() bool {
	if it.c == nil {
		return false
	}

	if it.ck == nil {
		it.ck, it.cv = it.c.Seek(it.prefix)
	} else {
		it.ck, it.cv = it.c.Next()
	}
	if !bytes.HasPrefix(it.ck, it.prefix) {
		it.c.Close()
		it.c = nil
		return false
	}

	err := it.readElem()
	if err != nil {
		it.err = err
		return false
	}
	return true
}

func (it *creditIterator) close() {
	if it.c == nil {
		return
	}
	it.c.Close()
}

// The unspent index records all outpoints for mined credits which are not spent
// by any other mined transaction records (but may be spent by a mempool
// transaction).
//
// Keys are use the canonical outpoint serialization:
//
//   [0:32]  Transaction hash (32 bytes)
//   [32:36] Output index (4 bytes)
//
// Values are serialized as such:
//
//   [0:4]   Block height (4 bytes)
//   [4:36]  Block hash (32 bytes)

func valueUnspent(block *Block) []byte {
	v := make([]byte, 36)
	byteOrder.PutUint32(v, uint32(block.Height))
	copy(v[4:36], block.Hash[:])
	return v
}

func putUnspent(ns walletdb.ReadWriteBucket, outPoint *wire.OutPoint, block *Block) error {
	k := canonicalOutPoint(&outPoint.Hash, outPoint.Index)
	v := valueUnspent(block)
	err := ns.NestedReadWriteBucket(bucketUnspent).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func putRawUnspent(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnspent).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func readUnspentBlock(v []byte, block *Block) error {
	if len(v) < 36 {
		return errors.E(errors.IO, errors.Errorf("unspent len %d", len(v)))
	}
	block.Height = int32(byteOrder.Uint32(v))
	copy(block.Hash[:], v[4:36])
	return nil
}

// existsUnspent returns the key for the unspent output and the corresponding
// key for the credits bucket.  If there is no unspent output recorded, the
// credit key is nil.
func existsUnspent(ns walletdb.ReadBucket, outPoint *wire.OutPoint) (k, credKey []byte) {
	k = canonicalOutPoint(&outPoint.Hash, outPoint.Index)
	credKey = existsRawUnspent(ns, k)
	return k, credKey
}

// existsRawUnspent returns the credit key if there exists an output recorded
// for the raw unspent key.  It returns nil if the k/v pair does not exist.
func existsRawUnspent(ns walletdb.ReadBucket, k []byte) (credKey []byte) {
	if len(k) < 36 {
		return nil
	}
	v := ns.NestedReadBucket(bucketUnspent).Get(k)
	if len(v) < 36 {
		return nil
	}
	credKey = make([]byte, 72)
	copy(credKey, k[:32])
	copy(credKey[32:68], v)
	copy(credKey[68:72], k[32:36])
	return credKey
}

func deleteRawUnspent(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnspent).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// All transaction debits (inputs which spend credits) are keyed as such:
//
//   [0:32]  Transaction hash (32 bytes)
//   [32:36] Block height (4 bytes)
//   [36:68] Block hash (32 bytes)
//   [68:72] Input index (4 bytes)
//
// The first 68 bytes match the key for the transaction record and may be used
// as a prefix filter to iterate through all debits in order.
//
// The debit value is serialized as such:
//
//   [0:8]   Amount (8 bytes)
//   [8:80]  Credits bucket key (72 bytes)
//             [8:40]  Transaction hash (32 bytes)
//             [40:44] Block height (4 bytes)
//             [44:76] Block hash (32 bytes)
//             [76:80] Output index (4 bytes)

func keyDebit(txHash *chainhash.Hash, index uint32, block *Block) []byte {
	k := make([]byte, 72)
	copy(k, txHash[:])
	byteOrder.PutUint32(k[32:36], uint32(block.Height))
	copy(k[36:68], block.Hash[:])
	byteOrder.PutUint32(k[68:72], index)
	return k
}

func valueDebit(amount dcrutil.Amount, credKey []byte) []byte {
	v := make([]byte, 80)
	byteOrder.PutUint64(v, uint64(amount))
	copy(v[8:80], credKey)
	return v
}

func putDebit(ns walletdb.ReadWriteBucket, txHash *chainhash.Hash, index uint32, amount dcrutil.Amount, block *Block, credKey []byte) error {
	k := keyDebit(txHash, index, block)
	v := valueDebit(amount, credKey)

	err := ns.NestedReadWriteBucket(bucketDebits).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func putRawDebit(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketDebits).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func extractRawDebitHash(k []byte) []byte {
	return k[:32]
}

func extractRawDebitAmount(v []byte) dcrutil.Amount {
	return dcrutil.Amount(byteOrder.Uint64(v[:8]))
}

func extractRawDebitCreditKey(v []byte) []byte {
	return v[8:80]
}

func extractRawDebitUnspentValue(v []byte) []byte {
	return v[40:76]
}

// existsDebit checks for the existence of a debit.  If found, the debit and
// previous credit keys are returned.  If the debit does not exist, both keys
// are nil.
func existsDebit(ns walletdb.ReadBucket, txHash *chainhash.Hash, index uint32, block *Block) (k, credKey []byte, err error) {
	k = keyDebit(txHash, index, block)
	v := ns.NestedReadBucket(bucketDebits).Get(k)
	if v == nil {
		return nil, nil, nil
	}
	if len(v) < 80 {
		return nil, nil, errors.E(errors.IO, errors.Errorf("debit len %d", len(v)))
	}
	return k, v[8:80], nil
}

func existsInvalidatedDebit(ns walletdb.ReadBucket, txHash *chainhash.Hash, index uint32,
	block *Block) (k, credKey []byte, err error) {
	k = keyDebit(txHash, index, block)
	v := ns.NestedReadBucket(bucketStakeInvalidatedDebits).Get(k)
	if v == nil {
		return nil, nil, nil
	}
	if len(v) < 80 {
		return nil, nil, errors.E(errors.IO, errors.Errorf("debit len %d", len(v)))
	}
	return k, v[8:80], nil
}

func deleteRawDebit(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketDebits).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// debitIterator allows for in-order iteration of all debit records for a
// mined transaction.
//
// Example usage:
//
//   prefix := keyTxRecord(txHash, block)
//   it := makeDebitIterator(ns, prefix)
//   for it.next() {
//           // Use it.elem
//           // If necessary, read additional details from it.ck, it.cv
//   }
//   if it.err != nil {
//           // Handle error
//   }
type debitIterator struct {
	c      walletdb.ReadWriteCursor // Set to nil after final iteration
	prefix []byte
	ck     []byte
	cv     []byte
	elem   DebitRecord
	err    error
}

func makeReadDebitIterator(ns walletdb.ReadBucket, prefix []byte) debitIterator {
	c := ns.NestedReadBucket(bucketDebits).ReadCursor()
	return debitIterator{c: readCursor{c}, prefix: prefix}
}

func (it *debitIterator) readElem() error {
	if len(it.ck) < 72 {
		return errors.E(errors.IO, errors.Errorf("debit key len %d", len(it.ck)))
	}
	if len(it.cv) < 80 {
		return errors.E(errors.IO, errors.Errorf("debit len %d", len(it.cv)))
	}
	it.elem.Index = byteOrder.Uint32(it.ck[68:72])
	it.elem.Amount = dcrutil.Amount(byteOrder.Uint64(it.cv))
	return nil
}

func (it *debitIterator) next() bool {
	if it.c == nil {
		return false
	}

	if it.ck == nil {
		it.ck, it.cv = it.c.Seek(it.prefix)
	} else {
		it.ck, it.cv = it.c.Next()
	}
	if !bytes.HasPrefix(it.ck, it.prefix) {
		it.c.Close()
		it.c = nil
		return false
	}

	err := it.readElem()
	if err != nil {
		it.err = err
		return false
	}
	return true
}

func (it *debitIterator) close() {
	if it.c == nil {
		return
	}
	it.c.Close()
}

// All unmined transactions are saved in the unmined bucket keyed by the
// transaction hash.  The value matches that of mined transaction records:
//
//   [0:8]   Received time (8 bytes)
//   [8:]    Serialized transaction (varies)

func putRawUnmined(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnmined).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func readRawUnminedHash(k []byte, txHash *chainhash.Hash) error {
	if len(k) < 32 {
		return errors.E(errors.IO, errors.Errorf("unmined key len %d", len(k)))
	}
	copy(txHash[:], k)
	return nil
}

func existsRawUnmined(ns walletdb.ReadBucket, k []byte) (v []byte) {
	return ns.NestedReadBucket(bucketUnmined).Get(k)
}

func deleteRawUnmined(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnmined).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func fetchRawUnminedReceiveTime(v []byte) (int64, error) {
	if len(v) < 8 {
		return 0, errors.E(errors.IO, errors.Errorf("unmined len %d", len(v)))
	}
	return int64(byteOrder.Uint64(v[:8])), nil
}

func extractRawUnminedTx(v []byte) []byte {
	return v[8:]
}

// Unmined transaction credits use the canonical serialization format:
//
//  [0:32]   Transaction hash (32 bytes)
//  [32:36]  Output index (4 bytes)
//
// The value matches the format used by mined credits, but the spent flag is
// never set and the optional debit record is never included.  The simplified
// format is thus:
//
//   [0:8]   Amount (8 bytes)
//   [8]     Flags (1 byte)
//             0x01: Unused
//             0x02: Change
//             0x1c: P2PKH stake flag
//                 0x00: None (translates to OP_NOP10)
//                 0x04: OP_SSTX
//                 0x08: OP_SSGEN
//                 0x0c: OP_SSRTX
//                 0x10: OP_SSTXCHANGE
//             0x20: IsCoinbase
//             0x40: HasExpiry
//   [9] Script type (P2PKH, P2SH, etc) and bit flag for account stored
//   [10:14] Byte index (4 bytes, uint32)
//   [14:18] Length of script (4 bytes, uint32)
//   [18:22] Account (4 bytes, uint32)
//
const (
	// unconfCreditKeySize is the total size of an unconfirmed credit
	// key in bytes.
	unconfCreditKeySize = 36

	// unconfValueSizeLegacy is the total size of an unconfirmed legacy
	// credit value in bytes (version 1).
	unconfValueSizeLegacy = 9

	// unconfValueSize is the total size of an unconfirmed credit
	// value in bytes (version 2).
	unconfValueSize = 22
)

func valueUnminedCredit(amount dcrutil.Amount, change bool, opCode uint8,
	IsCoinbase bool, hasExpiry bool, scrType scriptType, scrLoc uint32, scrLen uint32,
	account uint32, dbVersion uint32) []byte {
	v := make([]byte, unconfValueSize)
	byteOrder.PutUint64(v, uint64(amount))
	v[8] = condenseOpCode(opCode)
	if change {
		v[8] |= 1 << 1
	}
	if hasExpiry {
		switch {
		case dbVersion >= hasExpiryFixedVersion:
			v[8] |= 1 << 6
		case dbVersion >= hasExpiryVersion:
			v[8] |= 1 << 4
		}
	}
	if IsCoinbase {
		v[8] |= 1 << 5
	}

	v[9] = byte(scrType)
	v[9] |= accountExistsMask
	byteOrder.PutUint32(v[10:14], scrLoc)
	byteOrder.PutUint32(v[14:18], scrLen)
	byteOrder.PutUint32(v[18:22], account)

	return v
}

func putRawUnminedCredit(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnminedCredits).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func fetchRawUnminedCreditIndex(k []byte) (uint32, error) {
	if len(k) < unconfCreditKeySize {
		return 0, errors.E(errors.IO, errors.Errorf("unmined credit key len %d", len(k)))
	}
	return byteOrder.Uint32(k[32:36]), nil
}

func fetchRawUnminedCreditAmount(v []byte) (dcrutil.Amount, error) {
	if len(v) < unconfValueSizeLegacy {
		return 0, errors.E(errors.IO, errors.Errorf("unmined credit len %d", len(v)))
	}
	return dcrutil.Amount(byteOrder.Uint64(v)), nil
}

func fetchRawUnminedCreditAmountChange(v []byte) (dcrutil.Amount, bool, error) {
	if len(v) < unconfValueSizeLegacy {
		return 0, false, errors.E(errors.IO, errors.Errorf("unmined credit len %d", len(v)))
	}
	amt := dcrutil.Amount(byteOrder.Uint64(v))
	change := v[8]&(1<<1) != 0
	return amt, change, nil
}

func fetchRawUnminedCreditTagOpcode(v []byte) uint8 {
	return (((v[8] >> 2) & 0x07) + 0xb9)
}

func fetchRawUnminedCreditTagIsCoinbase(v []byte) bool {
	return v[8]&(1<<5) != 0
}

func fetchRawUnminedCreditScriptType(v []byte) scriptType {
	if len(v) < unconfValueSize {
		return scriptTypeNonexisting
	}
	return scriptType(v[9] & ^accountExistsMask)
}

func fetchRawUnminedCreditScriptOffset(v []byte) uint32 {
	if len(v) < unconfValueSize {
		return 0
	}
	return byteOrder.Uint32(v[10:14])
}

func fetchRawUnminedCreditScriptLength(v []byte) uint32 {
	if len(v) < unconfValueSize {
		return 0
	}
	return byteOrder.Uint32(v[14:18])
}

func fetchRawUnminedCreditAccount(v []byte) (uint32, error) {
	if len(v) < unconfValueSize {
		return 0, errors.E(errors.IO, errors.Errorf("unmined credit len %d", len(v)))
	}

	// Was the account ever set?
	if v[9]&accountExistsMask != accountExistsMask {
		return 0, errors.E(errors.IO, "unmined credit account unset")
	}

	return byteOrder.Uint32(v[18:22]), nil
}

func existsRawUnminedCredit(ns walletdb.ReadBucket, k []byte) []byte {
	return ns.NestedReadBucket(bucketUnminedCredits).Get(k)
}

func deleteRawUnminedCredit(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnminedCredits).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// unminedCreditIterator allows for cursor iteration over all credits, in order,
// from a single unmined transaction.
//
//  Example usage:
//
//   it := makeUnminedCreditIterator(ns, txHash)
//   for it.next() {
//           // Use it.elem, it.ck and it.cv
//           // Optionally, use it.delete() to remove this k/v pair
//   }
//   if it.err != nil {
//           // Handle error
//   }
//
// The spentness of the credit is not looked up for performance reasons (because
// for unspent credits, it requires another lookup in another bucket).  If this
// is needed, it may be checked like this:
//
//   spent := existsRawUnminedInput(ns, it.ck) != nil
type unminedCreditIterator struct {
	c         walletdb.ReadWriteCursor
	dbVersion uint32
	prefix    []byte
	ck        []byte
	cv        []byte
	elem      CreditRecord
	err       error
}

type readCursor struct {
	walletdb.ReadCursor
}

func (r readCursor) Delete() error {
	return errors.E(errors.IO, "delete called from read-only cursor")
}

func makeReadUnminedCreditIterator(ns walletdb.ReadBucket, txHash *chainhash.Hash, dbVersion uint32) unminedCreditIterator {
	c := ns.NestedReadBucket(bucketUnminedCredits).ReadCursor()
	return unminedCreditIterator{c: readCursor{c}, prefix: txHash[:], dbVersion: dbVersion}
}

func (it *unminedCreditIterator) readElem() error {
	index, err := fetchRawUnminedCreditIndex(it.ck)
	if err != nil {
		return err
	}
	amount, change, err := fetchRawUnminedCreditAmountChange(it.cv)
	if err != nil {
		return err
	}

	it.elem.Index = index
	it.elem.Amount = amount
	it.elem.Change = change
	it.elem.HasExpiry = fetchRawCreditHasExpiry(it.cv, it.dbVersion)
	// Spent intentionally not set

	return nil
}

func (it *unminedCreditIterator) next() bool {
	if it.c == nil {
		return false
	}

	if it.ck == nil {
		it.ck, it.cv = it.c.Seek(it.prefix)
	} else {
		it.ck, it.cv = it.c.Next()
	}
	if !bytes.HasPrefix(it.ck, it.prefix) {
		it.c.Close()
		it.c = nil
		return false
	}

	err := it.readElem()
	if err != nil {
		it.err = err
		return false
	}
	return true
}

// unavailable until https://github.com/boltdb/bolt/issues/620 is fixed.
// func (it *unminedCreditIterator) delete() error {
// 	err := it.c.Delete()
// 	if err != nil {
// 		return errors.E(errors.IO, err)
// 	}
// 	return nil
// }

func (it *unminedCreditIterator) reposition(txHash *chainhash.Hash, index uint32) {
	it.c.Seek(canonicalOutPoint(txHash, index))
}

func (it *unminedCreditIterator) close() {
	if it.c == nil {
		return
	}
	it.c.Close()
}

// OutPoints spent by unmined transactions are saved in the unmined inputs
// bucket.  This bucket maps between each previous output spent, for both mined
// and unmined transactions, to the hash of the unmined transaction.
//
// The key is serialized as such:
//
//   [0:32]   Transaction hash (32 bytes)
//   [32:36]  Output index (4 bytes)
//
// The value is serialized as such:
//
//   [0:32]   Transaction hash (32 bytes)

func putRawUnminedInput(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnminedInputs).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func existsRawUnminedInput(ns walletdb.ReadBucket, k []byte) (v []byte) {
	return ns.NestedReadBucket(bucketUnminedInputs).Get(k)
}

func deleteRawUnminedInput(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketUnminedInputs).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func readRawUnminedInputSpenderHash(v []byte, hash *chainhash.Hash) {
	copy(hash[:], v[:32])
}

// Ticket purchase metadata is recorded in the tickets bucket.  The bucket key
// is the ticket purchase transaction hash.  The value is serialized as such:
//
//   [0:4]		Block height ticket was picked (-1 if not picked)

func valueTicketRecord(pickedHeight int32) []byte {
	v := make([]byte, 4)
	byteOrder.PutUint32(v, uint32(pickedHeight))
	return v
}

func putRawTicketRecord(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketTickets).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func putTicketRecord(ns walletdb.ReadWriteBucket, ticketHash *chainhash.Hash, pickedHeight int32) error {
	k := ticketHash[:]
	v := valueTicketRecord(pickedHeight)
	return putRawTicketRecord(ns, k, v)
}

func existsRawTicketRecord(ns walletdb.ReadBucket, k []byte) (v []byte) {
	return ns.NestedReadBucket(bucketTickets).Get(k)
}

func extractRawTicketPickedHeight(v []byte) int32 {
	return int32(byteOrder.Uint32(v))
}

// Tx scripts are stored as the raw serialized script. The key in the database
// for the TxScript itself is the hash160 of the script.
func keyTxScript(script []byte) []byte {
	return dcrutil.Hash160(script)
}

func putTxScript(ns walletdb.ReadWriteBucket, script []byte) error {
	k := keyTxScript(script)
	err := ns.NestedReadWriteBucket(bucketScripts).Put(k, script)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func existsTxScript(ns walletdb.ReadBucket, hash []byte) []byte {
	vOrig := ns.NestedReadBucket(bucketScripts).Get(hash)
	if vOrig == nil {
		return nil
	}
	v := make([]byte, len(vOrig))
	copy(v, vOrig)
	return v
}

// The multisig bucket stores utxos that are P2SH output scripts to the user.
// These are handled separately and less efficiently than the more typical
// P2PKH types.
// Transactions with multisig outputs are keyed to serialized outpoints:
// [0:32]    Hash (32 bytes)
// [32:36]   Index (uint32)
//
// The value is the following:
// [0:20]    P2SH Hash (20 bytes)
// [20]      m (in m-of-n) (uint8)
// [21]      n (in m-of-n) (uint8)
// [22]      Flags (1 byte)
//           [0]: Spent
//           [1]: Tree
// [23:55]   Block hash (32 byte hash)
// [55:59]   Block height (uint32)
// [59:67]   Amount (int64)
// [67:99]   SpentBy (32 byte hash)
// [99:103]  SpentByIndex (uint32)
// [103:135] TxHash (32 byte hash)
//
// The structure is set up so that the user may easily spend from any unspent
// P2SH multisig outpoints they own an address in.
func keyMultisigOut(hash chainhash.Hash, index uint32) []byte {
	return canonicalOutPoint(&hash, index)
}

func valueMultisigOut(sh [ripemd160.Size]byte, m uint8, n uint8,
	spent bool, tree int8, blockHash chainhash.Hash,
	blockHeight uint32, amount dcrutil.Amount, spentBy chainhash.Hash,
	sbi uint32, txHash chainhash.Hash) []byte {
	v := make([]byte, 135)

	copy(v[0:20], sh[0:20])
	v[20] = m
	v[21] = n
	v[22] = uint8(0)

	if spent {
		v[22] |= 1 << 0
	}

	if tree == wire.TxTreeStake {
		v[22] |= 1 << 1
	}

	copy(v[23:55], blockHash[:])
	byteOrder.PutUint32(v[55:59], blockHeight)
	byteOrder.PutUint64(v[59:67], uint64(amount))

	copy(v[67:99], spentBy[:])
	byteOrder.PutUint32(v[99:103], sbi)

	copy(v[103:135], txHash[:])

	return v
}

func fetchMultisigOut(k, v []byte) (*MultisigOut, error) {
	if len(k) != 36 {
		return nil, errors.E(errors.IO, "multisig output key len %d", len(k))
	}
	if len(v) != 135 {
		return nil, errors.E(errors.IO, "multisig output len %d", len(v))
	}

	var mso MultisigOut

	var op wire.OutPoint
	err := readCanonicalOutPoint(k, &op)
	if err != nil {
		return nil, err
	}
	mso.OutPoint = &op
	mso.OutPoint.Tree = wire.TxTreeRegular

	copy(mso.ScriptHash[0:20], v[0:20])

	mso.M = v[20]
	mso.N = v[21]
	mso.Spent = v[22]&(1<<0) != 0
	mso.Tree = 0
	isStakeTree := v[22]&(1<<1) != 0
	if isStakeTree {
		mso.Tree = 1
	}

	copy(mso.BlockHash[0:32], v[23:55])
	mso.BlockHeight = byteOrder.Uint32(v[55:59])
	mso.Amount = dcrutil.Amount(byteOrder.Uint64(v[59:67]))

	copy(mso.SpentBy[0:32], v[67:99])
	mso.SpentByIndex = byteOrder.Uint32(v[99:103])

	copy(mso.TxHash[0:32], v[103:135])

	return &mso, nil
}

func fetchMultisigOutScrHash(v []byte) [ripemd160.Size]byte {
	var sh [ripemd160.Size]byte
	copy(sh[0:20], v[0:20])
	return sh
}

func fetchMultisigOutMN(v []byte) (uint8, uint8) {
	return v[20], v[21]
}

func fetchMultisigOutSpent(v []byte) bool {
	spent := v[22]&(1<<0) != 0

	return spent
}

func fetchMultisigOutTree(v []byte) int8 {
	isStakeTree := v[22]&(1<<1) != 0
	tree := wire.TxTreeRegular
	if isStakeTree {
		tree = wire.TxTreeStake
	}

	return tree
}

func fetchMultisigOutSpentVerbose(v []byte) (bool, chainhash.Hash, uint32) {
	spent := v[22]&(1<<0) != 0
	spentBy := chainhash.Hash{}
	copy(spentBy[0:32], v[67:99])
	spentIndex := byteOrder.Uint32(v[99:103])

	return spent, spentBy, spentIndex
}

func fetchMultisigOutMined(v []byte) (chainhash.Hash, uint32) {
	blockHash := chainhash.Hash{}
	copy(blockHash[0:32], v[23:55])
	blockHeight := byteOrder.Uint32(v[55:59])

	return blockHash, blockHeight
}

func fetchMultisigOutAmount(v []byte) dcrutil.Amount {
	return dcrutil.Amount(byteOrder.Uint64(v[59:67]))
}

func setMultisigOutSpent(v []byte, spendHash chainhash.Hash, spendIndex uint32) {
	spentByte := uint8(0)
	spentByte |= 1 << 0
	v[22] = spentByte
	copy(v[67:99], spendHash[:])
	byteOrder.PutUint32(v[99:103], spendIndex)
}

func setMultisigOutUnSpent(v []byte) {
	empty := chainhash.Hash{}
	spentByte := uint8(0)
	v[22] = spentByte
	copy(v[67:98], empty[:])
	byteOrder.PutUint32(v[99:103], 0xFFFFFFFF)
}

func setMultisigOutMined(v []byte, blockHash chainhash.Hash,
	blockHeight uint32) {
	copy(v[23:55], blockHash[:])
	byteOrder.PutUint32(v[55:59], blockHeight)
}

func setMultisigOutUnmined(v []byte) {
	empty := chainhash.Hash{}
	copy(v[23:55], empty[:])
	byteOrder.PutUint32(v[55:59], 0)
}

func putMultisigOutRawValues(ns walletdb.ReadWriteBucket, k []byte, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketMultisig).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func existsMultisigOut(ns walletdb.ReadBucket, k []byte) []byte {
	return ns.NestedReadBucket(bucketMultisig).Get(k)
}

func existsMultisigOutCopy(ns walletdb.ReadBucket, k []byte) []byte {
	vOrig := ns.NestedReadBucket(bucketMultisig).Get(k)
	if vOrig == nil {
		return nil
	}
	v := make([]byte, 135)
	copy(v, vOrig)
	return v
}

func putMultisigOutUS(ns walletdb.ReadWriteBucket, k []byte) error {
	blank := []byte{0x00}
	err := ns.NestedReadWriteBucket(bucketMultisigUsp).Put(k, blank)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func deleteMultisigOutUS(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketMultisigUsp).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func existsMultisigOutUS(ns walletdb.ReadBucket, k []byte) bool {
	v := ns.NestedReadBucket(bucketMultisigUsp).Get(k)
	return v != nil
}

// The compact filter bucket stores serialized regular compact filters for
// blocks.  This bucket was added during a database upgrade, but the actual
// filters are not saved during the upgrade and an additional step to save all
// compact filters for main chain blocks is required before any additional
// blocks can be processed.
//
// Compact filters are keyed by their block hash.  The value is the serialized
// filter.

func putRawCFilter(ns walletdb.ReadWriteBucket, k, v []byte) error {
	return ns.NestedReadWriteBucket(bucketCFilters).Put(k, v)
}

func fetchRawCFilter(ns walletdb.ReadBucket, k []byte) ([]byte, error) {
	v := ns.NestedReadBucket(bucketCFilters).Get(k)
	if v == nil {
		var hash chainhash.Hash
		copy(hash[:], k)
		return nil, errors.E(errors.NotExist, errors.Errorf("no cfilter saved for block %v", &hash))
	}
	return v, nil
}

// The ticket commitments bucket stores serialized information about ticket
// commitment outputs which belong to the wallet.
//
// The keys in this bucket use the canonicalOutPoint format (that is,
// transaction hash and output index) of the corresponding ticket commitment.
//
//   [0:32]  Transaction hash (32 bytes)
//   [32:36] Output index (4 bytes)
//
// Values in this bucket are serialized using the following format:
//
//   [0:8]   Amount (8 bytes)
//   [8:12]  Account Index (4 bytes)

func keyTicketCommitment(ticketHash chainhash.Hash, index uint32) []byte {
	return canonicalOutPoint(&ticketHash, index)
}

func valueTicketCommitment(amount dcrutil.Amount, account uint32) []byte {
	v := make([]byte, 12)
	byteOrder.PutUint64(v, uint64(amount))
	byteOrder.PutUint32(v[8:], account)
	return v
}

func existsRawTicketCommitment(ns walletdb.ReadBucket, k []byte) []byte {
	return ns.NestedReadBucket(bucketTicketCommitments).Get(k)
}

func putRawTicketCommitment(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketTicketCommitments).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func fetchRawTicketCommitmentAmount(v []byte) (dcrutil.Amount, error) {
	if len(v) < 8 {
		return 0, errors.E(errors.IO, errors.Errorf("ticket commitment amount %d", len(v)))
	}
	return dcrutil.Amount(byteOrder.Uint64(v)), nil
}

func fetchRawTicketCommitmentAccount(v []byte) (uint32, error) {
	if len(v) < 12 {
		return 0, errors.E(errors.IO, errors.Errorf("ticket commitment acount %d", len(v)))
	}
	return byteOrder.Uint32(v[8:]), nil
}

func deleteRawTicketCommitment(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketTicketCommitments).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// The Unspent Ticket Commitment bucket stores serialized information about
// ticket commitments owned by the wallet that are still outstanding in live
// tickets. This is meant to work as an index for fast balance calculations and
// to be used in concert with the ticket commitment bucket.
//
// The keys in this bucket use the same keys as the corresponding element in the
// bucketTicketCommitment.
//
// Values in this bucket are serialized using the following format:
//
//   [0]  Flags (1 byte)
//             0x01: UnminedSpent (whether there is an unmined tx redeeming this commitment)

func valueUnspentTicketCommitment(unminedSpent bool) []byte {
	flags := uint8(0)
	if unminedSpent {
		flags |= 0x01
	}

	v := make([]byte, 1)
	v[0] = flags

	return v
}

func putRawUnspentTicketCommitment(ns walletdb.ReadWriteBucket, k, v []byte) error {
	err := ns.NestedReadWriteBucket(bucketTicketCommitmentsUsp).Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func deleteRawUnspentTicketCommitment(ns walletdb.ReadWriteBucket, k []byte) error {
	err := ns.NestedReadWriteBucket(bucketTicketCommitmentsUsp).Delete(k)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

type unspentTicketCommitsIterator struct {
	c  walletdb.ReadCursor
	ns walletdb.ReadBucket
	ck []byte

	unminedSpent bool
	amount       dcrutil.Amount
	account      uint32

	err error
}

func makeUnspentTicketCommitsIterator(ns walletdb.ReadBucket) *unspentTicketCommitsIterator {
	c := ns.NestedReadBucket(bucketTicketCommitmentsUsp).ReadCursor()
	return &unspentTicketCommitsIterator{c: c, ns: ns}
}

func (it *unspentTicketCommitsIterator) next() bool {
	if it.c == nil {
		return false
	}

	var v []byte

	if it.ck == nil {
		it.ck, v = it.c.Seek(nil)
	} else {
		it.ck, v = it.c.Next()
	}

	if it.ck == nil {
		it.c.Close()
		it.c = nil
		return false
	}

	if len(v) < 1 {
		it.err = errors.E(errors.IO, errors.Errorf("wrong unspent ticket commitment len %d", len(v)))
		return false
	}

	it.unminedSpent = v[0]&0x01 == 0x01

	// Fetch the original commitment amount and account.
	v = existsRawTicketCommitment(it.ns, it.ck)
	it.account, it.err = fetchRawTicketCommitmentAccount(v)
	if it.err != nil {
		return false
	}
	it.amount, it.err = fetchRawTicketCommitmentAmount(v)
	if it.err != nil {
		return false
	}

	return true
}

func (it *unspentTicketCommitsIterator) close() {
	if it.c == nil {
		return
	}
	it.c.Close()
}

// createStore creates the tx store (with the latest db version) in the passed
// namespace.
func createStore(ns walletdb.ReadWriteBucket, chainParams *chaincfg.Params) error {
	// Ensure that nothing currently exists in the namespace bucket.
	c := ns.ReadCursor()
	defer c.Close()
	ck, cv := c.First()
	if ck != nil || cv != nil {
		return errors.E(errors.IO, "bucket is not empty")
	}

	// Save the creation date of the store.
	v := make([]byte, 8)
	byteOrder.PutUint64(v, uint64(time.Now().Unix()))
	err := ns.Put(rootCreateDate, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Write a zero balance.
	v = make([]byte, 8)
	err = ns.Put(rootMinedBalance, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketBlocks)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketHeaders)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketTxRecords)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketCredits)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketDebits)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketUnspent)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketUnmined)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketUnminedCredits)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketUnminedInputs)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketScripts)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketMultisig)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketMultisigUsp)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketStakeInvalidatedCredits)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	_, err = ns.CreateBucket(bucketStakeInvalidatedDebits)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Insert the genesis block header.
	var serializedGenesisBlock RawBlockHeader
	buf := bytes.NewBuffer(serializedGenesisBlock[:0])
	err = chainParams.GenesisBlock.Header.Serialize(buf)
	if err != nil {
		// we have bigger problems.
		panic(err)
	}
	err = putRawBlockHeader(ns, keyBlockHeader(&chainParams.GenesisHash),
		serializedGenesisBlock[:])
	if err != nil {
		return err
	}

	// Insert block record for the genesis block.
	genesisBlockKey := keyBlockRecord(0)
	genesisBlockVal := valueBlockRecordEmptyFromHeader(
		chainParams.GenesisHash[:], serializedGenesisBlock[:])
	err = putRawBlockRecord(ns, genesisBlockKey, genesisBlockVal)
	if err != nil {
		return err
	}

	// Mark the genesis block as the tip block.
	err = ns.Put(rootTipBlock, chainParams.GenesisHash[:])
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

// upgradeTxDB performs any necessary upgrades to the transaction history
// contained in the wallet database, namespaced by the top level bucket key
// namespaceKey.
func upgradeTxDB(ns walletdb.ReadWriteBucket, chainParams *chaincfg.Params) error {
	v := ns.Get(rootVersion)
	if len(v) != 4 {
		return errors.E(errors.IO, "no transaction store version")
	}
	version := byteOrder.Uint32(v)

	// Versions start at 1, 0 is an error.
	if version == 0 {
		return errors.E(errors.IO, "invalid transaction store version 0")
	}

	// Perform version upgrades as necessary.
	for {
		var err error
		switch version {
		case 1:
			err = upgradeToVersion2(ns)
		case 2:
			err = upgradeToVersion3(ns, chainParams)
		default: // >= 3
			return nil
		}
		if err != nil {
			return err
		}
		version++
	}
}

// upgradeToVersion2 upgrades the transaction store from version 1 to version 2.
// This must only be called after the caller has asserted the database is
// currently at version 1.  This upgrade is only a version bump as the new DB
// format is forwards compatible with version 1, but old software that does not
// know about version 2 should not be opening the upgraded DBs.
func upgradeToVersion2(ns walletdb.ReadWriteBucket) error {
	versionBytes := make([]byte, 4)
	byteOrder.PutUint32(versionBytes, 2)
	err := ns.Put(rootVersion, versionBytes)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// upgradeToVersion3 performs an upgrade from version 2 to 3.  The store must
// already be at version 2.
//
// This update adds a new nested bucket in the namespace bucket for block
// headers and a new namespace k/v pair for the current tip block.
//
// Headers, except for the genesis block, are not immediately filled in during
// the upgrade (the information is not available) but should be inserted by the
// caller along with setting the best block.  Some features now require headers
// to be saved, so if this step is skipped the store will not operate correctly.
//
// In addition to the headers, an additional byte is added to the block record
// values at position 42, between the vote bits and the number of
// transactions.  This byte is used as a boolean and records whether or not the
// block has been stake invalidated by the next block in the main chain.
func upgradeToVersion3(ns walletdb.ReadWriteBucket, chainParams *chaincfg.Params) error {
	versionBytes := make([]byte, 4)
	byteOrder.PutUint32(versionBytes, 3)
	err := ns.Put(rootVersion, versionBytes)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(bucketHeaders)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	_, err = ns.CreateBucket(bucketStakeInvalidatedCredits)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	_, err = ns.CreateBucket(bucketStakeInvalidatedDebits)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// For all block records, add the byte for marking stake invalidation.  The
	// function passed to ForEach may not modify the bucket, so record all
	// values and write the updates outside the ForEach.
	type kvpair struct{ k, v []byte }
	var blockRecsToUpgrade []kvpair
	blockRecordsBucket := ns.NestedReadWriteBucket(bucketBlocks)
	err = blockRecordsBucket.ForEach(func(k, v []byte) error {
		blockRecsToUpgrade = append(blockRecsToUpgrade, kvpair{k, v})
		return nil
	})
	if err != nil {
		return errors.E(errors.IO, err)
	}
	for _, kvp := range blockRecsToUpgrade {
		v := make([]byte, len(kvp.v)+1)
		copy(v, kvp.v[:42])
		copy(v[43:], kvp.v[42:])
		err = blockRecordsBucket.Put(kvp.k, v)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	// Insert the genesis block header.
	var serializedGenesisBlock RawBlockHeader
	buf := bytes.NewBuffer(serializedGenesisBlock[:0])
	err = chainParams.GenesisBlock.Header.Serialize(buf)
	if err != nil {
		// we have bigger problems.
		panic(err)
	}
	err = putRawBlockHeader(ns, keyBlockHeader(&chainParams.GenesisHash),
		serializedGenesisBlock[:])
	if err != nil {
		return err
	}

	// Insert block record for the genesis block if one doesn't yet exist.
	genesisBlockKey, genesisBlockVal := existsBlockRecord(ns, 0)
	if genesisBlockVal == nil {
		genesisBlockVal = valueBlockRecordEmptyFromHeader(
			chainParams.GenesisHash[:], serializedGenesisBlock[:])
		err = putRawBlockRecord(ns, genesisBlockKey, genesisBlockVal)
		if err != nil {
			return err
		}
	}

	// Mark the genesis block as the tip block.  It would not be a good idea
	// to find a newer tip block from the mined blocks bucket since headers
	// are still missing and the latest recorded block is unlikely to be the
	// actual block the wallet was marked in sync with anyways (that
	// information was saved in waddrmgr).
	err = ns.Put(rootTipBlock, chainParams.GenesisHash[:])
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}
