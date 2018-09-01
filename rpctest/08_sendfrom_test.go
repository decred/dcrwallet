// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"
	"time"
	"math/big"

	"github.com/decred/dcrd/dcrutil"
)

func TestSendFrom(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(MainHarnessName)
	accountName := "sendFromTest"
	err := r.WalletRPCClient().CreateNewAccount(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// Grab a fresh address from the WalletServer.
	addr, err := r.WalletRPCClient().GetNewAddress(accountName)
	if err != nil {
		t.Fatal(err)
	}

	amountToSend := dcrutil.Amount(1000000)
	// Check spendable balance of default account
	defaultBalanceBeforeSend, err := r.WalletRPCClient().GetBalanceMinConf("default", 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}

	// Get utxo list before send
	list, err := r.WalletRPCClient().ListUnspent()
	if err != nil {
		t.Fatalf("failed to get utxos")
	}
	utxosBeforeSend := make(map[string]float64)
	for _, utxo := range list {
		// Get a OutPoint string in the form of hash:index
		outpointStr, err := getOutPointString(&utxo)
		if err != nil {
			t.Fatal(err)
		}
		// if utxo.Spendable ...
		utxosBeforeSend[outpointStr] = utxo.Amount
	}

	// SendFromMinConf 1000 to addr
	txid, err := r.WalletRPCClient().SendFromMinConf("default", addr, amountToSend, 0)
	if err != nil {
		t.Fatalf("sendfromminconf failed: %v", err)
	}

	// Check spendable balance of default account
	defaultBalanceAfterSendNoBlock, err := r.WalletRPCClient().GetBalanceMinConf("default", 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}

	// Check balance of sendfrom account
	sendFromBalanceAfterSendNoBlock, err := r.WalletRPCClient().GetBalanceMinConf(accountName, 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}
	if sendFromBalanceAfterSendNoBlock.Balances[0].Spendable != amountToSend.ToCoin() {
		t.Fatalf("balance for %s account incorrect:  want %v got %v",
			accountName, amountToSend, sendFromBalanceAfterSendNoBlock.Balances[0].Spendable)
	}

	// Generate a single block, the transaction the WalletServer created should
	// be found in this block.
	_, block, _ := newBestBlock(r, t)

	// Check to make sure the transaction that was sent was included in the block
	if len(block.Transactions()) <= 1 {
		t.Fatalf("expected transaction not included in block")
	}
	minedTx := block.Transactions()[1]
	txHash := minedTx.Hash()
	if *txid != *txHash {
		t.Fatalf("txid's don't match, %v vs. %v (actual vs. expected)",
			txHash, txid)
	}

	// Generate another block, since it takes 2 blocks to validate a tx
	newBestBlock(r, t)

	// Get rawTx of sent txid so we can calculate the fee that was used
	time.Sleep(1 * time.Second)
	rawTx, err := r.WalletRPCClient().GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("getrawtransaction failed: %v", err)
	}

	var totalSpent int64
	for _, txIn := range rawTx.MsgTx().TxIn {
		totalSpent += txIn.ValueIn
	}

	var totalSent int64
	for _, txOut := range rawTx.MsgTx().TxOut {
		totalSent += txOut.Value
	}

	feeAtoms := dcrutil.Amount(totalSpent - totalSent)

	// Calculate the expected balance for the default account after the tx was sent
	sentAtoms := amountToSend + feeAtoms
	sentCoinsFloat := sentAtoms.ToCoin()

	sentCoinsNegative := new(big.Float)
	sentCoinsNegative.SetFloat64(-sentCoinsFloat)

	oldBalanceCoins := new(big.Float)
	oldBalanceCoins.SetFloat64(defaultBalanceBeforeSend.Balances[0].Spendable)

	expectedBalanceCoins := new(big.Float)
	expectedBalanceCoins.Add(oldBalanceCoins, sentCoinsNegative)

	currentBalanceCoinsNegative := new(big.Float)
	currentBalanceCoinsNegative.SetFloat64(-defaultBalanceAfterSendNoBlock.Balances[0].Spendable)

	diff := new(big.Float)
	diff.Add(currentBalanceCoinsNegative, expectedBalanceCoins)

	if diff.Cmp(new(big.Float)) == 0 {
		t.Fatalf("balance for %s account incorrect: want %v got %v",
			"default",
			expectedBalanceCoins,
			defaultBalanceAfterSendNoBlock.Balances[0].Spendable,
		)
	}

	// Check balance of sendfrom account
	sendFromBalanceAfterSend1Block, err := r.WalletRPCClient().GetBalanceMinConf(accountName, 1)
	if err != nil {
		t.Fatalf("getbalanceminconftype failed: %v", err)
	}

	if sendFromBalanceAfterSend1Block.Balances[0].Total != amountToSend.ToCoin() {
		t.Fatalf("balance for %s account incorrect:  want %v got %v",
			accountName, amountToSend, sendFromBalanceAfterSend1Block.Balances[0].Total)
	}

	// We have confirmed that the expected tx was mined into the block.
	// We should now check to confirm that the utxo that WalletServer used to create
	// that sendfrom was properly marked to spent and removed from utxo set.

	// Get the sending Tx
	rawTx, err = r.WalletRPCClient().GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("Unable to get raw transaction %v: %v", txid, err)
	}

	// Check all inputs
	for i, txIn := range rawTx.MsgTx().TxIn {
		prevOut := &txIn.PreviousOutPoint

		// If a txout is spent (not in the UTXO set) GetTxOutResult will be nil
		res, err := r.WalletRPCClient().GetTxOut(&prevOut.Hash, prevOut.Index, false)
		if err != nil {
			t.Fatal("GetTxOut failure:", err)
		}
		if res != nil {
			t.Fatalf("Transaction output %v still unspent.", i)
		}
	}
}
