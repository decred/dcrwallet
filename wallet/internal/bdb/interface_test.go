// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file intended to be copied into each backend driver directory.  Each
// driver should have their own driver_test.go file which creates a database and
// invokes the testInterface function in this file to ensure the driver properly
// implements the interface.  See the bdb backend driver for a working example.
//
// NOTE: When copying this file into the backend driver folder, the package name
// will need to be changed accordingly.

// Test must be updated for API changes.

package bdb_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// errSubTestFail is used to signal that a sub test returned false.
var errSubTestFail = errors.Errorf("sub test failure")

// testContext is used to store context information about a running test which
// is passed into helper functions.
type testContext struct {
	t           *testing.T
	db          walletdb.DB
	bucketDepth int
	isWritable  bool
}

// rollbackValues returns a copy of the provided map with all values set to an
// empty string.  This is used to test that values are properly rolled back.
func rollbackValues(values map[string]string) map[string]string {
	retMap := make(map[string]string, len(values))
	for k := range values {
		retMap[k] = ""
	}
	return retMap
}

// testGetValues checks that all of the provided key/value pairs can be
// retrieved from the database and the retrieved values match the provided
// values.
func testGetValues(tc *testContext, bucket walletdb.ReadBucket, values map[string]string) bool {
	for k, v := range values {
		var vBytes []byte
		if v != "" {
			vBytes = []byte(v)
		}

		gotValue := bucket.Get([]byte(k))
		if !bytes.Equal(gotValue, vBytes) {
			tc.t.Errorf("Get: unexpected value - got %s, want %s",
				gotValue, vBytes)
			return false
		}
	}

	return true
}

// testPutValues stores all of the provided key/value pairs in the provided
// bucket while checking for errors.
func testPutValues(tc *testContext, bucket walletdb.ReadWriteBucket, values map[string]string) bool {
	for k, v := range values {
		var vBytes []byte
		if v != "" {
			vBytes = []byte(v)
		}
		if err := bucket.Put([]byte(k), vBytes); err != nil {
			tc.t.Errorf("Put: unexpected error: %v", err)
			return false
		}
	}

	return true
}

// testDeleteValues removes all of the provided key/value pairs from the
// provided bucket.
func testDeleteValues(tc *testContext, bucket walletdb.ReadWriteBucket, values map[string]string) bool {
	for k := range values {
		if err := bucket.Delete([]byte(k)); err != nil {
			tc.t.Errorf("Delete: unexpected error: %v", err)
			return false
		}
	}

	return true
}

// testNestedReadWriteBucket reruns the testReadWriteBucketInterface against a
// nested bucket along with a counter to only test a couple of level deep.
func testNestedReadWriteBucket(tc *testContext, testBucket walletdb.ReadWriteBucket) bool {
	// Don't go more than 2 nested level deep.
	if tc.bucketDepth > 1 {
		return true
	}

	tc.bucketDepth++
	defer func() {
		tc.bucketDepth--
	}()
	if !testReadWriteBucketInterface(tc, testBucket) {
		return false
	}

	return true
}

