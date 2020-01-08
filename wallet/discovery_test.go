// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"testing"

	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// TestDiscoveryCursorPos tests that the account cursor index is not reset
// during address discovery such that an address could be reused.
func TestDiscoveryCursorPos(t *testing.T) {
	ctx := context.Background()

	cfg := basicWalletConfig
	// normally would just do the upgrade, but the buffers record
	// off-by-ones after the upgrade.  will be fixed in a later commit.
	cfg.DisableCoinTypeUpgrades = true

	w, teardown := testWallet(t, &cfg)
	defer teardown()

	/*
		// Upgrade the cointype before proceeding.  The test is invalid if a
		// cointype upgrade occurs during discovery.
		err := w.UpgradeToSLIP0044CoinType(ctx)
		if err != nil {
			t.Fatal(err)
		}
	*/

	// Advance the cursor within the gap limit but without recording the
	// returned addresses in the database (these may be persisted during a
	// later update).
	w.addressBuffersMu.Lock()
	xpub := w.addressBuffers[0].albExternal.branchXpub
	w.addressBuffers[0].albExternal.cursor = 9 // 0-9 have been returned
	w.addressBuffersMu.Unlock()

	// Perform address discovery
	// All peer funcs may be left unimplemented; wallet only records the genesis block.
	peer := &peerFuncs{}
	err := w.DiscoverActiveAddresses(ctx, peer, &w.chainParams.GenesisHash, false)
	if err != nil {
		t.Fatal(err)
	}

	w.addressBuffersMu.Lock()
	lastUsed := w.addressBuffers[0].albExternal.lastUsed
	cursor := w.addressBuffers[0].albExternal.cursor
	w.addressBuffersMu.Unlock()
	wasLastUsed := ^uint32(0)
	wasCursor := uint32(9)
	if lastUsed != wasLastUsed || cursor != wasCursor {
		t.Errorf("cursor was reset: lastUsed=%d (want %d) cursor=%d (want %d)",
			lastUsed, wasLastUsed, cursor, wasCursor)
	}

	// Manually mark an address between the lastUsed and cursor as used, and
	// addresses through the cursor as returned, then perform discovery
	// again.  The cursor should be reduced such that the next returned
	// address would be the same as before, without introducing a backwards
	// reset or wasted addresses.
	addr4, err := deriveChildAddress(xpub, 4, w.chainParams)
	if err != nil {
		t.Fatal(err)
	}
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
		err = w.Manager.MarkReturnedChildIndex(dbtx, 0, 0, 9) // 0-9 have been returned
		if err != nil {
			return err
		}
		maddr4, err := w.Manager.Address(ns, addr4)
		if err != nil {
			return err
		}
		return w.markUsedAddress("", dbtx, maddr4)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = w.DiscoverActiveAddresses(ctx, peer, &w.chainParams.GenesisHash, false)
	if err != nil {
		t.Fatal(err)
	}

	w.addressBuffersMu.Lock()
	lastUsed = w.addressBuffers[0].albExternal.lastUsed
	cursor = w.addressBuffers[0].albExternal.cursor
	w.addressBuffersMu.Unlock()
	wasLastUsed += 5
	wasCursor -= 5
	if lastUsed != wasLastUsed || cursor != wasCursor {
		t.Errorf("cursor was reset: lastUsed=%d (want %d) cursor=%d (want %d)",
			lastUsed, wasLastUsed, cursor, wasCursor)
	}
}
