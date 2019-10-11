// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// testContext is used to store context information about a running test which
// is passed into helper functions.
type testContext struct {
	t            *testing.T
	db           walletdb.DB
	manager      *Manager
	account      uint32
	create       bool
	unlocked     bool
	watchingOnly bool
}

// expectedAddr is used to house the expected return values from a managed
// address.  Not all fields for used for all managed address types.
type expectedAddr struct {
	address     string
	addressHash []byte
	internal    bool
	compressed  bool
	imported    bool
	pubKey      []byte
	privKey     []byte
	privKeyWIF  string
	script      []byte
}

// testNamePrefix is a helper to return a prefix to show for test errors based
// on the state of the test context.
func testNamePrefix(tc *testContext) string {
	prefix := "Open "
	if tc.create {
		prefix = "Create "
	}

	return prefix + fmt.Sprintf("account #%d", tc.account)
}

// testManagedPubKeyAddress ensures the data returned by exported functions
// provided by the passed managed public key address matches the corresponding
// fields in the provided expected address.
func testManagedPubKeyAddress(tc *testContext, prefix string, gotAddr ManagedPubKeyAddress, wantAddr *expectedAddr) bool {
	// Ensure pubkey is the expected value for the managed address.
	var gpubBytes []byte
	if gotAddr.Compressed() {
		gpubBytes = gotAddr.PubKey().SerializeCompressed()
	} else {
		gpubBytes = gotAddr.PubKey().SerializeUncompressed()
	}

	if !bytes.Equal(gpubBytes, wantAddr.pubKey) {
		tc.t.Errorf("%s PubKey: unexpected public key - got %x, want "+
			"%x", prefix, gpubBytes, wantAddr.pubKey)
		return false
	}

	// Ensure exported pubkey string is the expected value for the managed
	// address.
	gpubHex := gotAddr.ExportPubKey()
	wantPubHex := hex.EncodeToString(wantAddr.pubKey)
	if gpubHex != wantPubHex {
		tc.t.Errorf("%s ExportPubKey: unexpected public key - got %s, "+
			"want %s", prefix, gpubHex, wantPubHex)
		return false
	}

	return true
}

// testManagedScriptAddress ensures the data returned by exported functions
// provided by the passed managed script address matches the corresponding
// fields in the provided expected address.
func testManagedScriptAddress(tc *testContext, prefix string, gotAddr ManagedScriptAddress, wantAddr *expectedAddr) bool {
	if !bytes.Equal(gotAddr.Address().ScriptAddress(),
		dcrutil.Hash160(wantAddr.script)) {
		tc.t.Errorf("%s Script: unexpected script  - got %x, want "+
			"%x", prefix, gotAddr.Address().ScriptAddress(),
			dcrutil.Hash160(wantAddr.script))
		return false
	}

	return true
}

// testAddress ensures the data returned by all exported functions provided by
// the passed managed address matches the corresponding fields in the provided
// expected address.  It also type asserts the managed address to determine its
// specific type and calls the corresponding testing functions accordingly.
func testAddress(tc *testContext, prefix string, gotAddr ManagedAddress, wantAddr *expectedAddr) bool {
	if gotAddr.Account() != tc.account {
		tc.t.Errorf("ManagedAddress.Account: unexpected account - got "+
			"%d, want %d", gotAddr.Account(), tc.account)
		return false
	}

	if gotAddr.Address().Address() != wantAddr.address {
		tc.t.Errorf("%s EncodeAddress: unexpected address - got %s, "+
			"want %s", prefix, gotAddr.Address().Address(),
			wantAddr.address)
		return false
	}

	if !bytes.Equal(gotAddr.AddrHash(), wantAddr.addressHash) {
		tc.t.Errorf("%s AddrHash: unexpected address hash - got %x, "+
			"want %x", prefix, gotAddr.AddrHash(), wantAddr.addressHash)
		return false
	}

	if gotAddr.Internal() != wantAddr.internal {
		tc.t.Errorf("%s Internal: unexpected internal flag - got %v, "+
			"want %v", prefix, gotAddr.Internal(), wantAddr.internal)
		return false
	}

	if gotAddr.Compressed() != wantAddr.compressed {
		tc.t.Errorf("%s Compressed: unexpected compressed flag - got "+
			"%v, want %v", prefix, gotAddr.Compressed(),
			wantAddr.compressed)
		return false
	}

	if gotAddr.Imported() != wantAddr.imported {
		tc.t.Errorf("%s Imported: unexpected imported flag - got %v, "+
			"want %v", prefix, gotAddr.Imported(), wantAddr.imported)
		return false
	}

	switch addr := gotAddr.(type) {
	case ManagedPubKeyAddress:
		if !testManagedPubKeyAddress(tc, prefix, addr, wantAddr) {
			return false
		}

	case ManagedScriptAddress:
		if !testManagedScriptAddress(tc, prefix, addr, wantAddr) {
			return false
		}
	}

	return true
}

