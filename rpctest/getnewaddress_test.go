// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"testing"

	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrd/rpcclient"
)

func TestGetNewAddress(t *testing.T) {
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

	// Get a new address from "default" account
	addr, err := wcl.GetNewAddress("default")
	if err != nil {
		t.Fatal(err)
	}

	// Verify that address is for current network
	if !addr.IsForNet(r.ActiveNet()) {
		t.Fatalf("Address not for active network (%s)", r.ActiveNet().Name)
	}

	// ValidateAddress
	validRes, err := wcl.ValidateAddress(addr)
	if err != nil {
		t.Fatalf("Unable to validate address %s: %v", addr, err)
	}
	if !validRes.IsValid {
		t.Fatalf("Address not valid: %s", addr)
	}

	// Create new account
	accountName := "newAddressTest"
	err = r.WalletRPCClient().CreateNewAccount(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// Get a new address from new "newAddressTest" account
	addrA, err := r.WalletRPCClient().GetNewAddress(accountName)
	if err != nil {
		t.Fatal(err)
	}

	// Verify that address is for current network
	if !addrA.IsForNet(r.ActiveNet()) {
		t.Fatalf("Address not for active network (%s)", r.ActiveNet().Name)
	}

	validRes, err = wcl.ValidateAddress(addrA)
	if err != nil {
		t.Fatalf("Unable to validate address %s: %v", addrA, err)
	}
	if !validRes.IsValid {
		t.Fatalf("Address not valid: %s", addr)
	}

	// respect DefaultGapLimit
	for i := 0; i < wallet.DefaultGapLimit-1; i++ {
		addr, err = wcl.GetNewAddress("default")
		if err != nil {
			t.Fatal(err)
		}

		validRes, err = wcl.ValidateAddress(addr)
		if err != nil {
			t.Fatalf(
				"Unable to validate address %s: %v",
				addr,
				err,
			)
		}
		if !validRes.IsValid {
			t.Fatalf("Address not valid: %s", addr)
		}
	}

	// Expecting error:
	// "policy violation: generating next address violates
	// the unused address gap limit policy"
	addr, err = wcl.GetNewAddress("default")
	if err == nil {
		t.Fatalf(
			"Should report gap policy violation (%d)",
			wallet.DefaultGapLimit,
		)
	}

	// gap policy with wrapping
	// reuse each address numOfReusages times
	numOfReusages := 3
	addrCounter := make(map[string]int)
	for i := 0; i < wallet.DefaultGapLimit*numOfReusages; i++ {
		addr, err = wcl.GetNewAddressGapPolicy(
			"default", rpcclient.GapPolicyWrap)

		// count address
		num := addrCounter[addr.String()]
		num++
		addrCounter[addr.String()] = num

		if err != nil {
			t.Fatal(err)
		}

		validRes, err = wcl.ValidateAddress(addr)
		if err != nil {
			t.Fatalf(
				"Unable to validate address %s: %v",
				addr,
				err,
			)
		}
		if !validRes.IsValid {
			t.Fatalf("Address not valid: %s", addr)
		}
	}

	// check reusages
	for _, reused := range addrCounter {
		if reused != numOfReusages {
			t.Fatalf(
				"Each address is expected to be reused: %d times, actual %d",
				numOfReusages,
				reused,
			)
		}
	}

	// ignore gap policy
	for i := 0; i < wallet.DefaultGapLimit*2; i++ {
		addr, err = wcl.GetNewAddressGapPolicy(
			"default", rpcclient.GapPolicyIgnore)
		if err != nil {
			t.Fatal(err)
		}

		validRes, err = wcl.ValidateAddress(addr)
		if err != nil {
			t.Fatalf(
				"Unable to validate address %s: %v",
				addr,
				err,
			)
		}
		if !validRes.IsValid {
			t.Fatalf("Address not valid: %s", addr)
		}
	}

}
