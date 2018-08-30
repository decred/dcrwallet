// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"

	"github.com/decred/dcrd/dcrutil"
)

func TestGetSetRelayFee(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(MainHarnessName)

	// dcrrpcclient does not have a getwalletfee or any direct method, so we
	// need to use walletinfo to get.  SetTxFee can be used to set.

	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// Increase the ticket fee so these SSTx get mined first
	walletInfo, err := wcl.WalletInfo()
	if err != nil {
		t.Fatal("WalletInfo failed:", err)
	}
	// Save the original fee
	origTxFee, err := dcrutil.NewAmount(walletInfo.TxFee)
	if err != nil {
		t.Fatalf("Invalid Amount %f. %v", walletInfo.TxFee, err)
	}
	// Increase fee by 50%
	newTxFeeCoin := walletInfo.TxFee * 1.5
	newTxFee, err := dcrutil.NewAmount(newTxFeeCoin)
	if err != nil {
		t.Fatalf("Invalid Amount %f. %v", newTxFeeCoin, err)
	}

	err = wcl.SetTxFee(newTxFee)
	if err != nil {
		t.Fatal("SetTxFee failed:", err)
	}

	// Check that WalletServer thinks the fee is as expected
	walletInfo, err = wcl.WalletInfo()
	if err != nil {
		t.Fatal("WalletInfo failed:", err)
	}
	newTxFeeActual, err := dcrutil.NewAmount(walletInfo.TxFee)
	if err != nil {
		t.Fatalf("Invalid Amount %f. %v", walletInfo.TxFee, err)
	}
	if newTxFee != newTxFeeActual {
		t.Fatalf("Expected tx fee %v, got %v.", newTxFee, newTxFeeActual)
	}

	// Create a transaction and compute the effective fee
	accountName := "testGetSetRelayFee"
	err = wcl.CreateNewAccount(accountName)
	if err != nil {
		t.Fatal("Failed to create account.")
	}

	// Grab a fresh address from the test account
	addr, err := r.WalletRPCClient().GetNewAddress(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// SendFromMinConf to addr
	amountToSend := dcrutil.Amount(700000000)
	txid, err := wcl.SendFromMinConf("default", addr, amountToSend, 0)
	if err != nil {
		t.Fatalf("sendfromminconf failed: %v", err)
	}

	newBestBlock(r, t)

	// Compute the fee
	rawTx, err := r.DcrdRPCClient().GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("getrawtransaction failed: %v", err)
	}

	fee := getWireMsgTxFee(rawTx)
	feeRate := fee.ToCoin() / float64(rawTx.MsgTx().SerializeSize()) * 1000

	// Ensure actual fee is at least nominal
	t.Logf("Set relay fee: %v, actual: %v", walletInfo.TxFee, feeRate)
	if feeRate < walletInfo.TxFee {
		t.Errorf("Regular tx fee rate difference (actual-set) too high: %v",
			walletInfo.TxFee-feeRate)
	}

	// Negative fee should throw an error
	err = wcl.SetTxFee(dcrutil.Amount(-1))
	if err == nil {
		t.Fatal("SetTxFee accepted negative fee")
	}

	// Set it back
	err = wcl.SetTxFee(origTxFee)
	if err != nil {
		t.Fatal("SetTxFee failed:", err)
	}

	// Validate last tx before we complete
	newBestBlock(r, t)
}
