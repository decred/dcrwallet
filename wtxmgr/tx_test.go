// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build ignore

package wtxmgr_test

import (
	"bytes"
	"encoding/hex"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
	"github.com/decred/dcrwallet/wtxmgr"
	. "github.com/decred/dcrwallet/wtxmgr"
)

// Received transaction output for mainnet outpoint
// a4cb6a9f0ebc428f2f46f4d33a1b076062bccc759b189fd419ab515a98198af9:1
var (
	TstRecvSerializedTx, _          = hex.DecodeString("010000000145d6449ec7d0438aefbc3c9736ad32313e97d0a2c0bf81bc0090f9bfee9410420000000000ffffffff02b46f76450c00000000001976a9144673d67fca24747a537da5147ddddc49325f463188ac4001eaa70400000000001976a914b387bf5af183188b71d85d0408a9aebd4a21804888ac000000000000000001ffffffffffffffff00000000ffffffff6a4730440220425b01cdb792d2c9ed757d30447e05eb89f043a172dd3a7c35facf9b02720c0e0220071e89b8cdec1f6ff9d5bc4f8c5f75cc3c46c6b19270f732e64f34bf4139d9bd0121037c119d1b667ce7c986bd7ed8bdef25914fca8a1671030a5d8ca9c7d96f39157d")
	TstRecvTx, _                    = dcrutil.NewTxFromBytes(TstRecvSerializedTx)
	TstRecvTxSpendingTxBlockHash, _ = chainhash.NewHashFromStr("000000000000355502a7a77f2ef24ae10a625aa81444aec9caf250fabf826490")
	TstRecvAmt                      = int64(19997000000)
	TstRecvTxBlockDetails           = &BlockMeta{
		Block: Block{Hash: *TstRecvTxSpendingTxBlockHash, Height: 10638},
		Time:  time.Unix(1458060337, 0),
	}

	TstRecvCurrentHeight = int32(10652) // mainnet blockchain height at time of writing
	TstRecvTxOutConfirms = 14           // hardcoded number of confirmations given the above block height

	TstSpendingSerializedTx, _ = hex.DecodeString("0100000001f98a19985a51ab19d49f189b75ccbc6260071b3ad3f4462f8f42bc0e9f6acba40100000000ffffffff03b7db9c350000000000001aba76a9142d39435107f5e5ef88fb4705574042fc035b539d88ac00000000000000000000206a1ea6d7fcfa36e50f964bdd5248bc416d3cf6a7c1d0f726e93500000000005849da00720400000000001abd76a9143535c52cad743f84beb97f715aac6d161404ca2c88ac0000000000000000014001eaa7040000008e290000010000006a473044022079e1067940497fb111a36abb8a29a85f5d3bc74a81a1252fcb6d376fd4e838a502203b14af35326d2fe46d34a51b759e88711da18744b379b2fa255df22bc7c1b1e4012103e98d6feb5bc5ffe362d727e7f83c08744f41e06406bbd65c11660351564bb384")
	TstSpendingTx, _           = dcrutil.NewTxFromBytes(TstSpendingSerializedTx)
	TstSpendingTxBlockHeight   = int32(10640)
	TstSignedTxBlockHash, _    = chainhash.NewHashFromStr("000000000000156de3056ed122a76d79a4887e03d5b93c2d0bc1981565490e44")
	TstSignedTxBlockDetails    = &BlockMeta{
		Block: Block{Hash: *TstSignedTxBlockHash, Height: TstSpendingTxBlockHeight},
		Time:  time.Unix(1458060416, 0),
	}
)

func testDB() (walletdb.DB, func(), error) {
	tmpDir, err := ioutil.TempDir("", "wtxmgr_test")
	if err != nil {
		return nil, func() {}, err
	}
	db, err := walletdb.Create("bdb", filepath.Join(tmpDir, "db"))
	return db, func() { os.RemoveAll(tmpDir) }, err
}