// testReadWriteBucketInterface ensures the bucket interface is working
// properly by exercising all of its functions.
func testReadWriteBucketInterface(tc *testContext, bucket walletdb.ReadWriteBucket) bool {
	// keyValues holds the keys and values to use when putting
	// values into the bucket.
	var keyValues = map[string]string{
		"bucketkey1": "foo1",
		"bucketkey2": "foo2",
		"bucketkey3": "foo3",
	}
	if !testPutValues(tc, bucket, keyValues) {
		return false
	}

	if !testGetValues(tc, bucket, keyValues) {
		return false
	}

	// Iterate all of the keys using ForEach while making sure the
	// stored values are the expected values.
	keysFound := make(map[string]struct{}, len(keyValues))
	err := bucket.ForEach(func(k, v []byte) error {
		kString := string(k)
		wantV, ok := keyValues[kString]
		if !ok {
			return errors.Errorf("ForEach: key '%s' should "+
				"exist", kString)
		}

		if !bytes.Equal(v, []byte(wantV)) {
			return errors.Errorf("ForEach: value for key '%s' "+
				"does not match - got %s, want %s",
				kString, v, wantV)
		}

		keysFound[kString] = struct{}{}
		return nil
	})
	if err != nil {
		tc.t.Errorf("%v", err)
		return false
	}

	// Ensure all keys were iterated.
	for k := range keyValues {
		if _, ok := keysFound[k]; !ok {
			tc.t.Errorf("ForEach: key '%s' was not iterated "+
				"when it should have been", k)
			return false
		}
	}

	// Delete the keys and ensure they were deleted.
	if !testDeleteValues(tc, bucket, keyValues) {
		return false
	}
	if !testGetValues(tc, bucket, rollbackValues(keyValues)) {
		return false
	}

	// Ensure creating a new bucket works as expected.
	testBucketName := []byte("testbucket")
	testBucket, err := bucket.CreateBucket(testBucketName)
	if err != nil {
		tc.t.Errorf("CreateBucket: unexpected error: %v", err)
		return false
	}
	if !testNestedReadWriteBucket(tc, testBucket) {
		return false
	}

	// Ensure creating a bucket that already exists fails with the
	// expected error.
	if _, err := bucket.CreateBucket(testBucketName); !errors.Is(errors.Exist, err) {
		tc.t.Errorf("CreateBucket: unexpected error: %v", err)
		return false
	}

	// Ensure CreateBucketIfNotExists returns an existing bucket.
	testBucket, err = bucket.CreateBucketIfNotExists(testBucketName)
	if err != nil {
		tc.t.Errorf("CreateBucketIfNotExists: unexpected "+
			"error: %v", err)
		return false
	}
	if !testNestedReadWriteBucket(tc, testBucket) {
		return false
	}

	// Ensure retrieving and existing bucket works as expected.
	testBucket = bucket.NestedReadWriteBucket(testBucketName)
	if !testNestedReadWriteBucket(tc, testBucket) {
		return false
	}

	// Ensure deleting a bucket works as intended.
	if err := bucket.DeleteNestedBucket(testBucketName); err != nil {
		tc.t.Errorf("DeleteBucket: unexpected error: %v", err)
		return false
	}
	if b := bucket.NestedReadWriteBucket(testBucketName); b != nil {
		tc.t.Errorf("DeleteBucket: bucket '%s' still exists",
			testBucketName)
		return false
	}

	// Ensure deleting a bucket that doesn't exist returns the
	// expected error.
	if err := bucket.DeleteNestedBucket(testBucketName); !errors.Is(errors.NotExist, err) {
		tc.t.Errorf("DeleteBucket: unexpected error: %v", err)
		return false
	}

	// Ensure CreateBucketIfNotExists creates a new bucket when
	// it doesn't already exist.
	testBucket, err = bucket.CreateBucketIfNotExists(testBucketName)
	if err != nil {
		tc.t.Errorf("CreateBucketIfNotExists: unexpected error: %v", err)
		return false
	}
	if !testNestedReadWriteBucket(tc, testBucket) {
		return false
	}

	// Delete the test bucket to avoid leaving it around for future
	// calls.
	if err := bucket.DeleteNestedBucket(testBucketName); err != nil {
		tc.t.Errorf("DeleteBucket: unexpected error: %v", err)
		return false
	}
	if b := bucket.NestedReadWriteBucket(testBucketName); b != nil {
		tc.t.Errorf("DeleteBucket: bucket '%s' still exists",
			testBucketName)
		return false
	}

	return true
}

