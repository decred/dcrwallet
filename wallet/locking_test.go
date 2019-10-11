// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/decred/dcrwallet/errors/v2"
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
		testHoldDoesNotBlockSuccessfulUnlock,
		testHoldBlocksBadUnlock,
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
	_, err := w.holdUnlock()
	if err == nil {
		t.Fatal("expected error when holding unlocked state when locked")
	}
	// Unlock without timeout
	err = w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	if w.Locked() {
		t.Fatal("expected wallet to be unlocked")
	}
	hold1, err := w.holdUnlock()
	if err != nil {
		t.Fatal("expected to hold unlock")
	}
	hold2, err := w.holdUnlock()
	if err != nil {
		t.Fatal("expected to hold unlock")
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
		t.Fatal("locked with held unlock")
	default:
	}
	if w.Locked() {
		t.Fatal("expected wallet to be unlocked")
	}
	hold1.release()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-completedLock:
		t.Fatal("locked with held unlock")
	default:
	}
	if w.Locked() {
		t.Fatal("expected wallet to be unlocked")
	}
	hold2.release()
	select {
	case <-completedLock:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("didn't lock after final released held unlock")
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
	hold, err := w.holdUnlock()
	if err != nil {
		t.Fatal("expected to hold unlock")
	}
	c := make(chan error)
	go func() {
		c <- w.Unlock(ctx, []byte("incorrect"), nil)
	}()
	runtime.Gosched()
	select {
	case <-c:
		t.Fatal("failed Unlock should not return during hold")
	default:
	}
	if w.Locked() {
		t.Fatal("expected wallet to still be unlocked during hold")
	}
	hold.release()
	err = <-c
	if !errors.Is(err, errors.Passphrase) {
		t.Fatal("expected Passphrase error on bad Unlock")
	}
	if !w.Locked() {
		t.Fatal("expected wallet to lock after releasing hold")
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

// Test:
// If the wallet is unlocked and the unlocked state is held by some routine,
// Unlock with the correct passphrase is not blocked by any mutex.
func testHoldDoesNotBlockSuccessfulUnlock(t *testing.T, w *Wallet) {
	ctx := context.Background()
	err := w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	hold, err := w.holdUnlock()
	if err != nil {
		t.Fatal("expected to hold unlock")
	}
	defer hold.release()
	err = w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("expected Unlock to return without error")
	}
	err = w.Unlock(ctx, testPrivPass, make(chan time.Time, 1))
	if err != nil {
		t.Fatal("expected Unlock to return without error")
	}
}

// Test:
// If the wallet is unlocked and the unlocked state is held by some
// routine, Unlock with an incorrect passphase will block until the hold is
// released, and return with the wallet in a locked state.
func testHoldBlocksBadUnlock(t *testing.T, w *Wallet) {
	ctx := context.Background()
	err := w.Unlock(ctx, testPrivPass, nil)
	if err != nil {
		t.Fatal("failed to unlock wallet")
	}
	hold, err := w.holdUnlock()
	if err != nil {
		t.Fatal("expected to hold unlock")
	}
	c := make(chan error)
	go func() {
		c <- w.Unlock(ctx, []byte("incorrect"), nil)
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-c:
		t.Fatal("failing Unlock returned during hold")
	default:
	}
	hold.release()
	err = <-c
	if !errors.Is(err, errors.Passphrase) {
		t.Fatal("expected Unlock to return with Passphase error")
	}
	if !w.Locked() {
		t.Fatal("wallet wasn't locked after failed Unlock")
	}
}