// testLocking tests the basic locking semantics of the address manager work
// as expected.  Other tests ensure addresses behave as expected under locked
// and unlocked conditions.
func testLocking(tc *testContext, rb walletdb.ReadBucket) {
	if tc.unlocked {
		tc.t.Fatal("testLocking called with an unlocked manager")
	}
	if !tc.manager.IsLocked() {
		tc.t.Fatal("IsLocked: returned false on locked manager")
	}

	// Locking an already lock manager should return an error.  The error
	// should be ErrLocked or ErrWatchingOnly depending on the type of the
	// address manager.
	err := tc.manager.Lock()
	wantErrCode := errors.Locked
	if tc.watchingOnly {
		wantErrCode = errors.WatchingOnly
	}
	if !errors.Is(err, wantErrCode) {
		tc.t.Fatalf("Lock: unexpected error: %v", err)
	}

	// Ensure unlocking with the correct passphrase doesn't return any
	// unexpected errors and the manager properly reports it is unlocked.
	// Since watching-only address managers can't be unlocked, also ensure
	// the correct error for that case.
	err = tc.manager.Unlock(rb, privPassphrase)
	if tc.watchingOnly {
		if !errors.Is(err, errors.WatchingOnly) {
			tc.t.Fatalf("Unlock: unexpected error: %v", err)
		}
	} else if err != nil {
		tc.t.Fatalf("Unlock: unexpected error: %v", err)
	}
	if !tc.watchingOnly && tc.manager.IsLocked() {
		tc.t.Fatal("IsLocked: returned true on unlocked manager")
	}

	// Unlocking the manager again is allowed.  Since watching-only address
	// managers can't be unlocked, also ensure the correct error for that
	// case.
	err = tc.manager.Unlock(rb, privPassphrase)
	if tc.watchingOnly {
		if !errors.Is(err, errors.WatchingOnly) {
			tc.t.Fatalf("Unlock: unexpected error: %v", err)
		}
	} else if err != nil {
		tc.t.Fatalf("Unlock: unexpected error: %v", err)
	}
	if !tc.watchingOnly && tc.manager.IsLocked() {
		tc.t.Fatal("IsLocked: returned true on unlocked manager")
	}

	// Unlocking the manager with an invalid passphrase must result in an
	// error and a locked manager.
	err = tc.manager.Unlock(rb, []byte("invalidpassphrase"))
	wantErrCode = errors.Passphrase
	if tc.watchingOnly {
		wantErrCode = errors.WatchingOnly
	}
	if !errors.Is(err, wantErrCode) {
		tc.t.Fatalf("Unlock: unexpected error: %v", err)
	}
	if !tc.manager.IsLocked() {
		tc.t.Fatal("IsLocked: manager is unlocked after failed unlock attempt")
	}
}

