// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"io"

	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// DB represents an ACID database for a wallet.
type DB interface {
	io.Closer

	// internal returns the internal interface, allowing DB to be used as an
	// opaque type in other packages without revealing the walletdb.DB
	// interface.
	internal() walletdb.DB
}

type opaqueDB struct {
	walletdb.DB
}

func (db opaqueDB) internal() walletdb.DB { return db.DB }

// OpenDB opens a database with some specific driver implementation.  Args
// specify the arguments to open the database and may differ based on driver.
func OpenDB(driver string, args ...interface{}) (DB, error) {
	const op errors.Op = "wallet.OpenDB"
	db, err := walletdb.Open(driver, args...)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return opaqueDB{db}, nil
}

// CreateDB creates a new database with some specific driver implementation.
// Args specify the arguments to open the database and may differ based on
// driver.
func CreateDB(driver string, args ...interface{}) (DB, error) {
	const op errors.Op = "wallet.CreateDB"
	db, err := walletdb.Create(driver, args...)
	if err != nil {
		return nil, errors.E(op, err)
	}
	return opaqueDB{db}, nil
}
