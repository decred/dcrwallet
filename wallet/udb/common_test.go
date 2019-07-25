// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

var (
	// seed is the master seed used throughout the tests.
	seed = []byte{
		0xb4, 0x6b, 0xc6, 0x50, 0x2a, 0x30, 0xbe, 0xb9, 0x2f,
		0x0a, 0xeb, 0xc7, 0x76, 0x40, 0x3c, 0x3d, 0xbf, 0x11,
		0xbf, 0xb6, 0x83, 0x05, 0x96, 0x7c, 0x36, 0xda, 0xc9,
		0xef, 0x8d, 0x64, 0x15, 0x67,
	}

	emptyDbPath = ""

	pubPassphrase   = []byte("_DJr{fL4H0O}*-0\n:V1izc)(6BomK")
	privPassphrase  = []byte("81lUHXnOMZ@?XXd7O9xyDIWIbXX-lj")
	pubPassphrase2  = []byte("-0NV4P~VSJBWbunw}%<Z]fuGpbN[ZI")
	privPassphrase2 = []byte("~{<]08%6!-?2s<$(8$8:f(5[4/!/{Y")
)

// hexToBytes is a wrapper around hex.DecodeString that panics if there is an
// error.  It MUST only be used with hard coded values in the tests.
func hexToBytes(origHex string) []byte {
	buf, err := hex.DecodeString(origHex)
	if err != nil {
		panic(err)
	}
	return buf
}

// createEmptyDB is a helper function for creating an empty wallet db.
func createEmptyDB() error {
	db, err := walletdb.Create("bdb", emptyDbPath)
	defer db.Close()

	if err != nil {
		return err
	}

	err = Initialize(db, chaincfg.TestNet3Params(), seed, pubPassphrase,
		privPassphrase)
	if err != nil {
		return err
	}

	err = Upgrade(db, pubPassphrase, chaincfg.TestNet3Params())
	if err != nil {
		return err
	}

	return nil
}

// cloneDB makes a copy of an empty wallet db. It returns a wallet db, store, a
// stake store and a teardown function.
func cloneDB(cloneName string) (walletdb.DB, *Manager, *Store, *StakeStore, func(), error) {
	file, err := ioutil.ReadFile(emptyDbPath)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	err = ioutil.WriteFile(cloneName, file, 0644)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	db, err := walletdb.Open("bdb", cloneName)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	mgr, txStore, stkStore, err := Open(db, chaincfg.TestNet3Params(), pubPassphrase)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	teardown := func() {
		os.Remove(cloneName)
		db.Close()
	}

	return db, mgr, txStore, stkStore, teardown, err
}
