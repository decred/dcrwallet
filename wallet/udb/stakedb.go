// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

const (
	// Size of various types in bytes.
	int8Size  = 1
	int16Size = 2
	int32Size = 4
	int64Size = 8
	hashSize  = 32

	// stakePoolUserTicketSize is the size
	// of a serialized stake pool user
	// ticket.
	// hash + uint32 + uint8 + uint32 + hash
	stakePoolUserTicketSize = 32 + 4 + 1 + 4 + 32

	// stakePoolTicketsPrefixSize is the length of
	// stakePoolTicketsPrefix.
	stakePoolTicketsPrefixSize = 5

	// stakePoolInvalidPrefixSize is the length of
	// stakePoolInvalidPrefix.
	stakePoolInvalidPrefixSize = 5

	// scriptHashLen is the length of a HASH160
	// hash.
	scriptHashSize = 20
)

var (
	// sstxTicket2PKHPrefix is the PkScript byte prefix for an SStx
	// P2PKH ticket output. The entire prefix is 0xba76a914, but we
	// only use the first 3 bytes.
	sstxTicket2PKHPrefix = []byte{0xba, 0x76, 0xa9}

	// sstxTicket2SHPrefix is the PkScript byte prefix for an SStx
	// P2SH ticket output.
	sstxTicket2SHPrefix = []byte{0xba, 0xa9, 0x14}

	// stakePoolTicketsPrefix is the byte slice prefix for valid
	// tickets in the stake pool for a given user.
	stakePoolTicketsPrefix = []byte("tickt")

	// stakePoolInvalidPrefix is the byte slice prefix for invalid
	// tickets in the stake pool for a given user.
	stakePoolInvalidPrefix = []byte("invld")
)

// Key names for various database fields.
// sstxRecords
//     key: sstx tx hash
//     val: sstxRecord
// ssgenRecords
//     key: sstx tx hash
//     val: serialized slice of ssgenRecords
//
var (
	// Bucket names.
	sstxRecordsBucketName  = []byte("sstxrecords")
	ssgenRecordsBucketName = []byte("ssgenrecords")
	ssrtxRecordsBucketName = []byte("ssrtxrecords")

	// Db related key names (main bucket).
	stakeStoreCreateDateName = []byte("stakestorecreated")
)

// deserializeSStxRecord deserializes the passed serialized tx record information.
func deserializeSStxRecord(serializedSStxRecord []byte, dbVersion uint32) (*sstxRecord, error) {
	switch {
	case dbVersion < 3:
		record := new(sstxRecord)

		curPos := 0

		// Read MsgTx size (as a uint64).
		msgTxLen := int(binary.LittleEndian.Uint64(
			serializedSStxRecord[curPos : curPos+int64Size]))
		curPos += int64Size

		// Pretend to read the pkScrLoc for the 0th output pkScript.
		curPos += int32Size

		// Read the intended voteBits and extended voteBits length (uint8).
		record.voteBitsSet = false
		voteBitsLen := int(serializedSStxRecord[curPos])
		if voteBitsLen != 0 {
			record.voteBitsSet = true
		}
		curPos += int8Size

		// Read the assumed 2 byte VoteBits as well as the extended
		// votebits (75 bytes max).
		record.voteBits = binary.LittleEndian.Uint16(
			serializedSStxRecord[curPos : curPos+int16Size])
		curPos += int16Size
		if voteBitsLen != 0 {
			record.voteBitsExt = make([]byte, voteBitsLen-int16Size)
			copy(record.voteBitsExt, serializedSStxRecord[curPos:curPos+voteBitsLen-int16Size])
		}
		curPos += stake.MaxSingleBytePushLength - int16Size

		// Prepare a buffer for the msgTx.
		buf := bytes.NewBuffer(serializedSStxRecord[curPos : curPos+msgTxLen])
		curPos += msgTxLen

		// Deserialize transaction.
		msgTx := new(wire.MsgTx)
		err := msgTx.Deserialize(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}

		// Create and save the dcrutil.Tx of the read MsgTx and set its index.
		tx := dcrutil.NewTx(msgTx)
		tx.SetIndex(dcrutil.TxIndexUnknown)
		tx.SetTree(wire.TxTreeStake)
		record.tx = tx

		// Read received unix time (int64).
		received := int64(binary.LittleEndian.Uint64(
			serializedSStxRecord[curPos : curPos+int64Size]))
		record.ts = time.Unix(received, 0)

		return record, nil

	case dbVersion >= 3:
		// Don't need to read the pkscript location, so first four bytes are
		// skipped.
		serializedSStxRecord = serializedSStxRecord[4:]

		var tx wire.MsgTx
		err := tx.Deserialize(bytes.NewReader(serializedSStxRecord))
		if err != nil {
			return nil, err
		}
		unixTime := int64(binary.LittleEndian.Uint64(serializedSStxRecord[tx.SerializeSize():]))
		return &sstxRecord{tx: dcrutil.NewTx(&tx), ts: time.Unix(unixTime, 0)}, nil

	default:
		panic("unreachable")
	}
}

