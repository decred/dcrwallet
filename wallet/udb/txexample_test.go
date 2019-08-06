// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build ignore

package udb

import (
	"testing"
	"time"

	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

func ExampleStore_Rollback(t *testing.T) {
	t.Parallel()
	db, s, teardown, err := setup("store_rollback")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	cb := newCoinBase(30e8)
	cbRec, err := NewTxRecordFromMsgTx(cb, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	defaultAccount := uint32(0)
	g := makeBlockGenerator()
	b1H := g.generate(dcrutil.BlockValid)
	b1Hash := b1H.BlockHash()
	b1Meta := makeBlockMeta(b1H)
	headers := []*wire.BlockHeader{b1H}
	headerData := makeHeaderDataSlice(headers...)
	filters := emptyFilters(1)

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(wtxmgrBucketKey)
		addrmgrNs := tx.ReadBucket(waddrmgrBucketKey)

		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData[0:1], filters[0:1])
		if err != nil {
			t.Fatal(err)
		}

		// Insert coinbase and mark outputs 0 as credits.
		err = s.InsertMemPoolTx(ns, cbRec)
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMinedTx(ns, addrmgrNs, cbRec, &b1Hash)
		if err != nil {
			t.Fatal(err)
		}

		s.AddCredit(ns, cbRec, b1Meta, 0, false, defaultAccount)
		if err != nil {
			t.Fatal(err)
		}

		// Spends: b1H coinbase
		// Outputs: 10 Coin
		aTx := spendOutput(&b1Hash, 0, 0, 10e8)
		txRecordA, err := NewTxRecordFromMsgTx(aTx, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMemPoolTx(ns, txRecordA)
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMinedTx(ns, addrmgrNs, txRecordA, &b1Hash)
		if err != nil {
			t.Fatal(err)
		}

		// Rollback everything.
		err = s.Rollback(ns, addrmgrNs, int32(b1H.Height))
		if err != nil {
			t.Fatal(err)
		}

		// Assert that the transaction is now unmined.
		details, err := s.TxDetails(ns, &txRecordA.Hash)
		if err != nil {
			t.Fatal(err)
		}
		if details == nil {
			t.Fatal("no tx details found")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func Example_basicUsage(t *testing.T) {
	t.Parallel()
	db, s, teardown, err := setup("basic_usage")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	cb := newCoinBase(10e8, 5e8)
	cbRec, err := NewTxRecordFromMsgTx(cb, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	defaultAccount := uint32(0)
	g := makeBlockGenerator()
	b1H := g.generate(dcrutil.BlockValid)
	b1Hash := b1H.BlockHash()
	b1Meta := makeBlockMeta(b1H)
	headers := []*wire.BlockHeader{b1H}
	b2H := g.generate(dcrutil.BlockValid)
	b2Hash := b2H.BlockHash()
	// b2Meta := makeBlockMeta(b2H)
	headers = append(headers, b2H)
	headerData := makeHeaderDataSlice(headers...)
	filters := emptyFilters(2)

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(wtxmgrBucketKey)
		addrmgrNs := tx.ReadBucket(waddrmgrBucketKey)

		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData[0:1], filters[0:1])
		if err != nil {
			t.Fatal(err)
		}

		// Insert coinbase and mark outputs 0 as credits.
		err = s.InsertMemPoolTx(ns, cbRec)
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMinedTx(ns, addrmgrNs, cbRec, &b1Hash)
		if err != nil {
			t.Fatal(err)
		}

		s.AddCredit(ns, cbRec, b1Meta, 0, false, defaultAccount)
		if err != nil {
			t.Fatal(err)
		}

		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData[1:2], filters[1:2])
		if err != nil {
			t.Fatalf("t:%v", err)
		}

		// Mine spending transactions in the next block.

		// Spends: b1H coinbase
		// Outputs: 10 Coin
		aTx := spendOutput(&b1Hash, 0, 0, 10e8)
		txRecordA, err := NewTxRecordFromMsgTx(aTx, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMemPoolTx(ns, txRecordA)
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMinedTx(ns, addrmgrNs, txRecordA, &b2Hash)
		if err != nil {
			t.Fatal(err)
		}

		// Spends: A:0
		// Outputs: 5 Coin, 5 Coin
		bTx := spendOutput(&txRecordA.Hash, 0, 0, 5e8, 5e8)
		txRecordB, err := NewTxRecordFromMsgTx(bTx, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMemPoolTx(ns, txRecordB)
		if err != nil {
			t.Fatal(err)
		}

		err = s.InsertMinedTx(ns, addrmgrNs, txRecordB, &b2Hash)
		if err != nil {
			t.Fatal(err)
		}

		// Fetch unspent outputs.
		utxos, err := s.UnspentOutputs(ns)
		if err != nil {
			t.Fatal(err)
		}

		expectedOutPoint := wire.OutPoint{Hash: cbRec.Hash, Index: 0}
		match := false
		for _, utxo := range utxos {
			if utxo.OutPoint == expectedOutPoint {
				match = true
			}
		}

		if !match {
			t.Fatal("expected an unspect coinbase output")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// // Output:
// // 5 Coin
// true