// testImportPrivateKey tests that importing private keys works properly.  It
// ensures they can be retrieved by Address after they have been imported and
// the addresses give the expected values when the manager is locked and
// unlocked.
//
// This function expects the manager is already locked when called and returns
// with the manager locked.
func testImportPrivateKey(tc *testContext, ns walletdb.ReadWriteBucket) {
	if !tc.create {
		return
	}

	// The manager cannot be unlocked to import a private key in watching-only
	// mode.
	if tc.watchingOnly {
		return
	}

	tests := []struct {
		name     string
		in       string
		expected expectedAddr
	}{
		{
			name: "wif for compressed pubkey address",
			in:   "PtWUqkS3apLoZUevFtG3Bwt6uyX8LQfYttycGkt2XCzgxquPATQgG",
			expected: expectedAddr{
				address:     "TsSYVKf24LcrxyWHBqj4oBcU542PcjH1iA2",
				addressHash: hexToBytes("10b601a41d2320527c95eb4cdae2c75b45ae45e1"),
				internal:    false,
				imported:    true,
				compressed:  true,
				pubKey:      hexToBytes("03df8852b90ce8da7de6bcbacd26b78534ad9e46dc1b62a01dcf43f5837d7f9f5e"),
				privKey:     hexToBytes("ac4cb1a53c4f04a71fffbff26d4500c8a95443936deefd1b6ed89727a6858e08"),
				// privKeyWIF is set to the in field during tests
			},
		},
	}

	if err := tc.manager.Unlock(ns, privPassphrase); err != nil {
		tc.t.Fatalf("Unlock: unexpected error: %v", err)
	}
	tc.unlocked = true

	// Only import the private keys when in the create phase of testing.
	tc.account = ImportedAddrAccount
	prefix := testNamePrefix(tc) + " testImportPrivateKey"
	chainParams := tc.manager.ChainParams()
	for i, test := range tests {
		test.expected.privKeyWIF = test.in
		wif, err := dcrutil.DecodeWIF(test.in, chainParams.PrivateKeyID)
		if err != nil {
			tc.t.Errorf("%s DecodeWIF #%d (%s) (%s): unexpected "+
				"error: %v", prefix, i, test.in, test.name, err)
			continue
		}
		addr, err := tc.manager.ImportPrivateKey(ns, wif)
		if err != nil {
			tc.t.Fatalf("%s ImportPrivateKey #%d (%s): "+
				"unexpected error: %v", prefix, i,
				test.name, err)
		}
		if !testAddress(tc, prefix+" ImportPrivateKey", addr, &test.expected) {
			continue
		}
	}

	// Setup a closure to test the results since the same tests need to be
	// repeated with the manager unlocked and locked.
	testResults := func() {
		for i, test := range tests {
			test.expected.privKeyWIF = test.in

			// Use the Address API to retrieve each of the expected
			// new addresses and ensure they're accurate.
			utilAddr, err := dcrutil.NewAddressPubKeyHash(
				test.expected.addressHash, chainParams, dcrec.STEcdsaSecp256k1)
			if err != nil {
				tc.t.Fatalf("%s NewAddressPubKeyHash #%d (%s): "+
					"unexpected error: %v", prefix, i, test.name, err)
			}
			taPrefix := fmt.Sprintf("%s Address #%d (%s)", prefix,
				i, test.name)
			ma, err := tc.manager.Address(ns, utilAddr)
			if err != nil {
				tc.t.Fatalf("%s: unexpected error: %v", taPrefix, err)
				continue
			}
			if !testAddress(tc, taPrefix, ma, &test.expected) {
				tc.t.Fatalf("testAddress for %v failed", ma.Address().String())
			}
		}
	}

	// The address manager could either be locked or unlocked here depending
	// on whether or not it's a watching-only manager.  When it's unlocked,
	// this will test both the public and private address data are accurate.
	// When it's locked, it must be watching-only, so only the public
	// address  nformation is tested and the private functions are checked
	// to ensure they return the expected ErrWatchingOnly error.
	testResults()

	// Lock the manager and retest all of the addresses to ensure the
	// private information returns the expected error.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Fatalf("Lock: unexpected error: %v", err)
	}
	tc.unlocked = false

	testResults()
}

