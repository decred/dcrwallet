// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "decred.org/dcrwallet/v5/wallet/internal/badgerdb"
	_ "decred.org/dcrwallet/v5/wallet/internal/bdb"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/v3"
)

var (
	// seed is the master seed used throughout the tests.
	seed = []byte{
		0xb4, 0x6b, 0xc6, 0x50, 0x2a, 0x30, 0xbe, 0xb9, 0x2f,
		0x0a, 0xeb, 0xc7, 0x76, 0x40, 0x3c, 0x3d, 0xbf, 0x11,
		0xbf, 0xb6, 0x83, 0x05, 0x96, 0x7c, 0x36, 0xda, 0xc9,
		0xef, 0x8d, 0x64, 0x15, 0x67,
	}

	// rewritten to absolute paths by TestMain
	emptyDBDir  string
	emptyDBPath string

	dbName = "test.db"

	pubPassphrase   = []byte("_DJr{fL4H0O}*-0\n:V1izc)(6BomK")
	privPassphrase  = []byte("81lUHXnOMZ@?XXd7O9xyDIWIbXX-lj")
	pubPassphrase2  = []byte("-0NV4P~VSJBWbunw}%<Z]fuGpbN[ZI")
	privPassphrase2 = []byte("~{<]08%6!-?2s<$(8$8:f(5[4/!/{Y")
)

var (
	dbDriver = flag.String("dbdriver", "bdb", "database driver (bdb or badgerdb)")
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
func createEmptyDB(ctx context.Context) error {
	db, err := walletdb.Create(*dbDriver, emptyDBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	err = Initialize(ctx, db, chaincfg.TestNet3Params(), seed, pubPassphrase,
		privPassphrase)
	if err != nil {
		return err
	}

	err = Upgrade(ctx, db, pubPassphrase, chaincfg.TestNet3Params())
	if err != nil {
		return err
	}

	return nil
}

// cloneDB makes a copy of an empty wallet db. It returns a wallet db, address
// manager, and the tx store.
func cloneDB(ctx context.Context, t *testing.T, cloneName string) (walletdb.DB, *Manager, *Store, error) {
	cloneDir := filepath.Join(filepath.Dir(emptyDBDir), cloneName)
	err := os.CopyFS(cloneDir, os.DirFS(emptyDBDir))
	if err != nil {
		t.Logf("%v %v", cloneDir, emptyDBDir)
		return nil, nil, nil, fmt.Errorf("CopyFS unexpected error: %v", err)
	}

	db, err := walletdb.Open(*dbDriver, filepath.Join(cloneDir, dbName))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("walletdb.Open unexpected error: %v", err)
	}

	mgr, txStore, err := Open(ctx, db, chaincfg.TestNet3Params(), pubPassphrase)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("udb.Open unexpected error: %v", err)
	}

	t.Cleanup(func() {
		db.Close()
		os.Remove(cloneName)
	})

	return db, mgr, txStore, err
}

func TestMain(m *testing.M) {
	flag.Parse()

	testDir, err := os.MkdirTemp("", "udb-")
	if err != nil {
		fmt.Printf("Unable to create temp directory: %v", err)
		os.Exit(1)
	}

	emptyDBDir = filepath.Join(testDir, "empty-db")
	err = os.Mkdir(emptyDBDir, 0o777)
	if err != nil {
		fmt.Printf("Unable to create empty-db directory: %v", err)
		os.Exit(1)
	}

	emptyDBPath = filepath.Join(emptyDBDir, dbName)
	teardown := func() {
		os.RemoveAll(testDir)
	}

	ctx := context.Background()
	err = createEmptyDB(ctx)
	if err != nil {
		fmt.Printf("Unable to create empty test db: %v\n", err)
		teardown()
		os.Exit(1)
	}

	exitCode := m.Run()
	teardown()
	os.Exit(exitCode)
}
