// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"reflect"
	"testing"

	"github.com/decred/dcrd/dcrutil"
)

func TestListAccounts(t *testing.T) {
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

	// Create a new account and verify that we can see it
	listBeforeCreateAccount, err := wcl.ListAccounts()
	if err != nil {
		t.Fatal("Failed to create new account ", err)
	}

	// New account
	accountName := "listaccountsTestAcct"
	err = wcl.CreateNewAccount(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// Account list after creating new
	accountsBalancesDefault1, err := wcl.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}

	// Verify that new account is in the list, with zero balance
	foundNewAcct := false
	for acct, amt := range accountsBalancesDefault1 {
		if _, ok := listBeforeCreateAccount[acct]; !ok {
			// Found new account.  Now check name and balance
			if amt != 0 {
				t.Fatalf("New account (%v) found with non-zero balance: %v",
					acct, amt)
			}
			if accountName == acct {
				foundNewAcct = true
				break
			}
			t.Fatalf("Found new account, %v; Expected %v", acct, accountName)
		}
	}
	if !foundNewAcct {
		t.Fatalf("Failed to find newly created account, %v.", accountName)
	}

	// Grab a fresh address from the test account
	addr, err := r.WalletRPCClient().GetNewAddress(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// For ListAccountsCmd: MinConf *int `jsonrpcdefault:"1"`
	// Let's test that ListAccounts() is equivalent to explicit minconf=1
	accountsBalancesMinconf1, err := wcl.ListAccountsMinConf(1)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(accountsBalancesDefault1, accountsBalancesMinconf1) {
		t.Fatal("ListAccounts() returned different result from ListAccountsMinConf(1): ",
			accountsBalancesDefault1, accountsBalancesMinconf1)
	}

	// Get accounts with minconf=0 pre-send
	accountsBalancesMinconf0PreSend, err := wcl.ListAccountsMinConf(0)
	if err != nil {
		t.Fatal(err)
	}

	// Get balance of test account prior to a send
	acctBalancePreSend := accountsBalancesMinconf0PreSend[accountName]

	// Send from default to test account
	sendAmount := dcrutil.Amount(700000000)
	if _, err = wcl.SendFromMinConf("default", addr, sendAmount, 1); err != nil {
		t.Fatal("SendFromMinConf failed.", err)
	}

	// Get accounts with minconf=0 post-send
	accountsBalancesMinconf0PostSend, err := wcl.ListAccountsMinConf(0)
	if err != nil {
		t.Fatal(err)
	}

	// Get balance of test account prior to a send
	acctBalancePostSend := accountsBalancesMinconf0PostSend[accountName]

	// Check if reported balances match expectations
	if sendAmount != (acctBalancePostSend - acctBalancePreSend) {
		t.Fatalf("Test account balance not changed by expected amount after send: "+
			"%v -%v != %v", acctBalancePostSend, acctBalancePreSend, sendAmount)
	}

	// Verify minconf>0 works: list, mine, list

	// List BEFORE mining a block
	accountsBalancesMinconf1PostSend, err := wcl.ListAccountsMinConf(1)
	if err != nil {
		t.Fatal(err)
	}

	// Get balance of test account prior to a send
	acctBalanceMin1PostSend := accountsBalancesMinconf1PostSend[accountName]

	// Mine 2 new blocks to validate tx
	newBestBlock(r, t)
	newBestBlock(r, t)

	// List AFTER mining a block
	accountsBalancesMinconf1PostMine, err := wcl.ListAccountsMinConf(1)
	if err != nil {
		t.Fatal(err)
	}

	// Get balance of test account prior to a send
	acctBalanceMin1PostMine := accountsBalancesMinconf1PostMine[accountName]

	// Check if reported balances match expectations
	if sendAmount != (acctBalanceMin1PostMine - acctBalanceMin1PostSend) {
		t.Fatalf("Test account balance (minconf=1) not changed by expected "+
			"amount after new block: %v - %v != %v", acctBalanceMin1PostMine,
			acctBalanceMin1PostSend, sendAmount)
	}

	// Note that ListAccounts uses Store.balanceFullScan to handle a UTXO scan
	// for each specific account. We can compare against GetBalanceMinConfType.
	// Also, I think there is the same bug that allows negative minconf values,
	// but does not handle unconfirmed outputs the same way as minconf=0.

	GetBalancePostSend, err := wcl.GetBalanceMinConf(accountName, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Note that BFBalanceSpendable is used with GetBalanceMinConf (not Type),
	// which uses BFBalanceFullScan when a single account is specified.
	// Recall thet fullscan is used by listaccounts.

	if GetBalancePostSend.Balances[0].Spendable != acctBalancePostSend.ToCoin() {
		t.Fatalf("Balance for default account from GetBalanceMinConf does not "+
			"match balance from ListAccounts: %v != %v", GetBalancePostSend,
			acctBalancePostSend)
	}

	// Mine 2 blocks to validate the tx and clean up UTXO set
	newBestBlock(r, t)
	newBestBlock(r, t)
}
