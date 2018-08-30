// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
)

func TestGetTickets(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(MainHarnessName)
	// Wallet.purchaseTicket() in WalletServer/createtx.go

	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// Initial number of mature (live) tickets
	ticketHashes, err := wcl.GetTickets(false)
	if err != nil {
		t.Fatal("GetTickets failed:", err)
	}
	numTicketsInitLive := len(ticketHashes)

	// Initial number of immature (not live) and unconfirmed (unmined) tickets
	ticketHashes, err = wcl.GetTickets(true)
	if err != nil {
		t.Fatal("GetTickets failed:", err)
	}

	numTicketsInit := len(ticketHashes)

	// Purchase a full blocks worth of tickets
	minConf, numTicketsPurchased := 1, int(chaincfg.SimNetParams.MaxFreshStakePerBlock)
	priceLimit, err := dcrutil.NewAmount(2 * mustGetStakeDiffNext(r, t))
	if err != nil {
		t.Fatal("Invalid Amount. ", err)
	}
	noSplitTransactions := false
	hashes, err := wcl.PurchaseTicket("default", priceLimit,
		&minConf, nil, &numTicketsPurchased, nil, nil, nil, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Unable to purchase tickets:", err)
	}
	if len(hashes) != numTicketsPurchased {
		t.Fatalf("Expected %v ticket hashes, got %v.", numTicketsPurchased,
			len(hashes))
	}

	// Verify GetTickets(true) sees these unconfirmed SSTx
	ticketHashes, err = wcl.GetTickets(true)
	if err != nil {
		t.Fatal("GetTickets failed:", err)
	}

	if numTicketsInit+numTicketsPurchased != len(ticketHashes) {
		t.Fatal("GetTickets(true) did not include unmined tickets")
	}

	// Compare GetTickets(includeImmature = false) before the purchase with
	// GetTickets(includeImmature = true) after the purchase. This tests that
	// the former does exclude unconfirmed tickets, which we now have following
	// the above purchase.
	if len(ticketHashes) <= numTicketsInitLive {
		t.Fatalf("Number of live tickets (%d) not less than total tickets (%d).",
			numTicketsInitLive, len(ticketHashes))
	}

	// Mine the split tx and THEN stake submission itself
	newBestBlock(r, t)
	_, block, _ := newBestBlock(r, t)

	// Verify stake submissions were mined
	for _, hash := range hashes {
		if !includesStakeTx(hash, block) {
			t.Errorf("SSTx expected, not found in block %v.", block.Height())
		}
	}

	// Verify each SSTx hash
	for _, hash := range ticketHashes {
		tx, err := wcl.GetRawTransaction(hash)
		if err != nil {
			t.Fatalf("Invalid transaction %v: %v", tx, err)
		}

		// Ensure result is a SSTx
		if !stake.IsSStx(tx.MsgTx()) {
			t.Fatal("Ticket hash is not for a SSTx.")
		}
	}
}

// includesTx checks if a block contains a transaction hash
func includesStakeTx(txHash *chainhash.Hash, block *dcrutil.Block) bool {
	if len(block.STransactions()) <= 1 {
		return false
	}

	blockTxs := block.STransactions()

	for _, minedTx := range blockTxs {
		minedTxHash := minedTx.Hash()
		if *txHash == *minedTxHash {
			return true
		}
	}

	return false
}
