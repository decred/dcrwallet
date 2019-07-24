// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bdb

import (
	"io"
	"os"

	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	bolt "go.etcd.io/bbolt"
)

// convertErr wraps a driver-specific error with an error code.
func convertErr(err error) error {
	if err == nil {
		return nil
	}
	var kind errors.Kind
	switch err {
	case bolt.ErrInvalid: // Invalid database file, not invalid operation
		kind = errors.IO
	case bolt.ErrDatabaseNotOpen, bolt.ErrTxNotWritable, bolt.ErrTxClosed:
		kind = errors.Invalid
	case bolt.ErrBucketNameRequired, bolt.ErrKeyRequired, bolt.ErrKeyTooLarge, bolt.ErrValueTooLarge, bolt.ErrIncompatibleValue:
		kind = errors.Invalid
	case bolt.ErrBucketNotFound:
		kind = errors.NotExist
	case bolt.ErrBucketExists:
		kind = errors.Exist
	}
	return errors.E(kind, err)
}

// transaction represents a database transaction.  It can either by read-only or
// read-write and implements the walletdb Tx interfaces.  The transaction
// provides a root bucket against which all read and writes occur.
type transaction struct {
	boltTx *bolt.Tx
}

func (tx *transaction) ReadBucket(key []byte) walletdb.ReadBucket {
	return tx.ReadWriteBucket(key)
}

func (tx *transaction) ReadWriteBucket(key []byte) walletdb.ReadWriteBucket {
	boltBucket := tx.boltTx.Bucket(key)
	if boltBucket == nil {
		return nil
	}
	return (*bucket)(boltBucket)
}

func (tx *transaction) CreateTopLevelBucket(key []byte) (walletdb.ReadWriteBucket, error) {
	boltBucket, err := tx.boltTx.CreateBucket(key)
	if err != nil {
		return nil, convertErr(err)
	}
	return (*bucket)(boltBucket), nil
}

func (tx *transaction) DeleteTopLevelBucket(key []byte) error {
	err := tx.boltTx.DeleteBucket(key)
	if err != nil {
		return convertErr(err)
	}
	return nil
}

// Commit commits all changes that have been made through the root bucket and
// all of its sub-buckets to persistent storage.
//
// This function is part of the walletdb.Tx interface implementation.
func (tx *transaction) Commit() error {
	return convertErr(tx.boltTx.Commit())
}

// Rollback undoes all changes that have been made to the root bucket and all of
// its sub-buckets.
//
// This function is part of the walletdb.Tx interface implementation.
func (tx *transaction) Rollback() error {
	return convertErr(tx.boltTx.Rollback())
}

// bucket is an internal type used to represent a collection of key/value pairs
// and implements the walletdb Bucket interfaces.
type bucket bolt.Bucket

// Enforce bucket implements the walletdb Bucket interfaces.
var _ walletdb.ReadWriteBucket = (*bucket)(nil)

// NestedReadWriteBucket retrieves a nested bucket with the given key.  Returns
// nil if the bucket does not exist.
//
// This function is part of the walletdb.ReadWriteBucket interface implementation.
func (b *bucket) NestedReadWriteBucket(key []byte) walletdb.ReadWriteBucket {
	boltBucket := (*bolt.Bucket)(b).Bucket(key)
	// Don't return a non-nil interface to a nil pointer.
	if boltBucket == nil {
		return nil
	}
	return (*bucket)(boltBucket)
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
	boltBucket, err := (*bolt.Bucket)(b).CreateBucket(key)
	if err != nil {
		return nil, convertErr(err)
	}
	return (*bucket)(boltBucket), nil
}

// CreateBucketIfNotExists creates and returns a new nested bucket with the
// given key if it does not already exist.  Errors with code Invalid if the key
// is empty or otherwise invalid for the driver.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) CreateBucketIfNotExists(key []byte) (walletdb.ReadWriteBucket, error) {
	boltBucket, err := (*bolt.Bucket)(b).CreateBucketIfNotExists(key)
	if err != nil {
		return nil, convertErr(err)
	}
	return (*bucket)(boltBucket), nil
}

// DeleteNestedBucket removes a nested bucket with the given key.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) DeleteNestedBucket(key []byte) error {
	return convertErr((*bolt.Bucket)(b).DeleteBucket(key))
}

