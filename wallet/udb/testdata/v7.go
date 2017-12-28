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
	"fmt"
	"io"
	"os"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/hdkeychain"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
	"github.com/decred/dcrwallet/walletseed"
)

const dbname = "v7.db"

var (
	epoch    time.Time
	pubPass  = []byte("public")
	privPass = []byte("private")
	privKey  = []byte{31: 1}
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

func setup() error {
	var chainParams = &chaincfg.TestNet2Params
	os.Remove(dbname)
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

	amgr, txmgr, _, err := udb.Open(db, chainParams, pubPass)
	if err != nil {
		return err
	}

	return walletdb.Update(db, func(dbtx walletdb.ReadWriteTx) error {
		amgrns := dbtx.ReadWriteBucket([]byte("waddrmgr"))
		txmgrns := dbtx.ReadWriteBucket([]byte("wtxmgr"))
		err := amgr.Unlock(amgrns, privPass)
		if err != nil {
			return err
		}

		privKey, _ := secp256k1.PrivKeyFromBytes(privKey)
		wif, err := dcrutil.NewWIF(privKey, chainParams, chainec.ECTypeSecp256k1)
		if err != nil {
			return err
		}
		maddr, err := amgr.ImportPrivateKey(amgrns, wif)
		if err != nil {
			return err
		}
		addr := maddr.Address()

		// Add a block
		prevBlock := chainParams.GenesisHash
		buf := bytes.Buffer{}
		err = (&wire.BlockHeader{
			Version:      1,
			PrevBlock:    *prevBlock,
			StakeVersion: 1,
			VoteBits:     1,
			Height:       uint32(1),
		}).Serialize(&buf)
		if err != nil {
			return err
		}

		headerData := udb.BlockHeaderData{
			BlockHash: chainhash.Hash{31: byte(1)},
		}
		copy(headerData.SerializedHeader[:], buf.Bytes())
		err = txmgr.ExtendMainChain(txmgrns, &headerData)
		if err != nil {
			return err
		}

		blockMeta := &udb.BlockMeta{
			Block: udb.Block{
				Hash:   headerData.BlockHash,
				Height: int32(1),
			},
			Time: epoch,
		}

		// Add 6 sstxchange outputs (3 mined, 3 unmined)
		for count := 1; count < 7; count++ {
			msgTx := wire.NewMsgTx()
			pkScript, err := txscript.PayToSStxChange(addr)
			if err != nil {
				return fmt.Errorf("cannot create txout script: %s", err)
			}

			msgTx.AddTxOut(wire.NewTxOut(int64(dcrutil.Amount(1*count)), pkScript))
			// set tx expiry
			msgTx.Expiry = 500

			rec, err := udb.NewTxRecordFromMsgTx(msgTx, epoch)
			if err != nil {
				return err
			}

			if count%2 == 0 {
				err = txmgr.InsertMinedTx(txmgrns, amgrns, rec, &headerData.BlockHash)
				err = txmgr.AddCredit(txmgrns, rec, blockMeta, 0, true, 0)
				if err != nil {
					return err
				}
			} else {
				err = txmgr.InsertMemPoolTx(txmgrns, rec)
				if err != nil {
					return err
				}

				err = txmgr.AddCredit(txmgrns, rec, nil, 0, true, 0)
				if err != nil {
					return err
				}
			}
		}

		// add a regular tx
		msgTx := wire.NewMsgTx()
		p2pkh, err := dcrutil.DecodeAddress("TsmbdTvkBrXqAhy6W4nror8XERdgLdDW8DM")
		if err != nil {
			return fmt.Errorf("failed to decode address: %s", err)
		}
		pkScript, err := txscript.PayToAddrScript(p2pkh)
		if err != nil {
			return fmt.Errorf("cannot create txout script: %s", err)
		}

		// using second mined tx (value of 2 DCR) as input
		msgTx.AddTxIn(wire.NewTxIn(&wire.OutPoint{Index: 1}, nil))
		msgTx.AddTxOut(wire.NewTxOut(int64(dcrutil.Amount(1)), pkScript))
		// set tx expiry
		msgTx.Expiry = 500
		rec, err := udb.NewTxRecordFromMsgTx(msgTx, epoch)
		if err != nil {
			return err
		}

		err = txmgr.InsertMemPoolTx(txmgrns, rec)
		if err != nil {
			return err
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
