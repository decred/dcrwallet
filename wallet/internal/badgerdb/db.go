// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package badgerdb

import (
	"bytes"
	"fmt"
	"os"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
)

const (
	metaBucket byte = 'b'
)

// unsafely return value of an item.  panics on value errors.
func unsafeValue(item *badger.Item) []byte {
	var v []byte
	err := item.Value(func(value []byte) error {
		v = value
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("item.Value: %v", err)) // XXX
	}
	return v
}

// unsafely return stripped key and value for an item.
// panics on value errors.
func itemKV(stripPrefix []byte, item *badger.Item) (k, v []byte) {
	k, v = item.Key(), unsafeValue(item)
	if !bytes.HasPrefix(k, stripPrefix) {
		return nil, nil
	}
	k = k[len(stripPrefix):]
	return k, v
}

// creates the prefix for a top level bucket.
func topLevelPrefix(key []byte) []byte {
	prefix := make([]byte, 0, len(key)+3)
	prefix = append(prefix, '{')
	prefix = append(prefix, key...)
	prefix = append(prefix, "}/"...)
	return prefix
}

// append key to the bucket prefix, reusing the alloc when possible.
func reusePrefixedKey(prefix *[]byte, key []byte) []byte {
	appendedKey := append(*prefix, key...)
	*prefix = appendedKey[:len(*prefix)]
	return appendedKey
}

// append key to the bucket prefix, always creating a new allocation to do so.
func allocPrefixedKey(prefix []byte, key []byte) []byte {
	return append(prefix[:len(prefix):len(prefix)], key...)
}

// creates a new bucket prefix from a parent bucket prefix and the child
// bucket name.
func nestedBucketPrefix(parentPrefix, child []byte) []byte {
	prefix := make([]byte, 0, len(parentPrefix)+1+len(child))
	prefix = append(prefix, parentPrefix[:len(parentPrefix)-2]...)
	prefix = append(prefix, ',')
	prefix = append(prefix, child...)
	prefix = append(prefix, "}/"...)
	return prefix
}

// strips bucket prefix from a key.
// panics if the key does not begin with the prefix.
func strippedKey(prefix, key []byte) []byte {
	if !bytes.HasPrefix(key, prefix) {
		panic(fmt.Sprintf("key %q does not have prefix %q", key, prefix)) // XXX
	}
	return key[len(prefix):]
}

// convertErr wraps a driver-specific error with an error code.
func convertErr(err error) error {
	if err == nil {
		return nil
	}
	var kind errors.Kind
	switch err {
	case badger.ErrBannedKey, badger.ErrBlockedWrites, badger.ErrDBClosed, badger.ErrDiscardedTxn, badger.ErrEmptyKey,
		badger.ErrEncryptionKeyMismatch, badger.ErrGCInMemoryMode, badger.ErrInvalidDataKeyID, badger.ErrInvalidDump,
		badger.ErrInvalidEncryptionKey, badger.ErrInvalidKey, badger.ErrInvalidRequest, badger.ErrManagedTxn,
		badger.ErrNamespaceMode, badger.ErrNilCallback, badger.ErrNoRewrite, badger.ErrPlan9NotSupported,
		badger.ErrReadOnlyTxn, badger.ErrRejected, badger.ErrThresholdZero, badger.ErrTruncateNeeded,
		badger.ErrTxnTooBig, badger.ErrValueLogSize, badger.ErrWindowsNotSupported, badger.ErrZeroBandwidth:
		kind = errors.Invalid
	case badger.ErrKeyNotFound:
		kind = errors.NotExist
	case badger.ErrConflict:
		kind = errors.IO
	}
	return errors.E(kind, err)
}

// transaction represents a database transaction.  It can either by read-only or
// read-write and implements the walletdb Tx interfaces.
type transaction struct {
	txn    *badger.Txn
	closed bool
}

func (tx *transaction) ReadBucket(key []byte) walletdb.ReadBucket {
	return tx.ReadWriteBucket(key)
}

func (tx *transaction) ReadWriteBucket(key []byte) walletdb.ReadWriteBucket {
	// XXX: close enough
	b, _ := tx.CreateTopLevelBucket(key)
	return b
}

func (tx *transaction) CreateTopLevelBucket(key []byte) (walletdb.ReadWriteBucket, error) {
	prefix := topLevelPrefix(key)
	b := &bucket{
		prefix: prefix,
		txn:    tx.txn,
	}
	return b, nil
}