// deserializeSStxTicketHash160 deserializes and returns a 20 byte script
// hash for a ticket's 0th output.
func deserializeSStxTicketHash160(serializedSStxRecord []byte, dbVersion uint32) (hash160 []byte, p2sh bool, err error) {
	var pkscriptLocOffset int
	var txOffset int
	switch {
	case dbVersion < 3:
		pkscriptLocOffset = 8 // After transaction size
		txOffset = 8 + 4 + 1 + stake.MaxSingleBytePushLength
	case dbVersion >= 3:
		pkscriptLocOffset = 0
		txOffset = 4
	}

	pkscriptLoc := int(binary.LittleEndian.Uint32(serializedSStxRecord[pkscriptLocOffset:])) + txOffset

	// Pop off the script prefix, then pop off the 20 bytes
	// HASH160 pubkey or script hash.
	prefixBytes := serializedSStxRecord[pkscriptLoc : pkscriptLoc+3]
	scriptHash := make([]byte, 20)
	p2sh = false
	switch {
	case bytes.Equal(prefixBytes, sstxTicket2PKHPrefix):
		scrHashLoc := pkscriptLoc + 4
		if scrHashLoc+20 >= len(serializedSStxRecord) {
			return nil, false, errors.E(errors.IO, "bad sstx record size")
		}
		copy(scriptHash, serializedSStxRecord[scrHashLoc:scrHashLoc+20])
	case bytes.Equal(prefixBytes, sstxTicket2SHPrefix):
		scrHashLoc := pkscriptLoc + 3
		if scrHashLoc+20 >= len(serializedSStxRecord) {
			return nil, false, errors.E(errors.IO, "bad sstx record size")
		}
		copy(scriptHash, serializedSStxRecord[scrHashLoc:scrHashLoc+20])
		p2sh = true
	}

	return scriptHash, p2sh, nil
}