// testManualTxInterface ensures that manual transactions work as expected.
func testManualTxInterface(tc *testContext, bucketKey []byte) bool {
	db := tc.db

	// populateValues tests that populating values works as expected.
	//
	// When the writable flag is false, a read-only tranasction is created,
	// standard bucket tests for read-only transactions are performed, and
	// the Commit function is checked to ensure it fails as expected.
	//
	// Otherwise, a read-write transaction is created, the values are
	// written, standard bucket tests for read-write transactions are
	// performed, and then the transaction is either committed or rolled
	// back depending on the flag.
	populateValues := func(writable, rollback bool, putValues map[string]string) bool {
		var dbtx walletdb.ReadTx
		var rootBucket walletdb.ReadBucket
		var err error
		if writable {
			dbtx, err = db.BeginReadWriteTx()
			if err != nil {
				tc.t.Errorf("BeginReadWriteTx: unexpected error %v", err)
				return false
			}
			rootBucket = dbtx.(walletdb.ReadWriteTx).ReadWriteBucket(bucketKey)
		} else {
			dbtx, err = db.BeginReadTx()
			if err != nil {
				tc.t.Errorf("BeginReadTx: unexpected error %v", err)
				return false
			}
			rootBucket = dbtx.ReadBucket(bucketKey)
		}
		if rootBucket == nil {
			tc.t.Errorf("ReadWriteBucket/ReadBucket: unexpected nil root bucket")
			_ = dbtx.Rollback()
			return false
		}

		if writable {
			tc.isWritable = writable
			if !testReadWriteBucketInterface(tc, rootBucket.(walletdb.ReadWriteBucket)) {
				_ = dbtx.Rollback()
				return false
			}
		}

		if !writable {
			// Rollback the transaction.
			if err := dbtx.Rollback(); err != nil {
				tc.t.Errorf("Commit: unexpected error %v", err)
				return false
			}
		} else {
			rootBucket := rootBucket.(walletdb.ReadWriteBucket)
			if !testPutValues(tc, rootBucket, putValues) {
				return false
			}

			if rollback {
				// Rollback the transaction.
				if err := dbtx.Rollback(); err != nil {
					tc.t.Errorf("Rollback: unexpected "+
						"error %v", err)
					return false
				}
			} else {
				// The commit should succeed.
				if err := dbtx.(walletdb.ReadWriteTx).Commit(); err != nil {
					tc.t.Errorf("Commit: unexpected error "+
						"%v", err)
					return false
				}
			}
		}

		return true
	}

	// checkValues starts a read-only transaction and checks that all of
	// the key/value pairs specified in the expectedValues parameter match
	// what's in the database.
	checkValues := func(expectedValues map[string]string) bool {
		// Begin another read-only transaction to ensure...
		dbtx, err := db.BeginReadTx()
		if err != nil {
			tc.t.Errorf("BeginReadTx: unexpected error %v", err)
			return false
		}

		rootBucket := dbtx.ReadBucket(bucketKey)
		if rootBucket == nil {
			tc.t.Errorf("ReadBucket: unexpected nil root bucket")
			_ = dbtx.Rollback()
			return false
		}

		if !testGetValues(tc, rootBucket, expectedValues) {
			_ = dbtx.Rollback()
			return false
		}

		// Rollback the read-only transaction.
		if err := dbtx.Rollback(); err != nil {
			tc.t.Errorf("Commit: unexpected error %v", err)
			return false
		}

		return true
	}

	// deleteValues starts a read-write transaction and deletes the keys
	// in the passed key/value pairs.
	deleteValues := func(values map[string]string) bool {
		dbtx, err := db.BeginReadWriteTx()
		if err != nil {
			tc.t.Errorf("BeginReadWriteTx: unexpected error %v", err)
			_ = dbtx.Rollback()
			return false
		}

		rootBucket := dbtx.ReadWriteBucket(bucketKey)
		if rootBucket == nil {
			tc.t.Errorf("RootBucket: unexpected nil root bucket")
			_ = dbtx.Rollback()
			return false
		}

		// Delete the keys and ensure they were deleted.
		if !testDeleteValues(tc, rootBucket, values) {
			_ = dbtx.Rollback()
			return false
		}
		if !testGetValues(tc, rootBucket, rollbackValues(values)) {
			_ = dbtx.Rollback()
			return false
		}

		// Commit the changes and ensure it was successful.
		if err := dbtx.Commit(); err != nil {
			tc.t.Errorf("Commit: unexpected error %v", err)
			return false
		}

		return true
	}

	// keyValues holds the keys and values to use when putting values
	// into a bucket.
	var keyValues = map[string]string{
		"umtxkey1": "foo1",
		"umtxkey2": "foo2",
		"umtxkey3": "foo3",
	}

	// Ensure that attempting populating the values using a read-only
	// transaction fails as expected.
	if !populateValues(false, true, keyValues) {
		return false
	}
	if !checkValues(rollbackValues(keyValues)) {
		return false
	}

	// Ensure that attempting populating the values using a read-write
	// transaction and then rolling it back yields the expected values.
	if !populateValues(true, true, keyValues) {
		return false
	}
	if !checkValues(rollbackValues(keyValues)) {
		return false
	}

	// Ensure that attempting populating the values using a read-write
	// transaction and then committing it stores the expected values.
	if !populateValues(true, false, keyValues) {
		return false
	}
	if !checkValues(keyValues) {
		return false
	}

	// Clean up the keys.
	if !deleteValues(keyValues) {
		return false
	}

	return true
}

