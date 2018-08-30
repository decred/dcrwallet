// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrjson"
)

func TestWalletPassphrase(t *testing.T) {
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

	// Remember to leave the WalletServer unlocked for any subsequent tests
	defaultWalletPassphrase := "password"
	defer func() {
		if err := wcl.WalletPassphrase(defaultWalletPassphrase, 0); err != nil {
			t.Fatal("Unable to unlock WalletServer:", err)
		}
	}()

	// Lock the WalletServer since test WalletServer is unlocked by default
	err := wcl.WalletLock()
	if err != nil {
		t.Fatal("Unable to lock WalletServer.")
	}

	// Check that WalletServer is locked
	walletInfo, err := wcl.WalletInfo()
	if err != nil {
		t.Fatal("walletinfo failed.")
	}
	if walletInfo.Unlocked {
		t.Fatal("WalletLock failed to lock the WalletServer")
	}

	// Try incorrect password
	err = wcl.WalletPassphrase("Wrong Password", 0)
	// Check for "-14: invalid passphrase for master private key"
	if err != nil && err.(*dcrjson.RPCError).Code !=
		dcrjson.ErrRPCWalletPassphraseIncorrect {
		// dcrjson.ErrWalletPassphraseIncorrect.Code
		t.Fatalf("WalletPassphrase with INCORRECT passphrase exited with: %v",
			err)
	}

	// Check that WalletServer is still locked
	walletInfo, err = wcl.WalletInfo()
	if err != nil {
		t.Fatal("walletinfo failed.")
	}
	if walletInfo.Unlocked {
		t.Fatal("WalletPassphrase unlocked the WalletServer with the wrong passphrase")
	}

	// Verify that a restricted operation like createnewaccount fails
	accountName := "cannotCreateThisAccount"
	err = wcl.CreateNewAccount(accountName)
	if err == nil {
		t.Fatal("createnewaccount succeeded on a locked WalletServer.")
	}
	// dcrjson.ErrRPCWalletUnlockNeeded
	if !strings.HasPrefix(err.Error(),
		strconv.Itoa(int(dcrjson.ErrRPCWalletUnlockNeeded))) {
		t.Fatalf("createnewaccount returned error (%v) instead of %v",
			err, dcrjson.ErrRPCWalletUnlockNeeded)
	}

	// Unlock with correct passphrase
	err = wcl.WalletPassphrase(defaultWalletPassphrase, 0)
	if err != nil {
		t.Fatalf("WalletPassphrase failed: %v", err)
	}

	// Check that WalletServer is now unlocked
	walletInfo, err = wcl.WalletInfo()
	if err != nil {
		t.Fatal("walletinfo failed.")
	}
	if !walletInfo.Unlocked {
		t.Fatal("WalletPassphrase failed to unlock the WalletServer with the correct passphrase")
	}

	// Check for ErrRPCWalletAlreadyUnlocked
	err = wcl.WalletPassphrase(defaultWalletPassphrase, 0)
	// Check for "-17: Wallet is already unlocked"
	if err != nil && err.(*dcrjson.RPCError).Code !=
		dcrjson.ErrRPCWalletAlreadyUnlocked {
		t.Fatalf("WalletPassphrase failed: %v", err)
	}

	// Re-lock WalletServer
	err = wcl.WalletLock()
	if err != nil {
		t.Fatal("Unable to lock WalletServer.")
	}

	// Unlock with timeout
	timeOut := int64(6)
	err = wcl.WalletPassphrase(defaultWalletPassphrase, timeOut)
	if err != nil {
		t.Fatalf("WalletPassphrase failed: %v", err)
	}

	// Check that WalletServer is now unlocked
	walletInfo, err = wcl.WalletInfo()
	if err != nil {
		t.Fatal("walletinfo failed.")
	}
	if !walletInfo.Unlocked {
		t.Fatal("WalletPassphrase failed to unlock the WalletServer with the correct passphrase")
	}

	time.Sleep(time.Duration(timeOut+2) * time.Second)

	// Check that WalletServer is now locked
	walletInfo, err = wcl.WalletInfo()
	if err != nil {
		t.Fatal("walletinfo failed.")
	}
	if walletInfo.Unlocked {
		t.Fatal("Wallet still unlocked after timeout")
	}

	// TODO: Watching-only error?
}