// testImportScript tests that importing scripts works properly.  It ensures
// they can be retrieved by Address after they have been imported and the
// addresses give the expected values when the manager is locked and unlocked.
//
// This function expects the manager is already locked when called and returns
// with the manager locked.
func testImportScript(tc *testContext, wb walletdb.ReadWriteBucket) {
	if !tc.create {
		return
	}

	// The manager cannot be unlocked to import a script key in watching-only
	// mode.
	if tc.watchingOnly {
		return
	}

	tests := []struct {
		name     string
		in       []byte
		expected expectedAddr
	}{
		{
			name: "p2sh multisig",
			in: hexToBytes("51210373c717acda38b5aa4c00c33932e059cdbc" +
				"11deceb5f00490a9101704cc444c5151ae"),
			expected: expectedAddr{
				address:     "TcsXPUraiDWZoeQBEbw7T7LSgrvD7dar9DA",
				addressHash: hexToBytes("db7e6d507e3e291a5ab2fac10107f4479c1f4f9c"),
				internal:    false,
				imported:    true,
				compressed:  false,
				// script is set to the in field during tests.
			},
		},
	}

	rb := wb.(walletdb.ReadBucket)

	if err := tc.manager.Unlock(rb, privPassphrase); err != nil {
		tc.t.Fatalf("Unlock: unexpected error: %v", err)
	}
	tc.unlocked = true

	// Only import the scripts when in the create phase of testing.
	tc.account = ImportedAddrAccount
	prefix := testNamePrefix(tc)
	for i, test := range tests {
		test.expected.script = test.in
		prefix := fmt.Sprintf("%s ImportScript #%d (%s)", prefix,
			i, test.name)

		addr, err := tc.manager.ImportScript(wb, test.in)
		if err != nil {
			tc.t.Fatalf("%s: unexpected error: %v", prefix, err)
		}
		if !testAddress(tc, prefix, addr, &test.expected) {
			tc.t.Fatalf("%s: testAddress failed for %v", prefix,
				addr.Address().String())
		}
	}

	// Setup a closure to test the results since the same tests need to be
	// repeated with the manager unlocked and locked.
	chainParams := tc.manager.ChainParams()
	testResults := func() {
		for i, test := range tests {
			test.expected.script = test.in

			// Use the Address API to retrieve each of the expected
			// new addresses and ensure they're accurate.
			utilAddr, err := dcrutil.NewAddressScriptHash(test.in,
				chainParams)
			if err != nil {
				tc.t.Fatalf("%s NewAddressScriptHash #%d (%s): "+
					"unexpected error: %v", prefix, i, test.name, err)
			}
			taPrefix := fmt.Sprintf("%s Address #%d (%s)", prefix,
				i, test.name)
			ma, err := tc.manager.Address(rb, utilAddr)
			if err != nil {
				tc.t.Fatalf("%s: unexpected error: %v", taPrefix, err)
			}
			if !testAddress(tc, taPrefix, ma, &test.expected) {
				tc.t.Fatalf("%s: testAddress failed for %v", prefix,
					ma.Address().String())
			}
		}
	}

	// The address manager could either be locked or unlocked here depending
	// on whether or not it's a watching-only manager.  When it's unlocked,
	// this will test both the public and private address data are accurate.
	// When it's locked, it must be watching-only, so only the public
	// address information is tested and the private functions are checked
	// to ensure they return the expected ErrWatchingOnly error.
	testResults()

	// Lock the manager and retest all of the addresses to ensure the
	// private information returns the expected error.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Fatalf("Lock: unexpected error: %v", err)
	}
	tc.unlocked = false

	testResults()
}

