// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
)

func TestListTransactions(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(TestListTransactionsHarnessTag)
	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// List latest transaction
	txList1, err := wcl.ListTransactionsCount("*", 1)
	if err != nil {
		t.Fatal("ListTransactionsCount failed:", err)
	}

	// Verify that only one returned (a PoW coinbase since this is a fresh
	// harness with only blocks generated and no other transactions).
	if len(txList1) != 1 {
		t.Fatalf("Transaction list not len=1: %d", len(txList1))
	}

	// Verify paid to MiningAddress
	if txList1[0].Address != r.MiningAddress.String() {
		t.Fatalf("Unexpected address in latest transaction: %v",
			txList1[0].Address)
	}

	// Verify that it is a coinbase
	if !txList1[0].Generated {
		t.Fatal("Latest transaction output not a coinbase output.")
	}

	// Not "generate" category until mature
	if txList1[0].Category != "immature" {
		t.Fatalf("Latest transaction not immature. Category: %v",
			txList1[0].Category)
	}

	// Verify blockhash is non-nil and valid
	hash, err := chainhash.NewHashFromStr(txList1[0].BlockHash)
	if err != nil {
		t.Fatal("Blockhash not valid")
	}
	_, err = wcl.GetBlock(hash)
	if err != nil {
		t.Fatal("Blockhash does not refer to valid block")
	}

	// "regular" not "stake" txtype
	if *txList1[0].TxType != dcrjson.LTTTRegular {
		t.Fatal(`txtype not "regular".`)
	}

	// ListUnspent only shows validated (confirmations>=1) coinbase tx, so the
	// first result should have 2 confirmations.
	if txList1[0].Confirmations != 1 {
		t.Fatalf("Latest coinbase tx listed has %v confirmations, expected 1.",
			txList1[0].Confirmations)
	}

	// Check txid
	txid, err := chainhash.NewHashFromStr(txList1[0].TxID)
	if err != nil {
		t.Fatal("Invalid Txid: ", err)
	}

	rawTx, err := wcl.GetRawTransaction(txid)
	if err != nil {
		t.Fatal("Invalid Txid: ", err)
	}

	// Use Vout from listtransaction to index []TxOut from getrawtransaction.
	if len(rawTx.MsgTx().TxOut) <= int(txList1[0].Vout) {
		t.Fatal("Too few vouts.")
	}
	txOut := rawTx.MsgTx().TxOut[txList1[0].Vout]
	voutAmt := dcrutil.Amount(txOut.Value).ToCoin()
	// Verify amounts agree
	if txList1[0].Amount != voutAmt {
		t.Fatalf("Listed amount %v does not match expected vout amount %v",
			txList1[0].Amount, voutAmt)
	}

	// Test number of transactions (count).  With only coinbase in this harness,
	// length of result slice should be equal to number requested.
	txList2, err := wcl.ListTransactionsCount("*", 2)
	if err != nil {
		t.Fatal("ListTransactionsCount failed:", err)
	}

	// With only coinbase transactions, there will only be one result per tx
	if len(txList2) != 2 {
		t.Fatalf("Expected 2 transactions, got %v", len(txList2))
	}

	// List all transactions
	txListAllInit, err := wcl.ListTransactionsCount("*", 9999999)
	if err != nil {
		t.Fatal("ListTransactionsCount failed:", err)
	}
	initNumTx := len(txListAllInit)

	// Send within WalletServer, and check for both send and receive parts of tx.
	accountName := "listTransactionsTest"
	if wcl.CreateNewAccount(accountName) != nil {
		t.Fatal("Failed to create account for listtransactions test")
	}

	addr, err := wcl.GetNewAddress(accountName)
	if err != nil {
		t.Fatal("Failed to get new address.")
	}

	sendAmount := dcrutil.Amount(240000000)
	txHash, err := wcl.SendFromMinConf("default", addr, sendAmount, 6)
	if err != nil {
		t.Fatal("Failed to send:", err)
	}

	// Number of results should be +3 now
	txListAll, err := wcl.ListTransactionsCount("*", 9999999)
	if err != nil {
		t.Fatal("ListTransactionsCount failed:", err)
	}
	// Expect 3 more results in the list: a receive for the owned address in
	// the amount sent, a send in the amount sent, and the a send from the
	// original outpoint for the mined coins.
	expectedAdditional := 3
	if len(txListAll) != initNumTx+expectedAdditional {
		t.Fatalf("Expected %v listtransactions results, got %v", initNumTx+expectedAdditional,
			len(txListAll))
	}

	// The top of the list should be one send and one receive.  The coinbase
	// spend should be lower in the list.
	var sendResult, recvResult dcrjson.ListTransactionsResult
	if txListAll[0].Category == txListAll[1].Category {
		t.Fatal("Expected one send and one receive, got two", txListAll[0].Category)
	}
	// Use a map since order doesn't matter, and keys are not duplicate
	rxtxResults := map[string]dcrjson.ListTransactionsResult{
		txListAll[0].Category: txListAll[0],
		txListAll[1].Category: txListAll[1],
	}
	var ok bool
	if sendResult, ok = rxtxResults["send"]; !ok {
		t.Fatal("Expected send transaction not found.")
	}
	if recvResult, ok = rxtxResults["receive"]; !ok {
		t.Fatal("Expected receive transaction not found.")
	}

	// Verify send result amount
	if sendResult.Amount != -sendAmount.ToCoin() {
		t.Fatalf("Listed send tx amount incorrect. Expected %v, got %v",
			-sendAmount.ToCoin(), sendResult.Amount)
	}

	// Verify send result fee
	if sendResult.Fee == nil {
		t.Fatal("Fee in send tx result is nil.")
	}

	// Now that there's a new Tx on top, skip back to previoius transaction
	// using from=1
	txList1New, err := wcl.ListTransactionsCountFrom("*", 1, 1)
	if err != nil {
		t.Fatal("Failed to listtransactions:", err)
	}

	// Should be equal to earlier result with implicit from=0
	if !reflect.DeepEqual(txList1, txList1New) {
		t.Fatal("Listtransaction results not equal.")
	}

	// Get rawTx of sent txid so we can calculate the fee that was used
	newBestBlock(r, t) // or getrawtransaction is wrong
	rawTx, err = r.DcrdRPCClient().GetRawTransaction(txHash)
	if err != nil {
		t.Fatalf("getrawtransaction failed: %v", err)
	}

	expectedFee := getWireMsgTxFee(rawTx).ToCoin()
	gotFee := -*sendResult.Fee
	if gotFee != expectedFee {
		t.Fatalf("Expected fee %v, got %v", expectedFee, gotFee)
	}

	// Verify receive results amount
	if recvResult.Amount != sendAmount.ToCoin() {
		t.Fatalf("Listed send tx amount incorrect. Expected %v, got %v",
			sendAmount.ToCoin(), recvResult.Amount)
	}

	// Verify TxID in both send and receive results
	txstr := txHash.String()
	if sendResult.TxID != txstr {
		t.Fatalf("TxID in send tx result was %v, expected %v.",
			sendResult.TxID, txstr)
	}
	if recvResult.TxID != txstr {
		t.Fatalf("TxID in receive tx result was %v, expected %v.",
			recvResult.TxID, txstr)
	}

	// Should only accept "*" account
	_, err = wcl.ListTransactions("default")
	if err == nil {
		t.Fatal(`Listtransactions should only work on "*" account. "default" succeeded.`)
	}

	txList0, err := wcl.ListTransactionsCount("*", 0)
	if err != nil {
		t.Fatal("listtransactions failed:", err)
	}
	if len(txList0) != 0 {
		t.Fatal("Length of listransactions result not zero:", len(txList0))
	}

	txListAll, err = wcl.ListTransactionsCount("*", 99999999)
	if err != nil {
		t.Fatal("ListTransactionsCount failed:", err)
	}

	// Create 2 accounts to receive funds
	accountNames := []string{"listTxA", "listTxB"}
	amountsToSend := []dcrutil.Amount{700000000, 1400000000}

	for _, acct := range accountNames {
		err := wcl.CreateNewAccount(acct)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Grab new addresses from the WalletServer, under each account.
	// Set corresponding amount to send to each address.
	addressAmounts := make(map[dcrutil.Address]dcrutil.Amount)

	for i, acct := range accountNames {
		addr, err := wcl.GetNewAddress(acct)
		if err != nil {
			t.Fatal(err)
		}

		// Set the amounts to send to each address
		addressAmounts[addr] = amountsToSend[i]
	}

	// SendMany to two addresses
	_, err = wcl.SendMany("default", addressAmounts)
	if err != nil {
		t.Fatalf("sendmany failed: %v", err)
	}

	// This should add 5 results: coinbase send, 2 receives, 2 sends
	listSentMany, err := wcl.ListTransactionsCount("*", 99999999)
	if err != nil {
		t.Fatalf("ListTransactionsCount failed: %v", err)
	}
	if len(listSentMany) != len(txListAll)+5 {
		t.Fatalf("Expected %v tx results, got %v", len(txListAll)+5,
			len(listSentMany))
	}
}
