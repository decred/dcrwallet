// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	_ "github.com/decred/dcrwallet/wallet/v3/drivers/bdb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

func TestInsertsCreditsDebitsRollbacks(t *testing.T) {
	db, _, s, _, teardown, err := cloneDB("inserts_credits_debits_rollbacks.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	g := makeBlockGenerator()
	b1H := g.generate(dcrutil.BlockValid)
	b1Hash := b1H.BlockHash()
	b2H := g.generate(dcrutil.BlockValid)
	b2Hash := b2H.BlockHash()
	b3H := g.generate(dcrutil.BlockValid)
	headerData := makeHeaderDataSlice(b1H, b2H, b3H)
	filters := emptyFilters(3)

	tx1 := wire.MsgTx{TxOut: []*wire.TxOut{{Value: 2e8}}}
	tx1Rec, err := NewTxRecordFromMsgTx(&tx1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	sTx1 := wire.MsgTx{
		TxIn: []*wire.TxIn{{
			PreviousOutPoint: wire.OutPoint{
				Hash:  tx1.TxHash(),
				Index: 0,
				Tree:  wire.TxTreeRegular,
			},
			ValueIn:     tx1Rec.MsgTx.TxOut[0].Value,
			BlockHeight: b2H.Height,
			BlockIndex:  0,
		}},
		TxOut: []*wire.TxOut{{Value: tx1Rec.MsgTx.TxOut[0].Value}},
	}
	sTx1Rec, err := NewTxRecordFromMsgTx(&sTx1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	tx2 := wire.MsgTx{TxOut: []*wire.TxOut{{Value: 3e8}}}
	tx2Rec, err := NewTxRecordFromMsgTx(&tx2, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	sTx2 := wire.MsgTx{
		TxIn: []*wire.TxIn{{
			PreviousOutPoint: wire.OutPoint{
				Hash:  tx2.TxHash(),
				Index: 0,
				Tree:  wire.TxTreeRegular,
			},
			ValueIn:     tx2Rec.MsgTx.TxOut[0].Value,
			BlockHeight: b3H.Height,
			BlockIndex:  0,
		}},
		TxOut: []*wire.TxOut{{Value: tx2Rec.MsgTx.TxOut[0].Value}},
	}
	sTx2Rec, err := NewTxRecordFromMsgTx(&sTx2, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(wtxmgrBucketKey)
		addrmgrNs := tx.ReadBucket(waddrmgrBucketKey)
		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData, filters)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	defaultAccount := uint32(0)
	tests := []struct {
		name     string
		f        func(*Store, walletdb.ReadWriteBucket, walletdb.ReadBucket) (*Store, error)
		bal, unc dcrutil.Amount
		unspents map[wire.OutPoint]struct{}
		unmined  map[chainhash.Hash]struct{}
	}{
		{
			name: "new store",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				return s, nil
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined:  map[chainhash.Hash]struct{}{},
		},
		{
			name: "txout insert",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err = s.InsertMemPoolTx(txmgrNs, tx1Rec)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(txmgrNs, tx1Rec, nil, 0, false, defaultAccount)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(tx1Rec.MsgTx.TxOut[0].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  tx1Rec.Hash,
					Index: 0,
					Tree:  wire.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				tx1Rec.Hash: {},
			},
		},
		{
			name: "insert duplicate unconfirmed",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err = s.InsertMemPoolTx(txmgrNs, tx1Rec)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(tx1Rec.MsgTx.TxOut[0].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  tx1Rec.Hash,
					Index: 0,
					Tree:  wire.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				tx1Rec.Hash: {},
			},
		},
		{
			name: "confirmed txout insert",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err = s.InsertMinedTx(txmgrNs, addrmgrNs, tx1Rec, &b1Hash)
				return s, err
			},
			bal: dcrutil.Amount(tx1Rec.MsgTx.TxOut[0].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  tx1Rec.Hash,
					Index: 0,
					Tree:  wire.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "rollback confirmed credit",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err := s.Rollback(txmgrNs, addrmgrNs, int32(b1H.Height))
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(tx1Rec.MsgTx.TxOut[0].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  tx1Rec.Hash,
					Index: 0,
					Tree:  wire.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				tx1Rec.Hash: {},
			},
		},
		{
			name: "insert duplicate confirmed",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err = insertMainChainHeaders(s, txmgrNs, addrmgrNs, headerData, filters)
				if err != nil {
					return nil, err
				}

				err = s.InsertMinedTx(txmgrNs, addrmgrNs, tx1Rec, &b1Hash)
				return s, nil
			},
			bal: dcrutil.Amount(tx1Rec.MsgTx.TxOut[0].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  tx1Rec.Hash,
					Index: 0,
					Tree:  wire.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "insert confirmed double spend",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err = s.InsertMinedTx(txmgrNs, addrmgrNs, sTx1Rec, &b2Hash)
				if err != nil {
					return nil, err
				}

				err = s.InsertMinedTx(txmgrNs, addrmgrNs, sTx1Rec, &b2Hash)
				return s, err
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined:  map[chainhash.Hash]struct{}{},
		},
		{
			name: "rollback after spending tx",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err := s.Rollback(txmgrNs, addrmgrNs, int32(b2H.Height))
				return s, err
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined: map[chainhash.Hash]struct{}{
				sTx1Rec.Hash: {},
			},
		},
		{
			name: "insert unconfirmed debit",
			f: func(s *Store, txmgrNs walletdb.ReadWriteBucket, addrmgrNs walletdb.ReadBucket) (*Store, error) {
				err = s.InsertMemPoolTx(txmgrNs, sTx2Rec)
				return s, err
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined: map[chainhash.Hash]struct{}{
				sTx1Rec.Hash: {},
				sTx2Rec.Hash: {},
			},
		},
	}

	for _, test := range tests {
		err := walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
			ns := tx.ReadWriteBucket(wtxmgrBucketKey)
			addrmgrNs := tx.ReadBucket(waddrmgrBucketKey)

			tmpStore, err := test.f(s, ns, addrmgrNs)
			if err != nil {
				t.Fatalf("%s: got error: %v", test.name, err)
			}

			s := tmpStore
			bal, err := s.AccountBalance(ns, addrmgrNs, 1, defaultAccount)
			if err != nil {
				t.Fatalf("%s: Confirmed Balance failed: %v", test.name, err)
			}
			if bal.Spendable != test.bal {
				t.Fatalf("%s: balance mismatch: expected: %d, got: %v",
					test.name, test.bal, bal.Spendable)
			}
			unc, err := s.AccountBalance(ns, addrmgrNs, 1, defaultAccount)
			if err != nil {
				t.Fatalf("%s: Unconfirmed Balance failed: %v", test.name, err)
			}
			if unc.Unconfirmed != test.unc {
				t.Fatalf("%s: unconfirmed balance mismatch: expected %d, got %d",
					test.name, test.unc, unc)
			}

			// Check that unspent outputs match expected.
			unspent, err := s.UnspentOutputs(ns)
			if err != nil {
				t.Fatalf("%s: failed to fetch unspent outputs: %v", test.name, err)
			}
			for _, cred := range unspent {
				if _, ok := test.unspents[cred.OutPoint]; !ok {
					t.Errorf("%s: unexpected unspent output: %v",
						test.name, cred.OutPoint)
				}
				delete(test.unspents, cred.OutPoint)
			}
			if len(test.unspents) != 0 {
				t.Fatalf("%s: missing expected unspent output(s)", test.name)
			}

			// Check that unmined txs match expected.
			unmined, err := s.UnminedTxs(ns)
			if err != nil {
				t.Fatalf("%s: cannot load unmined transactions: %v",
					test.name, err)
			}
			for _, tx := range unmined {
				txHash := tx.TxHash()
				if _, ok := test.unmined[txHash]; !ok {
					t.Fatalf("%s: unexpected unmined tx: %v",
						test.name, txHash)
				}
				delete(test.unmined, txHash)
			}
			if len(test.unmined) != 0 {
				t.Fatalf("%s: missing expected unmined tx(s)", test.name)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func newCoinBase(outputValues ...int64) *wire.MsgTx {
	tx := wire.MsgTx{
		TxIn: []*wire.TxIn{
			{
				PreviousOutPoint: wire.OutPoint{Index: ^uint32(0)},
			},
		},
	}
	for _, val := range outputValues {
		tx.TxOut = append(tx.TxOut, &wire.TxOut{Value: val})
	}
	return &tx
}

func spendOutput(txHash *chainhash.Hash, index uint32, tree int8, outputValues ...int64) *wire.MsgTx {
	tx := wire.MsgTx{
		TxIn: []*wire.TxIn{
			{
				PreviousOutPoint: wire.OutPoint{Hash: *txHash, Index: index, Tree: tree},
			},
		},
	}
	for _, val := range outputValues {
		tx.TxOut = append(tx.TxOut, &wire.TxOut{Value: val})
	}
	return &tx
}

func TestCoinbases(t *testing.T) {
	db, _, s, _, teardown, err := cloneDB("coinbases.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	cb := newCoinBase(20e8, 10e8, 30e8)
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

	// Generate enough blocks for tests.
	for idx := 0; idx < 18; idx++ {
		bh := g.generate(dcrutil.BlockValid)
		headers = append(headers, bh)
	}

	headerData := makeHeaderDataSlice(headers...)
	filters := emptyFilters(18)

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(wtxmgrBucketKey)
		addrmgrNs := tx.ReadBucket(waddrmgrBucketKey)

		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData[0:1], filters[0:1])
		if err != nil {
			t.Fatal(err)
		}

		// Insert coinbase and mark outputs 0 and 2 as credits.
		err = s.InsertMinedTx(ns, addrmgrNs, cbRec, &b1Hash)
		if err != nil {
			t.Fatal(err)
		}

		s.AddCredit(ns, cbRec, b1Meta, 0, false, defaultAccount)
		if err != nil {
			t.Fatal(err)
		}

		s.AddCredit(ns, cbRec, b1Meta, 2, false, defaultAccount)
		if err != nil {
			t.Fatal(err)
		}

		type coinbaseTest struct {
			immature  dcrutil.Amount
			spendable dcrutil.Amount
		}

		testMaturity := func(tests []coinbaseTest) error {
			for i, tst := range tests {
				bal, err := s.AccountBalance(ns, addrmgrNs, 0, defaultAccount)
				if err != nil {
					t.Fatalf("Coinbase test %d: Store.Balance failed: %v", i, err)
				}

				if bal.ImmatureCoinbaseRewards != tst.immature {
					t.Fatalf("Coinbase test %d: Got %v immature coinbase, Expected %v",
						i, bal.ImmatureCoinbaseRewards, tst.immature)
				}

				if bal.ImmatureCoinbaseRewards != tst.immature {
					t.Fatalf("Coinbase test %d: Got %v spendable balance, Expected %v",
						i, bal.Spendable, tst.spendable)
				}
			}

			return nil
		}

		expectedImmature := []coinbaseTest{
			{
				immature:  dcrutil.Amount(50e8),
				spendable: dcrutil.Amount(0),
			},
		}

		// At Block 1, 16 blocks from testnet coinbase maturity .
		err := testMaturity(expectedImmature)
		if err != nil {
			t.Fatal(err)
		}

		// Extend chain by 6 blocks.
		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData[1:7], filters[1:7])
		if err != nil {
			t.Fatal(err)
		}

		// At Block 7, 10 blocks from testnet coinbase maturity.
		err = testMaturity(expectedImmature)
		if err != nil {
			t.Fatal(err)
		}

		// Extend chain by 6 blocks.
		err = insertMainChainHeaders(s, ns, addrmgrNs, headerData[7:13], filters[7:13])
		if err != nil {
			t.Fatal(err)
		}

		// At Block 13, 4 blocks from testnet coinbase maturity.
		err = testMaturity(expectedImmature)
		if err != nil {
			t.Fatal(err)
		}

		expectedMature := []coinbaseTest{
			{
				immature:  dcrutil.Amount(0),
				spendable: dcrutil.Amount(50e8),
			},
		}

		// Extend chain by 3 blocks. The coinbase should still be immature since
		// it is still a block away from maturity.
		err = insertMainChainHeaders(s, ns, addrmgrNs,
			headerData[13:16], filters[13:16])
		if err != nil {
			t.Fatal(err)
		}

		// At Block 16, 1 block from testnet coinbase maturity.
		err = testMaturity(expectedImmature)
		if err != nil {
			t.Fatal(err)
		}

		// Extend chain by 1 block.
		err = insertMainChainHeaders(s, ns, addrmgrNs,
			headerData[16:17], filters[16:17])
		if err != nil {
			t.Fatal(err)
		}

		// At Block 17, testnet coinbase maturity reached. The coinbase should
		// be available to spend.
		err = testMaturity(expectedMature)
		if err != nil {
			t.Fatal(err)
		}

		// Spend an output from the coinbase. This should deduct the amount
		// spent by the tx from the matured coinbase amount.
		spenderA := spendOutput(&cbRec.Hash, 0, 0, 5e8, 15e8)
		spenderARec, err := NewTxRecordFromMsgTx(spenderA, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		b17H := headers[16]
		b17Hash := b17H.BlockHash()
		err = s.InsertMinedTx(ns, addrmgrNs, spenderARec, &b17Hash)
		if err != nil {
			t.Fatal(err)
		}

		expectedMatureRemainder := []coinbaseTest{
			{
				immature:  dcrutil.Amount(0),
				spendable: dcrutil.Amount(30e8),
			},
		}

		err = testMaturity(expectedMatureRemainder)
		if err != nil {
			t.Fatal(err)
		}

		// Reorg out the block that matured the coinbase and spends part of the
		// coinbase. The immature coinbase should be deducted by the amount
		// being spent by the tx.
		err = s.Rollback(ns, addrmgrNs, int32(b17H.Height))
		if err != nil {
			t.Fatal(err)
		}

		expectedReorgImmature := []coinbaseTest{
			{
				immature:  dcrutil.Amount(30e8),
				spendable: dcrutil.Amount(0),
			},
		}

		err = testMaturity(expectedReorgImmature)
		if err != nil {
			t.Fatal(err)
		}

		// Reorg out the block that contained the coinbase. Since the block
		// with the coinbase is no longer part of the chain there should not be
		// any mature or immature amounts reported.
		err = s.Rollback(ns, addrmgrNs, int32(b1H.Height))
		if err != nil {
			t.Fatal(err)
		}

		expectedReorgToFirstBlock := []coinbaseTest{
			{
				immature:  dcrutil.Amount(0),
				spendable: dcrutil.Amount(0),
			},
		}

		err = testMaturity(expectedReorgToFirstBlock)
		if err != nil {
			t.Fatal(err)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
