// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should be compiled from the commit the file was introduced,
// otherwise it may not compile due to API changes, or may not create the
// database with the correct old version.  This file should not be updated for
// API changes.

// V24 test database layout and v25 upgrade test plan
//
// importVotingAccount is the 25th version of the database. This version
// indicates that importing a new account type has been enabled. This
// account facilitates voting using a special account where the private
// key from indexes of the internal branch are shared with a vsp. The
// new type is not recognized by previous wallet versions. This version
// only updates the db version number so that previous versions will
// error on startup.
//
// This test only creates an empty v24 database.

package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	_ "decred.org/dcrwallet/v2/wallet/internal/bdb"
	"decred.org/dcrwallet/v2/wallet/udb"
	"decred.org/dcrwallet/v2/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/v3"
)

const dbname = "v24.db"

var (
	epoch    time.Time
	pubPass  = []byte("public")
	privPass = []byte("private")
)

var params = chaincfg.TestNet3Params()

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := setup(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}
	err = compress()
	if err != nil {
		fmt.Fprintf(os.Stderr, "compress: %v\n", err)
		os.Exit(1)
	}
}

func setup(ctx context.Context) error {
	db, err := walletdb.Create("bdb", dbname)
	if err != nil {
		return err
	}
	defer db.Close()
	var seed [32]byte
	if err = udb.Initialize(ctx, db, params, seed[:], pubPass, privPass); err != nil {
		return err
	}
	return nil
}

func compress() error {
	db, err := os.Open(dbname)
	if err != nil {
		return err
	}
	defer os.Remove(dbname)
	defer db.Close()
	dbgz, err := os.Create(dbname + ".gz")
	if err != nil {
		return err
	}
	defer dbgz.Close()
	gz := gzip.NewWriter(dbgz)
	_, err = io.Copy(gz, db)
	if err != nil {
		return err
	}
	return gz.Close()
}
