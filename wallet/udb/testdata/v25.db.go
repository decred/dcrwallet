// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should be compiled from the commit the file was introduced,
// otherwise it may not compile due to API changes, or may not create the
// database with the correct old version.  This file should not be updated for
// API changes.

// v25 test database layout and v26 upgrade test plan
//
// The v25 database stored information about VSP servers in two different
// buckets:
//
//   - The bucket "vsphost", keyed by an integer ID, stored the VSP host URL.
//   - The bucket "vsppubkey", keyed by VSP host URL, stored the VSPs pubkey.
//
// The v26 database upgrade consolidates these buckets. Pubkeys will be moved
// into the "vsphost" bucket, and the "vsppubkey" bucket will be deleted.

package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"

	_ "decred.org/dcrwallet/v4/wallet/internal/bdb"
	"decred.org/dcrwallet/v4/wallet/udb"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

const dbname = "v25.db"

var (
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

	return walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {

		// Insert a single VSP ticket. This will also insert corresponding
		// entries to vsphost and vsppubkey buckets.

		var (
			ticketHash, _ = chainhash.NewHashFromStr("b74903a8bc780e64239914f7cb588d38157f8592d1557b0aa7ef9bbe06eca479")
			feeHash, _    = chainhash.NewHashFromStr("679c508625503e5cd00ba8d35badc40ce040df8986c11321758db831b78f94ec")
			host          = "Test host"
			pubkey        = []byte("Test pubkey")
		)

		return udb.SetVSPTicket(dbtx, ticketHash, &udb.VSPTicket{
			FeeHash:     *feeHash,
			FeeTxStatus: uint32(udb.VSPFeeProcessPaid),
			Host:        host,
			PubKey:      pubkey,
		})
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
