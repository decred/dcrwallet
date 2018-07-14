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
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
)

const dbname = "v9.db"

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
	seed := []byte{242, 190, 108, 53, 208, 141, 133, 74, 143, 51, 238, 146,
		79, 239, 165, 207, 116, 52, 224, 29, 196, 115, 159, 87, 65, 94, 46,
		97, 216, 2, 3, 183}

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

		xpub, err := amgr.AccountBranchExtendedPubKey(dbtx, 0, 0)
		if err != nil {
			return err
		}
		childXpub, err := xpub.Child(0)
		if err != nil {
			return err
		}

		// generated address is TshrHX1vRRRNzMDydqNJ5tFf2VJVGoztYyM
		addr, err := childXpub.Address(&chaincfg.TestNet2Params)
		if err != nil {
			return err
		}

		udb.PutChainedAddress(amgrns, addr.Hash160()[:], 0, 2, 0, 0)

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

		// Create 3 regular transactions and index them.
		for count := 1; count < 4; count++ {
			msgTx := wire.NewMsgTx()
			pkScript, err := txscript.PayToAddrScript(addr)
			if err != nil {
				return fmt.Errorf("failed to create pkscript: %s", err)
			}
			msgTx.AddTxOut(wire.NewTxOut(int64(dcrutil.Amount(1*count)), pkScript))
			msgTx.Expiry = wire.NoExpiryValue
			rec, err := udb.NewTxRecordFromMsgTx(msgTx, epoch)
			if err != nil {
				return err
			}
			err = txmgr.InsertMinedTx(txmgrns, amgrns, rec, &headerData.BlockHash)
			if err != nil {
				return err
			}

			err = udb.PutAddrTransactionIndex(amgrns, addr.Hash160()[:], &rec.Hash)
			if err != nil {
				return err
			}
		}
		// Create 3 unmined regular transactions and index them.
		for count := 1; count < 4; count++ {
			msgTx := wire.NewMsgTx()
			pkScript, err := txscript.PayToAddrScript(addr)
			if err != nil {
				return fmt.Errorf("failed to create pkscript: %s", err)
			}
			msgTx.AddTxOut(wire.NewTxOut(int64(dcrutil.Amount(10*count)), pkScript))
			rec, err := udb.NewTxRecordFromMsgTx(msgTx, epoch)
			if err != nil {
				return err
			}
			err = txmgr.InsertMemPoolTx(txmgrns, rec)
			if err != nil {
				return err
			}

			err = udb.PutAddrTransactionIndex(amgrns, addr.Hash160()[:], &rec.Hash)
			if err != nil {
				return err
			}
		}

		err = amgr.MarkUsed(amgrns, addr)
		return err
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