func (tx *transaction) DeleteTopLevelBucket(key []byte) error {
	prefix := topLevelPrefix(key)
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	iter := tx.txn.NewIterator(opts)
	defer iter.Close()
	for iter.Rewind(); iter.ValidForPrefix(prefix); iter.Next() {
		item := iter.Item()
		key := item.Key()
		err := tx.txn.Delete(key)
		if err != nil {
			return convertErr(err)
		}
	}
	return nil
}

// Commit commits all changes that have been made through the root bucket and
// all of its sub-buckets to persistent storage.
//
// This function is part of the walletdb.Tx interface implementation.
func (tx *transaction) Commit() error {
	if tx.closed {
		return convertErr(badger.ErrDiscardedTxn)
	}
	err := tx.txn.Commit()
	tx.closed = true
	return convertErr(err)
}

// Rollback undoes all changes that have been made to the root bucket and all of
// its sub-buckets.
//
// This function is part of the walletdb.Tx interface implementation.
func (tx *transaction) Rollback() error {
	tx.txn.Discard()
	if tx.closed {
		return convertErr(badger.ErrDiscardedTxn)
	}
	tx.closed = true
	return nil
}

// bucket is an internal type used to represent a collection of key/value pairs
// and implements the walletdb Bucket interfaces.
type bucket struct {
	// badger does not implement anything similar to bbolt's buckets, so
	// this is done via a prefix on all keys.  There is no built in
	// namespace separation between nested buckets and keys in the outer
	// bucket with the same prefix.
	//
	// To work around this, we use a "{key1,key2,...}/" prefix.  An empty
	// entry with special bucket metadata marks the existence of the
	// bucket.  Nested buckets can have up to two metadata entries: one
	// for the nested bucket's full prefix, and another in the parent
	// bucket (if any) to signal the bucket's existence during cursor
	// iteration.
	prefix []byte
	txn    *badger.Txn
}

// Enforce bucket implements the walletdb Bucket interfaces.
var _ walletdb.ReadWriteBucket = (*bucket)(nil)

// NestedReadWriteBucket retrieves a nested bucket with the given key.  Returns
// nil if the bucket does not exist.
//
// This function is part of the walletdb.ReadWriteBucket interface implementation.
func (b *bucket) NestedReadWriteBucket(key []byte) walletdb.ReadWriteBucket {
	prefix := nestedBucketPrefix(b.prefix, key)
	item, err := b.txn.Get(prefix)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err == nil && item.UserMeta() != metaBucket {
		return nil
	}
	nestedBucket := &bucket{
		prefix: prefix,
		txn:    b.txn,
	}
	return nestedBucket
}

func (b *bucket) NestedReadBucket(key []byte) walletdb.ReadBucket {
	return b.NestedReadWriteBucket(key)
}

// CreateBucket creates and returns a new nested bucket with the given key.
// Errors with code Exist if the bucket already exists, and Invalid if the key
// is empty or otherwise invalid for the driver.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) CreateBucket(key []byte) (walletdb.ReadWriteBucket, error) {
	if len(key) == 0 {
		return nil, convertErr(badger.ErrEmptyKey)
	}
	prefix := nestedBucketPrefix(b.prefix, key)
	_, err := b.txn.Get(prefix)
	if !errors.Is(err, badger.ErrKeyNotFound) {
		return nil, errors.E(errors.Exist, "CreateBucket: bucket exists")
	}
	e := badger.NewEntry(prefix, nil)
	e.UserMeta = metaBucket
	err = b.txn.SetEntry(e)
	if err != nil {
		return nil, convertErr(err)
	}
	nestedBucket := &bucket{
		prefix: prefix,
		txn:    b.txn,
	}
	return nestedBucket, nil
}

// CreateBucketIfNotExists creates and returns a new nested bucket with the
// given key if it does not already exist.  Errors with code Invalid if the key
// is empty or otherwise invalid for the driver.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) CreateBucketIfNotExists(key []byte) (walletdb.ReadWriteBucket, error) {
	if len(key) == 0 {
		return nil, convertErr(badger.ErrEmptyKey)
	}
	prefix := nestedBucketPrefix(b.prefix, key)
	_, err := b.txn.Get(prefix)
	if errors.Is(err, badger.ErrKeyNotFound) {
		e := badger.NewEntry(prefix, nil)
		e.UserMeta = metaBucket
		err := b.txn.SetEntry(e)
		if err != nil {
			return nil, convertErr(err)
		}
	}
	nestedBucket := &bucket{
		prefix: prefix,
		txn:    b.txn,
	}
	return nestedBucket, nil
}

