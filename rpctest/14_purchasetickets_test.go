// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
)

func TestPurchaseTickets(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(t.Name())
	// Wallet.purchaseTicket() in WalletServer/createtx.go

	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// Grab a fresh address from the WalletServer.
	addr, err := wcl.GetNewAddress("default")
	if err != nil {
		t.Fatal(err)
	}

	// Set various variables for the test
	minConf := 0
	expiry := 0
	priceLimit, err := dcrutil.NewAmount(2 * mustGetStakeDiffNext(r, t))
	if err != nil {
		t.Fatal("Invalid Amount.", err)
	}

	// Test nil ticketAddress
	oneTix := 1
	noSplitTransactions := false
	hashes, err := wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, &oneTix, nil, nil, &expiry, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Unable to purchase with nil ticketAddr:", err)
	}
	if len(hashes) != 1 {
		t.Fatal("More than one tx hash returned purchasing single ticket.")
	}
	_, err = wcl.GetRawTransaction(hashes[0])
	if err != nil {
		t.Fatal("Invalid Txid:", err)
	}

	// test numTickets == nil
	hashes, err = wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, nil, nil, nil, &expiry, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Unable to purchase with nil numTickets:", err)
	}
	if len(hashes) != 1 {
		t.Fatal("More than one tx hash returned. Expected one.")
	}
	_, err = wcl.GetRawTransaction(hashes[0])
	if err != nil {
		t.Fatal("Invalid Txid:", err)
	}

	// Get current blockheight to make sure chain is at the desiredHeight
	curBlockHeight := getBestBlockHeight(r, t)

	// Test expiry - earliest is next height + 1
	// invalid
	expiry = int(curBlockHeight)
	_, err = wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, nil, nil, nil, &expiry, &noSplitTransactions, nil)
	if err == nil {
		t.Fatal("Invalid expiry used to purchase tickets")
	}
	// invalid
	expiry = int(curBlockHeight) + 1
	_, err = wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, nil, nil, nil, &expiry, &noSplitTransactions, nil)
	if err == nil {
		t.Fatal("Invalid expiry used to purchase tickets")
	}

	// valid expiry
	expiry = int(curBlockHeight) + 2
	hashes, err = wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, nil, nil, nil, &expiry, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Unable to purchase tickets:", err)
	}
	if len(hashes) != 1 {
		t.Fatal("More than one tx hash returned. Expected one.")
	}
	ticketWithExpiry := hashes[0]
	_, err = wcl.GetRawTransaction(ticketWithExpiry)
	if err != nil {
		t.Fatal("Invalid Txid:", err)
	}

	// Now purchase 2 blocks worth of tickets to be mined before the above
	// ticket with an expiry 2 blocks away.

	// Increase the ticket fee so these SSTx get mined first
	walletInfo, err := wcl.WalletInfo()
	if err != nil {
		t.Fatal("WalletInfo failed.", err)
	}
	origTicketFee, err := dcrutil.NewAmount(walletInfo.TicketFee)
	if err != nil {
		t.Fatalf("Invalid Amount %f. %v", walletInfo.TicketFee, err)
	}
	newTicketFee, err := dcrutil.NewAmount(walletInfo.TicketFee * 1.5)
	if err != nil {
		t.Fatalf("Invalid Amount %f. %v", walletInfo.TicketFee, err)
	}

	if err = wcl.SetTicketFee(newTicketFee); err != nil {
		t.Fatalf("SetTicketFee failed for Amount %v: %v", newTicketFee, err)
	}

	expiry = 0
	numTicket := 2 * int(chaincfg.SimNetParams.MaxFreshStakePerBlock)
	_, err = r.WalletRPCClient().PurchaseTicket("default", priceLimit,
		&minConf, addr, &numTicket, nil, nil, &expiry, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Unable to purchase tickets:", err)
	}

	if err = wcl.SetTicketFee(origTicketFee); err != nil {
		t.Fatalf("SetTicketFee failed for Amount %v: %v", origTicketFee, err)
	}

	// Check for the ticket
	_, err = wcl.GetTransaction(ticketWithExpiry)
	if err != nil {
		t.Fatal("Ticket not found:", err)
	}

	// Mine 2 blocks, should include the higher fee tickets with no expiry
	curBlockHeight, _, _ = newBlockAt(curBlockHeight, r, t)
	curBlockHeight, _, _ = newBlockAt(curBlockHeight, r, t)

	// Ticket with expiry set should now be expired (unmined and removed from
	// mempool).  An unmined and expired tx should have been removed/pruned
	txRawVerbose, err := wcl.GetRawTransactionVerbose(ticketWithExpiry)
	if err == nil {
		t.Fatalf("Found transaction that should be expired (height %v): %v",
			txRawVerbose.BlockHeight, err)
	}

	// Test too low price
	lowPrice := dcrutil.Amount(1)
	hashes, err = wcl.PurchaseTicket("default", lowPrice,
		&minConf, nil, nil, nil, nil, nil, &noSplitTransactions, nil)
	if err == nil {
		t.Fatalf("PurchaseTicket succeeded with limit of %f, but diff was %f.",
			lowPrice.ToCoin(), mustGetStakeDiff(r, t))
	}
	if len(hashes) > 0 {
		t.Fatal("At least one tickets hash returned. Expected none.")
	}

	// NOTE: ticket maturity = 16 (spendable at 17), stakeenabled height = 144
	// Must have tickets purchased before block 128

	// Keep generating blocks until desiredHeight is achieved
	desiredHeight := uint32(150)
	numTicket = int(chaincfg.SimNetParams.MaxFreshStakePerBlock)
	for curBlockHeight < desiredHeight {
		priceLimit, err = dcrutil.NewAmount(2 * mustGetStakeDiffNext(r, t))
		if err != nil {
			t.Fatal("Invalid Amount.", err)
		}
		_, err = r.WalletRPCClient().PurchaseTicket("default", priceLimit,
			&minConf, addr, &numTicket, nil, nil, nil, &noSplitTransactions, nil)

		// Do not allow even ErrSStxPriceExceedsSpendLimit since price is set
		if err != nil {
			t.Fatal("Failed to purchase tickets:", err)
		}
		curBlockHeight, _, _ = newBlockAtQuick(curBlockHeight, r, t)
		time.Sleep(100 * time.Millisecond)
	}

	// Validate last tx
	newBestBlock(r, t)

	// TODO: test pool fees

}

func mustGetStakeDiff(r *Harness, t *testing.T) float64 {
	stakeDiffResult, err := r.WalletRPCClient().GetStakeDifficulty()
	if err != nil {
		t.Fatal("GetStakeDifficulty failed:", err)
	}

	return stakeDiffResult.CurrentStakeDifficulty
}