// ForEach invokes the passed function with every key/value pair in the bucket.
// This includes nested buckets, in which case the value is nil, but it does not
// include the key/value pairs within those nested buckets.
//
// NOTE: The values returned by this function are only valid during a
// transaction.  Attempting to access them after a transaction has ended will
// likely result in an access violation.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) ForEach(fn func(k, v []byte) error) error {
	return convertErr((*bolt.Bucket)(b).ForEach(fn))
}

// Put saves the specified key/value pair to the bucket.  Keys that do not
// already exist are added and keys that already exist are overwritten.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) Put(key, value []byte) error {
	return convertErr((*bolt.Bucket)(b).Put(key, value))
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
	return (*bolt.Bucket)(b).Get(key)
}

// Delete removes the specified key from the bucket.  Deleting a key that does
// not exist does not return an error.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) Delete(key []byte) error {
	return convertErr((*bolt.Bucket)(b).Delete(key))
}

func (b *bucket) ReadCursor() walletdb.ReadCursor {
	return b.ReadWriteCursor()
}

// ReadWriteCursor returns a new cursor, allowing for iteration over the bucket's
// key/value pairs and nested buckets in forward or backward order.
//
// This function is part of the walletdb.Bucket interface implementation.
func (b *bucket) ReadWriteCursor() walletdb.ReadWriteCursor {
	return (*cursor)((*bolt.Bucket)(b).Cursor())
}

// cursor represents a cursor over key/value pairs and nested buckets of a
// bucket.
//
// Note that open cursors are not tracked on bucket changes and any
// modifications to the bucket, with the exception of cursor.Delete, invalidate
// the cursor. After invalidation, the cursor must be repositioned, or the keys
// and values returned may be unpredictable.
type cursor bolt.Cursor

// Delete removes the current key/value pair the cursor is at without
// invalidating the cursor.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Delete() error {
	return convertErr((*bolt.Cursor)(c).Delete())
}

// First positions the cursor at the first key/value pair and returns the pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) First() (key, value []byte) {
	return (*bolt.Cursor)(c).First()
}

// Last positions the cursor at the last key/value pair and returns the pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Last() (key, value []byte) {
	return (*bolt.Cursor)(c).Last()
}

// Next moves the cursor one key/value pair forward and returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Next() (key, value []byte) {
	return (*bolt.Cursor)(c).Next()
}

// Prev moves the cursor one key/value pair backward and returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Prev() (key, value []byte) {
	return (*bolt.Cursor)(c).Prev()
}

// Seek positions the cursor at the passed seek key. If the key does not exist,
// the cursor is moved to the next key after seek. Returns the new pair.
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Seek(seek []byte) (key, value []byte) {
	return (*bolt.Cursor)(c).Seek(seek)
}

// Closes the cursor
//
// This function is part of the walletdb.Cursor interface implementation.
func (c *cursor) Close() {}

// db represents a collection of namespaces which are persisted and implements
// the walletdb.Db interface.  All database access is performed through
// transactions which are obtained through the specific Namespace.
type db bolt.DB

// Enforce db implements the walletdb.Db interface.
var _ walletdb.DB = (*db)(nil)

func (db *db) beginTx(writable bool) (*transaction, error) {
	boltTx, err := (*bolt.DB)(db).Begin(writable)
	if err != nil {
		return nil, convertErr(err)
	}
	return &transaction{boltTx: boltTx}, nil
}

func (db *db) BeginReadTx() (walletdb.ReadTx, error) {
	return db.beginTx(false)
}

func (db *db) BeginReadWriteTx() (walletdb.ReadWriteTx, error) {
	return db.beginTx(true)
}

// Copy writes a copy of the database to the provided writer.  This call will
// start a read-only transaction to perform all operations.
//
// This function is part of the walletdb.Db interface implementation.
func (db *db) Copy(w io.Writer) error {
	return convertErr((*bolt.DB)(db).View(func(tx *bolt.Tx) error {
		return tx.Copy(w)
	}))
}

// Close cleanly shuts down the database and syncs all data.
//
// This function is part of the walletdb.Db interface implementation.
func (db *db) Close() error {
	return convertErr((*bolt.DB)(db).Close())
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// openDB opens the database at the provided path.
func openDB(dbPath string, create bool) (walletdb.DB, error) {
	if !create && !fileExists(dbPath) {
		return nil, errors.E(errors.NotExist, "missing database file")
	}

	boltDB, err := bolt.Open(dbPath, 0600, nil)
	return (*db)(boltDB), convertErr(err)
}
