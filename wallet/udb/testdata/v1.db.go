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

const dbname = "v1.db"

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
	err = udb.Initialize(db, chainParams, seed, pubPass, privPass, false)
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

		accounts := []struct {
			numAddrs  uint32
			usedAddrs []uint32
		}{
			{0, nil},
			{20, []uint32{0, 10, 18}},
			{20, []uint32{0, 10, 18, 19}},
			{20, []uint32{19}},
			{30, []uint32{0, 10, 20, 25}},
			{30, []uint32{9, 18, 29}},
			{30, []uint32{29}},
			{200, []uint32{0, 20, 40, 60, 80, 100, 120, 140, 160, 180, 185}},
			{200, []uint32{199}},
		}
		for i, a := range accounts {
			var account uint32
			if i != 0 {
				account, err = amgr.NewAccount(ns, fmt.Sprintf("account-%d", i))
				if err != nil {
					return err
				}
				if account != uint32(i) {
					panic("NewAccount created wrong account")
				}
			}
			for _, branch := range []uint32{0, 1} {
				if a.numAddrs != 0 {
					// There is an off-by-one in this version of API.  Normally
					// we would pass a.numAddrs-1 here to sync through the last
					// child index (BIP0044 child indexes are zero-indexed) but
					// instead, the total number of addresses is passed as the
					// sync index to work around a bug where the API only
					// generated addresses in the range [0,syncToIndex) instead
					// of [0,syncToIndex].  This has been fixed in the updated
					// code after the DB upgrade has been performed, but that
					// code can't be called here.
					_, err = amgr.SyncAccountToAddrIndex(ns, account, a.numAddrs, branch)
					if err != nil {
						return err
					}
				}
				for _, usedChild := range a.usedAddrs {
					xpub, err := amgr.AccountBranchExtendedPubKey(tx, account, branch)
					if err != nil {
						return err
					}
					childXpub, err := xpub.Child(usedChild)
					if err != nil {
						return err
					}
					addr, err := childXpub.Address(chainParams)
					if err != nil {
						return err
					}
					err = amgr.MarkUsed(ns, addr)
					if err != nil {
						return err
					}
				}
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