func testStore() (*Store, func(), error) {
	tmpDir, err := ioutil.TempDir("", "wtxmgr_test")
	if err != nil {
		return nil, func() {}, err
	}
	db, err := walletdb.Create("bdb", filepath.Join(tmpDir, "db"))
	if err != nil {
		teardown := func() {
			os.RemoveAll(tmpDir)
		}
		return nil, teardown, err
	}
	teardown := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	ns, err := db.Namespace([]byte("txstore"))
	if err != nil {
		return nil, teardown, err
	}
	err = Create(ns)
	if err != nil {
		return nil, teardown, err
	}
	s, err := Open(ns, &chaincfg.TestNetParams)
	return s, teardown, err
}

func serializeTx(tx *dcrutil.Tx) []byte {
	var buf bytes.Buffer
	err := tx.MsgTx().Serialize(&buf)
	if err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func TestInsertsCreditsDebitsRollbacks(t *testing.T) {
	t.Parallel()

	// Create a double spend of the received blockchain transaction.
	dupRecvTx, err := dcrutil.NewTxFromBytesLegacy(TstRecvSerializedTx)
	if err != nil {
		t.Errorf("failed to deserialize test transaction: %v", err.Error())
		return
	}

	// Switch txout amount to 1 DCR.  Transaction store doesn't
	// validate txs, so this is fine for testing a double spend
	// removal.

	TstDupRecvAmount := int64(1e8)
	newDupMsgTx := dupRecvTx.MsgTx()
	newDupMsgTx.TxOut[0].Value = TstDupRecvAmount
	TstDoubleSpendTx := dcrutil.NewTx(newDupMsgTx)
	TstDoubleSpendSerializedTx := serializeTx(TstDoubleSpendTx)

	// Create a "signed" (with invalid sigs) tx that spends output 0 of
	// the double spend.
	spendingTx := wire.NewMsgTx()
	spendingTxIn := wire.NewTxIn(wire.NewOutPoint(TstDoubleSpendTx.Sha(), 0, dcrutil.TxTreeRegular), []byte{0, 1, 2, 3, 4})
	spendingTx.AddTxIn(spendingTxIn)
	spendingTxOut1 := wire.NewTxOut(1e7, []byte{5, 6, 7, 8, 9})
	spendingTxOut2 := wire.NewTxOut(9e7, []byte{10, 11, 12, 13, 14})
	spendingTx.AddTxOut(spendingTxOut1)
	spendingTx.AddTxOut(spendingTxOut2)
	TstSpendingTx := dcrutil.NewTx(spendingTx)
	TstSpendingSerializedTx := serializeTx(TstSpendingTx)
	var _ = TstSpendingTx
	defaultAccount := uint32(0)

	tests := []struct {
		name     string
		f        func(*Store) (*Store, error)
		bal, unc dcrutil.Amount
		unspents map[wire.OutPoint]struct{}
		unmined  map[chainhash.Hash]struct{}
	}{
		{
			name: "new store",
			f: func(s *Store) (*Store, error) {
				return s, nil
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined:  map[chainhash.Hash]struct{}{},
		},
		{
			name: "txout insert",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstRecvSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, nil)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(rec, nil, 1, false, defaultAccount)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstRecvTx.MsgTx().TxOut[1].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstRecvTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstRecvTx.Sha(): {},
			},
		},
		{
			name: "insert duplicate unconfirmed",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstRecvSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, nil)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(rec, nil, 1, false, defaultAccount)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstRecvTx.MsgTx().TxOut[1].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstRecvTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstRecvTx.Sha(): {},
			},
		},
		{
			name: "confirmed txout insert",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstRecvSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, TstRecvTxBlockDetails)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(rec, TstRecvTxBlockDetails, 1, false,
					defaultAccount)
				return s, err
			},
			bal: dcrutil.Amount(TstRecvTx.MsgTx().TxOut[1].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstRecvTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeREgular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "insert duplicate confirmed",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstRecvSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, TstRecvTxBlockDetails)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(rec, TstRecvTxBlockDetails, 1, false,
					defaultAccount)
				return s, err
			},
			bal: dcrutil.Amount(TstRecvTx.MsgTx().TxOut[1].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstRecvTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "rollback confirmed credit",
			f: func(s *Store) (*Store, error) {
				err := s.Rollback(TstRecvTxBlockDetails.Height)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstRecvTx.MsgTx().TxOut[1].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstRecvTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstRecvTx.Sha(): {},
			},
		},
		{
			name: "insert confirmed double spend",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstDoubleSpendSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, TstRecvTxBlockDetails)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(rec, TstRecvTxBlockDetails, 1, false,
					defaultAccount)
				return s, err
			},
			bal: dcrutil.Amount(TstDoubleSpendTx.MsgTx().TxOut[1].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstDoubleSpendTx.Sha(),
					Index: 0,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "insert unconfirmed debit",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstSpendingSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, nil)
				return s, err
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined: map[chainhash.Hash]struct{}{
				*TstSpendingTx.Sha(): {},
			},
		},
		{
			name: "insert unconfirmed debit again",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstDoubleSpendSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, TstRecvTxBlockDetails)
				return s, err
			},
			bal:      0,
			unc:      0,
			unspents: map[wire.OutPoint]struct{}{},
			unmined: map[chainhash.Hash]struct{}{
				*TstSpendingTx.Sha(): {},
			},
		},
		{
			name: "insert change (index 0)",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstSpendingSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, nil)
				if err != nil {
					return nil, err
				}

				err = s.AddCredit(rec, nil, 0, true, defaultAccount)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 0,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstSpendingTx.Sha(): {},
			},
		},
		{
			name: "insert output back to this own wallet (index 1)",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstSpendingSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, nil)
				if err != nil {
					return nil, err
				}
				err = s.AddCredit(rec, nil, 1, true, defaultAccount)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value + TstSpendingTx.MsgTx().TxOut[1].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 0,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstSpendingTx.Sha(): {},
			},
		},
		{
			name: "confirm signed tx",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstSpendingSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, TstSignedTxBlockDetails)
				return s, err
			},
			bal: dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value + TstSpendingTx.MsgTx().TxOut[1].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 0,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "rollback after spending tx",
			f: func(s *Store) (*Store, error) {
				err := s.Rollback(TstSignedTxBlockDetails.Height + 1)
				return s, err
			},
			bal: dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value + TstSpendingTx.MsgTx().TxOut[1].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 0,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
		{
			name: "rollback spending tx block",
			f: func(s *Store) (*Store, error) {
				err := s.Rollback(TstSignedTxBlockDetails.Height)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value + TstSpendingTx.MsgTx().TxOut[1].Value),
			unspents: map[wire.OutPoint]struct{}{
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 0,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
				{
					Hash:  *TstSpendingTx.Sha(),
					Index: 1,
					Tree:  dcrutil.TxTreeRegular,
				}: {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstSpendingTx.Sha(): {},
			},
		},
		{
			name: "rollback double spend tx block",
			f: func(s *Store) (*Store, error) {
				err := s.Rollback(TstRecvTxBlockDetails.Height)
				return s, err
			},
			bal: 0,
			unc: dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value + TstSpendingTx.MsgTx().TxOut[1].Value),
			unspents: map[wire.OutPoint]struct{}{
				*wire.NewOutPoint(TstSpendingTx.Sha(), 0, dcrutil.TxTreeRegular): {},
				*wire.NewOutPoint(TstSpendingTx.Sha(), 1, dcrutil.TxTreeRegular): {},
			},
			unmined: map[chainhash.Hash]struct{}{
				*TstDoubleSpendTx.Sha(): {},
				*TstSpendingTx.Sha():    {},
			},
		},
		{
			name: "insert original recv txout",
			f: func(s *Store) (*Store, error) {
				rec, err := NewTxRecord(TstRecvSerializedTx, time.Now())
				if err != nil {
					return nil, err
				}
				err = s.InsertTx(rec, TstRecvTxBlockDetails)
				if err != nil {
					return nil, err
				}
				err = s.AddCredit(rec, TstRecvTxBlockDetails, 1, false,
					defaultAccount)
				return s, err
			},
			bal: dcrutil.Amount(TstRecvTx.MsgTx().TxOut[1].Value),
			unc: 0,
			unspents: map[wire.OutPoint]struct{}{
				*wire.NewOutPoint(TstRecvTx.Sha(), 1, dcrutil.TxTreeRegular): {},
			},
			unmined: map[chainhash.Hash]struct{}{},
		},
	}

	s, teardown, err := testStore()
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range tests {
		tmpStore, err := test.f(s)
		if err != nil {
			t.Fatalf("%s: got error: %v", test.name, err)
		}
		s = tmpStore
		bal, err := s.Balance(1, TstRecvCurrentHeight, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("%s: Confirmed Balance failed: %v", test.name, err)
		}
		if bal != test.bal {
			t.Fatalf("%s: balance mismatch: expected: %d, got: %d", test.name, test.bal, bal)
		}
		unc, err := s.Balance(0, TstRecvCurrentHeight, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("%s: Unconfirmed Balance failed: %v", test.name, err)
		}
		unc -= bal
		if unc != test.unc {
			t.Fatalf("%s: unconfirmed balance mismatch: expected %d, got %d", test.name, test.unc, unc)
		}

		// Check that unspent outputs match expected.
		unspent, err := s.UnspentOutputs()
		if err != nil {
			t.Fatalf("%s: failed to fetch unspent outputs: %v", test.name, err)
		}
		for _, cred := range unspent {
			if _, ok := test.unspents[cred.OutPoint]; !ok {
				t.Errorf("%s: unexpected unspent output: %v", test.name, cred.OutPoint)
			}
			delete(test.unspents, cred.OutPoint)
		}
		if len(test.unspents) != 0 {
			t.Fatalf("%s: missing expected unspent output(s)", test.name)
		}

		// Check that unmined txs match expected.
		unmined, err := s.UnminedTxs()
		if err != nil {
			t.Fatalf("%s: cannot load unmined transactions: %v", test.name, err)
		}
		for _, tx := range unmined {
			txHash := tx.TxSha()
			if _, ok := test.unmined[txHash]; !ok {
				t.Fatalf("%s: unexpected unmined tx: %v", test.name, txHash)
			}
			delete(test.unmined, txHash)
		}
		if len(test.unmined) != 0 {
			t.Fatalf("%s: missing expected unmined tx(s)", test.name)
		}

	}
}

