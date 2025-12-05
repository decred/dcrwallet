// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/*
Package badgerdb implements an instance of walletdb that uses badger for the
backing datastore.

# Usage

This package is only a driver to the walletdb package and provides the database
type of "badgerdb".  The only parameter the Open and Create functions take is the
database path as a string:

	db, err := walletdb.Open("badgerdb", "path/to/database.db")
	if err != nil {
		// Handle error
	}

	db, err := walletdb.Create("badgerdb", "path/to/database.db")
	if err != nil {
		// Handle error
	}
*/
package badgerdb
