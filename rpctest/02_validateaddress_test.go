// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrd/chaincfg"
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
	//-----------------------------------------
	newAccountName := "testValidateAddress"
	// Create a non-default account
	err := r.WalletRPCClient().CreateNewAccount(newAccountName)
	if err != nil {
		t.Fatalf("Unable to create account %s: %v", newAccountName, err)
	}
	accounts := []string{"default", newAccountName}
	//-----------------------------------------
	addrStr := "SsqvxBX8MZC5iiKCgBscwt69jg4u4hHhDKU"
	// Try to validate an address that is not owned by WalletServer
	otherAddress, err := dcrutil.DecodeAddress(addrStr)
	if err != nil {
		t.Fatalf("Unable to decode address %v: %v", otherAddress, err)
	}
	validRes, err := wcl.ValidateAddress(otherAddress)
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
	//-----------------------------------------
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

	validRes, err = wcl.ValidateAddress(DevAddr)
	if err != nil {
		t.Fatalf("Unable to validate address %s: ", devSubAddrStr)
	}
	if !validRes.IsValid {
		t.Fatalf("Address not valid: %s", devSubAddrStr)
	}
	if validRes.IsMine {
		t.Fatalf("Address incorrectly identified as mine: %s", devSubAddrStr)
	}
	//-----------------------------------------
	for _, acct := range accounts {
		// let's overflow DefaultGapLimit
		for i := 0; i < wallet.DefaultGapLimit+5; i++ {
			// Get a new address from current account
			addr, err := wcl.GetNewAddressGapPolicy(
				acct, rpcclient.GapPolicyIgnore)
			if err != nil {
				t.Fatal(err)
			}
			// Verify that address is for current network
			if !addr.IsForNet(r.ActiveNet()) {
				t.Fatalf(
					"Address[%d] not for active network (%s), <%s>",
					i,
					r.ActiveNet().Name,
					acct,
				)
			}
			// ValidateAddress
			addrStr := addr.String()
			validRes, err := wcl.ValidateAddress(addr)
			if err != nil {
				t.Fatalf(
					"Unable to validate address[%d] %s: %v for <%s>",
					i,
					addrStr,
					err,
					acct,
				)
			}
			if !validRes.IsValid {
				t.Fatalf(
					"Address[%d] not valid: %s for <%s>",
					i,
					addrStr,
					acct,
				)
			}
			if !validRes.IsMine {
				t.Fatalf(
					"Address[%d] incorrectly identified as NOT mine: %s for <%s>",
					i,
					addrStr,
					acct,
				)
			}
			if validRes.IsScript {
				t.Fatalf(
					"Address[%d] incorrectly identified as script: %s for <%s>",
					i,
					addrStr,
					acct,
				)
			}
			// Address is "mine", so we can check account
			if strings.Compare(acct, validRes.Account) != 0 {
				t.Fatalf("Address[%d] %s reported as not from <%s> account",
					i,
					addrStr,
					acct,
				)
			}
			// Decode address
			_, err = dcrutil.DecodeAddress(addrStr)
			if err != nil {
				t.Fatalf("Unable to decode address[%d] %s: %v for <%s>",
					i,
					addr.String(),
					err,
					acct,
				)
			}
		}

	}

}
