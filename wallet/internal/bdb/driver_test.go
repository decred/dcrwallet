// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Test must be updated for API changes.
package bdb_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/decred/dcrwallet/errors/v2"
	_ "github.com/decred/dcrwallet/wallet/v3/internal/bdb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// dbType is the database type name for this driver.
const dbType = "bdb"

// TestCreateOpenFail ensures that errors related to creating and opening a
// database are handled properly.
func TestCreateOpenFail(t *testing.T) {
	// Ensure that attempting to open a database that doesn't exist returns
	// the expected error.
	if _, err := walletdb.Open(dbType, "noexist.db"); !errors.Is(err, errors.NotExist) {
		t.Errorf("Open: unexpected error: %v", err)
		return
	}

	// Ensure that attempting to open a database with the wrong number of
	// parameters returns the expected error.
	wantErr := errors.Errorf("invalid arguments to %s.Open -- expected "+
		"database path", dbType)
	if _, err := walletdb.Open(dbType, 1, 2, 3); err.Error() != wantErr.Error() {
		t.Errorf("Open: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to open a database with an invalid type for
	// the first parameter returns the expected error.
	wantErr = errors.Errorf("first argument to %s.Open is invalid -- "+
		"expected database path string", dbType)
	if _, err := walletdb.Open(dbType, 1); err.Error() != wantErr.Error() {
		t.Errorf("Open: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to create a database with the wrong number of
	// parameters returns the expected error.
	wantErr = errors.Errorf("invalid arguments to %s.Create -- expected "+
		"database path", dbType)
	if _, err := walletdb.Create(dbType, 1, 2, 3); err.Error() != wantErr.Error() {
		t.Errorf("Create: did not receive expected error - got %v, "+
			"want %v", err, wantErr)
		return
	}

	// Ensure that attempting to open a database with an invalid type for
	// the first parameter returns the expected error.
	wantErr = errors.Errorf("first argument to %s.Create is invalid -- "+
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

	if _, err := db.BeginReadTx(); !errors.Is(err, errors.Invalid) {
		t.Errorf("BeginReadTx: unexpected error: %v", err)
		return
	}
}

// TestPersistence ensures that values stored are still valid after closing and
// reopening the database.
func TestPersistence(t *testing.T) {
	ctx := context.Background()
	// Create a new database to run tests against.
	dbPath := "persistencetest.db"
	db, err := walletdb.Create(dbType, dbPath)
	if err != nil {
		t.Errorf("Failed to create test database (%s) %v", dbType, err)
		return
	}
	defer os.Remove(dbPath)
	defer db.Close()

	// Create a bucket and put some values into it so they can be tested
	// for existence on re-open.
	storeValues := map[string]string{
		"ns1key1": "foo1",
		"ns1key2": "foo2",
		"ns1key3": "foo3",
	}
	ns1Key := []byte("ns1")

	walletdb.Update(ctx, db, func(tx walletdb.ReadWriteTx) error {
		ns1Bkt, err := tx.CreateTopLevelBucket(ns1Key)
		if err != nil {
			return errors.E(errors.IO, err)
		}

		for k, v := range storeValues {
			if err := ns1Bkt.Put([]byte(k), []byte(v)); err != nil {
				return errors.Errorf("Put: unexpected error: %v", err)
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

	// Ensure the values previously stored in the bucket still exist
	// and are correct.
	walletdb.View(ctx, db, func(tx walletdb.ReadTx) error {
		ns1Bkt := tx.ReadBucket(ns1Key)
		for k, v := range storeValues {
			val := ns1Bkt.Get([]byte(k))
			if !bytes.Equal([]byte(v), val) {
				return errors.Errorf("Get: key '%s' does not "+
					"match expected value - got %s, want %s",
					k, string(val), v)
			}
		}

		return nil
	})
}
