// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package walletdb_test

import (
	"os"
	"testing"

	"github.com/decred/dcrwallet/errors/v2"
	_ "github.com/decred/dcrwallet/wallet/v3/internal/bdb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// dbType is the database type name for this driver.
const dbType = "bdb"

// TestAddDuplicateDriver ensures that adding a duplicate driver does not
// overwrite an existing one.
func TestAddDuplicateDriver(t *testing.T) {
	supportedDrivers := walletdb.SupportedDrivers()
	if len(supportedDrivers) == 0 {
		t.Errorf("no backends to test")
		return
	}
	dbType := supportedDrivers[0]

	// bogusCreateDB is a function which acts as a bogus create and open
	// driver function and intentionally returns a failure that can be
	// detected if the interface allows a duplicate driver to overwrite an
	// existing one.
	bogusCreateDB := func(args ...interface{}) (walletdb.DB, error) {
		return nil, errors.Errorf("duplicate driver allowed for database "+
			"type [%v]", dbType)
	}

	// Create a driver that tries to replace an existing one.  Set its
	// create and open functions to a function that causes a test failure if
	// they are invoked.
	driver := walletdb.Driver{
		DbType: dbType,
		Create: bogusCreateDB,
		Open:   bogusCreateDB,
	}
	err := walletdb.RegisterDriver(driver)
	if !errors.Is(err, errors.Exist) {
		t.Errorf("unexpected duplicate driver registration error: %v", err)
	}

	dbPath := "dupdrivertest.db"
	db, err := walletdb.Create(dbType, dbPath)
	if err != nil {
		t.Errorf("failed to create database: %v", err)
		return
	}
	db.Close()
	_ = os.Remove(dbPath)

}

// TestCreateOpenFail ensures that errors which occur while opening or closing
// a database are handled properly.
func TestCreateOpenFail(t *testing.T) {
	// bogusCreateDB is a function which acts as a bogus create and open
	// driver function that intentionally returns a failure which can be
	// detected.
	dbType := "createopenfail"
	openError := errors.Errorf("failed to create or open database for "+
		"database type [%v]", dbType)
	bogusCreateDB := func(args ...interface{}) (walletdb.DB, error) {
		return nil, openError
	}

	// Create and add driver that intentionally fails when created or opened
	// to ensure errors on database open and create are handled properly.
	driver := walletdb.Driver{
		DbType: dbType,
		Create: bogusCreateDB,
		Open:   bogusCreateDB,
	}
	walletdb.RegisterDriver(driver)

	// Ensure creating a database with the new type fails with the expected
	// error.
	_, err := walletdb.Create(dbType)
	if !errors.Is(err, openError) {
		t.Errorf("expected error not received - got: %v, want %v", err,
			openError)
		return
	}

	// Ensure opening a database with the new type fails with the expected
	// error.
	_, err = walletdb.Open(dbType)
	if !errors.Is(err, openError) {
		t.Errorf("expected error not received - got: %v, want %v", err,
			openError)
		return
	}
}

// TestCreateOpenUnsupported ensures that attempting to create or open an
// unsupported database type is handled properly.
func TestCreateOpenUnsupported(t *testing.T) {
	// Ensure creating a database with an unsupported type fails with the
	// expected error.
	dbType := "unsupported"
	_, err := walletdb.Create(dbType)
	if !errors.Is(err, errors.Invalid) {
		t.Errorf("walletdb.Create: %v", err)
	}

	// Ensure opening a database with the an unsupported type fails with the
	// expected error.
	_, err = walletdb.Open(dbType)
	if !errors.Is(err, errors.Invalid) {
		t.Errorf("walletdb.Create: %v", err)
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
