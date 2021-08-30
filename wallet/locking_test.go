// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"testing"
	"time"

	"decred.org/dcrwallet/v2/errors"
)

var testPrivPass = []byte("private")

func TestLocking(t *testing.T) {
	w, teardown := testWallet(t, &basicWalletConfig)
	defer teardown()

	var tests = []func(t *testing.T, w *Wallet){
		testUnlock,
		testLockOnBadPassphrase,
		testNoNilTimeoutReplacement,
		testNonNilTimeoutLock,
		testTimeoutReplacement,
	}
	for _, test := range tests {
		test(t, w)
		w.Lock()
	}
}

func testUnlock(t *testing.T, w *Wallet) {
	ctx := context.Background()
	if !w.Locked() {
		t.Fatal("expected wallet to be locked")
	}
	// Unlock without timeout
	err := w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	if w.Locked() {
		t.Fatal("expected wallet to be unlocked")
	}
	completedLock := make(chan struct{})
	go func() {
		w.Lock()
		completedLock <- struct{}{}
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-completedLock:
	default:
		t.Fatal("expected wallet to lock")
	}
	if !w.Locked() {
		t.Fatal("expected wallet to be locked")
	}
}

func testLockOnBadPassphrase(t *testing.T, w *Wallet) {
	ctx := context.Background()
	err := w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	err = w.Unlock(ctx, []byte("incorrect"), nil)
	if !errors.Is(err, errors.Passphrase) {
		t.Fatal("expected Passphrase error on bad Unlock")
	}
	if !w.Locked() {
		t.Fatal("expected wallet to be locked after failed Unlock")
	}

	err = w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	err = w.Unlock(ctx, []byte("incorrect"), nil)
	if !errors.Is(err, errors.Passphrase) {
		t.Fatal("expected Passphrase error on bad Unlock")
	}
	if !w.Locked() {
		t.Fatal("expected wallet to lock after unlocking with bad passphrase")
	}
}

// Test:
// If the wallet is currently unlocked without any timeout, timeout is ignored
// and if non-nil, is read in a background goroutine to avoid blocking sends.
func testNoNilTimeoutReplacement(t *testing.T, w *Wallet) {
	ctx := context.Background()
	err := w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	timeChan := make(chan time.Time)
	err = w.Unlock(ctx, testPrivPass, timeChan)
	if err != nil {
		t.Fatal("failed to unlock wallet with time channel")
	}
	select {
	case timeChan <- time.Time{}:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("time channel was not read in 100ms")
	}
	if w.Locked() {
		t.Fatal("expected wallet to remain unlocked due to previous unlock without timeout")
	}
}

// Test:
// If the wallet is locked and a non-nil timeout is provided, the wallet will be
// locked in the background after reading from the channel.
func testNonNilTimeoutLock(t *testing.T, w *Wallet) {
	timeChan := make(chan time.Time)
	ctx := context.Background()
	err := w.Unlock(ctx, testPrivPass, timeChan)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	timeChan <- time.Time{}
	time.Sleep(100 * time.Millisecond) // Allow time for lock in background
	if !w.Locked() {
		t.Fatal("wallet should have locked after timeout")
	}
}

// Test:
// If the wallet is already unlocked with a previous timeout, the new timeout
// replaces the prior.
func testTimeoutReplacement(t *testing.T, w *Wallet) {
	timeChan1 := make(chan time.Time)
	timeChan2 := make(chan time.Time)
	ctx := context.Background()
	err := w.Unlock(ctx, testPrivPass, timeChan1)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	err = w.Unlock(ctx, testPrivPass, timeChan2)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	timeChan2 <- time.Time{}
	time.Sleep(100 * time.Millisecond) // Allow time for lock in background
	if !w.Locked() {
		t.Fatal("wallet did not lock using replacement timeout")
	}
	select {
	case timeChan1 <- time.Time{}:
	default:
		t.Fatal("previous timeout was not read in background")
	}
}
