// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build ignore

package udb

import (
	"fmt"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

var (
	// Spends: bogus
	// Outputs: 10 Coin
	exampleTxRecordA *TxRecord

	// Spends: A:0
	// Outputs: 5 Coin, 5 Coin
	exampleTxRecordB *TxRecord
)

func init() {
	tx := spendOutput(&chainhash.Hash{}, 0, 0, 10e8)
	rec, err := NewTxRecordFromMsgTx(tx, timeNow())
	if err != nil {
		panic(err)
	}
	exampleTxRecordA = rec

	tx = spendOutput(&exampleTxRecordA.Hash, 0, 0, 5e8, 5e8)
	rec, err = NewTxRecordFromMsgTx(tx, timeNow())
	if err != nil {
		panic(err)
	}
	exampleTxRecordB = rec
}

var exampleBlock100 = makeBlockMeta(100)

func ExampleStore_Rollback() {
	s, teardown, err := testStore()
	defer teardown()
	if err != nil {
		fmt.Println(err)
		return
	}

	// Insert a transaction which outputs 10 Coin in a block at height 100.
	err = s.InsertTx(exampleTxRecordA, &exampleBlock100)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Rollback everything from block 100 onwards.
	err = s.Rollback(100)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Assert that the transaction is now unmined.
	details, err := s.TxDetails(&exampleTxRecordA.Hash)
	if err != nil {
		fmt.Println(err)
		return
	}
	if details == nil {
		fmt.Println("No details found")
		return
	}
	fmt.Println(details.Block.Height)

	// Output:
	// -1
}

func Example_basicUsage() {
	// Open the database.
	db, dbTeardown, err := testDB()
	defer dbTeardown()
	if err != nil {
		fmt.Println(err)
		return
	}

	// Create or open a db namespace for the transaction store.
	ns, err := db.Namespace([]byte("txstore"))
	if err != nil {
		fmt.Println(err)
		return
	}

	// Create (or open) the transaction store in the provided namespace.
	err = Create(ns)
	if err != nil {
		fmt.Println(err)
		return
	}
	s, err := Open(ns, &chaincfg.TestNetParams)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Insert an unmined transaction that outputs 10 Coin to a wallet address
	// at output 0.
	err = s.InsertTx(exampleTxRecordA, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	defaultAccount := uint32(0)
	err = s.AddCredit(exampleTxRecordA, nil, 0, false, defaultAccount)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Insert a second transaction which spends the output, and creates two
	// outputs.  Mark the second one (5 Coin) as wallet change.
	err = s.InsertTx(exampleTxRecordB, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = s.AddCredit(exampleTxRecordB, nil, 1, true, defaultAccount)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Mine each transaction in a block at height 100.
	err = s.InsertTx(exampleTxRecordA, &exampleBlock100)
	if err != nil {
		fmt.Println(err)
		return
	}
	err = s.InsertTx(exampleTxRecordB, &exampleBlock100)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Print the one confirmation balance.
	bal, err := s.Balance(1, 100, BFBalanceSpendable, true,
		defaultAccount)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(bal)

	// Fetch unspent outputs.
	utxos, err := s.UnspentOutputs()
	if err != nil {
		fmt.Println(err)
	}
	expectedOutPoint := wire.OutPoint{Hash: exampleTxRecordB.Hash, Index: 1}
	for _, utxo := range utxos {
		fmt.Println(utxo.OutPoint == expectedOutPoint)
	}

	// Output:
	// 5 Coin
	// true
}
