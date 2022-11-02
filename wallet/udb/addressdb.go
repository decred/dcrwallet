// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"crypto/sha256"
	"encoding/binary"
	"time"

	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/kdf"
	"decred.org/dcrwallet/v3/wallet/walletdb"
)

var (
	// latestMgrVersion is the most recent manager version as a variable so
	// the tests can change it to force errors.
	latestMgrVersion uint32 = 6
)

// ObtainUserInputFunc is a function that reads a user input and returns it as
// a byte stream. It is used to accept data required during upgrades, for e.g.
// wallet seed and private passphrase.
type ObtainUserInputFunc func() ([]byte, error)

// addressType represents a type of address stored in the database.
type addressType uint8

// These constants define the various supported address types.
// They must remain stable as their values are recorded to the DB.
const (
	adtChain  addressType = iota // seed-derived BIP0044
	adtImport                    // individually imported privkey
	adtScript                    // individually imported p2sh script
)

type dbAccount interface {
	accountType() accountType
	rowData() []byte
}

// accountType represents a type of address stored in the database.
type accountType uint8

// These constants define the various supported account types.
// They must remain stable as their values are recorded to the DB.
const (
	actBIP0044Legacy accountType = iota
	actBIP0044
	importedVoting
)

// dbAccountRow houses information stored about an account in the database.
type dbAccountRow struct {
	acctType accountType
	rawData  []byte // Varies based on account type field.
}

// dbBIP0044AccountRow houses additional information stored about a BIP0044
// account in the database.
type dbBIP0044AccountRow struct {
	dbAccountRow
	pubKeyEncrypted           []byte
	privKeyEncrypted          []byte
	nextExternalIndex         uint32 // Removed by version 2
	nextInternalIndex         uint32 // Removed by version 2
	lastUsedExternalIndex     uint32 // Added in version 2
	lastUsedInternalIndex     uint32 // Added in version 2
	lastReturnedExternalIndex uint32 // Added in version 5
	lastReturnedInternalIndex uint32 // Added in version 5
	name                      string

	// variables subbucket is used to record remaining fields
	uniqueKey *kdf.Argon2idParams
}

func (r *dbBIP0044AccountRow) accountType() accountType { return actBIP0044Legacy }
func (r *dbBIP0044AccountRow) rowData() []byte          { return r.dbAccountRow.rawData }

// dbBIP0044Account records both the static metadata for a BIP0044 account, as
// well as the variables which change over time.
type dbBIP0044Account struct {
	dbAccountRow
	pubKeyEncrypted  []byte
	privKeyEncrypted []byte

	// variables subbucket is used to record remaining fields
	lastUsedExternalIndex     uint32
	lastUsedInternalIndex     uint32
	lastReturnedExternalIndex uint32
	lastReturnedInternalIndex uint32
	name                      string
	uniqueKey                 *kdf.Argon2idParams
}

func (a *dbBIP0044Account) accountType() accountType { return a.dbAccountRow.acctType }
func (a *dbBIP0044Account) rowData() []byte          { return a.dbAccountRow.rawData }

func (a *dbBIP0044Account) serializeRow() []byte {
	// Format:
	//   <len + encpubkey><len + encprivkey>

	data := make([]byte, 8+len(a.pubKeyEncrypted)+len(a.privKeyEncrypted))
	binary.LittleEndian.PutUint32(data, uint32(len(a.pubKeyEncrypted)))
	off := 4
	off += copy(data[off:], a.pubKeyEncrypted)
	binary.LittleEndian.PutUint32(data[off:], uint32(len(a.privKeyEncrypted)))
	off += 4
	copy(data[off:], a.privKeyEncrypted)

	a.rawData = data
	return data
}

func (a *dbBIP0044Account) deserializeRow(v []byte) error {
	if len(v) < 8 {
		err := errors.Errorf("BIP0044 account row bad len %d", len(v))
		return errors.E(errors.IO, err)
	}

	encPubLen := binary.LittleEndian.Uint32(v)
	off := uint32(4)
	encPub := append([]byte(nil), v[off:off+encPubLen]...)
	off += encPubLen
	encPrivLen := binary.LittleEndian.Uint32(v[off:])
	off += 4
	encPriv := append([]byte(nil), v[off:off+encPrivLen]...)
	off += encPrivLen
	if int(off) != len(v) {
		return errors.E(errors.IO, "extra bytes in BIP0044 account row")
	}

	a.pubKeyEncrypted = encPub
	a.privKeyEncrypted = encPriv
	a.rawData = v
	return nil
}

// dbAddressRow houses common information stored about an address in the
// database.
type dbAddressRow struct {
	addrType addressType
	account  uint32
	addTime  uint64
	rawData  []byte // Varies based on address type field.
}

// dbChainAddressRow houses additional information stored about a chained
// address in the database.
type dbChainAddressRow struct {
	dbAddressRow
	branch uint32
	index  uint32
}

// dbImportedAddressRow houses additional information stored about an imported
// public key address in the database.
type dbImportedAddressRow struct {
	dbAddressRow
	encryptedPubKey  []byte
	encryptedPrivKey []byte
}

// dbImportedAddressRow houses additional information stored about a script
// address in the database.
type dbScriptAddressRow struct {
	dbAddressRow
	encryptedHash []byte
	script        []byte
}

// Key names for various database fields.
var (
	// nullVall is null byte used as a flag value in a bucket entry
	nullVal = []byte{0}

	// Bucket names.
	acctBucketName     = []byte("acct")
	acctVarsBucketName = []byte("acctvars")
	addrBucketName     = []byte("addr")

	// addrAcctIdxBucketName is used to index account addresses
	// Entries in this index may map:
	// * addr hash => account id
	// * account bucket -> addr hash => null
	// To fetch the account of an address, lookup the value using
	// the address hash.
	// To fetch all addresses of an account, fetch the account bucket, iterate
	// over the keys and fetch the address row from the addr bucket.
	// The index needs to be updated whenever an address is created e.g.
	// NewAddress
	addrAcctIdxBucketName = []byte("addracctidx")

	// acctNameIdxBucketName is used to create an index
	// mapping an account name string to the corresponding
	// account id.
	// The index needs to be updated whenever the account name
	// and id changes e.g. RenameAccount
	acctNameIdxBucketName = []byte("acctnameidx")

	// acctIDIdxBucketName is used to create an index
	// mapping an account id to the corresponding
	// account name string.
	// The index needs to be updated whenever the account name
	// and id changes e.g. RenameAccount
	acctIDIdxBucketName = []byte("acctididx")

	// meta is used to store meta-data about the address manager
	// e.g. last account number
	metaBucketName = []byte("meta")

	// addrPoolMetaKeyLen is the byte length of the address pool
	// prefixes. It is 11 bytes for the prefix and 4 bytes for
	// the account number.
	addrPoolMetaKeyLen = 15

	// addrPoolKeyPrefixExt is the prefix for keys mapping the
	// last used address pool index to a BIP0044 account. The
	// BIP0044 account is appended to this slice in order to
	// derive the key. This is the external branch.
	// e.g. in pseudocode:
	// key = append([]byte("addrpoolext"), []byte(account))
	//
	// This was removed by database version 2.
	addrPoolKeyPrefixExt = []byte("addrpoolext")

	// addrPoolKeyPrefixInt is the prefix for keys mapping the
	// last used address pool index to a BIP0044 account. The
	// BIP0044 account is appended to this slice in order to
	// derive the key. This is the internal branch.
	//
	// This was removed by database version 2.
	addrPoolKeyPrefixInt = []byte("addrpoolint")

	// lastAccountName is used to store the metadata - last account
	// in the manager
	lastAccountName = []byte("lastaccount")

	// lastImportedAccountName is the metadata key use for the last imported
	// xpub account.
	lastImportedAccountName = []byte("lastimportedaccount")

	mainBucketName = []byte("main")

	// Db related key names (main bucket).
	mgrVersionName    = []byte("mgrver")
	mgrCreateDateName = []byte("mgrcreated")

	// Crypto related key names (main bucket).
	seedName                    = []byte("seed")
	masterPrivKeyName           = []byte("mpriv")
	masterPubKeyName            = []byte("mpub")
	cryptoPrivKeyName           = []byte("cpriv")
	cryptoPubKeyName            = []byte("cpub")
	cryptoScriptKeyName         = []byte("cscript") // removed in db v14
	coinTypeLegacyPrivKeyName   = []byte("ctpriv")
	coinTypeLegacyPubKeyName    = []byte("ctpub")
	coinTypeSLIP0044PrivKeyName = []byte("ctpriv-slip0044")
	coinTypeSLIP0044PubKeyName  = []byte("ctpub-slip0044")
	watchingOnlyName            = []byte("watchonly")
	slip0044Account0RowName     = []byte("slip0044acct0")

	// Used addresses (used bucket).  This was removed by database version 2.
	usedAddrBucketName = []byte("usedaddrs")
)

