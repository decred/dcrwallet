// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should compiled from the commit the file was introduced, otherwise
// it may not compile due to API changes, or may not create the database with
// the correct old version.  This file should not be updated for API changes.

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrutil/hdkeychain"
	_ "github.com/decred/dcrwallet/wallet/internal/bdb"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrwallet/walletseed"
)

const dbname = "v2.db"

var (
	pubPass  = []byte("public")
	privPass = []byte("private")
)

var chainParams = &chaincfg.TestNet2Params

// From testnet2 ticket 4516ef1d548f3284c1a27b3e706c4677392031df7071ad2022050af376837033
const ticketPurchaseHex = "01000000024bf0a303a7e6d174833d9eb761815b61f8ba8c6fa8852a6bf51c703daefc0ef60400000000ffffffff4bf0a303a7e6d174833d9eb761815b61f8ba8c6fa8852a6bf51c703daefc0ef60500000000ffffffff056f78d37a00000000000018baa914ec97b165a5f028b50fb12ae717c5f6c1b9057b5f8700000000000000000000206a1e7f686bc0e548bbb92f487db6da070e43a34117288ed59100000000000058000000000000000000001abd76a914000000000000000000000000000000000000000088ac00000000000000000000206a1e9d8e8bdc618035be32a14ab752af2e331f9abf3651074a7a000000000058000000000000000000001abd76a914000000000000000000000000000000000000000088ac00000000ad480000028ed59100000000009c480000010000006b483045022100c240bdd6a656c20e9035b839fc91faae6c766772f76149adb91a1fdcf20faf9c02203d68038b83263293f864b173c8f3f00e4371b67bf36fb9ec9f5132bdf68d2858012102adc226dec4de09a18c5a522f8f00917fb6d4eb2361a105218ac3f87d802ae3d451074a7a000000009c480000010000006a47304402205af53185f2662a30a22014b0d19760c1bfde8ec8f065b19cacab6a7abcec76a202204a2614cfcb4db3fc1c86eb0b1ca577f9039ec6db29e9c44ddcca2fe6e3c8bd5d012102adc226dec4de09a18c5a522f8f00917fb6d4eb2361a105218ac3f87d802ae3d4"

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
	err = udb.Initialize(db, chainParams, seed, pubPass, privPass, false)
	if err != nil {
		return err
	}

	_, _, smgr, err := udb.Open(db, chainParams, pubPass)
	if err != nil {
		return err
	}

	ticketPurchaseBytes, err := hex.DecodeString(ticketPurchaseHex)
	if err != nil {
		return err
	}
	var ticketPurchase wire.MsgTx
	err = ticketPurchase.Deserialize(bytes.NewReader(ticketPurchaseBytes))
	if err != nil {
		return err
	}

	return walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket([]byte("wstakemgr"))

		vb := stake.VoteBits{
			Bits:         1,
			ExtendedBits: []byte{0, 0, 0, 4},
		}
		return smgr.InsertSStx(ns, dcrutil.NewTx(&ticketPurchase), vb)
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