// serializeSSTxRecord returns the serialization of the passed txrecord row.
func serializeSStxRecord(record *sstxRecord, dbVersion uint32) ([]byte, error) {
	switch {
	case dbVersion < 3:
		msgTx := record.tx.MsgTx()
		msgTxSize := int64(msgTx.SerializeSize())

		size := 0

		// tx tree is implicit (stake)

		// size of msgTx (recast to int64)
		size += int64Size

		// byte index of the ticket pk script
		size += int32Size

		// intended votebits length (uint8)
		size += int8Size

		// intended votebits (75 bytes)
		size += stake.MaxSingleBytePushLength

		// msgTx size is variable.
		size += int(msgTxSize)

		// timestamp (int64)
		size += int64Size

		buf := make([]byte, size)

		curPos := 0

		// Write msgTx size (as a uint64).
		binary.LittleEndian.PutUint64(buf[curPos:curPos+int64Size], uint64(msgTxSize))
		curPos += int64Size

		// Write the pkScript loc for the ticket output as a uint32.
		pkScrLoc := msgTx.PkScriptLocs()
		binary.LittleEndian.PutUint32(buf[curPos:curPos+int32Size], uint32(pkScrLoc[0]))
		curPos += int32Size

		// Write the intended votebits length (uint8). Hardcode the uint16
		// size for now.
		buf[curPos] = byte(int16Size + len(record.voteBitsExt))
		curPos += int8Size

		// Write the first two bytes for the intended votebits (75 bytes max),
		// then write the extended vote bits.
		binary.LittleEndian.PutUint16(buf[curPos:curPos+int16Size], record.voteBits)
		curPos += int16Size
		copy(buf[curPos:], record.voteBitsExt)
		curPos += stake.MaxSingleBytePushLength - 2

		// Serialize and write transaction.
		var b bytes.Buffer
		b.Grow(msgTx.SerializeSize())
		err := msgTx.Serialize(&b)
		if err != nil {
			return buf, err
		}
		copy(buf[curPos:curPos+int(msgTxSize)], b.Bytes())
		curPos += int(msgTxSize)

		// Write received unix time (int64).
		binary.LittleEndian.PutUint64(buf[curPos:curPos+int64Size], uint64(record.ts.Unix()))

		return buf, nil

	case dbVersion >= 3:
		tx := record.tx.MsgTx()
		txSize := tx.SerializeSize()

		buf := make([]byte, 4+txSize+8) // pkscript location + tx + unix timestamp
		pkScrLoc := tx.PkScriptLocs()
		binary.LittleEndian.PutUint32(buf, uint32(pkScrLoc[0]))
		err := tx.Serialize(bytes.NewBuffer(buf[4:4]))
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf[4+txSize:], uint64(record.ts.Unix()))
		return buf, nil

	default:
		panic("unreachable")
	}
}

// stakeStoreExists returns whether or not the stake store has already
// been created in the given database namespace.
func stakeStoreExists(ns walletdb.ReadBucket) bool {
	mainBucket := ns.NestedReadBucket(mainBucketName)
	return mainBucket != nil
}

// fetchSStxRecord retrieves a tx record from the sstx records bucket
// with the given hash.
func fetchSStxRecord(ns walletdb.ReadBucket, hash *chainhash.Hash, dbVersion uint32) (*sstxRecord, error) {
	bucket := ns.NestedReadBucket(sstxRecordsBucketName)

	key := hash[:]
	val := bucket.Get(key)
	if val == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no ticket purchase %v", hash))
	}

	return deserializeSStxRecord(val, dbVersion)
}

// fetchSStxRecordSStxTicketHash160 retrieves a ticket 0th output script or
// pubkeyhash from the sstx records bucket with the given hash.
func fetchSStxRecordSStxTicketHash160(ns walletdb.ReadBucket, hash *chainhash.Hash, dbVersion uint32) (hash160 []byte, p2sh bool, err error) {
	bucket := ns.NestedReadBucket(sstxRecordsBucketName)

	key := hash[:]
	val := bucket.Get(key)
	if val == nil {
		return nil, false, errors.E(errors.NotExist, errors.Errorf("no ticket purchase %v", hash))
	}

	return deserializeSStxTicketHash160(val, dbVersion)
}

