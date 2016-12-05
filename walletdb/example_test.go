// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Test must be updated for API changes.
//+build disabled

package walletdb_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
)

// This example demonstrates creating a new database.
func ExampleCreate() {
	// This example assumes the bdb (bolt db) driver is imported.
	//
	// import (
	// 	"github.com/btcsuite/btcwallet/walletdb"
	// 	_ "github.com/btcsuite/btcwallet/walletdb/bdb"
	// )

	// Create a database and schedule it to be closed and removed on exit.
	// Typically you wouldn't want to remove the database right away like
	// this, but it's done here in the example to ensure the example cleans
	// up after itself.
	dbPath := filepath.Join(os.TempDir(), "examplecreate.db")
	db, err := walletdb.Create("bdb", dbPath)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.Remove(dbPath)
	defer db.Close()

	// Output:
}

// exampleNum is used as a counter in the exampleLoadDB function to provided
// a unique database name for each example.
var exampleNum = 0

// exampleLoadDB is used in the examples to elide the setup code.
func exampleLoadDB() (walletdb.DB, func(), error) {
	dbName := fmt.Sprintf("exampleload%d.db", exampleNum)
	dbPath := filepath.Join(os.TempDir(), dbName)
	db, err := walletdb.Create("bdb", dbPath)
	if err != nil {
		return nil, nil, err
	}
	teardownFunc := func() {
		db.Close()
		os.Remove(dbPath)
	}
	exampleNum++

	return db, teardownFunc, err
}

// This example demonstrates creating a new namespace.
func ExampleDB_namespace() {
	// Load a database for the purposes of this example and schedule it to
	// be closed and removed on exit. See the Create example for more
	// details on what this step is doing.
	db, teardownFunc, err := exampleLoadDB()
	if err != nil {
		fmt.Println(err)
		return
	}
	defer teardownFunc()

	// Get or create a namespace in the database as needed.  This namespace
	// is what is typically passed to specific sub-packages so they have
	// their own area to work in without worrying about conflicting keys.
	namespaceKey := []byte("walletsubpackage")
	namespace, err := db.Namespace(namespaceKey)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Prevent unused error.  Ordinarily the namespace would be used at this
	// point to start a managed or manual transaction.
	_ = namespace

	// Output:
}

// This example demonstrates creating a new database, getting a namespace from
// it, and using a managed read-write transaction against the namespace to store
// and retrieve data.
func Example_basicUsage() {
	// This example assumes the bdb (bolt db) driver is imported.
	//
	// import (
	// 	"github.com/btcsuite/btcwallet/walletdb"
	// 	_ "github.com/btcsuite/btcwallet/walletdb/bdb"
	// )

	// Create a database and schedule it to be closed and removed on exit.
	// Typically you wouldn't want to remove the database right away like
	// this, but it's done here in the example to ensure the example cleans
	// up after itself.
	dbPath := filepath.Join(os.TempDir(), "exampleusage.db")
	db, err := walletdb.Create("bdb", dbPath)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.Remove(dbPath)
	defer db.Close()

	// Get or create a namespace in the database as needed.  This namespace
	// is what is typically passed to specific sub-packages so they have
	// their own area to work in without worrying about conflicting keys.
	namespaceKey := []byte("walletsubpackage")
	namespace, err := db.Namespace(namespaceKey)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Use the Update function of the namespace to perform a managed
	// read-write transaction.  The transaction will automatically be rolled
	// back if the supplied inner function returns a non-nil error.
	err = namespace.Update(func(tx walletdb.Tx) error {
		// All data is stored against the root bucket of the namespace,
		// or nested buckets of the root bucket.  It's not really
		// necessary to store it in a separate variable like this, but
		// it has been done here for the purposes of the example to
		// illustrate.
		rootBucket := tx.RootBucket()

		// Store a key/value pair directly in the root bucket.
		key := []byte("mykey")
		value := []byte("myvalue")
		if err := rootBucket.Put(key, value); err != nil {
			return err
		}

		// Read the key back and ensure it matches.
		if !bytes.Equal(rootBucket.Get(key), value) {
			return fmt.Errorf("unexpected value for key '%s'", key)
		}

		// Create a new nested bucket under the root bucket.
		nestedBucketKey := []byte("mybucket")
		nestedBucket, err := rootBucket.CreateBucket(nestedBucketKey)
		if err != nil {
			return err
		}

		// The key from above that was set in the root bucket does not
		// exist in this new nested bucket.
		if nestedBucket.Get(key) != nil {
			return fmt.Errorf("key '%s' is not expected nil", key)
		}

		return nil
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	// Output:
}
