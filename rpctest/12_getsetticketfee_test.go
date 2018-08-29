// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"

	"github.com/decred/dcrd/dcrutil"
)

func TestGetSetTicketFee(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(t.Name())
	// dcrrpcclient does not have a getticketee or any direct method, so we
	// need to use walletinfo to get.  SetTicketFee can be used to set.

	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// Get the current ticket fee
	walletInfo, err := wcl.WalletInfo()
	if err != nil {
		t.Fatal("WalletInfo failed:", err)
	}
	nominalTicketFee := walletInfo.TicketFee
	origTicketFee, err := dcrutil.NewAmount(nominalTicketFee)
	if err != nil {
		t.Fatal("Invalid Amount:", nominalTicketFee)
	}

	// Increase the ticket fee to ensure the SSTx in ths test gets mined
	newTicketFeeCoin := nominalTicketFee * 1.5
	newTicketFee, err := dcrutil.NewAmount(newTicketFeeCoin)
	if err != nil {
		t.Fatal("Invalid Amount:", newTicketFeeCoin)
	}

	err = wcl.SetTicketFee(newTicketFee)
	if err != nil {
		t.Fatal("SetTicketFee failed:", err)
	}

	// Check that WalletServer is set to use the new fee
	walletInfo, err = wcl.WalletInfo()
	if err != nil {
		t.Fatal("WalletInfo failed:", err)
	}
	nominalTicketFee = walletInfo.TicketFee
	newTicketFeeActual, err := dcrutil.NewAmount(nominalTicketFee)
	if err != nil {
		t.Fatalf("Invalid Amount %f. %v", nominalTicketFee, err)
	}
	if newTicketFee != newTicketFeeActual {
		t.Fatalf("Expected ticket fee %v, got %v.", newTicketFee,
			newTicketFeeActual)
	}

	// Purchase ticket
	minConf, numTickets := 0, 1
	priceLimit, err := dcrutil.NewAmount(2 * mustGetStakeDiffNext(r, t))
	if err != nil {
		t.Fatal("Invalid Amount. ", err)
	}
	noSplitTransactions := false
	hashes, err := wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, &numTickets, nil, nil, nil, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Unable to purchase ticket:", err)
	}
	if len(hashes) != numTickets {
		t.Fatalf("Number of returned hashes does not equal expected."+
			"got %v, want %v", len(hashes), numTickets)
	}

	// Need 2 blocks or the vin is incorrect in getrawtransaction
	// Not yet at StakeValidationHeight, so no voting.
	newBestBlock(r, t)
	newBestBlock(r, t)

	// Compute the actual fee for the ticket purchase
	rawTx, err := wcl.GetRawTransaction(hashes[0])
	if err != nil {
		t.Fatal("Invalid Txid:", err)
	}

	fee := getWireMsgTxFee(rawTx)
	feeRate := fee.ToCoin() / float64(rawTx.MsgTx().SerializeSize()) * 1000

	// Ensure actual fee is at least nominal
	t.Logf("Set ticket fee: %v, actual: %v", nominalTicketFee, feeRate)
	if feeRate < nominalTicketFee {
		t.Errorf("Ticket fee rate difference (actual-set) too high: %v",
			nominalTicketFee-feeRate)
	}

	// Negative fee should throw and error
	err = wcl.SetTicketFee(dcrutil.Amount(-1))
	if err == nil {
		t.Fatal("SetTicketFee accepted negative fee")
	}

	// Set it back
	err = wcl.SetTicketFee(origTicketFee)
	if err != nil {
		t.Fatal("SetTicketFee failed:", err)
	}

	// Validate last tx before we complete
	newBestBlock(r, t)
}
