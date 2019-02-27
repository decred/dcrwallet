// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should compiled from the commit the file was introduced, otherwise
// it may not compile due to API changes, or may not create the database with
// the correct old version.  This file should not be updated for API changes.

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrutil/hdkeychain"
	_ "github.com/decred/dcrwallet/wallet/internal/bdb"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrwallet/walletseed"
)

const dbname = "v4.db"

var (
	pubPass  = []byte("public")
	privPass = []byte("private")
)

var chainParams = &chaincfg.TestNet2Params

func main() {
	err := setup()
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

func setup() error {
	db, err := walletdb.Create("bdb", dbname)
	if err != nil {
		return err
	}
	defer db.Close()
	seed, err := walletseed.GenerateRandomSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return err
	}
	err = udb.Initialize(db, chainParams, seed, pubPass, privPass)
	if err != nil {
		return err
	}

	amgr, _, _, err := udb.Open(db, chainParams, pubPass)
	if err != nil {
		return err
	}

	return walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket([]byte("waddrmgr"))

		err := amgr.Unlock(ns, privPass)
		if err != nil {
			return err
		}

		data := []struct {
			lastUsedExtChild uint32
			lastUsedIntChild uint32
		}{
			{0, 0},
			{9, 9},
			{5, 15},
			{19, 20},
			{20, 19},
			{29, 30},
			{30, 29},
			{1<<31 - 1, 1<<31 - 1},
		}
		for i := range data {
			acct := uint32(i + 1)
			_, err := amgr.NewAccount(ns, fmt.Sprintf("account-%d", acct))
			if err != nil {
				return err
			}
			err = amgr.MarkUsedChildIndex(tx, acct, 0, data[i].lastUsedExtChild)
			if err != nil {
				return err
			}
			err = amgr.MarkUsedChildIndex(tx, acct, 1, data[i].lastUsedIntChild)
			if err != nil {
				return err
			}
		}
		return nil
	})
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