// putSStxRecord inserts a given SStx record to the SStxrecords bucket.
func putSStxRecord(ns walletdb.ReadWriteBucket, record *sstxRecord, dbVersion uint32) error {
	bucket := ns.NestedReadWriteBucket(sstxRecordsBucketName)

	// Write the serialized txrecord keyed by the tx hash.
	serializedSStxRecord, err := serializeSStxRecord(record, dbVersion)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	err = bucket.Put(record.tx.Hash()[:], serializedSStxRecord)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// deserializeUserTicket deserializes the passed serialized user
// ticket information.
func deserializeUserTicket(serializedTicket []byte) (*PoolTicket, error) {
	// Cursory check to make sure that the size of the
	// ticket makes sense.
	if len(serializedTicket)%stakePoolUserTicketSize != 0 {
		return nil, errors.E(errors.IO, "invalid pool ticket record size")
	}

	record := new(PoolTicket)

	curPos := 0

	// Insert the ticket hash into the record.
	copy(record.Ticket[:], serializedTicket[curPos:curPos+hashSize])
	curPos += hashSize

	// Insert the ticket height into the record.
	record.HeightTicket = binary.LittleEndian.Uint32(
		serializedTicket[curPos : curPos+int32Size])
	curPos += int32Size

	// Insert the status into the record.
	record.Status = TicketStatus(serializedTicket[curPos])
	curPos += int8Size

	// Insert the spent by height into the record.
	record.HeightSpent = binary.LittleEndian.Uint32(
		serializedTicket[curPos : curPos+int32Size])
	curPos += int32Size

	// Insert the spending hash into the record.
	copy(record.SpentBy[:], serializedTicket[curPos:curPos+hashSize])

	return record, nil
}

// deserializeUserTickets deserializes the passed serialized pool
// users tickets information.
func deserializeUserTickets(serializedTickets []byte) ([]*PoolTicket, error) {
	// Cursory check to make sure that the number of records
	// makes sense.
	if len(serializedTickets)%stakePoolUserTicketSize != 0 {
		err := io.ErrUnexpectedEOF
		return nil, err
	}

	numRecords := len(serializedTickets) / stakePoolUserTicketSize

	records := make([]*PoolTicket, numRecords)

	// Loop through all the records, deserialize them, and
	// store them.
	for i := 0; i < numRecords; i++ {
		record, err := deserializeUserTicket(
			serializedTickets[i*stakePoolUserTicketSize : (i+
				1)*stakePoolUserTicketSize])
		if err != nil {
			return nil, err
		}

		records[i] = record
	}

	return records, nil
}

// serializeUserTicket returns the serialization of a single stake pool
// user ticket.
func serializeUserTicket(record *PoolTicket) []byte {
	buf := make([]byte, stakePoolUserTicketSize)

	curPos := 0

	// Write the ticket hash.
	copy(buf[curPos:curPos+hashSize], record.Ticket[:])
	curPos += hashSize

	// Write the ticket block height.
	binary.LittleEndian.PutUint32(buf[curPos:curPos+int32Size], record.HeightTicket)
	curPos += int32Size

	// Write the ticket status.
	buf[curPos] = byte(record.Status)
	curPos += int8Size

	// Write the spending height.
	binary.LittleEndian.PutUint32(buf[curPos:curPos+int32Size], record.HeightSpent)
	curPos += int32Size

	// Write the spending tx hash.
	copy(buf[curPos:curPos+hashSize], record.SpentBy[:])

	return buf
}

// serializeUserTickets returns the serialization of the passed stake pool
// user tickets slice.
func serializeUserTickets(records []*PoolTicket) []byte {
	numRecords := len(records)

	buf := make([]byte, numRecords*stakePoolUserTicketSize)

	// Serialize and write each record into the slice sequentially.
	for i := 0; i < numRecords; i++ {
		recordBytes := serializeUserTicket(records[i])

		copy(buf[i*stakePoolUserTicketSize:(i+1)*stakePoolUserTicketSize],
			recordBytes)
	}

	return buf
}

// fetchStakePoolUserTickets retrieves pool user tickets from the meta bucket with
// the given hash.
func fetchStakePoolUserTickets(ns walletdb.ReadBucket, scriptHash [20]byte) ([]*PoolTicket, error) {
	bucket := ns.NestedReadBucket(metaBucketName)

	key := make([]byte, stakePoolTicketsPrefixSize+scriptHashSize)
	copy(key[0:stakePoolTicketsPrefixSize], stakePoolTicketsPrefix)
	copy(key[stakePoolTicketsPrefixSize:stakePoolTicketsPrefixSize+scriptHashSize],
		scriptHash[:])
	val := bucket.Get(key)
	if val == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no ticket purchase for hash160 %x", &scriptHash))
	}

	return deserializeUserTickets(val)
}

// duplicateExistsInUserTickets checks to see if an exact duplicated of a
// record already exists in a slice of user ticket records.
func duplicateExistsInUserTickets(record *PoolTicket, records []*PoolTicket) bool {
	for _, r := range records {
		if *r == *record {
			return true
		}
	}
	return false
}

// recordExistsInUserTickets checks to see if a record already exists
// in a slice of user ticket records. If it does exist, it returns
// the location where it exists in the slice.
func recordExistsInUserTickets(record *PoolTicket, records []*PoolTicket) (bool, int) {
	for i, r := range records {
		if r.Ticket == record.Ticket {
			return true, i
		}
	}
	return false, 0
}