// testNamespaceAndTxInterfaces creates a namespace using the provided key and
// tests all facets of it interface as well as  transaction and bucket
// interfaces under it.
func testNamespaceAndTxInterfaces(tc *testContext, namespaceKey string) bool {
	namespaceKeyBytes := []byte(namespaceKey)
	err := walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		_, err := tx.CreateTopLevelBucket(namespaceKeyBytes)
		return err
	})
	if err != nil {
		tc.t.Errorf("CreateTopLevelBucket: unexpected error: %v", err)
		return false
	}
	defer func() {
		// Remove the namespace now that the tests are done for it.
		err := walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
			return tx.DeleteTopLevelBucket(namespaceKeyBytes)
		})
		if err != nil {
			tc.t.Errorf("DeleteTopLevelBucket: unexpected error: %v", err)
			return
		}
	}()

	if !testManualTxInterface(tc, namespaceKeyBytes) {
		return false
	}

	// keyValues holds the keys and values to use when putting values
	// into a bucket.
	var keyValues = map[string]string{
		"mtxkey1": "foo1",
		"mtxkey2": "foo2",
		"mtxkey3": "foo3",
	}

	// Test the bucket interface via a managed read-only transaction.
	err = walletdb.View(tc.db, func(tx walletdb.ReadTx) error {
		rootBucket := tx.ReadBucket(namespaceKeyBytes)
		if rootBucket == nil {
			return fmt.Errorf("ReadBucket: unexpected nil root bucket")
		}

		return nil
	})
	if err != nil {
		if err != errSubTestFail {
			tc.t.Errorf("%v", err)
		}
		return false
	}

	// Test the bucket interface via a managed read-write transaction.
	// Also, put a series of values and force a rollback so the following
	// code can ensure the values were not stored.
	forceRollbackError := fmt.Errorf("force rollback")
	err = walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		rootBucket := tx.ReadWriteBucket(namespaceKeyBytes)
		if rootBucket == nil {
			return fmt.Errorf("ReadWriteBucket: unexpected nil root bucket")
		}

		tc.isWritable = true
		if !testReadWriteBucketInterface(tc, rootBucket) {
			return errSubTestFail
		}

		if !testPutValues(tc, rootBucket, keyValues) {
			return errSubTestFail
		}

		// Return an error to force a rollback.
		return forceRollbackError
	})
	if err != forceRollbackError {
		if err == errSubTestFail {
			return false
		}

		tc.t.Errorf("Update: inner function error not returned - got "+
			"%v, want %v", err, forceRollbackError)
		return false
	}

	// Ensure the values that should have not been stored due to the forced
	// rollback above were not actually stored.
	err = walletdb.View(tc.db, func(tx walletdb.ReadTx) error {
		rootBucket := tx.ReadBucket(namespaceKeyBytes)
		if rootBucket == nil {
			return fmt.Errorf("ReadBucket: unexpected nil root bucket")
		}

		if !testGetValues(tc, rootBucket, rollbackValues(keyValues)) {
			return errSubTestFail
		}

		return nil
	})
	if err != nil {
		if err != errSubTestFail {
			tc.t.Errorf("%v", err)
		}
		return false
	}

	// Store a series of values via a managed read-write transaction.
	err = walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		rootBucket := tx.ReadWriteBucket(namespaceKeyBytes)
		if rootBucket == nil {
			return fmt.Errorf("ReadWriteBucket: unexpected nil root bucket")
		}

		if !testPutValues(tc, rootBucket, keyValues) {
			return errSubTestFail
		}

		return nil
	})
	if err != nil {
		if err != errSubTestFail {
			tc.t.Errorf("%v", err)
		}
		return false
	}

	// Ensure the values stored above were committed as expected.
	err = walletdb.View(tc.db, func(tx walletdb.ReadTx) error {
		rootBucket := tx.ReadBucket(namespaceKeyBytes)
		if rootBucket == nil {
			return fmt.Errorf("ReadBucket: unexpected nil root bucket")
		}

		if !testGetValues(tc, rootBucket, keyValues) {
			return errSubTestFail
		}

		return nil
	})
	if err != nil {
		if err != errSubTestFail {
			tc.t.Errorf("%v", err)
		}
		return false
	}

	// Clean up the values stored above in a managed read-write transaction.
	err = walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		rootBucket := tx.ReadWriteBucket(namespaceKeyBytes)
		if rootBucket == nil {
			return fmt.Errorf("ReadWriteBucket: unexpected nil root bucket")
		}

		if !testDeleteValues(tc, rootBucket, keyValues) {
			return errSubTestFail
		}

		return nil
	})
	if err != nil {
		if err != errSubTestFail {
			tc.t.Errorf("%v", err)
		}
		return false
	}

	return true
}

