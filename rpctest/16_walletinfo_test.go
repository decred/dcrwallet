// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import "testing"

// testWalletInfo
func TestWalletInfo(t *testing.T) {
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

	// WalletInfo is tested exhaustively in other test, so only do some basic
	// checks here
	walletInfo, err := wcl.WalletInfo()
	if err != nil {
		t.Fatal("walletinfo failed.")
	}
	if !walletInfo.DaemonConnected {
		t.Fatal("WalletInfo indicates that daemon is not connected.")
	}
}