func TestFindingSpentCredits(t *testing.T) {
	t.Parallel()

	s, teardown, err := testStore()
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	// Insert transaction and credit which will be spent.
	recvRec, err := NewTxRecord(TstRecvSerializedTx, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	err = s.InsertTx(recvRec, TstRecvTxBlockDetails)
	if err != nil {
		t.Fatal(err)
	}
	defaultAccount := uint32(0)
	err = s.AddCredit(recvRec, TstRecvTxBlockDetails, 1, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}

	// Insert confirmed transaction which spends the above credit.
	spendingRec, err := NewTxRecord(TstSpendingSerializedTx, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	err = s.InsertTx(spendingRec, TstSignedTxBlockDetails)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spendingRec, TstSignedTxBlockDetails, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}

	bal, err := s.Balance(1, TstSignedTxBlockDetails.Height, wtxmgr.BFBalanceLockedStake, true, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	expectedBal := dcrutil.Amount(TstSpendingTx.MsgTx().TxOut[0].Value)
	if bal != expectedBal {
		t.Fatalf("bad balance: %v != %v", bal, expectedBal)
	}
	unspents, err := s.UnspentOutputs()
	if err != nil {
		t.Fatal(err)
	}
	op := wire.NewOutPoint(TstSpendingTx.Sha(), 0, dcrutil.TxTreeStake)
	if unspents[0].OutPoint != *op {
		t.Fatal("unspent outpoint doesn't match expected")
	}
	if len(unspents) > 1 {
		t.Fatal("has more than one unspent credit")
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
	t.Parallel()

	s, teardown, err := testStore()
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	b100 := BlockMeta{
		Block: Block{Height: 100},
		Time:  time.Now(),
	}

	cb := newCoinBase(20e8, 10e8, 30e8)
	cbRec, err := NewTxRecordFromMsgTx(cb, b100.Time)
	if err != nil {
		t.Fatal(err)
	}

	// Insert coinbase and mark outputs 0 and 2 as credits.
	err = s.InsertTx(cbRec, &b100)
	if err != nil {
		t.Fatal(err)
	}
	defaultAccount := uint32(0)
	err = s.AddCredit(cbRec, &b100, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(cbRec, &b100, 2, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}

	// Balance should be 0 if the coinbase is immature, 50 DCR at and beyond
	// maturity.
	//
	// Outputs when depth is below maturity are never included, no matter
	// the required number of confirmations.  Matured outputs which have
	// greater depth than minConf are still excluded.
	type balTest struct {
		height  int32
		minConf int32
		bal     dcrutil.Amount
	}
	balTests := []balTest{
		// Next block it is still immature
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 2,
			minConf: 0,
			bal:     0,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 2,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity),
			bal:     0,
		},

		// Next block it matures
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: 0,
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: 1,
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity),
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 1,
			bal:     0,
		},

		// Matures at this block
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: 0,
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: 1,
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity),
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 1,
			bal:     50e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 2,
			bal:     0,
		},
	}
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			false, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks after inserting coinbase")
	}

	// Spend an output from the coinbase tx in an unmined transaction when
	// the next block will mature the coinbase.
	spenderATime := time.Now()
	spenderA := spendOutput(&cbRec.Hash, 0, 0, 5e8, 15e8)
	spenderARec, err := NewTxRecordFromMsgTx(spenderA, spenderATime)
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTx(spenderARec, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spenderARec, nil, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}

	balTests = []balTest{
		// Next block it matures
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: 0,
			bal:     35e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: 1,
			bal:     30e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity),
			bal:     30e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity) - 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 1,
			bal:     0,
		},

		// Matures at this block
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: 0,
			bal:     35e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: 1,
			bal:     30e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity),
			bal:     30e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 1,
			bal:     30e8,
		},
		{
			height:  b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity),
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 2,
			bal:     0,
		},
	}
	balTestsBeforeMaturity := balTests
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks after spending coinbase with unmined transaction")
	}

	// Mine the spending transaction in the block the coinbase matures.
	bMaturity := BlockMeta{
		Block: Block{Height: b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity)},
		Time:  time.Now(),
	}
	err = s.InsertTx(spenderARec, &bMaturity)
	if err != nil {
		t.Fatal(err)
	}

	balTests = []balTest{
		// Maturity height
		{
			height:  bMaturity.Height,
			minConf: 0,
			bal:     35e8,
		},
		{
			height:  bMaturity.Height,
			minConf: 1,
			bal:     35e8,
		},
		{
			height:  bMaturity.Height,
			minConf: 2,
			bal:     30e8,
		},
		{
			height:  bMaturity.Height,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity),
			bal:     30e8,
		},
		{
			height:  bMaturity.Height,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 1,
			bal:     30e8,
		},
		{
			height:  bMaturity.Height,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 2,
			bal:     0,
		},

		// Next block after maturity height
		{
			height:  bMaturity.Height + 1,
			minConf: 0,
			bal:     35e8,
		},
		{
			height:  bMaturity.Height + 1,
			minConf: 2,
			bal:     35e8,
		},
		{
			height:  bMaturity.Height + 1,
			minConf: 3,
			bal:     30e8,
		},
		{
			height:  bMaturity.Height + 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 2,
			bal:     30e8,
		},
		{
			height:  bMaturity.Height + 1,
			minConf: int32(chaincfg.TestNetParams.CoinbaseMaturity) + 3,
			bal:     0,
		},
	}
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks mining coinbase spending transaction")
	}

	// Create another spending transaction which spends the credit from the
	// first spender.  This will be used to test removing the entire
	// conflict chain when the coinbase is later reorged out.
	//
	// Use the same output amount as spender A and mark it as a credit.
	// This will mean the balance tests should report identical results.
	spenderBTime := time.Now()
	spenderB := spendOutput(&spenderARec.Hash, 0, 0, 5e8)
	spenderBRec, err := NewTxRecordFromMsgTx(spenderB, spenderBTime)
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTx(spenderBRec, &bMaturity)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spenderBRec, &bMaturity, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks mining second spending transaction")
	}

	// Reorg out the block that matured the coinbase and check balances
	// again.
	err = s.Rollback(bMaturity.Height)
	if err != nil {
		t.Fatal(err)
	}
	balTests = balTestsBeforeMaturity
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks after reorging maturity block")
	}

	// Reorg out the block which contained the coinbase.  There should be no
	// more transactions in the store (since the previous outputs referenced
	// by the spending tx no longer exist), and the balance will always be
	// zero.
	err = s.Rollback(b100.Height)
	if err != nil {
		t.Fatal(err)
	}
	balTests = []balTest{
		// Current height
		{
			height:  b100.Height - 1,
			minConf: 0,
			bal:     0,
		},
		{
			height:  b100.Height - 1,
			minConf: 1,
			bal:     0,
		},

		// Next height
		{
			height:  b100.Height,
			minConf: 0,
			bal:     0,
		},
		{
			height:  b100.Height,
			minConf: 1,
			bal:     0,
		},
	}
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks after reorging coinbase block")
	}
	unminedTxs, err := s.UnminedTxs()
	if err != nil {
		t.Fatal(err)
	}
	if len(unminedTxs) != 0 {
		t.Fatalf("Should have no unmined transactions after coinbase reorg, found %d", len(unminedTxs))
	}
}

