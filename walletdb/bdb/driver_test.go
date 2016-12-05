// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Test must be updated for API changes.
//+build disabled

package bdb_test

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
)

// dbType is the database type name for this driver.
const dbType = "bdb"

// TestCreateOpenFail ensures that errors related to creating and opening a
// database are handled properly.
func TestCreateOpenFail(t *testing.T) {
	// Ensure that attempting to open a database that doesn't exist returns
	// the expected error.
	wantErr := walletdb.ErrDbDoesNotExist
	if _, err := walletdb.Open(dbType, "noexist.db"); err != wantErr {
		t.Errorf("Open: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to open a database with the wrong number of
	// parameters returns the expected error.
	wantErr = fmt.Errorf("invalid arguments to %s.Open -- expected "+
		"database path", dbType)
	if _, err := walletdb.Open(dbType, 1, 2, 3); err.Error() != wantErr.Error() {
		t.Errorf("Open: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to open a database with an invalid type for
	// the first parameter returns the expected error.
	wantErr = fmt.Errorf("first argument to %s.Open is invalid -- "+
		"expected database path string", dbType)
	if _, err := walletdb.Open(dbType, 1); err.Error() != wantErr.Error() {
		t.Errorf("Open: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to create a database with the wrong number of
	// parameters returns the expected error.
	wantErr = fmt.Errorf("invalid arguments to %s.Create -- expected "+
		"database path", dbType)
	if _, err := walletdb.Create(dbType, 1, 2, 3); err.Error() != wantErr.Error() {
		t.Errorf("Create: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to open a database with an invalid type for
	// the first parameter returns the expected error.
	wantErr = fmt.Errorf("first argument to %s.Create is invalid -- "+
		"expected database path string", dbType)
	if _, err := walletdb.Create(dbType, 1); err.Error() != wantErr.Error() {
		t.Errorf("Create: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure operations against a closed database return the expected
	// error.
	dbPath := "createfail.db"
	db, err := walletdb.Create(dbType, dbPath)
	if err != nil {
		t.Errorf("Create: unexpected error: %v", err)
		return
	}
	defer os.Remove(dbPath)
	db.Close()

	wantErr = walletdb.ErrDbNotOpen
	if _, err := db.Namespace([]byte("ns1")); err != wantErr {
		t.Errorf("Namespace: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}
}

// TestPersistence ensures that values stored are still valid after closing and
// reopening the database.
func TestPersistence(t *testing.T) {
	// Create a new database to run tests against.
	dbPath := "persistencetest.db"
	db, err := walletdb.Create(dbType, dbPath)
	if err != nil {
		t.Errorf("Failed to create test database (%s) %v", dbType, err)
		return
	}
	defer os.Remove(dbPath)
	defer db.Close()

	// Create a namespace and put some values into it so they can be tested
	// for existence on re-open.
	storeValues := map[string]string{
		"ns1key1": "foo1",
		"ns1key2": "foo2",
		"ns1key3": "foo3",
	}
	ns1Key := []byte("ns1")
	ns1, err := db.Namespace(ns1Key)
	if err != nil {
		t.Errorf("Namespace: unexpected error: %v", err)
		return
	}
	err = ns1.Update(func(tx walletdb.Tx) error {
		rootBucket := tx.RootBucket()
		if rootBucket == nil {
			return fmt.Errorf("RootBucket: unexpected nil root bucket")
		}

		for k, v := range storeValues {
			if err := rootBucket.Put([]byte(k), []byte(v)); err != nil {
				return fmt.Errorf("Put: unexpected error: %v", err)
			}
		}

		return nil
	})
	if err != nil {
		t.Errorf("ns1 Update: unexpected error: %v", err)
		return
	}

	// Close and reopen the database to ensure the values persist.
	db.Close()
	db, err = walletdb.Open(dbType, dbPath)
	if err != nil {
		t.Errorf("Failed to open test database (%s) %v", dbType, err)
		return
	}
	defer db.Close()

	// Ensure the values previously stored in the 3rd namespace still exist
	// and are correct.
	ns1, err = db.Namespace(ns1Key)
	if err != nil {
		t.Errorf("Namespace: unexpected error: %v", err)
		return
	}
	err = ns1.View(func(tx walletdb.Tx) error {
		rootBucket := tx.RootBucket()
		if rootBucket == nil {
			return fmt.Errorf("RootBucket: unexpected nil root bucket")
		}

		for k, v := range storeValues {
			gotVal := rootBucket.Get([]byte(k))
			if !reflect.DeepEqual(gotVal, []byte(v)) {
				return fmt.Errorf("Get: key '%s' does not "+
					"match expected value - got %s, want %s",
					k, gotVal, v)
			}
		}

		return nil
	})
	if err != nil {
		t.Errorf("ns1 View: unexpected error: %v", err)
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
