// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"
	"time"
)

func TestSendToAddress(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(MainHarnessName)
	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// Grab a fresh address from the WalletServer.
	addr, err := wcl.GetNewAddress("default")
	if err != nil {
		t.Fatal(err)
	}

	// Check balance of default account
	_, err = wcl.GetBalanceMinConf("default", 1)
	if err != nil {
		t.Fatalf("GetBalanceMinConfType failed: %v", err)
	}

	// SendToAddress
	txid, err := wcl.SendToAddress(addr, 1000000)
	if err != nil {
		t.Fatalf("SendToAddress failed: %v", err)
	}

	// Generate a single block, in which the transaction the WalletServer created
	// should be found.
	_, block, _ := newBestBlock(r, t)

	if len(block.Transactions()) <= 1 {
		t.Fatalf("expected transaction not included in block")
	}
	// Confirm that the expected tx was mined into the block.
	minedTx := block.Transactions()[1]
	txHash := minedTx.Hash()
	if *txid != *txHash {
		t.Fatalf("txid's don't match, %v vs %v", txHash, txid)
	}

	// We should now check to confirm that the utxo that WalletServer used to create
	// that sendfrom was properly marked as spent and removed from utxo set. Use
	// GetTxOut to tell if the outpoint is spent.
	//
	// The spending transaction has to be off the tip block for the previous
	// outpoint to be spent, out of the UTXO set. Generate another block.
	_, err = r.GenerateBlock(block.MsgBlock().Header.Height)
	if err != nil {
		t.Fatal(err)
	}

	// Check each PreviousOutPoint for the sending tx.
	time.Sleep(1 * time.Second)
	// Get the sending Tx
	rawTx, err := wcl.GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("Unable to get raw transaction %v: %v", txid, err)
	}
	// txid is rawTx.MsgTx().TxIn[0].PreviousOutPoint.Hash

	// Check all inputs
	for i, txIn := range rawTx.MsgTx().TxIn {
		prevOut := &txIn.PreviousOutPoint
		t.Logf("Checking previous outpoint %v, %v", i, prevOut.String())

		// If a txout is spent (not in the UTXO set) GetTxOutResult will be nil
		res, err := wcl.GetTxOut(&prevOut.Hash, prevOut.Index, false)
		if err != nil {
			t.Fatal("GetTxOut failure:", err)
		}
		if res != nil {
			t.Fatalf("Transaction output %v still unspent.", i)
		}
	}
}
