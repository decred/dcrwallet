// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
)

// testGetStakeInfo gets a FRESH harness
func TestGetStakeInfo(t *testing.T) {
	// Skip tests when running with -short
	if testing.Short() {
		t.Skip("Skipping RPC harness tests in short mode")
	}
	if skipTest(t) {
		t.Skip("Skipping test")
	}
	r := ObtainHarness(TestGetStakeInfoHarnessTag)
	// Wallet RPC client
	wcl := r.WalletRPCClient()

	// Compare stake difficulty from getstakeinfo with getstakeinfo
	sdiff, err := wcl.GetStakeDifficulty()
	if err != nil {
		t.Fatal("GetStakeDifficulty failed: ", err)
	}

	stakeinfo, err := wcl.GetStakeInfo()
	if err != nil {
		t.Fatal("GetStakeInfo failed: ", err)
	}
	// Ensure we are starting with a fresh harness
	if stakeinfo.AllMempoolTix != 0 || stakeinfo.Immature != 0 ||
		stakeinfo.Live != 0 {
		t.Fatalf("GetStakeInfo reported active tickets. Expected 0, got:\n"+
			"%d/%d/%d (allmempooltix/immature/live)",
			stakeinfo.AllMempoolTix, stakeinfo.Immature, stakeinfo.Live)
	}
	// At the expected block height
	height, block, _ := getBestBlock(r, t)
	if stakeinfo.BlockHeight != int64(height) {
		t.Fatalf("Block height reported by GetStakeInfo incorrect. Expected %d, got %d.",
			height, stakeinfo.BlockHeight)
	}
	poolSize := block.MsgBlock().Header.PoolSize
	if stakeinfo.PoolSize != poolSize {
		t.Fatalf("Reported pool size incorrect. Expected %d, got %d.",
			poolSize, stakeinfo.PoolSize)
	}

	// Ticket fate values should also be zero
	if stakeinfo.Voted != 0 || stakeinfo.Missed != 0 ||
		stakeinfo.Revoked != 0 {
		t.Fatalf("GetStakeInfo reported spent tickets:\n"+
			"%d/%d/%d (voted/missed/revoked/pct. missed)", stakeinfo.Voted,
			stakeinfo.Missed, stakeinfo.Revoked)
	}
	if stakeinfo.ProportionLive != 0 {
		t.Fatalf("ProportionLive incorrect. Expected %f, got %f.", 0.0,
			stakeinfo.ProportionLive)
	}
	if stakeinfo.ProportionMissed != 0 {
		t.Fatalf("ProportionMissed incorrect. Expected %f, got %f.", 0.0,
			stakeinfo.ProportionMissed)
	}

	// Verify getstakeinfo.difficulty == getstakedifficulty
	if sdiff.CurrentStakeDifficulty != stakeinfo.Difficulty {
		t.Fatalf("Stake difficulty mismatch: %f vs %f (getstakedifficulty, getstakeinfo)",
			sdiff.CurrentStakeDifficulty, stakeinfo.Difficulty)
	}

	// Buy tickets to check that they shows up in ownmempooltix/allmempooltix
	minConf := 1
	priceLimit, err := dcrutil.NewAmount(2 * mustGetStakeDiffNext(r, t))
	if err != nil {
		t.Fatal("Invalid Amount.", err)
	}
	numTickets := int(chaincfg.SimNetParams.MaxFreshStakePerBlock)
	noSplitTransactions := false
	tickets, err := r.WalletRPCClient().PurchaseTicket("default", priceLimit,
		&minConf, nil, &numTickets, nil, nil, nil, &noSplitTransactions, nil)
	if err != nil {
		t.Fatal("Failed to purchase tickets:", err)
	}

	// Before mining a block allmempooltix and ownmempooltix should be equal to
	// the number of tickets just purchesed in this fresh harness
	stakeinfo = mustGetStakeInfo(wcl, t)
	if stakeinfo.AllMempoolTix != uint32(numTickets) {
		t.Fatalf("getstakeinfo AllMempoolTix mismatch: %d vs %d",
			stakeinfo.AllMempoolTix, numTickets)
	}
	if stakeinfo.AllMempoolTix != stakeinfo.OwnMempoolTix {
		t.Fatalf("getstakeinfo AllMempoolTix/OwnMempoolTix mismatch: %d vs %d",
			stakeinfo.AllMempoolTix, stakeinfo.OwnMempoolTix)
	}

	// Mine the split tx, which creates the correctly-sized outpoints for the
	// actual SSTx
	newBestBlock(r, t)
	// Mine SSTx
	newBestBlock(r, t)

	// Compute the height at which these tickets mature
	ticketsTx, err := wcl.GetRawTransactionVerbose(tickets[0])
	if err != nil {
		t.Fatalf("Unable to gettransaction for ticket.")
	}
	maturityHeight := ticketsTx.BlockHeight + int64(chaincfg.SimNetParams.TicketMaturity)

	// After mining tickets, immature should be the number of tickets
	stakeinfo = mustGetStakeInfo(wcl, t)
	if stakeinfo.Immature != uint32(numTickets) {
		t.Fatalf("Tickets not reported as immature (got %d, expected %d)",
			stakeinfo.Immature, numTickets)
	}
	// mempool tickets should be zero
	if stakeinfo.OwnMempoolTix != 0 {
		t.Fatalf("Tickets reported in mempool (got %d, expected %d)",
			stakeinfo.OwnMempoolTix, 0)
	}
	// mempool tickets should be zero
	if stakeinfo.AllMempoolTix != 0 {
		t.Fatalf("Tickets reported in mempool (got %d, expected %d)",
			stakeinfo.AllMempoolTix, 0)
	}

	// Advance to maturity height
	t.Logf("Advancing to maturity height %d for tickets in block %d", maturityHeight,
		ticketsTx.BlockHeight)
	advanceToHeight(r, t, uint32(maturityHeight))
	// NOTE: voting does not begin until TicketValidationHeight

	// mature should be number of tickets now
	stakeinfo = mustGetStakeInfo(wcl, t)
	if stakeinfo.Live != uint32(numTickets) {
		t.Fatalf("Tickets not reported as live (got %d, expected %d)",
			stakeinfo.Live, numTickets)
	}
	// immature tickets should be zero
	if stakeinfo.Immature != 0 {
		t.Fatalf("Tickets reported as immature (got %d, expected %d)",
			stakeinfo.Immature, 0)
	}

	// Buy some more tickets (4 blocks worth) so chain doesn't stall when voting
	// burns through the batch purchased above
	for i := 0; i < 4; i++ {
		priceLimit, err := dcrutil.NewAmount(2 * mustGetStakeDiffNext(r, t))
		if err != nil {
			t.Fatal("Invalid Amount.", err)
		}
		numTickets := int(chaincfg.SimNetParams.MaxFreshStakePerBlock)
		_, err = r.WalletRPCClient().PurchaseTicket("default", priceLimit,
			&minConf, nil, &numTickets, nil, nil, nil, &noSplitTransactions, nil)
		if err != nil {
			t.Fatal("Failed to purchase tickets:", err)
		}

		newBestBlock(r, t)
	}

	// Advance to voting height and votes should happen right away
	votingHeight := chaincfg.SimNetParams.StakeValidationHeight
	advanceToHeight(r, t, uint32(votingHeight))
	time.Sleep(250 * time.Millisecond)

	// voted should be TicketsPerBlock
	stakeinfo = mustGetStakeInfo(wcl, t)
	expectedVotes := chaincfg.SimNetParams.TicketsPerBlock
	if stakeinfo.Voted != uint32(expectedVotes) {
		t.Fatalf("Tickets not reported as voted (got %d, expected %d)",
			stakeinfo.Voted, expectedVotes)
	}

	newBestBlock(r, t)
	// voted should be 2*TicketsPerBlock
	stakeinfo = mustGetStakeInfo(wcl, t)
	expectedVotes = 2 * chaincfg.SimNetParams.TicketsPerBlock
	if stakeinfo.Voted != uint32(expectedVotes) {
		t.Fatalf("Tickets not reported as voted (got %d, expected %d)",
			stakeinfo.Voted, expectedVotes)
	}

	// ProportionLive
	proportionLive := float64(stakeinfo.Live) / float64(stakeinfo.PoolSize)
	if stakeinfo.ProportionLive != proportionLive {
		t.Fatalf("ProportionLive mismatch.  Expected %f, got %f",
			proportionLive, stakeinfo.ProportionLive)
	}

	// ProportionMissed
	proportionMissed := float64(stakeinfo.Missed) /
		(float64(stakeinfo.Voted) + float64(stakeinfo.Missed))
	if stakeinfo.ProportionMissed != proportionMissed {
		t.Fatalf("ProportionMissed mismatch.  Expected %f, got %f",
			proportionMissed, stakeinfo.ProportionMissed)
	}
}