// Test moving multiple transactions from unmined buckets to the same block.
func TestMoveMultipleToSameBlock(t *testing.T) {
	t.Parallel()

	s, teardown, err := testStore()
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	b100 := BlockMeta{
		Block: Block{Height: 100},
		Time:  time.Now(),
	}

	cb := newCoinBase(20e8, 30e8)
	cbRec, err := NewTxRecordFromMsgTx(cb, b100.Time)
	if err != nil {
		t.Fatal(err)
	}

	// Insert coinbase and mark both outputs as credits.
	err = s.InsertTx(cbRec, &b100)
	if err != nil {
		t.Fatal(err)
	}
	defaultAccount := uint32(0)
	err = s.AddCredit(cbRec, &b100, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(cbRec, &b100, 1, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}

	// Create and insert two unmined transactions which spend both coinbase
	// outputs.
	spenderATime := time.Now()
	spenderA := spendOutput(&cbRec.Hash, 0, 0, 1e8, 2e8, 18e8)
	spenderARec, err := NewTxRecordFromMsgTx(spenderA, spenderATime)
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTx(spenderARec, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spenderARec, nil, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spenderARec, nil, 1, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	spenderBTime := time.Now()
	spenderB := spendOutput(&cbRec.Hash, 1, 0, 4e8, 8e8, 18e8)
	spenderBRec, err := NewTxRecordFromMsgTx(spenderB, spenderBTime)
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTx(spenderBRec, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spenderBRec, nil, 0, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}
	err = s.AddCredit(spenderBRec, nil, 1, false, defaultAccount)
	if err != nil {
		t.Fatal(err)
	}

	// Mine both transactions in the block that matures the coinbase.
	bMaturity := BlockMeta{
		Block: Block{Height: b100.Height + int32(chaincfg.TestNetParams.CoinbaseMaturity)},
		Time:  time.Now(),
	}
	err = s.InsertTx(spenderARec, &bMaturity)
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTx(spenderBRec, &bMaturity)
	if err != nil {
		t.Fatal(err)
	}

	// Check that both transactions can be queried at the maturity block.
	detailsA, err := s.UniqueTxDetails(&spenderARec.Hash, &bMaturity.Block)
	if err != nil {
		t.Fatal(err)
	}
	if detailsA == nil {
		t.Fatal("No details found for first spender")
	}
	detailsB, err := s.UniqueTxDetails(&spenderBRec.Hash, &bMaturity.Block)
	if err != nil {
		t.Fatal(err)
	}
	if detailsB == nil {
		t.Fatal("No details found for second spender")
	}

	// Verify that the balance was correctly updated on the block record
	// append and that no unmined transactions remain.
	balTests := []struct {
		height  int32
		minConf int32
		bal     dcrutil.Amount
	}{
		// Maturity height
		{
			height:  bMaturity.Height,
			minConf: 0,
			bal:     15e8,
		},
		{
			height:  bMaturity.Height,
			minConf: 1,
			bal:     15e8,
		},
		{
			height:  bMaturity.Height,
			minConf: 2,
			bal:     0,
		},

		// Next block after maturity height
		{
			height:  bMaturity.Height + 1,
			minConf: 0,
			bal:     15e8,
		},
		{
			height:  bMaturity.Height + 1,
			minConf: 2,
			bal:     15e8,
		},
		{
			height:  bMaturity.Height + 1,
			minConf: 3,
			bal:     0,
		},
	}
	for i, tst := range balTests {
		bal, err := s.Balance(tst.minConf, tst.height, wtxmgr.BFBalanceSpendable,
			true, defaultAccount)
		if err != nil {
			t.Fatalf("Balance test %d: Store.Balance failed: %v", i, err)
		}
		if bal != tst.bal {
			t.Errorf("Balance test %d: Got %v Expected %v", i, bal, tst.bal)
		}
	}
	if t.Failed() {
		t.Fatal("Failed balance checks after moving both coinbase spenders")
	}
	unminedTxs, err := s.UnminedTxs()
	if err != nil {
		t.Fatal(err)
	}
	if len(unminedTxs) != 0 {
		t.Fatalf("Should have no unmined transactions mining both, found %d", len(unminedTxs))
	}
}

// Test the optional-ness of the serialized transaction in a TxRecord.
// NewTxRecord and NewTxRecordFromMsgTx both save the serialized transaction, so
// manually strip it out to test this code path.
func TestInsertUnserializedTx(t *testing.T) {
	t.Parallel()

	s, teardown, err := testStore()
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	tx := newCoinBase(50e8)
	rec, err := NewTxRecordFromMsgTx(tx, timeNow())
	if err != nil {
		t.Fatal(err)
	}
	b100 := makeBlockMeta(100)
	err = s.InsertTx(stripSerializedTx(rec), &b100)
	if err != nil {
		t.Fatalf("Insert for stripped TxRecord failed: %v", err)
	}

	// Ensure it can be retreived successfully.
	details, err := s.UniqueTxDetails(&rec.Hash, &b100.Block)
	if err != nil {
		t.Fatal(err)
	}
	rec2, err := NewTxRecordFromMsgTx(&details.MsgTx, rec.Received)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.SerializedTx, rec2.SerializedTx) {
		t.Fatal("Serialized txs for coinbase do not match")
	}

	// Now test that path with an unmined transaction.
	tx = spendOutput(&rec.Hash, 0, 0, 50e8)
	rec, err = NewTxRecordFromMsgTx(tx, timeNow())
	if err != nil {
		t.Fatal(err)
	}
	err = s.InsertTx(rec, nil)
	if err != nil {
		t.Fatal(err)
	}
	details, err = s.UniqueTxDetails(&rec.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec2, err = NewTxRecordFromMsgTx(&details.MsgTx, rec.Received)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.SerializedTx, rec2.SerializedTx) {
		t.Fatal("Serialized txs for coinbase spender do not match")
	}
}