func TestManagerImports(t *testing.T) {
	ctx := context.Background()
	db, mgr, _, _, teardown, err := cloneDB("imports.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	tc := &testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       true,
		watchingOnly: false,
	}

	testImports := func(tc *testContext) {
		err := walletdb.Update(ctx, tc.db, func(tx walletdb.ReadWriteTx) error {
			ns := tx.ReadWriteBucket(waddrmgrBucketKey)
			testImportPrivateKey(tc, ns)
			testImportScript(tc, ns)
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	testImports(tc)

	tc.create = false
	tc.watchingOnly = true

	testImports(tc)
}

// testChangePassphrase ensures changes both the public and private passphrases
// works as intended.
func testChangePassphrase(tc *testContext, wb walletdb.ReadWriteBucket) {
	// Force an error when changing the passphrase due to failure to
	// generate a new secret key by replacing the generation function one
	// that intentionally errors.
	testName := "ChangePassphrase (public) with invalid new secret key"

	var err error
	TstRunWithReplacedNewSecretKey(func() {
		err = tc.manager.ChangePassphrase(wb, pubPassphrase, pubPassphrase2, false)
	})
	if !errors.Is(err, errors.Crypto) {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}

	rb := wb.(walletdb.ReadBucket)

	// Attempt to change public passphrase with invalid old passphrase.
	testName = "ChangePassphrase (public) with invalid old passphrase"
	err = tc.manager.ChangePassphrase(wb, []byte("bogus"), pubPassphrase2, false)
	if !errors.Is(err, errors.Passphrase) {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}

	// Change the public passphrase.
	testName = "ChangePassphrase (public)"
	err = tc.manager.ChangePassphrase(wb, pubPassphrase, pubPassphrase2, false)
	if err != nil {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}

	// Ensure the public passphrase was successfully changed.
	if !tc.manager.TstCheckPublicPassphrase(pubPassphrase2) {
		tc.t.Fatalf("%s: passphrase does not match", testName)
	}

	// Change the private passphrase back to what it was.
	err = tc.manager.ChangePassphrase(wb, pubPassphrase2, pubPassphrase, false)
	if err != nil {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}

	// Attempt to change private passphrase with invalid old passphrase.
	// The error should be ErrWrongPassphrase or ErrWatchingOnly depending
	// on the type of the address manager.
	testName = "ChangePassphrase (private) with invalid old passphrase"
	err = tc.manager.ChangePassphrase(wb, []byte("bogus"), privPassphrase2, true)
	wantErrCode := errors.Passphrase
	if tc.watchingOnly {
		wantErrCode = errors.WatchingOnly
	}
	if !errors.Is(err, wantErrCode) {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}

	// Everything after this point involves testing that the private
	// passphrase for the address manager can be changed successfully.
	// This is not possible for watching-only mode, so just exit now in that
	// case.
	if tc.watchingOnly {
		return
	}

	// Change the private passphrase.
	testName = "ChangePassphrase (private)"
	err = tc.manager.ChangePassphrase(wb, privPassphrase, privPassphrase2, true)
	if err != nil {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}

	// Unlock the manager with the new passphrase to ensure it changed as
	// expected.
	if err := tc.manager.Unlock(rb, privPassphrase2); err != nil {
		tc.t.Fatalf("%s: failed to unlock with new private "+
			"passphrase: %v", testName, err)
	}
	tc.unlocked = true

	// Change the private passphrase back to what it was while the manager
	// is unlocked to ensure that path works properly as well.
	err = tc.manager.ChangePassphrase(wb, privPassphrase2, privPassphrase, true)
	if err != nil {
		tc.t.Fatalf("%s: unexpected error: %v", testName, err)
	}
	if tc.manager.IsLocked() {
		tc.t.Fatalf("%s: manager is locked", testName)
	}

	// Relock the manager for future tests.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Fatalf("Lock: unexpected error: %v", err)
	}
	tc.unlocked = false
}

// testNewAccount tests the new account creation func of the address manager works
// as expected.
func testNewAccount(tc *testContext, wb walletdb.ReadWriteBucket) {
	if !tc.create {
		return
	}

	if tc.watchingOnly {
		// Creating new accounts in watching-only mode should return ErrWatchingOnly
		_, err := tc.manager.NewAccount(wb, "test")
		if !errors.Is(err, errors.WatchingOnly) {
			tc.t.Fatalf("NewAccount: expected ErrWatchingOnly, got %v", err)
		}
	}

	// Creating new accounts when wallet is locked should return ErrLocked
	_, err := tc.manager.NewAccount(wb, "test")
	if !errors.Is(err, errors.Locked) {
		tc.t.Fatalf("NewAccount: expected ErrLocked, got %v", err)
	}

	rb := wb.(walletdb.ReadBucket)

	// Unlock the wallet to decrypt cointype keys required to derive
	// account keys
	if err := tc.manager.Unlock(rb, privPassphrase); err != nil {
		tc.t.Fatalf("Unlock: unexpected error: %v", err)
	}
	tc.unlocked = true

	testName := "acct-create"
	expectedAccount := tc.account + 1
	account, err := tc.manager.NewAccount(wb, testName)
	if err != nil {
		tc.t.Fatalf("NewAccount: unexpected error: %v", err)
	}
	if account != expectedAccount {
		tc.t.Fatalf("NewAccount account mismatch -- got %d, want %d",
			account, expectedAccount)
	}

	// Test duplicate account name error
	_, err = tc.manager.NewAccount(wb, testName)
	if !errors.Is(err, errors.Exist) {
		tc.t.Fatalf("NewAccount: expected ErrExist, got %v", err)
	}
	// Test account name validation
	testName = "" // Empty account names are not allowed
	_, err = tc.manager.NewAccount(wb, testName)
	if !errors.Is(err, errors.Invalid) {
		tc.t.Fatalf("NewAccount: expected ErrInvalid, got %v", err)
	}
	testName = "imported" // A reserved account name
	_, err = tc.manager.NewAccount(wb, testName)
	if !errors.Is(err, errors.Invalid) {
		tc.t.Fatalf("NewAccount: expected ErrInvalid, got %v", err)
	}
}

// testLookupAccount tests the basic account lookup func of the address manager
// works as expected.
func testLookupAccount(tc *testContext, rb walletdb.ReadBucket) {
	// Lookup accounts created earlier in testNewAccount
	expectedAccounts := map[string]uint32{
		TstDefaultAccountName:   DefaultAccountNum,
		ImportedAddrAccountName: ImportedAddrAccount,
	}
	for acctName, expectedAccount := range expectedAccounts {
		account, err := tc.manager.LookupAccount(rb, acctName)
		if err != nil {
			tc.t.Fatalf("LookupAccount: unexpected error: %v", err)
		}
		if account != expectedAccount {
			tc.t.Fatalf("LookupAccount account mismatch -- got %d, want %d",
				account, expectedAccount)
		}
	}
	// Test account not found error
	testName := "non existent account"
	_, err := tc.manager.LookupAccount(rb, testName)
	if !errors.Is(err, errors.NotExist) {
		tc.t.Fatalf("LookupAccount: unexpected error: %v", err)
	}

	// Test last account
	lastAccount, err := tc.manager.LastAccount(rb)
	if err != nil {
		tc.t.Fatalf("LastAccount failed: %v", err)
	}

	expectedLastAccount := uint32(1)
	if lastAccount != expectedLastAccount {
		tc.t.Fatalf("LookupAccount account mismatch -- got %d, want %d",
			lastAccount, expectedLastAccount)
	}
}

// testRenameAccount tests the rename account func of the address manager works
// as expected.
func testRenameAccount(tc *testContext, wb walletdb.ReadWriteBucket) {
	rb := wb.(walletdb.ReadBucket)
	acctName, err := tc.manager.AccountName(rb, tc.account)
	if err != nil {
		tc.t.Fatalf("AccountName: unexpected error: %v", err)
	}
	testName := acctName + "-renamed"
	err = tc.manager.RenameAccount(wb, tc.account, testName)
	if err != nil {
		tc.t.Fatalf("RenameAccount: unexpected error: %v", err)
	}
	newName, err := tc.manager.AccountName(rb, tc.account)
	if err != nil {
		tc.t.Fatalf("AccountName: unexpected error: %v", err)
	}
	if newName != testName {
		tc.t.Fatalf("RenameAccount account name mismatch -- got %s, want %s",
			newName, testName)
	}
	// Test duplicate account name error
	err = tc.manager.RenameAccount(wb, tc.account, testName)
	if !errors.Is(err, errors.Exist) {
		tc.t.Fatalf("RenameAccount: unexpected error: %v", err)
	}
	// Test old account name is no longer valid
	_, err = tc.manager.LookupAccount(wb, acctName)
	if err == nil {
		tc.t.Fatalf("LookupAccount: unexpected error: %v", err)
	}
	if !errors.Is(err, errors.NotExist) {
		tc.t.Fatalf("LookupAccount: unexpected error: %v", err)
	}
}

// testForEachAccount tests the retrieve all accounts func of the address
// manager works as expected.
func testForEachAccount(tc *testContext, rb walletdb.ReadBucket) {
	prefix := testNamePrefix(tc) + " testForEachAccount"
	expectedAccounts := []uint32{0, 1, 2147483647}

	var accounts []uint32
	err := tc.manager.ForEachAccount(rb, func(account uint32) error {
		accounts = append(accounts, account)
		return nil
	})
	if err != nil {
		tc.t.Fatalf("%s: unexpected error: %v", prefix, err)
	}
	if len(accounts) != len(expectedAccounts) {
		tc.t.Fatalf("%s: unexpected number of accounts - got "+
			"%d, want %d", prefix, len(accounts), len(expectedAccounts))
	}
	for i, account := range accounts {
		if expectedAccounts[i] != account {
			tc.t.Fatalf("%s #%d: account mismatch -- got %d, want %d,"+
				" accounts: %v", prefix, i, account, expectedAccounts[i], accounts)
		}
	}
}

// testEncryptDecryptErrors ensures that errors which occur while encrypting and
// decrypting data return the expected errors.
func testEncryptDecryptErrors(tc *testContext) {
	ctx := context.Background()
	invalidKeyType := CryptoKeyType(0xff)
	if _, err := tc.manager.Encrypt(invalidKeyType, []byte{}); err == nil {
		tc.t.Fatal("Encrypt accepted an invalid key type!")
	}

	if _, err := tc.manager.Decrypt(invalidKeyType, []byte{}); err == nil {
		tc.t.Fatal("Encrypt accepted an invalid key type!")
	}

	if !tc.manager.IsLocked() {
		tc.t.Fatal("Manager should be locked at this point.")
	}

	var err error
	// Now the mgr is locked and encrypting/decrypting with private
	// keys should fail.
	_, err = tc.manager.Encrypt(CKTPrivate, []byte{})
	if !errors.Is(err, errors.Locked) {
		tc.t.Fatal("encryption with private key should fail when manager" +
			" is locked")
	}

	_, err = tc.manager.Decrypt(CKTPrivate, []byte{})
	if !errors.Is(err, errors.Locked) {
		tc.t.Fatal("decryption with private key should fail when manager" +
			" is locked")

	}

	walletdb.Update(ctx, tc.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)
		// Unlock the manager for these tests
		if err = tc.manager.Unlock(ns, privPassphrase); err != nil {
			tc.t.Fatal("Attempted to unlock the manager, but failed:", err)
		}
		return nil
	})

	// Make sure to cover the ErrCrypto error path in Encrypt.
	TstRunWithFailingCryptoKeyPriv(tc.manager, func() {
		_, err = tc.manager.Encrypt(CKTPrivate, []byte{})
	})
	if !errors.Is(err, errors.Crypto) {
		tc.t.Fatal("failed encryption")
	}

	// Lock the manager.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Fatal("Attempted to lock the manager, but failed:", err)
	}
}