// uint32ToBytes converts a 32 bit unsigned integer into a 4-byte slice in
// little-endian order: 1 -> [1 0 0 0].
func uint32ToBytes(number uint32) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, number)
	return buf
}

// stringToBytes converts a string into a variable length byte slice in
// little-endian order: "abc" -> [3 0 0 0 61 62 63]
func stringToBytes(s string) []byte {
	// The serialized format is:
	//   <size><string>
	//
	// 4 bytes string size + string
	size := len(s)
	buf := make([]byte, 4+size)
	copy(buf[0:4], uint32ToBytes(uint32(size)))
	copy(buf[4:4+size], s)
	return buf
}

// fetchManagerVersion fetches the current manager version from the database.
// Should only be called on managers in unmigrated DBs.
func fetchManagerVersion(ns walletdb.ReadBucket) (uint32, error) {
	mainBucket := ns.NestedReadBucket(mainBucketName)
	verBytes := mainBucket.Get(mgrVersionName)
	if verBytes == nil {
		return 0, errors.E(errors.IO, "missing address manager version")
	}
	version := binary.LittleEndian.Uint32(verBytes)
	return version, nil
}

// putManagerVersion stores the provided version to the database.  Should only
// be called on managers in unmigrated DBs.
func putManagerVersion(ns walletdb.ReadWriteBucket, version uint32) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	verBytes := uint32ToBytes(version)
	err := bucket.Put(mgrVersionName, verBytes)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// fetchMasterKeyParams loads the master key parameters needed to derive them
// (when given the correct user-supplied passphrase) from the database.  Either
// returned value can be nil, but in practice only the private key params will
// be nil for a watching-only database.
func fetchMasterKeyParams(ns walletdb.ReadBucket) ([]byte, []byte, error) {
	bucket := ns.NestedReadBucket(mainBucketName)

	// Load the master public key parameters.  Required.
	val := bucket.Get(masterPubKeyName)
	if val == nil {
		return nil, nil, errors.E(errors.IO, "missing master pubkey params")
	}
	pubParams := make([]byte, len(val))
	copy(pubParams, val)

	// Load the master private key parameters if they were stored.
	var privParams []byte
	val = bucket.Get(masterPrivKeyName)
	if val != nil {
		privParams = make([]byte, len(val))
		copy(privParams, val)
	}

	return pubParams, privParams, nil
}

