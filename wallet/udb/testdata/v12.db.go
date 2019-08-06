// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should compiled from the commit the file was introduced, otherwise
// it may not compile due to API changes, or may not create the database with
// the correct old version.  This file should not be updated for API changes.

package main

import (
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	_ "github.com/decred/dcrwallet/wallet/v3/drivers/bdb"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"github.com/decred/dcrwallet/walletseed"
)

const dbname = "v12.db"

var (
	pubPass  = []byte("public")
	privPass = []byte("private")
)

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

func hexToBytes(origHex string) []byte {
	buf, err := hex.DecodeString(origHex)
	if err != nil {
		panic(err)
	}
	return buf
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

	params := chaincfg.TestNet3Params()
	err = udb.Initialize(db, params, seed, pubPass, privPass)
	if err != nil {
		return err
	}

	amgr, txStore, _, err := udb.Open(db, params, pubPass)
	if err != nil {
		return err
	}

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		amgrns := tx.ReadWriteBucket([]byte("waddrmgr"))
		txmgrns := tx.ReadWriteBucket([]byte("wtxmgr"))

		if err := amgr.Unlock(amgrns, privPass); err != nil {
			return err
		}

		script := hexToBytes("51210373c717acda38b5aa4c00c33932e059cdbc" +
			"11deceb5f00490a9101704cc444c5151ae")

		_, err := amgr.ImportScript(amgrns, script)
		if err != nil {
			return err
		}

		return txStore.InsertTxScript(txmgrns, script)
	})

	return err
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