// testEncryptDecrypt ensures that encrypting and decrypting data with the
// the various crypto key types works as expected.
func testEncryptDecrypt(tc *testContext) {
	ctx := context.Background()
	plainText := []byte("this is a plaintext")

	walletdb.Update(ctx, tc.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)
		// Make sure address manager is unlocked
		if err := tc.manager.Unlock(ns, privPassphrase); err != nil {
			tc.t.Fatal("Attempted to unlock the manager, but failed:", err)
		}
		return nil
	})

	keyTypes := []CryptoKeyType{
		CKTPublic,
		CKTPrivate,
		CKTScript,
	}

	for _, keyType := range keyTypes {
		cipherText, err := tc.manager.Encrypt(keyType, plainText)
		if err != nil {
			tc.t.Fatalf("Failed to encrypt plaintext: %v", err)
		}

		decryptedCipherText, err := tc.manager.Decrypt(keyType, cipherText)
		if err != nil {
			tc.t.Fatalf("Failed to decrypt plaintext: %v", err)
		}

		if !reflect.DeepEqual(decryptedCipherText, plainText) {
			tc.t.Fatal("Got:", decryptedCipherText, ", want:", plainText)
		}
	}

	// Lock the manager.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Fatal("Attempted to lock the manager, but failed:", err)
	}
}