// testAdditionalErrors performs some tests for error cases not covered
// elsewhere in the tests and therefore improves negative test coverage.
func testAdditionalErrors(tc *testContext) bool {
	ns3Key := []byte("ns3")

	err := walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		// Create a new namespace
		rootBucket, err := tx.CreateTopLevelBucket(ns3Key)
		if err != nil {
			return fmt.Errorf("CreateTopLevelBucket: unexpected error: %v", err)
		}

		// Ensure CreateBucket returns the expected error when no bucket
		// key is specified.
		if _, err := rootBucket.CreateBucket(nil); !errors.Is(errors.Invalid, err) {
			return fmt.Errorf("CreateBucket: unexpected error - "+
				"got %v, want %v", err, errors.Invalid)
		}

		// Ensure DeleteNestedBucket returns the expected error when no bucket
		// key is specified.
		if err := rootBucket.DeleteNestedBucket(nil); !errors.Is(errors.Invalid, err) {
			return fmt.Errorf("DeleteNestedBucket: unexpected error - "+
				"got %v, want %v", err, errors.Invalid)
		}

		// Ensure Put returns the expected error when no key is
		// specified.
		if err := rootBucket.Put(nil, nil); !errors.Is(errors.Invalid, err) {
			return fmt.Errorf("Put: unexpected error - got %v, "+
				"want %v", err, errors.Invalid)
		}

		return nil
	})
	if err != nil {
		if err != errSubTestFail {
			tc.t.Errorf("%v", err)
		}
		return false
	}

	// Ensure that attempting to rollback or commit a transaction that is
	// already closed returns the expected error.
	tx, err := tc.db.BeginReadWriteTx()
	if err != nil {
		tc.t.Errorf("Begin: unexpected error: %v", err)
		return false
	}
	if err := tx.Rollback(); err != nil {
		tc.t.Errorf("Rollback: unexpected error: %v", err)
		return false
	}
	if err := tx.Rollback(); !errors.Is(errors.Invalid, err) {
		tc.t.Errorf("Rollback: unexpected error - got %v, want %v", err,
			errors.Invalid)
		return false
	}
	if err := tx.Commit(); !errors.Is(errors.Invalid, err) {
		tc.t.Errorf("Commit: unexpected error - got %v, want %v", err,
			errors.Invalid)
		return false
	}

	return true
}

// testInterface tests performs tests for the various interfaces of walletdb
// which require state in the database for the given database type.
func testInterface(t *testing.T, db walletdb.DB) {
	// Create a test context to pass around.
	context := testContext{t: t, db: db}

	// Create a namespace and test the interface for it.
	if !testNamespaceAndTxInterfaces(&context, "ns1") {
		return
	}

	// Create a second namespace and test the interface for it.
	if !testNamespaceAndTxInterfaces(&context, "ns2") {
		return
	}

	// Check a few more error conditions not covered elsewhere.
	if !testAdditionalErrors(&context) {
		return
	}
}

// TestInterface performs all interfaces tests for this database driver.
func TestInterface(t *testing.T) {
	// Create a new database to run tests against.
	dbPath := "interfacetest.db"
	db, err := walletdb.Create(dbType, dbPath)
	if err != nil {
		t.Errorf("Failed to create test database (%s) %v", dbType, err)
		return
	}
	defer os.Remove(dbPath)
	defer db.Close()

	// Run all of the interface tests against the database.
	testInterface(t, db)
}