func advanceToHeight(r *Harness, t *testing.T, height uint32) {
	curBlockHeight := getBestBlockHeight(r, t)
	initHeight := curBlockHeight

	if curBlockHeight >= height {
		return
	}

	for curBlockHeight != height {
		curBlockHeight, _, _ = newBlockAtQuick(curBlockHeight, r, t)
		time.Sleep(75 * time.Millisecond)
	}
	t.Logf("Advanced %d blocks to block height %d", curBlockHeight-initHeight,
		curBlockHeight)
}

func getBestBlock(r *Harness, t *testing.T) (uint32, *dcrutil.Block, *chainhash.Hash) {
	bestBlockHash, err := r.DcrdRPCClient().GetBestBlockHash()
	if err != nil {
		t.Fatalf("Unable to get best block hash: %v", err)
	}
	bestBlock, err := r.DcrdRPCClient().GetBlock(bestBlockHash)
	if err != nil {
		t.Fatalf("Unable to get block: %v", err)
	}
	curBlockHeight := bestBlock.Header.Height

	return curBlockHeight, dcrutil.NewBlock(bestBlock), bestBlockHash
}

func mustGetStakeInfo(wcl *rpcclient.Client, t *testing.T) *dcrjson.GetStakeInfoResult {
	stakeinfo, err := wcl.GetStakeInfo()
	if err != nil {
		t.Fatal("GetStakeInfo failed: ", err)
	}
	return stakeinfo
}