func TestManagerEncryptDecrypt(t *testing.T) {
	db, mgr, _, _, teardown, err := cloneDB("encrypt_decrypt.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	tc := &testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: false,
	}

	testEncryptDecryptErrors(tc)
	testEncryptDecrypt(tc)
}

func TestChangePassphrase(t *testing.T) {
	ctx := context.Background()
	db, mgr, _, _, teardown, err := cloneDB("change_passphrase.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	tc := &testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: false,
	}

	err = walletdb.Update(ctx, tc.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)
		testChangePassphrase(tc, ns)
		return nil
	})
	if err != nil {
		tc.t.Errorf("unexpected error: %v", err)
	}
}

// testManagerAPI tests the functions provided by the Manager API.
func testManagerAPI(tc *testContext) {
	ctx := context.Background()
	err := walletdb.Update(ctx, tc.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)

		testLocking(tc, ns)

		// Reset default account
		tc.account = 0
		testNewAccount(tc, ns)
		testLookupAccount(tc, ns)
		testForEachAccount(tc, ns)

		// Rename account 1 "acct-create"
		tc.account = 1
		testRenameAccount(tc, ns)

		return nil
	})
	if err != nil {
		tc.t.Errorf("unexpected error: %v", err)
	}
}

// TestManagerWatchingOnly tests various facets of a watching-only address
// manager such as running the full set of API tests against a newly converted
// copy as well as when it is opened from an existing namespace.
func TestManagerWatchingOnly(t *testing.T) {
	ctx := context.Background()
	db, mgr, _, _, teardown, err := cloneDB("mgr_watching_only.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	// Run all of the manager API tests in create mode
	testManagerAPI(&testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       true,
		watchingOnly: false,
	})
	mgr.Close()

	mgr, _, _, err = Open(ctx, db, chaincfg.TestNet3Params(), pubPassphrase)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer mgr.Close()

	err = walletdb.Update(ctx, db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)
		if err = mgr.ConvertToWatchingOnly(ns); err != nil {
			t.Fatalf("failed to convert manager to watching-only: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Run all of the manager API tests against the converted manager.
	testManagerAPI(&testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: true,
	})

	// Open the watching-only manager and run all the tests again.
	mgr, _, _, err = Open(ctx, db, chaincfg.TestNet3Params(), pubPassphrase)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer mgr.Close()

	testManagerAPI(&testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: true,
	})
}