// putMasterKeyParams stores the master key parameters needed to derive them
// to the database.  Either parameter can be nil in which case no value is
// written for the parameter.
func putMasterKeyParams(ns walletdb.ReadWriteBucket, pubParams, privParams []byte) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	if privParams != nil {
		err := bucket.Put(masterPrivKeyName, privParams)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	if pubParams != nil {
		err := bucket.Put(masterPubKeyName, pubParams)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}

// fetchCoinTypeKeys loads the encrypted cointype keys which are in turn used to
// derive the extended keys for all accounts.  If both the legacy and SLIP0044
// coin type keys are saved, the legacy keys are used for backwards
// compatibility reasons.
func fetchCoinTypeKeys(ns walletdb.ReadBucket) ([]byte, []byte, error) {
	bucket := ns.NestedReadBucket(mainBucketName)

	var coinTypeSLIP0044 bool

	coinTypePubKeyEnc := bucket.Get(coinTypeLegacyPubKeyName)
	if coinTypePubKeyEnc == nil {
		coinTypeSLIP0044 = true
		coinTypePubKeyEnc = bucket.Get(coinTypeSLIP0044PubKeyName)
	}
	if coinTypePubKeyEnc == nil {
		return nil, nil, errors.E(errors.IO, "missing encrypted cointype pubkey")
	}

	coinTypePrivKeyName := coinTypeLegacyPrivKeyName
	if coinTypeSLIP0044 {
		coinTypePrivKeyName = coinTypeSLIP0044PrivKeyName
	}
	coinTypePrivKeyEnc := bucket.Get(coinTypePrivKeyName)
	if coinTypePrivKeyEnc == nil {
		return nil, nil, errors.E(errors.IO, "missing encrypted cointype privkey")
	}

	return coinTypePubKeyEnc, coinTypePrivKeyEnc, nil
}

// putCoinTypeLegacyKeys stores the encrypted legacy cointype keys which are in
// turn used to derive the extended keys for all accounts.  Either parameter can
// be nil in which case no value is written for the parameter.
func putCoinTypeLegacyKeys(ns walletdb.ReadWriteBucket, coinTypePubKeyEnc []byte, coinTypePrivKeyEnc []byte) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	if coinTypePubKeyEnc != nil {
		err := bucket.Put(coinTypeLegacyPubKeyName, coinTypePubKeyEnc)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	if coinTypePrivKeyEnc != nil {
		err := bucket.Put(coinTypeLegacyPrivKeyName, coinTypePrivKeyEnc)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}

// putCoinTypeSLIP0044Keys stores the encrypted SLIP0044 cointype keys which are
// in turn used to derive the extended keys for all accounts.  Either parameter
// can be nil in which case no value is written for the parameter.
func putCoinTypeSLIP0044Keys(ns walletdb.ReadWriteBucket, coinTypePubKeyEnc []byte, coinTypePrivKeyEnc []byte) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	if coinTypePubKeyEnc != nil {
		err := bucket.Put(coinTypeSLIP0044PubKeyName, coinTypePubKeyEnc)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	if coinTypePrivKeyEnc != nil {
		err := bucket.Put(coinTypeSLIP0044PrivKeyName, coinTypePrivKeyEnc)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}

// fetchCryptoKeys loads the encrypted crypto keys which are in turn used to
// protect the extended keys and imported keys.  Any of the returned values can
// be nil, but in practice only the crypto private key will be nil for a
// watching-only database.
func fetchCryptoKeys(ns walletdb.ReadBucket) ([]byte, []byte, error) {
	bucket := ns.NestedReadBucket(mainBucketName)

	// Load the crypto public key parameters.  Required.
	val := bucket.Get(cryptoPubKeyName)
	if val == nil {
		return nil, nil, errors.E(errors.IO, "missing encrypted crypto pubkey")
	}
	pubKey := make([]byte, len(val))
	copy(pubKey, val)

	// Load the crypto private key parameters if they were stored.
	var privKey []byte
	val = bucket.Get(cryptoPrivKeyName)
	if val != nil {
		privKey = make([]byte, len(val))
		copy(privKey, val)
	}

	return pubKey, privKey, nil
}

// putCryptoKeys stores the encrypted crypto keys which are in turn used to
// protect the extended and imported keys.  Either parameter can be nil in which
// case no value is written for the parameter.
func putCryptoKeys(ns walletdb.ReadWriteBucket, pubKeyEncrypted, privKeyEncrypted []byte) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	if pubKeyEncrypted != nil {
		err := bucket.Put(cryptoPubKeyName, pubKeyEncrypted)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	if privKeyEncrypted != nil {
		err := bucket.Put(cryptoPrivKeyName, privKeyEncrypted)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}

// fetchWatchingOnly loads the watching-only flag from the database.
func fetchWatchingOnly(ns walletdb.ReadBucket) (bool, error) {
	bucket := ns.NestedReadBucket(mainBucketName)

	buf := bucket.Get(watchingOnlyName)
	if len(buf) != 1 {
		return false, errors.E(errors.IO, errors.Errorf("bad watching-only flag len %d", len(buf)))
	}

	return buf[0] != 0, nil
}

// putWatchingOnly stores the watching-only flag to the database.
func putWatchingOnly(ns walletdb.ReadWriteBucket, watchingOnly bool) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	var encoded byte
	if watchingOnly {
		encoded = 1
	}

	if err := bucket.Put(watchingOnlyName, []byte{encoded}); err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// deserializeAccountRow deserializes the passed serialized account information.
// This is used as a common base for the various account types to deserialize
// the common parts.
func deserializeAccountRow(accountID []byte, serializedAccount []byte) (*dbAccountRow, error) {
	// The serialized account format is:
	//   <acctType><rdlen><rawdata>
	//
	// 1 byte acctType + 4 bytes raw data length + raw data

	// Given the above, the length of the entry must be at a minimum
	// the constant value sizes.
	if len(serializedAccount) < 5 {
		return nil, errors.E(errors.IO, errors.Errorf("bad account len %d", len(serializedAccount)))
	}

	row := dbAccountRow{}
	row.acctType = accountType(serializedAccount[0])
	rdlen := binary.LittleEndian.Uint32(serializedAccount[1:5])
	row.rawData = make([]byte, rdlen)
	copy(row.rawData, serializedAccount[5:5+rdlen])

	return &row, nil
}

// serializeAccountRow returns the serialization of the passed account row.
func serializeAccountRow(row *dbAccountRow) []byte {
	// The serialized account format is:
	//   <acctType><rdlen><rawdata>
	//
	// 1 byte acctType + 4 bytes raw data length + raw data
	rdlen := len(row.rawData)
	buf := make([]byte, 5+rdlen)
	buf[0] = byte(row.acctType)
	binary.LittleEndian.PutUint32(buf[1:5], uint32(rdlen))
	copy(buf[5:5+rdlen], row.rawData)
	return buf
}

// deserializeBIP0044AccountRow deserializes the raw data from the passed
// account row as a BIP0044 account.
func deserializeBIP0044AccountRow(accountID []byte, row *dbAccountRow, dbVersion uint32) (*dbBIP0044AccountRow, error) {
	// The serialized BIP0044 account raw data format is:
	//   <encpubkeylen><encpubkey><encprivkeylen><encprivkey><lastusedext>
	//   <lastusedint><lastretext><lastretint><namelen><name>
	//
	// 4 bytes encrypted pubkey len + encrypted pubkey + 4 bytes encrypted
	// privkey len + encrypted privkey + 4 bytes last used external index +
	// 4 bytes last used internal index + 4 bytes last returned external +
	// 4 bytes last returned internal + 4 bytes name len + name

	// Given the above, the length of the entry must be at a minimum
	// the constant value sizes.
	switch {
	case dbVersion < 5 && len(row.rawData) < 20,
		dbVersion >= 5 && len(row.rawData) < 28:
		return nil, errors.E(errors.IO, errors.Errorf("bip0044 account %x bad len %d", accountID, len(row.rawData)))
	}

	retRow := dbBIP0044AccountRow{
		dbAccountRow: *row,
	}

	pubLen := binary.LittleEndian.Uint32(row.rawData[0:4])
	retRow.pubKeyEncrypted = make([]byte, pubLen)
	copy(retRow.pubKeyEncrypted, row.rawData[4:4+pubLen])
	offset := 4 + pubLen
	privLen := binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
	offset += 4
	retRow.privKeyEncrypted = make([]byte, privLen)
	copy(retRow.privKeyEncrypted, row.rawData[offset:offset+privLen])
	offset += privLen
	switch {
	case dbVersion == 1:
		retRow.nextExternalIndex = binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
		offset += 4
		retRow.nextInternalIndex = binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
		offset += 4
	case dbVersion >= 2:
		retRow.lastUsedExternalIndex = binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
		retRow.lastUsedInternalIndex = binary.LittleEndian.Uint32(row.rawData[offset+4 : offset+8])
		offset += 8
	}
	switch {
	case dbVersion >= 5:
		retRow.lastReturnedExternalIndex = binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
		retRow.lastReturnedInternalIndex = binary.LittleEndian.Uint32(row.rawData[offset+4 : offset+8])
		offset += 8
	}
	nameLen := binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
	offset += 4
	retRow.name = string(row.rawData[offset : offset+nameLen])

	return &retRow, nil
}

// serializeBIP0044AccountRow returns the serialization of the raw data field
// for a BIP0044 account.
func serializeBIP0044AccountRow(row *dbBIP0044AccountRow, dbVersion uint32) []byte {
	// The serialized BIP0044 account raw data format is:
	//   <encpubkeylen><encpubkey><encprivkeylen><encprivkey><lastusedext>
	//   <lastusedint><lastretext><lastretint><namelen><name>
	//
	// 4 bytes encrypted pubkey len + encrypted pubkey + 4 bytes encrypted
	// privkey len + encrypted privkey + 4 bytes last used external index +
	// 4 bytes last used internal index + 4 bytes last returned external +
	// 4 bytes last returned internal + 4 bytes name len + name
	pubLen := uint32(len(row.pubKeyEncrypted))
	privLen := uint32(len(row.privKeyEncrypted))
	nameLen := uint32(len(row.name))
	rowSize := 28 + pubLen + privLen + nameLen
	switch {
	case dbVersion < 5:
		rowSize -= 8
	}
	rawData := make([]byte, rowSize)
	binary.LittleEndian.PutUint32(rawData[0:4], pubLen)
	copy(rawData[4:4+pubLen], row.pubKeyEncrypted)
	offset := 4 + pubLen
	binary.LittleEndian.PutUint32(rawData[offset:offset+4], privLen)
	offset += 4
	copy(rawData[offset:offset+privLen], row.privKeyEncrypted)
	offset += privLen
	switch {
	case dbVersion == 1:
		binary.LittleEndian.PutUint32(rawData[offset:offset+4], row.nextExternalIndex)
		offset += 4
		binary.LittleEndian.PutUint32(rawData[offset:offset+4], row.nextInternalIndex)
		offset += 4
	case dbVersion >= 2:
		binary.LittleEndian.PutUint32(rawData[offset:offset+4], row.lastUsedExternalIndex)
		binary.LittleEndian.PutUint32(rawData[offset+4:offset+8], row.lastUsedInternalIndex)
		offset += 8
	}
	switch {
	case dbVersion >= 5:
		binary.LittleEndian.PutUint32(rawData[offset:offset+4], row.lastReturnedExternalIndex)
		binary.LittleEndian.PutUint32(rawData[offset+4:offset+8], row.lastReturnedInternalIndex)
		offset += 8
	}
	binary.LittleEndian.PutUint32(rawData[offset:offset+4], nameLen)
	offset += 4
	copy(rawData[offset:offset+nameLen], row.name)
	return rawData
}

func bip0044AccountInfo(pubKeyEnc, privKeyEnc []byte, nextExtIndex, nextIntIndex,
	lastUsedExtIndex, lastUsedIntIndex, lastRetExtIndex, lastRetIntIndex uint32,
	name string, dbVersion uint32) *dbBIP0044AccountRow {

	row := &dbBIP0044AccountRow{
		dbAccountRow: dbAccountRow{
			acctType: actBIP0044Legacy,
			rawData:  nil,
		},
		pubKeyEncrypted:           pubKeyEnc,
		privKeyEncrypted:          privKeyEnc,
		nextExternalIndex:         0,
		nextInternalIndex:         0,
		lastUsedExternalIndex:     0,
		lastUsedInternalIndex:     0,
		lastReturnedExternalIndex: 0,
		lastReturnedInternalIndex: 0,
		name:                      name,
	}
	switch {
	case dbVersion == 1:
		row.nextExternalIndex = nextExtIndex
		row.nextInternalIndex = nextIntIndex
	case dbVersion >= 2:
		row.lastUsedExternalIndex = lastUsedExtIndex
		row.lastUsedInternalIndex = lastUsedIntIndex
	}
	switch {
	case dbVersion >= 5:
		row.lastReturnedExternalIndex = lastRetExtIndex
		row.lastReturnedInternalIndex = lastRetIntIndex
	}
	row.rawData = serializeBIP0044AccountRow(row, dbVersion)
	return row
}

// forEachAccount calls the given function with each account stored in
// the manager, breaking early on error.
func forEachAccount(ns walletdb.ReadBucket, fn func(account uint32) error) error {
	bucket := ns.NestedReadBucket(acctBucketName)

	return bucket.ForEach(func(k, v []byte) error {
		// Skip buckets.
		if v == nil {
			return nil
		}
		return fn(binary.LittleEndian.Uint32(k))
	})
}

// fetchLastAccount retreives the last BIP0044 account from the database.
func fetchLastAccount(ns walletdb.ReadBucket) (uint32, error) {
	bucket := ns.NestedReadBucket(metaBucketName)

	val := bucket.Get(lastAccountName)
	if len(val) != 4 {
		return 0, errors.E(errors.IO, errors.Errorf("bad last account len %d", len(val)))
	}
	account := binary.LittleEndian.Uint32(val[0:4])
	return account, nil
}

// fetchLastAccount retreives the last imported xpub account from the
// database.
func fetchLastImportedAccount(ns walletdb.ReadBucket) (uint32, error) {
	bucket := ns.NestedReadBucket(metaBucketName)

	val := bucket.Get(lastImportedAccountName)
	// TODO: add this, set to old imported account num in db upgrade
	// TODO: also remove this hack
	if len(val) == 0 {
		return ImportedAddrAccount, nil
	}
	if len(val) != 4 {
		return 0, errors.E(errors.IO, errors.Errorf("bad last imported account len %d", len(val)))
	}
	account := binary.LittleEndian.Uint32(val[0:4])
	if account <= MaxAccountNum {
		return 0, errors.E(errors.IO, errors.Errorf("bad imported xpub account value %d", account))
	}
	return account, nil
}

// fetchAccountName retreives the account name given an account number from
// the database.
func fetchAccountName(ns walletdb.ReadBucket, account uint32) (string, error) {
	bucket := ns.NestedReadBucket(acctIDIdxBucketName)

	val := bucket.Get(uint32ToBytes(account))
	if val == nil {
		return "", errors.E(errors.NotExist, errors.Errorf("no account %d", account))
	}
	offset := uint32(0)
	nameLen := binary.LittleEndian.Uint32(val[offset : offset+4])
	offset += 4
	acctName := string(val[offset : offset+nameLen])
	return acctName, nil
}

// fetchAccountByName retreives the account number given an account name
// from the database.
func fetchAccountByName(ns walletdb.ReadBucket, name string) (uint32, error) {
	bucket := ns.NestedReadBucket(acctNameIdxBucketName)

	val := bucket.Get(stringToBytes(name))
	if val == nil {
		return 0, errors.E(errors.NotExist, errors.Errorf("no account %q", name))
	}

	return binary.LittleEndian.Uint32(val), nil
}

// fetchAccountRow loads the row serializing details regarding an account.
// This function does not perform any further parsing based on the account type.
func fetchAccountRow(ns walletdb.ReadBucket, account uint32, dbVersion uint32) (*dbAccountRow, error) {
	bucket := ns.NestedReadBucket(acctBucketName)

	accountID := uint32ToBytes(account)
	serializedRow := bucket.Get(accountID)
	if serializedRow == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no account %d", account))
	}

	return deserializeAccountRow(accountID, serializedRow)
}

// fetchAccountInfo loads information about the passed account from the
// database.
func fetchAccountInfo(ns walletdb.ReadBucket, account uint32, dbVersion uint32) (*dbBIP0044AccountRow, error) {
	row, err := fetchAccountRow(ns, account, dbVersion)
	if err != nil {
		return nil, err
	}

	accountID := uint32ToBytes(account)
	switch row.acctType {
	case actBIP0044Legacy, importedVoting:
		return deserializeBIP0044AccountRow(accountID, row, dbVersion)
	}

	return nil, errors.E(errors.IO, errors.Errorf("unknown account type %d", row.acctType))
}

func fetchDBAccount(ns walletdb.ReadBucket, account uint32, dbVersion uint32) (dbAccount, error) {
	row, err := fetchAccountRow(ns, account, dbVersion)
	if err != nil {
		return nil, err
	}

	accountID := uint32ToBytes(account)
	switch row.acctType {
	case actBIP0044Legacy:
		if dbVersion >= accountVariablesVersion {
			err := errors.Errorf("legacy BIP0044 account row unsupported "+
				"in db version %v", dbVersion)
			return nil, errors.E(err)
		}
		return deserializeBIP0044AccountRow(accountID, row, dbVersion)
	case actBIP0044, importedVoting:
		bucketKey := uint32ToBytes(account)
		varsBucket := ns.NestedReadBucket(acctVarsBucketName).
			NestedReadBucket(bucketKey)

		var r accountVarReader
		lastUsedExt := r.getAccountUint32Var(varsBucket, acctVarLastUsedExternal)
		lastUsedInt := r.getAccountUint32Var(varsBucket, acctVarLastUsedInternal)
		lastRetExt := r.getAccountUint32Var(varsBucket, acctVarLastReturnedExternal)
		lastRetInt := r.getAccountUint32Var(varsBucket, acctVarLastReturnedInternal)
		name := r.getAccountStringVar(varsBucket, acctVarName)
		kdfParams := r.getAccountKDFVar(varsBucket, acctVarKDF)
		if r.err != nil {
			return nil, errors.E(errors.IO, err)
		}

		a := new(dbBIP0044Account)
		a.dbAccountRow = *row
		err := a.deserializeRow(row.rawData)
		if err != nil {
			return nil, err
		}
		a.lastUsedExternalIndex = lastUsedExt
		a.lastUsedInternalIndex = lastUsedInt
		a.lastReturnedExternalIndex = lastRetExt
		a.lastReturnedInternalIndex = lastRetInt
		a.name = name
		a.uniqueKey = kdfParams

		return a, nil
	}

	return nil, errors.E(errors.IO, errors.Errorf("unknown account type %d", row.acctType))
}

func accountVarsBucket(ns walletdb.ReadWriteBucket, account uint32) walletdb.ReadWriteBucket {
	accountKey := uint32ToBytes(account)
	return ns.NestedReadWriteBucket(acctVarsBucketName).NestedReadWriteBucket(accountKey)
}

// deleteAccountNameIndex deletes the given key from the account name index of the database.
func deleteAccountNameIndex(ns walletdb.ReadWriteBucket, name string) error {
	bucket := ns.NestedReadWriteBucket(acctNameIdxBucketName)

	// Delete the account name key
	err := bucket.Delete(stringToBytes(name))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// deleteAccounIdIndex deletes the given key from the account id index of the database.
func deleteAccountIDIndex(ns walletdb.ReadWriteBucket, account uint32) error {
	bucket := ns.NestedReadWriteBucket(acctIDIdxBucketName)

	// Delete the account id key
	err := bucket.Delete(uint32ToBytes(account))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putAccountNameIndex stores the given key to the account name index of the database.
func putAccountNameIndex(ns walletdb.ReadWriteBucket, account uint32, name string) error {
	bucket := ns.NestedReadWriteBucket(acctNameIdxBucketName)

	// Write the account number keyed by the account name.
	err := bucket.Put(stringToBytes(name), uint32ToBytes(account))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putAccountIDIndex stores the given key to the account id index of the database.
func putAccountIDIndex(ns walletdb.ReadWriteBucket, account uint32, name string) error {
	bucket := ns.NestedReadWriteBucket(acctIDIdxBucketName)

	// Write the account number keyed by the account id.
	err := bucket.Put(uint32ToBytes(account), stringToBytes(name))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putAddrAccountIndex stores the given key to the address account index of the database.
func putAddrAccountIndex(ns walletdb.ReadWriteBucket, account uint32, addrHash []byte) error {
	bucket := ns.NestedReadWriteBucket(addrAcctIdxBucketName)

	// Write account keyed by address hash
	err := bucket.Put(addrHash, uint32ToBytes(account))
	if err != nil {
		return errors.E(errors.IO, err)
	}

	bucket, err = bucket.CreateBucketIfNotExists(uint32ToBytes(account))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	// In account bucket, write a null value keyed by the address hash
	err = bucket.Put(addrHash, nullVal)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putAccountRow stores the provided account information to the database.  This
// is used a common base for storing the various account types.
func putAccountRow(ns walletdb.ReadWriteBucket, account uint32, row *dbAccountRow) error {
	bucket := ns.NestedReadWriteBucket(acctBucketName)

	// Write the serialized value keyed by the account number.
	err := bucket.Put(uint32ToBytes(account), serializeAccountRow(row))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putBIP0044AccountInfo stores the provided account information to the database.
func putBIP0044AccountInfo(ns walletdb.ReadWriteBucket, account uint32, row *dbBIP0044AccountRow) error {
	if err := putAccountRow(ns, account, &row.dbAccountRow); err != nil {
		return err
	}
	// Update account id index
	if err := putAccountIDIndex(ns, account, row.name); err != nil {
		return err
	}
	// Update account name index
	return putAccountNameIndex(ns, account, row.name)
}

// putNewBIP0044Account writes a new account to the database, storing the
// account row and all account variables.
func putNewBIP0044Account(ns walletdb.ReadWriteBucket, account uint32, a *dbBIP0044Account) error {
	err := putAccountRow(ns, account, &a.dbAccountRow)
	if err != nil {
		return err
	}
	// Index the account by name
	err = putAccountIDIndex(ns, account, a.name)
	if err != nil {
		return err
	}
	err = putAccountNameIndex(ns, account, a.name)
	if err != nil {
		return err
	}
	// Create the bucket for this account's variables
	bucketKey := uint32ToBytes(account)
	varsBucket, err := ns.NestedReadWriteBucket(acctVarsBucketName).
		CreateBucketIfNotExists(bucketKey)
	if err != nil {
		return err
	}
	// Write the account's variables
	err = putAccountUint32Var(varsBucket, acctVarLastUsedExternal, a.lastUsedExternalIndex)
	if err != nil {
		return err
	}
	err = putAccountUint32Var(varsBucket, acctVarLastUsedInternal, a.lastUsedInternalIndex)
	if err != nil {
		return err
	}
	err = putAccountUint32Var(varsBucket, acctVarLastReturnedExternal, a.lastReturnedExternalIndex)
	if err != nil {
		return err
	}
	err = putAccountUint32Var(varsBucket, acctVarLastReturnedInternal, a.lastReturnedInternalIndex)
	if err != nil {
		return err
	}
	err = putAccountStringVar(varsBucket, acctVarName, a.name)
	if err != nil {
		return err
	}
	if a.uniqueKey != nil {
		err = putAccountKDFVar(varsBucket, acctVarKDF, a.uniqueKey)
		if err != nil {
			return err
		}
	}

	return nil
}

// putLastAccount stores the provided metadata - last account - to the database.
func putLastAccount(ns walletdb.ReadWriteBucket, account uint32) error {
	bucket := ns.NestedReadWriteBucket(metaBucketName)

	err := bucket.Put(lastAccountName, uint32ToBytes(account))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// putLastImportedAccount stores the provided metadata - last account - to the database.
func putLastImportedAccount(ns walletdb.ReadWriteBucket, account uint32) error {
	bucket := ns.NestedReadWriteBucket(metaBucketName)

	err := bucket.Put(lastImportedAccountName, uint32ToBytes(account))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// Account variable keys
var (
	acctVarLastUsedExternal     = []byte("extused")
	acctVarLastUsedInternal     = []byte("intused")
	acctVarLastReturnedExternal = []byte("extret")
	acctVarLastReturnedInternal = []byte("intret")
	acctVarName                 = []byte("name")
	acctVarKDF                  = []byte("kdf-params")
)

func putAccountUint32Var(varsBucket walletdb.ReadWriteBucket, varName []byte, value uint32) error {
	v := make([]byte, 4)
	binary.LittleEndian.PutUint32(v, value)
	err := varsBucket.Put(varName, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func putAccountStringVar(varsBucket walletdb.ReadWriteBucket, varName []byte, value string) error {
	err := varsBucket.Put(varName, []byte(value))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func putAccountKDFVar(varsBucket walletdb.ReadWriteBucket, varName []byte, value *kdf.Argon2idParams) error {
	marshaled, err := value.MarshalBinary()
	if err != nil {
		return err
	}
	err = varsBucket.Put(varName, marshaled)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

type accountVarReader struct {
	err error
}

func (r *accountVarReader) getAccountUint32Var(varsBucket walletdb.ReadBucket, varName []byte) uint32 {
	if r.err != nil {
		return 0
	}
	value := varsBucket.Get(varName)
	if len(value) != 4 {
		err := errors.Errorf(`bad len %d for uint32 value "%s"`, len(value), varName)
		r.err = errors.E(errors.IO, err)
		return 0
	}
	return binary.LittleEndian.Uint32(value)
}

func (r *accountVarReader) getAccountStringVar(varsBucket walletdb.ReadBucket, varName []byte) string {
	if r.err != nil {
		return ""
	}
	value := varsBucket.Get(varName)
	return string(value)
}

func (r *accountVarReader) getAccountKDFVar(varsBucket walletdb.ReadBucket, varName []byte) *kdf.Argon2idParams {
	if r.err != nil {
		return nil
	}
	value := varsBucket.Get(varName)
	if value == nil {
		return nil
	}
	params := new(kdf.Argon2idParams)
	r.err = params.UnmarshalBinary(value)
	return params
}

// deserializeAddressRow deserializes the passed serialized address information.
// This is used as a common base for the various address types to deserialize
// the common parts.
func deserializeAddressRow(serializedAddress []byte) (*dbAddressRow, error) {
	// The serialized address format is:
	//   <addrType><account><addedTime><syncStatus><rawdata>
	//
	// 1 byte addrType + 4 bytes account + 8 bytes addTime + 1 byte
	// syncStatus + 4 bytes raw data length + raw data

	// Given the above, the length of the entry must be at a minimum
	// the constant value sizes.
	if len(serializedAddress) < 18 {
		return nil, errors.E(errors.IO, errors.Errorf("bad address len %d", len(serializedAddress)))
	}

	row := dbAddressRow{}
	row.addrType = addressType(serializedAddress[0])
	row.account = binary.LittleEndian.Uint32(serializedAddress[1:5])
	row.addTime = binary.LittleEndian.Uint64(serializedAddress[5:13])
	rdlen := binary.LittleEndian.Uint32(serializedAddress[14:18])
	row.rawData = make([]byte, rdlen)
	copy(row.rawData, serializedAddress[18:18+rdlen])

	return &row, nil
}

// serializeAddressRow returns the serialization of the passed address row.
func serializeAddressRow(row *dbAddressRow) []byte {
	// The serialized address format is:
	//   <addrType><account><addedTime><syncStatus><commentlen><comment>
	//   <rawdata>
	//
	// 1 byte addrType + 4 bytes account + 8 bytes addTime + 1 byte
	// syncStatus + 4 bytes raw data length + raw data
	rdlen := len(row.rawData)
	buf := make([]byte, 18+rdlen)
	buf[0] = byte(row.addrType)
	binary.LittleEndian.PutUint32(buf[1:5], row.account)
	binary.LittleEndian.PutUint64(buf[5:13], row.addTime)
	buf[13] = 0 // not used
	binary.LittleEndian.PutUint32(buf[14:18], uint32(rdlen))
	copy(buf[18:18+rdlen], row.rawData)
	return buf
}

// deserializeChainedAddress deserializes the raw data from the passed address
// row as a chained address.
func deserializeChainedAddress(row *dbAddressRow) (*dbChainAddressRow, error) {
	// The serialized chain address raw data format is:
	//   <branch><index>
	//
	// 4 bytes branch + 4 bytes address index
	if len(row.rawData) != 8 {
		return nil, errors.E(errors.IO, errors.Errorf("bad chained address len %d", len(row.rawData)))
	}

	retRow := dbChainAddressRow{
		dbAddressRow: *row,
	}

	retRow.branch = binary.LittleEndian.Uint32(row.rawData[0:4])
	retRow.index = binary.LittleEndian.Uint32(row.rawData[4:8])

	return &retRow, nil
}

// serializeChainedAddress returns the serialization of the raw data field for
// a chained address.
func serializeChainedAddress(branch, index uint32) []byte {
	// The serialized chain address raw data format is:
	//   <branch><index>
	//
	// 4 bytes branch + 4 bytes address index
	rawData := make([]byte, 8)
	binary.LittleEndian.PutUint32(rawData[0:4], branch)
	binary.LittleEndian.PutUint32(rawData[4:8], index)
	return rawData
}

// deserializeImportedAddress deserializes the raw data from the passed address
// row as an imported address.
func deserializeImportedAddress(row *dbAddressRow) (*dbImportedAddressRow, error) {
	// The serialized imported address raw data format is:
	//   <encpubkeylen><encpubkey><encprivkeylen><encprivkey>
	//
	// 4 bytes encrypted pubkey len + encrypted pubkey + 4 bytes encrypted
	// privkey len + encrypted privkey

	// Given the above, the length of the entry must be at a minimum
	// the constant value sizes.
	if len(row.rawData) < 8 {
		return nil, errors.E(errors.IO, errors.Errorf("bad imported address len %d", len(row.rawData)))
	}

	retRow := dbImportedAddressRow{
		dbAddressRow: *row,
	}

	pubLen := binary.LittleEndian.Uint32(row.rawData[0:4])
	retRow.encryptedPubKey = make([]byte, pubLen)
	copy(retRow.encryptedPubKey, row.rawData[4:4+pubLen])
	offset := 4 + pubLen
	privLen := binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
	offset += 4
	retRow.encryptedPrivKey = make([]byte, privLen)
	copy(retRow.encryptedPrivKey, row.rawData[offset:offset+privLen])

	return &retRow, nil
}

// serializeImportedAddress returns the serialization of the raw data field for
// an imported address.
func serializeImportedAddress(encryptedPubKey, encryptedPrivKey []byte) []byte {
	// The serialized imported address raw data format is:
	//   <encpubkeylen><encpubkey><encprivkeylen><encprivkey>
	//
	// 4 bytes encrypted pubkey len + encrypted pubkey + 4 bytes encrypted
	// privkey len + encrypted privkey
	pubLen := uint32(len(encryptedPubKey))
	privLen := uint32(len(encryptedPrivKey))
	rawData := make([]byte, 8+pubLen+privLen)
	binary.LittleEndian.PutUint32(rawData[0:4], pubLen)
	copy(rawData[4:4+pubLen], encryptedPubKey)
	offset := 4 + pubLen
	binary.LittleEndian.PutUint32(rawData[offset:offset+4], privLen)
	offset += 4
	copy(rawData[offset:offset+privLen], encryptedPrivKey)
	return rawData
}

// deserializeScriptAddress deserializes the raw data from the passed address
// row as a script address.
func deserializeScriptAddress(row *dbAddressRow) (*dbScriptAddressRow, error) {
	// The serialized script address raw data format is:
	//   <encscripthashlen><encscripthash><scriptlen><script>
	//
	// 4 bytes encrypted script hash len + encrypted script hash + 4 bytes
	// script len + script

	// Given the above, the length of the entry must be at a minimum
	// the constant value sizes.
	if len(row.rawData) < 8 {
		return nil, errors.E(errors.IO, errors.Errorf("bad script address len %d", len(row.rawData)))
	}

	retRow := dbScriptAddressRow{
		dbAddressRow: *row,
	}

	hashLen := binary.LittleEndian.Uint32(row.rawData[0:4])
	retRow.encryptedHash = make([]byte, hashLen)
	copy(retRow.encryptedHash, row.rawData[4:4+hashLen])
	offset := 4 + hashLen
	scriptLen := binary.LittleEndian.Uint32(row.rawData[offset : offset+4])
	offset += 4
	retRow.script = make([]byte, scriptLen)
	copy(retRow.script, row.rawData[offset:offset+scriptLen])

	return &retRow, nil
}

// serializeScriptAddress returns the serialization of the raw data field for
// a script address.
func serializeScriptAddress(encryptedHash, script []byte) []byte {
	// The serialized script address raw data format is:
	//   <encscripthashlen><encscripthash><scriptlen><script>
	//
	// 4 bytes encrypted script hash len + encrypted script hash + 4 bytes
	// script len + script

	hashLen := uint32(len(encryptedHash))
	scriptLen := uint32(len(script))
	rawData := make([]byte, 8+hashLen+scriptLen)
	binary.LittleEndian.PutUint32(rawData[0:4], hashLen)
	copy(rawData[4:4+hashLen], encryptedHash)
	offset := 4 + hashLen
	binary.LittleEndian.PutUint32(rawData[offset:offset+4], scriptLen)
	offset += 4
	copy(rawData[offset:offset+scriptLen], script)
	return rawData
}

// fetchAddressByHash loads address information for the provided address hash
// from the database.  The returned value is one of the address rows for the
// specific address type.  The caller should use type assertions to ascertain
// the type.  The caller should prefix the error message with the address hash
// which caused the failure.
func fetchAddressByHash(ns walletdb.ReadBucket, addrHash []byte) (interface{}, error) {
	bucket := ns.NestedReadBucket(addrBucketName)

	serializedRow := bucket.Get(addrHash[:])
	if serializedRow == nil {
		return nil, errors.E(errors.NotExist, errors.Errorf("no address with hash %x", addrHash))
	}

	row, err := deserializeAddressRow(serializedRow)
	if err != nil {
		return nil, err
	}

	switch row.addrType {
	case adtChain:
		return deserializeChainedAddress(row)
	case adtImport:
		return deserializeImportedAddress(row)
	case adtScript:
		return deserializeScriptAddress(row)
	}

	return nil, errors.E(errors.IO, errors.Errorf("unknown address type %d", row.addrType))
}

// fetchAddress loads address information for the provided address id from the
// database.  The returned value is one of the address rows for the specific
// address type.  The caller should use type assertions to ascertain the type.
// The caller should prefix the error message with the address which caused the
// failure.
func fetchAddress(ns walletdb.ReadBucket, addressID []byte) (interface{}, error) {
	addrHash := sha256.Sum256(addressID)
	addr, err := fetchAddressByHash(ns, addrHash[:])
	if errors.Is(err, errors.NotExist) {
		return nil, errors.E(errors.NotExist, errors.Errorf("no address with id %x", addressID))
	}
	return addr, err
}

// putAddress stores the provided address information to the database.  This
// is used a common base for storing the various address types.
func putAddress(ns walletdb.ReadWriteBucket, addressID []byte, row *dbAddressRow) error {
	bucket := ns.NestedReadWriteBucket(addrBucketName)

	// Write the serialized value keyed by the hash of the address.  The
	// additional hash is used to conceal the actual address while still
	// allowed keyed lookups.
	addrHash := sha256.Sum256(addressID)
	err := bucket.Put(addrHash[:], serializeAddressRow(row))
	if err != nil {
		return errors.E(errors.IO, err)
	}
	// Update address account index
	return putAddrAccountIndex(ns, row.account, addrHash[:])
}

// putChainedAddress stores the provided chained address information to the
// database.
func putChainedAddress(ns walletdb.ReadWriteBucket, addressID []byte, account uint32,
	branch, index uint32) error {

	addrRow := dbAddressRow{
		addrType: adtChain,
		account:  account,
		addTime:  uint64(time.Now().Unix()),
		rawData:  serializeChainedAddress(branch, index),
	}
	return putAddress(ns, addressID, &addrRow)
}

// putImportedAddress stores the provided imported address information to the
// database.
func putImportedAddress(ns walletdb.ReadWriteBucket, addressID []byte, account uint32,
	encryptedPubKey, encryptedPrivKey []byte) error {

	rawData := serializeImportedAddress(encryptedPubKey, encryptedPrivKey)
	addrRow := dbAddressRow{
		addrType: adtImport,
		account:  account,
		addTime:  uint64(time.Now().Unix()),
		rawData:  rawData,
	}
	return putAddress(ns, addressID, &addrRow)
}

// putScriptAddress stores the provided script address information to the
// database.
func putScriptAddress(ns walletdb.ReadWriteBucket, addressID []byte, account uint32,
	encryptedHash, script []byte) error {

	rawData := serializeScriptAddress(encryptedHash, script)
	addrRow := dbAddressRow{
		addrType: adtScript,
		account:  account,
		addTime:  uint64(time.Now().Unix()),
		rawData:  rawData,
	}
	return putAddress(ns, addressID, &addrRow)
}

// existsAddress returns whether or not the address id exists in the database.
func existsAddress(ns walletdb.ReadBucket, addressID []byte) bool {
	bucket := ns.NestedReadBucket(addrBucketName)

	addrHash := sha256.Sum256(addressID)
	return bucket.Get(addrHash[:]) != nil
}

// fetchAddrAccount returns the account to which the given address belongs to.
// It looks up the account using the addracctidx index which maps the address
// hash to its corresponding account id.
func fetchAddrAccount(ns walletdb.ReadBucket, addressID []byte) (uint32, error) {
	bucket := ns.NestedReadBucket(addrAcctIdxBucketName)

	addrHash := sha256.Sum256(addressID)
	val := bucket.Get(addrHash[:])
	if val == nil {
		return 0, errors.E(errors.NotExist, errors.Errorf("no address for id %x", addressID))
	}
	return binary.LittleEndian.Uint32(val), nil
}

// forEachAccountAddress calls the given function with each address of
// the given account stored in the manager, breaking early on error.
func forEachAccountAddress(ns walletdb.ReadBucket, account uint32, fn func(rowInterface interface{}) error) error {
	bucket := ns.NestedReadBucket(addrAcctIdxBucketName).
		NestedReadBucket(uint32ToBytes(account))
	// if index bucket is missing the account, there hasn't been any address
	// entries yet
	if bucket == nil {
		return nil
	}

	c := bucket.ReadCursor()
	defer c.Close()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		// Skip buckets.
		if v == nil {
			continue
		}
		addrRow, err := fetchAddressByHash(ns, k)
		if err != nil {
			return errors.E(errors.IO, err)
		}

		err = fn(addrRow)
		if err != nil {
			return err
		}
	}
	return nil
}

// forEachActiveAddress calls the given function with each active address
// stored in the manager, breaking early on error.
func forEachActiveAddress(ns walletdb.ReadBucket, fn func(rowInterface interface{}) error) error {
	bucket := ns.NestedReadBucket(addrBucketName)
	c := bucket.ReadCursor()
	defer c.Close()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		// Skip buckets.
		if v == nil {
			continue
		}

		// Deserialize the address row first to determine the field
		// values.
		addrRow, err := fetchAddressByHash(ns, k)
		if err != nil {
			return errors.E(errors.IO, err)
		}

		err = fn(addrRow)
		if err != nil {
			return err
		}
	}
	return nil
}

// deletePrivateKeys removes all private key material from the database.
//
// NOTE: Care should be taken when calling this function.  It is primarily
// intended for use in converting to a watching-only copy.  Removing the private
// keys from the main database without also marking it watching-only will result
// in an unusable database.  It will also make any private keys unrecoverable
// unless there is a backup copy available.
func deletePrivateKeys(ns walletdb.ReadWriteBucket, dbVersion uint32) error {
	bucket := ns.NestedReadWriteBucket(mainBucketName)

	// Delete the master private key params and the crypto private keys.
	if err := bucket.Delete(masterPrivKeyName); err != nil {
		return errors.E(errors.IO, err)
	}
	if err := bucket.Delete(cryptoPrivKeyName); err != nil {
		return errors.E(errors.IO, err)
	}
	if err := bucket.Delete(coinTypeLegacyPrivKeyName); err != nil {
		return errors.E(errors.IO, err)
	}
	if err := bucket.Delete(coinTypeSLIP0044PrivKeyName); err != nil {
		return errors.E(errors.IO, err)
	}

	BIP0044Set := map[string]*dbAccountRow{}

	// Fetch all BIP0044 accounts.
	bucket = ns.NestedReadWriteBucket(acctBucketName)
	c := bucket.ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		// Skip buckets.
		if v == nil {
			continue
		}

		// Deserialize the account row first to determine the type.
		row, err := deserializeAccountRow(k, v)
		if err != nil {
			c.Close()
			return err
		}

		switch row.acctType {
		case actBIP0044Legacy:
			BIP0044Set[string(k)] = row
		}
	}
	c.Close()

	// Delete the account extended private key for all BIP0044 accounts.
	for k, row := range BIP0044Set {
		arow, err := deserializeBIP0044AccountRow([]byte(k), row, dbVersion)
		if err != nil {
			return err
		}

		// Reserialize the account without the private key and
		// store it.
		row := bip0044AccountInfo(arow.pubKeyEncrypted, nil,
			arow.nextExternalIndex, arow.nextInternalIndex,
			arow.lastUsedExternalIndex, arow.lastUsedInternalIndex,
			arow.lastReturnedExternalIndex, arow.lastReturnedInternalIndex,
			arow.name, dbVersion)
		err = bucket.Put([]byte(k), serializeAccountRow(&row.dbAccountRow))
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	importedAddrSet := map[string]*dbAddressRow{}

	// Fetch all imported addresses.
	bucket = ns.NestedReadWriteBucket(addrBucketName)
	c = bucket.ReadCursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		// Skip buckets.
		if v == nil {
			continue
		}

		// Deserialize the address row first to determine the field
		// values.
		row, err := deserializeAddressRow(v)
		if err != nil {
			c.Close()
			return err
		}

		switch row.addrType {
		case adtImport:
			importedAddrSet[string(k)] = row
		}
	}
	c.Close()

	// Delete the private key for all imported addresses.
	for k, row := range importedAddrSet {
		irow, err := deserializeImportedAddress(row)
		if err != nil {
			return err
		}

		// Reserialize the imported address without the private
		// key and store it.
		row.rawData = serializeImportedAddress(
			irow.encryptedPubKey, nil)
		err = bucket.Put([]byte(k), serializeAddressRow(row))
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	return nil
}

// accountNumberToAddrPoolKey converts an account into a meta-bucket key for
// the storage of the next to use address index as the value.
func accountNumberToAddrPoolKey(isInternal bool, account uint32) []byte {
	k := make([]byte, addrPoolMetaKeyLen)
	if isInternal {
		copy(k, addrPoolKeyPrefixInt)
		binary.LittleEndian.PutUint32(k[addrPoolMetaKeyLen-4:], account)
	} else {
		copy(k, addrPoolKeyPrefixExt)
		binary.LittleEndian.PutUint32(k[addrPoolMetaKeyLen-4:], account)
	}

	return k
}

// putNextToUseAddrPoolIdx stores an address pool address index for a
// given account and branch in the meta bucket of the address manager
// database.
func putNextToUseAddrPoolIdx(ns walletdb.ReadWriteBucket, isInternal bool, account uint32, index uint32) error {
	bucket := ns.NestedReadWriteBucket(metaBucketName)
	k := accountNumberToAddrPoolKey(isInternal, account)
	v := make([]byte, 4)
	binary.LittleEndian.PutUint32(v, index)

	err := bucket.Put(k, v)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

// managerExists returns whether or not the manager has already been created
// in the given database namespace.
func managerExists(ns walletdb.ReadBucket) bool {
	mainBucket := ns.NestedReadBucket(mainBucketName)
	return mainBucket != nil
}

// createManagerNS creates the initial namespace structure needed for all of the
// manager data.  This includes things such as all of the buckets as well as the
// version and creation date.
func createManagerNS(ns walletdb.ReadWriteBucket) error {
	mainBucket, err := ns.CreateBucket(mainBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(addrBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(acctBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(addrAcctIdxBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// usedAddrBucketName bucket was added after manager version 1 release
	_, err = ns.CreateBucket(usedAddrBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(acctNameIdxBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(acctIDIdxBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = ns.CreateBucket(metaBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	if err := putLastAccount(ns, DefaultAccountNum); err != nil {
		return err
	}

	if err := putManagerVersion(ns, latestMgrVersion); err != nil {
		return err
	}

	createDate := uint64(time.Now().Unix())
	var dateBytes [8]byte
	binary.LittleEndian.PutUint64(dateBytes[:], createDate)
	err = mainBucket.Put(mgrCreateDateName, dateBytes[:])
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

// upgradeToVersion5 upgrades the database from version 4 to version 5.
// Version 5 uses the metadata bucket to store the address pool indexes,
// so lastAddrs can be removed from the db.
func upgradeToVersion5(ns walletdb.ReadWriteBucket) error {
	lastDefaultAddsrName := []byte("lastaddrs")

	bucket := ns.NestedReadWriteBucket(mainBucketName)
	err := bucket.Delete(lastDefaultAddsrName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return putManagerVersion(ns, 5)
}

// upgradeToVersion6 upgrades the database from version 5 to 6.  Version 6
// removes the synchronization buckets that were no longer updated after
// switching the wallet to storing all block headers.
func upgradeToVersion6(ns walletdb.ReadWriteBucket) error {
	syncBucketName := []byte("sync")
	err := ns.DeleteNestedBucket(syncBucketName)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return putManagerVersion(ns, 6)
}

// upgradeManager upgrades the data in the provided manager namespace to newer
// versions as neeeded.
func upgradeManager(ns walletdb.ReadWriteBucket) error {
	version, err := fetchManagerVersion(ns)
	if err != nil {
		return err
	}

	// Below is some example code on how to properly perform DB
	// upgrades. Use it as a model for future upgrades.
	//
	// Upgrade one version at a time so it is possible to upgrade across
	// an aribtary number of versions without needing to write a bunch of
	// additional code to go directly from version X to Y.
	// if version < 2 {
	// 	// Upgrade from version 1 to 2.
	//	if err := upgradeToVersion2(namespace); err != nil {
	//		return err
	//	}
	//
	//	// The manager is now at version 2.
	//	version = 2
	// }
	// if version < 3 {
	// 	// Upgrade from version 2 to 3.
	//	if err := upgradeToVersion3(namespace); err != nil {
	//		return err
	//	}
	//
	//	// The manager is now at version 3.
	//	version = 3
	// }

	if version < 5 {
		err := upgradeToVersion5(ns)
		if err != nil {
			return err
		}

		// The manager is now at version 5.
		version = 5
	}

	if version < 6 {
		err := upgradeToVersion6(ns)
		if err != nil {
			return err
		}

		// The manager is now at version 6.
		version = 6
	}

	// Ensure the manager version is equal to the version used by the code.
	// This causes failures if the database was not upgraded to the latest
	// version or the there is a newer version that this code does not
	// understand.
	if version != latestMgrVersion {
		return errors.E(errors.Invalid, errors.Errorf("incompatible address manager version %d", version))
	}

	return nil
}