// DeleteNestedBucket removes a nested bucket with the given key.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) DeleteNestedBucket(key []byte) error {
	if len(key) == 0 {
		return convertErr(badger.ErrEmptyKey)
	}
	prefix := nestedBucketPrefix(b.prefix, key)
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix
	iter := b.txn.NewIterator(opts)
	defer iter.Close()
	var deleted bool
	for iter.Rewind(); iter.ValidForPrefix(prefix); iter.Next() {
		item := iter.Item()
		key := item.Key()
		err := b.txn.Delete(key)
		if err != nil {
			return convertErr(err)
		}
		deleted = true
	}
	if !deleted {
		return errors.E(errors.NotExist, "DeletedNestedBucket: nested bucket does not exist")
	}
	return nil
}

// ForEach invokes the passed function with every key/value pair in the bucket.
// This includes nested buckets, in which case the value is nil, but it does not
// include the key/value pairs within those nested buckets.
// XXX: above is very much untrue in the current impl.  nested buckets will not be
// iterated over at all.
//
// NOTE: The values returned by this function are only valid during a
// transaction.  Attempting to access them after a transaction has ended will
// likely result in an access violation.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) ForEach(fn func(k, v []byte) error) error {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = b.prefix
	iter := b.txn.NewIterator(opts)
	defer iter.Close()
	for iter.Rewind(); iter.ValidForPrefix(b.prefix); iter.Next() {
		item := iter.Item()
		k := item.Key()
		// Ignore metadata for the (non-child) bucket.
		if item.UserMeta() == metaBucket && len(k) == len(b.prefix) {
			continue
		}
		k = strippedKey(b.prefix, k)
		err := item.Value(func(v []byte) error {
			return fn(k, v)
		})
		if err != nil {
			return convertErr(err)
		}
	}
	return nil
}

// Put saves the specified key/value pair to the bucket.  Keys that do not
// already exist are added and keys that already exist are overwritten.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) Put(key, value []byte) error {
	if len(key) == 0 {
		return convertErr(badger.ErrEmptyKey)
	}
	err := b.txn.Set(allocPrefixedKey(b.prefix, key), value)
	return convertErr(err)
}

// Get returns the value for the given key.  Returns nil if the key does
// not exist in this bucket (or nested buckets).
//
// NOTE: The value returned by this function is only valid during a
// transaction.  Attempting to access it after a transaction has ended
// will likely result in an access violation.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) Get(key []byte) []byte {
	item, err := b.txn.Get(reusePrefixedKey(&b.prefix, key))
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		panic(fmt.Sprintf("badger.Txn.Get: %v", err)) // XXX
	}
	return unsafeValue(item)
}

// Delete removes the specified key from the bucket.  Deleting a key that does
// not exist does not return an error.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) Delete(key []byte) error {
	if len(key) == 0 {
		return convertErr(badger.ErrEmptyKey)
	}
	err := b.txn.Delete(allocPrefixedKey(b.prefix, key))
	return convertErr(err)
}

// KeyN returns the number of keys and value pairs inside a bucket.
//
// This function is part of the walletdb.ReadBucket interface implementation.
func (b *bucket) KeyN() int {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = b.prefix
	iter := b.txn.NewIterator(opts)
	defer iter.Close()
	var count int
	for iter.Rewind(); iter.ValidForPrefix(b.prefix); iter.Next() {
		item := iter.Item()
		// Skip the metadata entry for the bucket prefix.
		if item.UserMeta() == metaBucket {
			continue
		}
		count++
	}
	return count
}

func (b *bucket) ReadCursor() walletdb.ReadCursor {
	return b.ReadWriteCursor()
}

func (b *bucket) ReverseReadCursor() walletdb.ReadCursor {
	return b.ReverseReadWriteCursor()
}

// ReadWriteCursor returns a new cursor, allowing for iteration over the bucket's
// key/value pairs and nested buckets in forward order.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) ReadWriteCursor() walletdb.ReadWriteCursor {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = b.prefix
	iter := b.txn.NewIterator(opts)
	c := &cursor{
		reverse: false,
		prefix:  b.prefix,
		txn:     b.txn,
		iter:    iter,
	}
	return c
}

// ReadWriteCursor returns a new cursor, allowing for iteration over the bucket's
// key/value pairs and nested buckets in reverse order.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) ReverseReadWriteCursor() walletdb.ReadWriteCursor {
	opts := badger.DefaultIteratorOptions
	opts.Prefix = b.prefix
	opts.Reverse = true
	iter := b.txn.NewIterator(opts)
	c := &cursor{
		reverse: true,
		prefix:  b.prefix,
		txn:     b.txn,
		iter:    iter,
	}
	return c
}