// TestManager performs a full suite of tests against the address manager API in
// create mode. It makes use of a test context because the address manager is
// persistent and much of the testing involves having specific state.
func TestManager(t *testing.T) {
	if testing.Short() {
		t.Skip("short: skipping TestManager")
	}

	ctx := context.Background()
	db, mgr, _, _, teardown, err := cloneDB("mgr_watching_only.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	testManagerAPI(&testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       true,
		watchingOnly: false,
	})
	mgr.Close()

	// Open the manager and run all the tests again in open mode which
	// avoids reinserting new addresses like the create mode tests do.
	mgr, _, _, err = Open(ctx, db, chaincfg.TestNet3Params(), pubPassphrase)
	if err != nil {
		t.Fatalf("Open: unexpected error: %v", err)
	}
	defer mgr.Close()

	tc := &testContext{
		t:            t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: false,
	}
	testManagerAPI(tc)
}

func TestMain(m *testing.M) {
	testDir, err := ioutil.TempDir("", "udb-")
	if err != nil {
		fmt.Printf("Unable to create temp directory: %v", err)
		os.Exit(1)
	}

	emptyDbPath = filepath.Join(testDir, "empty.kv")
	teardown := func() {
		os.RemoveAll(testDir)
	}

	err = createEmptyDB()
	if err != nil {
		fmt.Printf("Unable to create empty test db: %v", err)
		teardown()
		os.Exit(1)
	}

	exitCode := m.Run()
	teardown()
	os.Exit(exitCode)
}
