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

func TestSendMany(t *testing.T) {
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

	// Create 2 accounts to receive funds
	accountNames := []string{"sendManyTestA", "sendManyTestB"}
	amountsToSend := []dcrutil.Amount{700000000, 1400000000}
	addresses := []dcrutil.Address{}

	var err error
	for _, acct := range accountNames {
		err = wcl.CreateNewAccount(acct)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Grab new addresses from the WalletServer, under each account.
	// Set corresponding amount to send to each address.
	addressAmounts := make(map[dcrutil.Address]dcrutil.Amount)
	totalAmountToSend := dcrutil.Amount(0)

	for i, acct := range accountNames {
		addr, err := wcl.GetNewAddress(acct)
		if err != nil {
			t.Fatal(err)
		}

		// Set the amounts to send to each address
		addresses = append(addresses, addr)
		addressAmounts[addr] = amountsToSend[i]
		totalAmountToSend += amountsToSend[i]
	}

	// Check spendable balance of default account
	defaultBalanceBeforeSend, err := wcl.GetBalanceMinConf("default", 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf default failed: %v", err)
	}

	// SendMany to two addresses
	txid, err := wcl.SendMany("default", addressAmounts)
	if err != nil {
		t.Fatalf("SendMany failed: %v", err)
	}

	// XXX
	time.Sleep(250 * time.Millisecond)

	// Check spendable balance of default account
	defaultBalanceAfterSendUnmined, err := r.WalletRPCClient().GetBalanceMinConf("default", 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}

	// Check balance of each receiving account
	for i, acct := range accountNames {
		bal, err := r.WalletRPCClient().GetBalanceMinConf(acct, 0)
		if err != nil {
			t.Fatalf("GetBalanceMinConf '%s' failed: %v", acct, err)
		}
		addr := addresses[i]
		if bal.Balances[0].Total != addressAmounts[addr].ToCoin() {
			t.Fatalf("Balance for %s account incorrect:  want %v got %v",
				acct, addressAmounts[addr], bal)
		}
	}

	// Get rawTx of sent txid so we can calculate the fee that was used
	rawTx, err := r.DcrdRPCClient().GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("getrawtransaction failed: %v", err)
	}
	fee := getWireMsgTxFee(rawTx)
	t.Log("Raw TX before mining block: ", rawTx, " Fee: ", fee)

	// Generate a single block, the transaction the WalletServer created should be
	// found in this block.
	_, block, _ := newBestBlock(r, t)

	rawTx, err = r.DcrdRPCClient().GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("getrawtransaction failed: %v", err)
	}
	fee = getWireMsgTxFee(rawTx)
	t.Log("Raw TX after mining block: ", rawTx, " Fee: ", fee)

	// Calculate the expected balance for the default account after the tx was sent

	sentAtoms := totalAmountToSend + fee
	sentCoinsFloat := sentAtoms.ToCoin()

	sentCoinsNegative := new(big.Float)
	sentCoinsNegative.SetFloat64(-sentCoinsFloat)

	oldBalanceCoins := new(big.Float)
	oldBalanceCoins.SetFloat64(defaultBalanceBeforeSend.Balances[0].Spendable)

	expectedBalanceCoins := new(big.Float)
	expectedBalanceCoins.Add(oldBalanceCoins, sentCoinsNegative)

	currentBalanceCoinsNegative := new(big.Float)
	currentBalanceCoinsNegative.SetFloat64(-defaultBalanceAfterSendUnmined.Balances[0].Spendable)

	diff := new(big.Float)
	diff.Add(currentBalanceCoinsNegative, expectedBalanceCoins)

	if diff.Cmp(new(big.Float)) == 0 {
		t.Fatalf("Balance for %s account (sender) incorrect: want %v got %v",
			"default",
			expectedBalanceCoins,
			defaultBalanceAfterSendUnmined.Balances[0].Spendable,
		)
	}

	// Check to make sure the transaction that was sent was included in the block
	if !includesTx(txid, block) {
		t.Fatalf("Expected transaction not included in block")
	}

	// Validate
	newBestBlock(r, t)

	// Check balance after confirmations
	for i, acct := range accountNames {
		balanceAcctValidated, err := wcl.GetBalanceMinConf(acct, 1)
		if err != nil {
			t.Fatalf("GetBalanceMinConf '%s' failed: %v", acct, err)
		}

		addr := addresses[i]
		if balanceAcctValidated.Balances[0].Total != addressAmounts[addr].ToCoin() {
			t.Fatalf("Balance for %s account incorrect:  want %v got %v",
				acct, addressAmounts[addr].ToCoin(), balanceAcctValidated.Balances[0].Total)
		}
	}

	// Check all inputs
	for i, txIn := range rawTx.MsgTx().TxIn {
		prevOut := &txIn.PreviousOutPoint

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