// updateStakePoolUserTickets updates a database entry for a pool user's tickets.
// The function pulls the current entry in the database, checks to see if the
// ticket is already there, updates it accordingly, or adds it to the list of
// tickets.
func updateStakePoolUserTickets(ns walletdb.ReadWriteBucket, scriptHash [20]byte, record *PoolTicket) error {
	// Fetch the current content of the key.
	// Possible buggy behaviour: If deserialization fails,
	// we won't detect it here. We assume we're throwing
	// ErrPoolUserTicketsNotFound.
	oldRecords, _ := fetchStakePoolUserTickets(ns, scriptHash)

	// Don't reinsert duplicate records we already have.
	if duplicateExistsInUserTickets(record, oldRecords) {
		return nil
	}

	// Does this modify an old record? If so, modify the record
	// itself and push. Otherwise, we need to insert a new
	// record.
	var records []*PoolTicket
	preExists, loc := recordExistsInUserTickets(record, oldRecords)
	if preExists {
		records = oldRecords
		records[loc] = record
	} else {
		// Either create a slice if currently nothing exists for this
		// key in the db, or append the entry to the slice.
		if oldRecords == nil {
			records = make([]*PoolTicket, 1)
			records[0] = record
		} else {
			records = append(oldRecords, record)
		}
	}

	bucket := ns.NestedReadWriteBucket(metaBucketName)
	key := make([]byte, stakePoolTicketsPrefixSize+scriptHashSize)
	copy(key[0:stakePoolTicketsPrefixSize], stakePoolTicketsPrefix)
	copy(key[stakePoolTicketsPrefixSize:stakePoolTicketsPrefixSize+scriptHashSize],
		scriptHash[:])

	// Write the serialized ticket data keyed by the script.
	serializedRecords := serializeUserTickets(records)

	err := bucket.Put(key, serializedRecords)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// deserializeUserInvalTickets deserializes the passed serialized pool
// users invalid tickets information.
func deserializeUserInvalTickets(serializedTickets []byte) ([]*chainhash.Hash, error) {
	// Cursory check to make sure that the number of records
	// makes sense.
	if len(serializedTickets)%chainhash.HashSize != 0 {
		err := io.ErrUnexpectedEOF
		return nil, err
	}

	numRecords := len(serializedTickets) / chainhash.HashSize

	records := make([]*chainhash.Hash, numRecords)

	// Loop through all the ssgen records, deserialize them, and
	// store them.
	for i := 0; i < numRecords; i++ {
		start := i * chainhash.HashSize
		end := (i + 1) * chainhash.HashSize
		h, err := chainhash.NewHash(serializedTickets[start:end])
		if err != nil {
			return nil, err
		}

		records[i] = h
	}

	return records, nil
}

// serializeUserInvalTickets returns the serialization of the passed stake pool
// invalid user tickets slice.
func serializeUserInvalTickets(records []*chainhash.Hash) []byte {
	numRecords := len(records)

	buf := make([]byte, numRecords*chainhash.HashSize)

	// Serialize and write each record into the slice sequentially.
	for i := 0; i < numRecords; i++ {
		start := i * chainhash.HashSize
		end := (i + 1) * chainhash.HashSize
		copy(buf[start:end], records[i][:])
	}

	return buf
}

// fetchStakePoolUserInvalTickets retrieves the list of invalid pool user tickets
// from the meta bucket with the given hash.
func fetchStakePoolUserInvalTickets(ns walletdb.ReadBucket, scriptHash [20]byte) ([]*chainhash.Hash, error) {
	bucket := ns.NestedReadBucket(metaBucketName)

	key := make([]byte, stakePoolInvalidPrefixSize+scriptHashSize)
	copy(key[0:stakePoolInvalidPrefixSize], stakePoolInvalidPrefix)
	copy(key[stakePoolInvalidPrefixSize:stakePoolInvalidPrefixSize+scriptHashSize],
		scriptHash[:])
	val := bucket.Get(key)
	if val == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no pool ticket for hash160 %x", &scriptHash))
	}

	return deserializeUserInvalTickets(val)
}

