// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package rpctest

import (
	"testing"

	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
)

func TestGetBalance(t *testing.T) {
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

	accountName := "getBalanceTest"
	err := wcl.CreateNewAccount(accountName)
	if err != nil {
		t.Fatalf("CreateNewAccount failed: %v", err)
	}

	// Grab a fresh address from the test account
	addr, err := r.WalletRPCClient().GetNewAddress(accountName)
	if err != nil {
		t.Fatalf("GetNewAddress failed: %v", err)
	}

	// Check invalid account name
	_, err = wcl.GetBalanceMinConf("invalid account", 0)
	// -4: account name 'invalid account' not found
	if err == nil {
		t.Fatalf("GetBalanceMinConfType failed to return non-nil error for invalid account name: %v", err)
	}

	// Check invalid minconf
	_, err = wcl.GetBalanceMinConf("default", -1)
	if err == nil {
		t.Fatalf("GetBalanceMinConf failed to return non-nil error for invalid minconf (-1)")
	}

	preBalances, err := wcl.GetBalanceMinConf("*", 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf(\"*\", 0) failed: %v", err)
	}

	preAccountBalanceSpendable := 0.0
	preAccountBalances := make(map[string]dcrjson.GetAccountBalanceResult)
	for _, bal := range preBalances.Balances {
		preAccountBalanceSpendable += bal.Spendable
		preAccountBalances[bal.AccountName] = bal
	}

	// Send from default to test account
	sendAmount := dcrutil.Amount(700000000)
	if _, err = wcl.SendFromMinConf("default", addr, sendAmount, 1); err != nil {
		t.Fatalf("SendFromMinConf failed: %v", err)
	}

	// Check invalid minconf
	postBalances, err := wcl.GetBalanceMinConf("*", 0)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}

	postAccountBalanceSpendable := 0.0
	postAccountBalances := make(map[string]dcrjson.GetAccountBalanceResult)
	for _, bal := range postBalances.Balances {
		postAccountBalanceSpendable += bal.Spendable
		postAccountBalances[bal.AccountName] = bal
	}

	// Fees prevent easy exact comparison
	if preAccountBalances["default"].Spendable <= postAccountBalances["default"].Spendable {
		t.Fatalf("spendable balance of account 'default' not decreased: %v <= %v",
			preAccountBalances["default"].Spendable,
			postAccountBalances["default"].Spendable)
	}

	if sendAmount.ToCoin() != (postAccountBalances[accountName].Spendable - preAccountBalances[accountName].Spendable) {
		t.Fatalf("spendable balance of account '%s' not increased: %v >= %v",
			accountName,
			preAccountBalances[accountName].Spendable,
			postAccountBalances[accountName].Spendable)
	}

	// Make sure "*" account balance has decreased (fees)
	if postAccountBalanceSpendable >= preAccountBalanceSpendable {
		t.Fatalf("Total balance over all accounts not decreased after send.")
	}

	// Test vanilla GetBalance()
	amtGetBalance, err := wcl.GetBalance("default")
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}

	// For GetBalance(), default minconf=1.
	defaultBalanceMinConf1, err := wcl.GetBalanceMinConf("default", 1)
	if err != nil {
		t.Fatalf("GetBalanceMinConfType failed: %v", err)
	}

	if amtGetBalance.Balances[0].Spendable != defaultBalanceMinConf1.Balances[0].Spendable {
		t.Fatalf(`Balance from GetBalance("default") does not equal amount `+
			`from GetBalanceMinConf: %v != %v`,
			amtGetBalance.Balances[0].Spendable,
			defaultBalanceMinConf1.Balances[0].Spendable)
	}

	// Verify minconf=1 balances of receiving account before/after new block
	// Before, getbalance minconf=1
	amtTestMinconf1BeforeBlock, err := wcl.GetBalanceMinConf(accountName, 1)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}

	// Mine 2 new blocks to validate tx
	newBestBlock(r, t)
	newBestBlock(r, t)

	// After, getbalance minconf=1
	amtTestMinconf1AfterBlock, err := wcl.GetBalanceMinConf(accountName, 1)
	if err != nil {
		t.Fatalf("GetBalanceMinConf failed: %v", err)
	}

	// Verify that balance (minconf=1) has increased
	if sendAmount.ToCoin() != (amtTestMinconf1AfterBlock.Balances[0].Spendable - amtTestMinconf1BeforeBlock.Balances[0].Spendable) {
		t.Fatalf(`Balance (minconf=1) not increased after new block: %v - %v != %v`,
			amtTestMinconf1AfterBlock.Balances[0].Spendable,
			amtTestMinconf1BeforeBlock.Balances[0].Spendable,
			sendAmount)
	}
}
