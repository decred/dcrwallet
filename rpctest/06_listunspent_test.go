// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
)

func TestListUnspent(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(t.Name())
	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// New account
	accountName := "listUnspentTestAcct"
	err := wcl.CreateNewAccount(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// Grab an address from the test account
	addr, err := wcl.GetNewAddress(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// UTXOs before send
	list, err := wcl.ListUnspent()
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

	// Check Min/Maxconf cookArguments
	defaultMaxConf := 9999999

	listMin1MaxBig, err := wcl.ListUnspentMinMax(1, defaultMaxConf)
	if err != nil {
		t.Fatalf("failed to get utxos")
	}
	if !reflect.DeepEqual(list, listMin1MaxBig) {
		t.Fatal("Outputs from ListUnspent() and ListUnspentMinMax() do not match.")
	}

	// Grab an address from known unspents to test the filter
	refOut := list[0]
	PkScript, err := hex.DecodeString(refOut.ScriptPubKey)
	if err != nil {
		t.Fatalf("Failed to decode ScriptPubKey into PkScript.")
	}
	// The Address field is broken, including only one address, so don't use it
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(
		txscript.DefaultScriptVersion, PkScript, r.ActiveNet())
	if err != nil {
		t.Fatal("Failed to extract addresses from PkScript:", err)
	}

	// List with all of the above address
	listAddressesKnown, err := wcl.ListUnspentMinMaxAddresses(1, defaultMaxConf, addrs)
	if err != nil {
		t.Fatalf("Failed to get utxos with addresses argument.")
	}

	// Check that there is at least one output for the input addresses
	if len(listAddressesKnown) == 0 {
		t.Fatalf("Failed to find expected UTXOs with addresses.")
	}

	// Make sure each found output's txid:vout is in original list
	var foundTxID = false
	for _, listRes := range listAddressesKnown {
		// Get a OutPoint string in the form of hash:index
		outpointStr, err := getOutPointString(&listRes)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := utxosBeforeSend[outpointStr]; !ok {
			t.Fatalf("Failed to find TxID")
		}
		// Also verify that the txid of the reference output is in the list
		if listRes.TxID == refOut.TxID {
			foundTxID = true
		}
	}
	if !foundTxID {
		t.Fatal("Original TxID not found in list by addresses.")
	}

	// SendFromMinConf to addr
	amountToSend := dcrutil.Amount(700000000)
	txid, err := wcl.SendFromMinConf("default", addr, amountToSend, 0)
	if err != nil {
		t.Fatalf("sendfromminconf failed: %v", err)
	}

	newBestBlock(r, t)
	time.Sleep(1 * time.Second)
	// New block is necessary for GetRawTransaction to give a tx with sensible
	// MsgTx().TxIn[:].ValueIn values.

	// Get *dcrutil.Tx of send to check the inputs
	rawTx, err := r.DcrdRPCClient().GetRawTransaction(txid)
	if err != nil {
		t.Fatalf("getrawtransaction failed: %v", err)
	}

	// Get previous OutPoint of each TxIn for send transaction
	txInIDs := make(map[string]float64)
	for _, txIn := range rawTx.MsgTx().TxIn {
		prevOut := &txIn.PreviousOutPoint
		// Outpoint.String() appends :index to the hash
		txInIDs[prevOut.String()] = dcrutil.Amount(txIn.ValueIn).ToCoin()
	}

	// First check to make sure we see these in the UTXO list prior to send,
	// then not in the UTXO list after send.
	for txinID, amt := range txInIDs {
		if _, ok := utxosBeforeSend[txinID]; !ok {
			t.Fatalf("Failed to find txid %v (%v DCR) in list of UTXOs",
				txinID, amt)
		}
	}

	// Validate the send Tx with 2 new blocks
	newBestBlock(r, t)
	newBestBlock(r, t)

	// Make sure these txInIDS are not in the new UTXO set
	time.Sleep(2 * time.Second)
	list, err = wcl.ListUnspent()
	if err != nil {
		t.Fatalf("Failed to get UTXOs")
	}
	for _, utxo := range list {
		// Get a OutPoint string in the form of hash:index
		outpointStr, err := getOutPointString(&utxo)
		if err != nil {
			t.Fatal(err)
		}
		if amt, ok := txInIDs[outpointStr]; ok {
			t.Fatalf("Found PreviousOutPoint of send still in UTXO set: %v, "+
				"%v DCR", outpointStr, amt)
		}
	}
}