// duplicateExistsInInvalTickets checks to see if an exact duplicated of a
// record already exists in a slice of invalid user ticket records.
func duplicateExistsInInvalTickets(record *chainhash.Hash, records []*chainhash.Hash) bool {
	for _, r := range records {
		if *r == *record {
			return true
		}
	}
	return false
}

// removeStakePoolInvalUserTickets removes the ticket hash from the inval
// ticket bucket.
func removeStakePoolInvalUserTickets(ns walletdb.ReadWriteBucket, scriptHash [20]byte, record *chainhash.Hash) error {
	// Fetch the current content of the key.
	// Possible buggy behaviour: If deserialization fails,
	// we won't detect it here. We assume we're throwing
	// ErrPoolUserInvalTcktsNotFound.
	oldRecords, _ := fetchStakePoolUserInvalTickets(ns, scriptHash)

	// Don't need to remove records that don't exist.
	if !duplicateExistsInInvalTickets(record, oldRecords) {
		return nil
	}

	var newRecords []*chainhash.Hash
	for i := range oldRecords {
		if record.IsEqual(oldRecords[i]) {
			newRecords = append(oldRecords[:i:i], oldRecords[i+1:]...)
		}
	}

	if newRecords == nil {
		return nil
	}

	bucket := ns.NestedReadWriteBucket(metaBucketName)
	key := make([]byte, stakePoolInvalidPrefixSize+scriptHashSize)
	copy(key[0:stakePoolInvalidPrefixSize], stakePoolInvalidPrefix)
	copy(key[stakePoolInvalidPrefixSize:stakePoolInvalidPrefixSize+scriptHashSize],
		scriptHash[:])

	// Write the serialized invalid user ticket hashes.
	serializedRecords := serializeUserInvalTickets(newRecords)

	err := bucket.Put(key, serializedRecords)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

// updateStakePoolInvalUserTickets updates a database entry for a pool user's
// invalid tickets. The function pulls the current entry in the database,
// checks to see if the ticket is already there. If it is it returns, otherwise
// it adds it to the list of tickets.
func updateStakePoolInvalUserTickets(ns walletdb.ReadWriteBucket, scriptHash [20]byte, record *chainhash.Hash) error {
	// Fetch the current content of the key.
	// Possible buggy behaviour: If deserialization fails,
	// we won't detect it here. We assume we're throwing
	// ErrPoolUserInvalTcktsNotFound.
	oldRecords, _ := fetchStakePoolUserInvalTickets(ns, scriptHash)

	// Don't reinsert duplicate records we already have.
	if duplicateExistsInInvalTickets(record, oldRecords) {
		return nil
	}

	// Either create a slice if currently nothing exists for this
	// key in the db, or append the entry to the slice.
	var records []*chainhash.Hash
	if oldRecords == nil {
		records = make([]*chainhash.Hash, 1)
		records[0] = record
	} else {
		records = append(oldRecords, record)
	}

	bucket := ns.NestedReadWriteBucket(metaBucketName)
	key := make([]byte, stakePoolInvalidPrefixSize+scriptHashSize)
	copy(key[0:stakePoolInvalidPrefixSize], stakePoolInvalidPrefix)
	copy(key[stakePoolInvalidPrefixSize:stakePoolInvalidPrefixSize+scriptHashSize],
		scriptHash[:])

	// Write the serialized invalid user ticket hashes.
	serializedRecords := serializeUserInvalTickets(records)

	err := bucket.Put(key, serializedRecords)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// initialize creates the DB if it doesn't exist, and otherwise
// loads the database.
func initializeEmpty(ns walletdb.ReadWriteBucket) error {
	// Initialize the buckets and main db fields as needed.
	mainBucket, err := ns.CreateBucketIfNotExists(mainBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucketIfNotExists(sstxRecordsBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucketIfNotExists(ssgenRecordsBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucketIfNotExists(ssrtxRecordsBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucketIfNotExists(metaBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	createBytes := mainBucket.Get(stakeStoreCreateDateName)
	if createBytes == nil {
		createDate := uint64(time.Now().Unix())
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], createDate)
		err := mainBucket.Put(stakeStoreCreateDateName, buf[:])
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}
