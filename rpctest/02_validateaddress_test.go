// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
)

func TestValidateAddress(t *testing.T) {
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

	accounts := []string{"default", "testValidateAddress"}

	for _, acct := range accounts {
		// Create a non-default account
		if strings.Compare("default", acct) != 0 &&
			strings.Compare("imported", acct) != 0 {
			err := r.WalletRPCClient().CreateNewAccount(acct)
			if err != nil {
				t.Fatalf("Unable to create account %s: %v", acct, err)
			}
		}

		// Get a new address from current account
		addr, err := wcl.GetNewAddress(acct)
		if err != nil {
			t.Fatal(err)
		}

		// Verify that address is for current network
		if !addr.IsForNet(r.ActiveNet()) {
			t.Fatalf("Address not for active network (%s)", r.ActiveNet().Name)
		}

		// ValidateAddress
		addrStr := addr.String()
		validRes, err := wcl.ValidateAddress(addr)
		if err != nil {
			t.Fatalf("Unable to validate address %s: %v", addrStr, err)
		}
		if !validRes.IsValid {
			t.Fatalf("Address not valid: %s", addrStr)
		}
		if !validRes.IsMine {
			t.Fatalf("Address incorrectly identified as NOT mine: %s", addrStr)
		}
		if validRes.IsScript {
			t.Fatalf("Address incorrectly identified as script: %s", addrStr)
		}

		// Address is "mine", so we can check account
		if strings.Compare(acct, validRes.Account) != 0 {
			t.Fatalf("Address %s reported as not from \"%s\" account",
				addrStr, acct)
		}

		// Decode address
		_, err = dcrutil.DecodeAddress(addrStr)
		if err != nil {
			t.Fatalf("Unable to decode address %s: %v", addr.String(), err)
		}

		// Try to validate an address that is not owned by WalletServer
		otherAddress, err := dcrutil.DecodeAddress("SsqvxBX8MZC5iiKCgBscwt69jg4u4hHhDKU")
		if err != nil {
			t.Fatalf("Unable to decode address %v: %v", otherAddress, err)
		}
		validRes, err = wcl.ValidateAddress(otherAddress)
		if err != nil {
			t.Fatalf("Unable to validate address %s with secondary WalletServer: %v",
				addrStr, err)
		}
		if !validRes.IsValid {
			t.Fatalf("Address not valid: %s", addrStr)
		}
		if validRes.IsMine {
			t.Fatalf("Address incorrectly identified as mine: %s", addrStr)
		}
		if validRes.IsScript {
			t.Fatalf("Address incorrectly identified as script: %s", addrStr)
		}

	}

	// Validate simnet dev subsidy address
	devSubPkScript := chaincfg.SimNetParams.OrganizationPkScript // "ScuQxvveKGfpG1ypt6u27F99Anf7EW3cqhq"
	devSubPkScrVer := chaincfg.SimNetParams.OrganizationPkScriptVersion
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(
		devSubPkScrVer, devSubPkScript, r.ActiveNet())
	if err != nil {
		t.Fatal("Failed to extract addresses from PkScript:", err)
	}
	devSubAddrStr := addrs[0].String()

	DevAddr, err := dcrutil.DecodeAddress(devSubAddrStr)
	if err != nil {
		t.Fatalf("Unable to decode address %s: %v", devSubAddrStr, err)
	}

	validRes, err := wcl.ValidateAddress(DevAddr)
	if err != nil {
		t.Fatalf("Unable to validate address %s: ", devSubAddrStr)
	}
	if !validRes.IsValid {
		t.Fatalf("Address not valid: %s", devSubAddrStr)
	}
	if validRes.IsMine {
		t.Fatalf("Address incorrectly identified as mine: %s", devSubAddrStr)
	}
	// for ismine==false, nothing else to test

}