// cursor represents a cursor over key/value pairs and nested buckets of a
// bucket.
//
// Note that open cursors are not tracked on bucket changes and any
// modifications to the bucket, with the exception of cursor.Delete, invalidate
// the cursor. After invalidation, the cursor must be repositioned, or the keys
// and values returned may be unpredictable.
type cursor struct {
	reverse bool
	prefix  []byte
	txn     *badger.Txn
	iter    *badger.Iterator
}

// Delete removes the current key/value pair the cursor is at without
// invalidating the cursor.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Delete() error {
	key := c.iter.Item().Key()
	err := c.txn.Delete(key)
	return convertErr(err)
}

// First positions the cursor at the first key/value pair and returns the pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) First() (key, value []byte) {
	if c.reverse {
		panic("First called on reverse cursor")
	}
	c.iter.Rewind()
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	// Skip the metadata entry for the bucket prefix.
	if item := c.iter.Item(); item.UserMeta() == metaBucket && len(item.Key()) == len(c.prefix) {
		c.iter.Next()
	}
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	item := c.iter.Item()
	return itemKV(c.prefix, item)
}

// Next moves the cursor one key/value pair forward and returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Next() (key, value []byte) {
	if c.reverse {
		return c.prev()
	}
	c.iter.Next()
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	// Skip the metadata entry for the bucket prefix.
	if item := c.iter.Item(); item.UserMeta() == metaBucket && len(item.Key()) == len(c.prefix) {
		c.iter.Next()
	}
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	item := c.iter.Item()
	return itemKV(c.prefix, item)
}

// prev moves the cursor one key/value pair backward and returns the new pair.
func (c *cursor) prev() (key, value []byte) {
	if !c.reverse {
		panic("prev called on forwards cursor")
	}
	c.iter.Next()
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	item := c.iter.Item()
	// Skip the metadata entry for the bucket prefix.
	if item.UserMeta() == metaBucket && len(item.Key()) == len(c.prefix) {
		return nil, nil
	}
	k, v := itemKV(c.prefix, item)
	return k, v
}

// Seek positions the cursor at the passed seek key. If the key does not exist,
// the cursor is moved to the next key after seek. Returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Seek(seek []byte) (key, value []byte) {
	c.iter.Seek(reusePrefixedKey(&c.prefix, seek))
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	// Skip the metadata entry for the bucket prefix.
	if item := c.iter.Item(); item.UserMeta() == metaBucket && len(item.Key()) == len(c.prefix) {
		c.iter.Next()
	}
	if !c.iter.ValidForPrefix(c.prefix) {
		return nil, nil
	}
	item := c.iter.Item()
	k, v := itemKV(c.prefix, item)
	return k, v
}

// Closes the cursor
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Close() {
	c.iter.Close()
}

// db represents a collection of namespaces which are persisted and implements
// the walletdb.Db interface.  All database access is performed through
// transactions which are obtained through the specific Namespace.
type db struct {
	db *badger.DB
}

// Enforce db implements the walletdb.Db interface.
var _ walletdb.DB = (*db)(nil)

func (db *db) beginTx(writable bool) (*transaction, error) {
	txn := db.db.NewTransaction(writable)
	if db.db.IsClosed() {
		return nil, convertErr(badger.ErrDBClosed)
	}
	return &transaction{txn: txn}, nil
}

func (db *db) BeginReadTx() (walletdb.ReadTx, error) {
	return db.beginTx(false)
}

func (db *db) BeginReadWriteTx() (walletdb.ReadWriteTx, error) {
	return db.beginTx(true)
}

// Close cleanly shuts down the database and syncs all data.
//
// This function is part of the walletdb.Db interface implementation.
func (db *db) Close() error {
	return convertErr(db.db.Close())
}

// dirExists returns whether the file with name exists and is a directory.
func dirExists(name string) bool {
	if stat, err := os.Stat(name); err == nil {
		return stat.IsDir()
	}
	return false
}

// openDB opens the database at the provided path.
func openDB(dbPath string, create bool) (walletdb.DB, error) {
	if !create && !dirExists(dbPath) {
		return nil, errors.E(errors.NotExist, "missing database directory")
	}

	opts := badger.DefaultOptions(dbPath)
	opts.ChecksumVerificationMode = options.OnTableAndBlockRead
	opts.VerifyValueChecksum = true
	opts.Logger = nil
	badgerDB, err := badger.Open(opts)
	if err != nil {
		return nil, convertErr(err)
	}
	return &db{badgerDB}, nil
}
