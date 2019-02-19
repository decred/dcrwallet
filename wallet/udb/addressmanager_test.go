// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrwallet/errors"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/wallet/walletdb"
)

func testDB(dbName string) (walletdb.DB, *Manager, *Store, *StakeStore, func(), error) {
	_ = os.Remove(dbName)
	db, err := walletdb.Create("bdb", dbName)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	err = Initialize(db, &chaincfg.TestNet3Params, seed, pubPassphrase,
		privPassphrase)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	mgr, txStore, stkStore, err := Open(db, &chaincfg.TestNet3Params, pubPassphrase)
	if err != nil {
		return nil, nil, nil, nil, nil, fmt.Errorf("unexpected error: %v", err)
	}

	teardown := func() {
		os.Remove(dbName)
		db.Close()
	}

	return db, mgr, txStore, stkStore, teardown, err
}

// newShaHash converts the passed big-endian hex string into a wire.ShaHash.
// It only differs from the one available in wire in that it panics on an
// error since it will only (and must only) be called with hard-coded, and
// therefore known good, hashes.
func newShaHash(hexStr string) *chainhash.Hash {
	sha, err := chainhash.NewHashFromStr(hexStr)
	if err != nil {
		panic(err)
	}
	return sha
}

// testContext is used to store context information about a running test which
// is passed into helper functions.  The useSpends field indicates whether or
// not the spend data should be empty or figure it out based on the specific
// test blocks provided.  This is needed because the first loop where the blocks
// are inserted, the tests are running against the latest block and therefore
// none of the outputs can be spent yet.  However, on subsequent runs, all
// blocks have been inserted and therefore some of the transaction outputs are
// spent.
type testContext struct {
	t            *testing.T
	db           walletdb.DB
	manager      *Manager
	account      uint32
	create       bool
	unlocked     bool
	watchingOnly bool
}

// addrType is the type of address being tested
type addrType byte

const (
	addrPubKeyHash addrType = iota
	addrScriptHash
)

// expectedAddr is used to house the expected return values from a managed
// address.  Not all fields for used for all managed address types.
type expectedAddr struct {
	address     string
	addressHash []byte
	internal    bool
	compressed  bool
	used        bool
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

	if bytes.Compare(gpubBytes, wantAddr.pubKey) != 0 {
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
	if bytes.Compare(gotAddr.Address().ScriptAddress(),
		dcrutil.Hash160(wantAddr.script)) != 0 {
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

	if gotAddr.Address().EncodeAddress() != wantAddr.address {
		tc.t.Errorf("%s EncodeAddress: unexpected address - got %s, "+
			"want %s", prefix, gotAddr.Address().EncodeAddress(),
			wantAddr.address)
		return false
	}

	if bytes.Compare(gotAddr.AddrHash(), wantAddr.addressHash) != 0 {
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
func testLocking(tc *testContext, rb walletdb.ReadBucket) bool {
	if tc.unlocked {
		tc.t.Error("testLocking called with an unlocked manager")
		return false
	}
	if !tc.manager.IsLocked() {
		tc.t.Error("IsLocked: returned false on locked manager")
		return false
	}

	// Locking an already lock manager should return an error.  The error
	// should be ErrLocked or ErrWatchingOnly depending on the type of the
	// address manager.
	err := tc.manager.Lock()
	wantErrCode := errors.Locked
	if tc.watchingOnly {
		wantErrCode = errors.WatchingOnly
	}
	if !errors.Is(wantErrCode, err) {
		return false
	}

	// Ensure unlocking with the correct passphrase doesn't return any
	// unexpected errors and the manager properly reports it is unlocked.
	// Since watching-only address managers can't be unlocked, also ensure
	// the correct error for that case.
	err = tc.manager.Unlock(rb, privPassphrase)
	if tc.watchingOnly {
		if !errors.Is(errors.WatchingOnly, err) {
			return false
		}
	} else if err != nil {
		tc.t.Errorf("Unlock: unexpected error: %v", err)
		return false
	}
	if !tc.watchingOnly && tc.manager.IsLocked() {
		tc.t.Error("IsLocked: returned true on unlocked manager")
		return false
	}

	// Unlocking the manager again is allowed.  Since watching-only address
	// managers can't be unlocked, also ensure the correct error for that
	// case.
	err = tc.manager.Unlock(rb, privPassphrase)
	if tc.watchingOnly {
		if !errors.Is(errors.WatchingOnly, err) {
			return false
		}
	} else if err != nil {
		tc.t.Errorf("Unlock: unexpected error: %v", err)
		return false
	}
	if !tc.watchingOnly && tc.manager.IsLocked() {
		tc.t.Error("IsLocked: returned true on unlocked manager")
		return false
	}

	// Unlocking the manager with an invalid passphrase must result in an
	// error and a locked manager.
	err = tc.manager.Unlock(rb, []byte("invalidpassphrase"))
	wantErrCode = errors.Passphrase
	if tc.watchingOnly {
		wantErrCode = errors.WatchingOnly
	}
	if !errors.Is(wantErrCode, err) {
		return false
	}
	if !tc.manager.IsLocked() {
		tc.t.Error("IsLocked: manager is unlocked after failed unlock " +
			"attempt")
		return false
	}

	return true
}

// testImportPrivateKey tests that importing private keys works properly.  It
// ensures they can be retrieved by Address after they have been imported and
// the addresses give the expected values when the manager is locked and
// unlocked.
//
// This function expects the manager is already locked when called and returns
// with the manager locked.
func testImportPrivateKey(tc *testContext, ns walletdb.ReadWriteBucket) bool {
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

	// The manager must be unlocked to import a private key, however a
	// watching-only manager can't be unlocked.
	if !tc.watchingOnly {
		if err := tc.manager.Unlock(ns, privPassphrase); err != nil {
			tc.t.Errorf("Unlock: unexpected error: %v", err)
			return false
		}
		tc.unlocked = true
	}

	// Only import the private keys when in the create phase of testing.
	tc.account = ImportedAddrAccount
	prefix := testNamePrefix(tc) + " testImportPrivateKey"
	if tc.create {
		for i, test := range tests {
			test.expected.privKeyWIF = test.in
			wif, err := dcrutil.DecodeWIF(test.in)
			if err != nil {
				tc.t.Errorf("%s DecodeWIF #%d (%s) (%s): unexpected "+
					"error: %v", prefix, i, test.in, test.name, err)
				continue
			}
			addr, err := tc.manager.ImportPrivateKey(ns, wif)
			if err != nil {
				tc.t.Errorf("%s ImportPrivateKey #%d (%s): "+
					"unexpected error: %v", prefix, i,
					test.name, err)
				continue
			}
			if !testAddress(tc, prefix+" ImportPrivateKey", addr,
				&test.expected) {
				continue
			}
		}
	}

	// Setup a closure to test the results since the same tests need to be
	// repeated with the manager unlocked and locked.
	chainParams := tc.manager.ChainParams()
	testResults := func() bool {
		failed := false
		for i, test := range tests {
			test.expected.privKeyWIF = test.in

			// Use the Address API to retrieve each of the expected
			// new addresses and ensure they're accurate.
			utilAddr, err := dcrutil.NewAddressPubKeyHash(
				test.expected.addressHash, chainParams, dcrec.STEcdsaSecp256k1)
			if err != nil {
				tc.t.Errorf("%s NewAddressPubKeyHash #%d (%s): "+
					"unexpected error: %v", prefix, i,
					test.name, err)
				failed = true
				continue
			}
			taPrefix := fmt.Sprintf("%s Address #%d (%s)", prefix,
				i, test.name)
			ma, err := tc.manager.Address(ns, utilAddr)
			if err != nil {
				tc.t.Errorf("%s: unexpected error: %v", taPrefix,
					err)
				failed = true
				continue
			}
			if !testAddress(tc, taPrefix, ma, &test.expected) {
				failed = true
				continue
			}
		}

		return !failed
	}

	// The address manager could either be locked or unlocked here depending
	// on whether or not it's a watching-only manager.  When it's unlocked,
	// this will test both the public and private address data are accurate.
	// When it's locked, it must be watching-only, so only the public
	// address  information is tested and the private functions are checked
	// to ensure they return the expected ErrWatchingOnly error.
	if !testResults() {
		return false
	}

	// Everything after this point involves locking the address manager and
	// retesting the addresses with a locked manager.  However, for
	// watching-only mode, this has already happened, so just exit now in
	// that case.
	if tc.watchingOnly {
		return true
	}

	// Lock the manager and retest all of the addresses to ensure the
	// private information returns the expected error.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Errorf("Lock: unexpected error: %v", err)
		return false
	}
	tc.unlocked = false
	if !testResults() {
		return false
	}

	return true
}

// testImportScript tests that importing scripts works properly.  It ensures
// they can be retrieved by Address after they have been imported and the
// addresses give the expected values when the manager is locked and unlocked.
//
// This function expects the manager is already locked when called and returns
// with the manager locked.
func testImportScript(tc *testContext, wb walletdb.ReadWriteBucket) bool {
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

	// The manager must be unlocked to import a private key and also for
	// testing private data.  However, a watching-only manager can't be
	// unlocked.
	if !tc.watchingOnly {
		if err := tc.manager.Unlock(rb, privPassphrase); err != nil {
			tc.t.Errorf("Unlock: unexpected error: %v", err)
			return false
		}
		tc.unlocked = true
	}

	// Only import the scripts when in the create phase of testing.
	tc.account = ImportedAddrAccount
	prefix := testNamePrefix(tc)
	if tc.create {
		for i, test := range tests {
			test.expected.script = test.in
			prefix := fmt.Sprintf("%s ImportScript #%d (%s)", prefix,
				i, test.name)

			addr, err := tc.manager.ImportScript(wb, test.in)
			if err != nil {
				tc.t.Errorf("%s: unexpected error: %v", prefix,
					err)
				continue
			}
			if !testAddress(tc, prefix, addr, &test.expected) {
				continue
			}
		}
	}

	// Setup a closure to test the results since the same tests need to be
	// repeated with the manager unlocked and locked.
	chainParams := tc.manager.ChainParams()
	testResults := func() bool {
		failed := false
		for i, test := range tests {
			test.expected.script = test.in

			// Use the Address API to retrieve each of the expected
			// new addresses and ensure they're accurate.
			utilAddr, err := dcrutil.NewAddressScriptHash(test.in,
				chainParams)
			if err != nil {
				tc.t.Errorf("%s NewAddressScriptHash #%d (%s): "+
					"unexpected error: %v", prefix, i,
					test.name, err)
				failed = true
				continue
			}
			taPrefix := fmt.Sprintf("%s Address #%d (%s)", prefix,
				i, test.name)
			ma, err := tc.manager.Address(rb, utilAddr)
			if err != nil {
				tc.t.Errorf("%s: unexpected error: %v", taPrefix,
					err)
				failed = true
				continue
			}
			if !testAddress(tc, taPrefix, ma, &test.expected) {
				failed = true
				continue
			}
		}

		return !failed
	}

	// The address manager could either be locked or unlocked here depending
	// on whether or not it's a watching-only manager.  When it's unlocked,
	// this will test both the public and private address data are accurate.
	// When it's locked, it must be watching-only, so only the public
	// address information is tested and the private functions are checked
	// to ensure they return the expected ErrWatchingOnly error.
	if !testResults() {
		return false
	}

	// Everything after this point involves locking the address manager and
	// retesting the addresses with a locked manager.  However, for
	// watching-only mode, this has already happened, so just exit now in
	// that case.
	if tc.watchingOnly {
		return true
	}

	// Lock the manager and retest all of the addresses to ensure the
	// private information returns the expected error.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Errorf("Lock: unexpected error: %v", err)
		return false
	}
	tc.unlocked = false
	if !testResults() {
		return false
	}

	return true
}

// testChangePassphrase ensures changes both the public and privte passphrases
// works as intended.
func testChangePassphrase(tc *testContext, wb walletdb.ReadWriteBucket) bool {
	// Force an error when changing the passphrase due to failure to
	// generate a new secret key by replacing the generation function one
	// that intentionally errors.
	testName := "ChangePassphrase (public) with invalid new secret key"

	var err error
	TstRunWithReplacedNewSecretKey(func() {
		err = tc.manager.ChangePassphrase(wb, pubPassphrase, pubPassphrase2, false)
	})
	if !errors.Is(errors.Crypto, err) {
		return false
	}

	rb := wb.(walletdb.ReadBucket)

	// Attempt to change public passphrase with invalid old passphrase.
	testName = "ChangePassphrase (public) with invalid old passphrase"
	err = tc.manager.ChangePassphrase(wb, []byte("bogus"), pubPassphrase2, false)
	if !errors.Is(errors.Passphrase, err) {
		return false
	}

	// Change the public passphrase.
	testName = "ChangePassphrase (public)"
	err = tc.manager.ChangePassphrase(wb, pubPassphrase, pubPassphrase2, false)
	if err != nil {
		tc.t.Errorf("%s: unexpected error: %v", testName, err)
		return false
	}

	// Ensure the public passphrase was successfully changed.
	if !tc.manager.TstCheckPublicPassphrase(pubPassphrase2) {
		tc.t.Errorf("%s: passphrase does not match", testName)
		return false
	}

	// Change the private passphrase back to what it was.
	err = tc.manager.ChangePassphrase(wb, pubPassphrase2, pubPassphrase, false)
	if err != nil {
		tc.t.Errorf("%s: unexpected error: %v", testName, err)
		return false
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
	if !errors.Is(wantErrCode, err) {
		return false
	}

	// Everything after this point involves testing that the private
	// passphrase for the address manager can be changed successfully.
	// This is not possible for watching-only mode, so just exit now in that
	// case.
	if tc.watchingOnly {
		return true
	}

	// Change the private passphrase.
	testName = "ChangePassphrase (private)"
	err = tc.manager.ChangePassphrase(wb, privPassphrase, privPassphrase2, true)
	if err != nil {
		tc.t.Errorf("%s: unexpected error: %v", testName, err)
		return false
	}

	// Unlock the manager with the new passphrase to ensure it changed as
	// expected.
	if err := tc.manager.Unlock(rb, privPassphrase2); err != nil {
		tc.t.Errorf("%s: failed to unlock with new private "+
			"passphrase: %v", testName, err)
		return false
	}
	tc.unlocked = true

	// Change the private passphrase back to what it was while the manager
	// is unlocked to ensure that path works properly as well.
	err = tc.manager.ChangePassphrase(wb, privPassphrase2, privPassphrase, true)
	if err != nil {
		tc.t.Errorf("%s: unexpected error: %v", testName, err)
		return false
	}
	if tc.manager.IsLocked() {
		tc.t.Errorf("%s: manager is locked", testName)
		return false
	}

	// Relock the manager for future tests.
	if err := tc.manager.Lock(); err != nil {
		tc.t.Errorf("Lock: unexpected error: %v", err)
		return false
	}
	tc.unlocked = false

	return true
}

// testNewAccount tests the new account creation func of the address manager works
// as expected.
func testNewAccount(tc *testContext, wb walletdb.ReadWriteBucket) bool {
	if tc.watchingOnly {
		// Creating new accounts in watching-only mode should return ErrWatchingOnly
		_, err := tc.manager.NewAccount(wb, "test")
		if !errors.Is(errors.WatchingOnly, err) {
			tc.manager.Close()
			return false
		}
		return true
	}
	// Creating new accounts when wallet is locked should return ErrLocked
	_, err := tc.manager.NewAccount(wb, "test")
	if !errors.Is(errors.Locked, err) {
		tc.manager.Close()
		return false
	}

	rb := wb.(walletdb.ReadBucket)

	// Unlock the wallet to decrypt cointype keys required
	// to derive account keys
	if err := tc.manager.Unlock(rb, privPassphrase); err != nil {
		tc.t.Errorf("Unlock: unexpected error: %v", err)
		return false
	}
	tc.unlocked = true

	testName := "acct-create"
	expectedAccount := tc.account + 1
	if !tc.create {
		// Create a new account in open mode
		testName = "acct-open"
		expectedAccount++
	}
	account, err := tc.manager.NewAccount(wb, testName)
	if err != nil {
		tc.t.Errorf("NewAccount: unexpected error: %v", err)
		return false
	}
	if account != expectedAccount {
		tc.t.Errorf("NewAccount "+
			"account mismatch -- got %d, "+
			"want %d", account, expectedAccount)
		return false
	}

	// Test duplicate account name error
	_, err = tc.manager.NewAccount(wb, testName)
	if !errors.Is(errors.Exist, err) {
		return false
	}
	// Test account name validation
	testName = "" // Empty account names are not allowed
	_, err = tc.manager.NewAccount(wb, testName)
	if !errors.Is(errors.Invalid, err) {
		return false
	}
	testName = "imported" // A reserved account name
	_, err = tc.manager.NewAccount(wb, testName)
	if !errors.Is(errors.Invalid, err) {
		return false
	}
	return true
}

// testLookupAccount tests the basic account lookup func of the address manager
// works as expected.
func testLookupAccount(tc *testContext, rb walletdb.ReadBucket) bool {
	// Lookup accounts created earlier in testNewAccount
	expectedAccounts := map[string]uint32{
		TstDefaultAccountName:   DefaultAccountNum,
		ImportedAddrAccountName: ImportedAddrAccount,
	}
	for acctName, expectedAccount := range expectedAccounts {
		account, err := tc.manager.LookupAccount(rb, acctName)
		if err != nil {
			tc.t.Errorf("LookupAccount: unexpected error: %v", err)
			return false
		}
		if account != expectedAccount {
			tc.t.Errorf("LookupAccount "+
				"account mismatch -- got %d, "+
				"want %d", account, expectedAccount)
			return false
		}
	}
	// Test account not found error
	testName := "non existent account"
	_, err := tc.manager.LookupAccount(rb, testName)
	if !errors.Is(errors.NotExist, err) {
		return false
	}

	// Test last account
	lastAccount, err := tc.manager.LastAccount(rb)
	if err != nil {
		tc.t.Errorf("LastAccount failed: %v", err)
		return false
	}
	var expectedLastAccount uint32
	expectedLastAccount = 1
	if !tc.create {
		// Existing wallet manager will have 3 accounts
		expectedLastAccount = 2
	}
	if lastAccount != expectedLastAccount {
		tc.t.Errorf("LookupAccount "+
			"account mismatch -- got %d, "+
			"want %d", lastAccount, expectedLastAccount)
		return false
	}

	// TODO: derive for decred's expectedAddrs
	// Test account lookup for default account address
	// var expectedAccount uint32
	// for i, addr := range expectedAddrs {
	// 	addr, err := dcrutil.NewAddressPubKeyHash(addr.addressHash,
	// 		tc.manager.ChainParams(), chainec.ECTypeSecp256k1)
	// 	if err != nil {
	// 		tc.t.Errorf("AddrAccount #%d: unexpected error: %v", i, err)
	// 		return false
	// 	}
	// 	account, err := tc.manager.AddrAccount(addr)
	// 	if err != nil {
	// 		tc.t.Errorf("AddrAccount #%d: unexpected error: %v", i, err)
	// 		return false
	// 	}
	// 	if account != expectedAccount {
	// 		tc.t.Errorf("AddrAccount "+
	// 			"account mismatch -- got %d, "+
	// 			"want %d", account, expectedAccount)
	// 		return false
	// 	}
	// }

	return true
}

// testRenameAccount tests the rename account func of the address manager works
// as expected.
func testRenameAccount(tc *testContext, wb walletdb.ReadWriteBucket) bool {
	rb := wb.(walletdb.ReadBucket)
	acctName, err := tc.manager.AccountName(rb, tc.account)
	if err != nil {
		tc.t.Errorf("AccountName: unexpected error: %v", err)
		return false
	}
	testName := acctName + "-renamed"
	err = tc.manager.RenameAccount(wb, tc.account, testName)
	if err != nil {
		tc.t.Errorf("RenameAccount: unexpected error: %v", err)
		return false
	}
	newName, err := tc.manager.AccountName(rb, tc.account)
	if err != nil {
		tc.t.Errorf("AccountName: unexpected error: %v", err)
		return false
	}
	if newName != testName {
		tc.t.Errorf("RenameAccount "+
			"account name mismatch -- got %s, "+
			"want %s", newName, testName)
		return false
	}
	// Test duplicate account name error
	err = tc.manager.RenameAccount(wb, tc.account, testName)
	if !errors.Is(errors.Exist, err) {
		return false
	}
	// Test old account name is no longer valid
	_, err = tc.manager.LookupAccount(wb, acctName)
	if !errors.Is(errors.NotExist, err) {
		return false
	}
	return true
}

// testForEachAccount tests the retrieve all accounts func of the address
// manager works as expected.
func testForEachAccount(tc *testContext, rb walletdb.ReadBucket) bool {
	prefix := testNamePrefix(tc) + " testForEachAccount"
	expectedAccounts := []uint32{0, 1}
	if !tc.create {
		// Existing wallet manager will have 3 accounts
		expectedAccounts = append(expectedAccounts, 2)
	}
	// Imported account
	expectedAccounts = append(expectedAccounts, ImportedAddrAccount)
	var accounts []uint32
	err := tc.manager.ForEachAccount(rb, func(account uint32) error {
		accounts = append(accounts, account)
		return nil
	})
	if err != nil {
		tc.t.Errorf("%s: unexpected error: %v", prefix, err)
		return false
	}
	if len(accounts) != len(expectedAccounts) {
		tc.t.Errorf("%s: unexpected number of accounts - got "+
			"%d, want %d", prefix, len(accounts),
			len(expectedAccounts))
		return false
	}
	for i, account := range accounts {
		if expectedAccounts[i] != account {
			tc.t.Errorf("%s #%d: "+
				"account mismatch -- got %d, "+
				"want %d", prefix, i, account, expectedAccounts[i])
		}
	}

	// TODO: derive decred's expectedAddrs
	// testForEachAccountAddress tests that iterating through the given
	// account addresses using the manager API works as expected.
	// func testForEachAccountAddress(tc *testContext) bool {
	// 	prefix := testNamePrefix(tc) + " testForEachAccountAddress"
	// 	// Make a map of expected addresses
	// 	expectedAddrMap := make(map[string]*expectedAddr, len(expectedAddrs))
	// 	for i := 0; i < len(expectedAddrs); i++ {
	// 		expectedAddrMap[expectedAddrs[i].address] = &expectedAddrs[i]
	// 	}

	// 	var addrs []ManagedAddress
	// 	err := tc.manager.ForEachAccountAddress(tc.account,
	// 		func(maddr ManagedAddress) error {
	// 			addrs = append(addrs, maddr)
	// 			return nil
	// 		})
	// 	if err != nil {
	// 		tc.t.Errorf("%s: unexpected error: %v", prefix, err)
	// 		return false
	// 	}

	// 	for i := 0; i < len(addrs); i++ {
	// 		prefix := fmt.Sprintf("%s: #%d", prefix, i)
	// 		gotAddr := addrs[i]
	// 		wantAddr := expectedAddrMap[gotAddr.Address().String()]
	// 		if !testAddress(tc, prefix, gotAddr, wantAddr) {
	// 			return false
	// 		}
	// 		delete(expectedAddrMap, gotAddr.Address().String())
	// 	}

	// 	if len(expectedAddrMap) != 0 {
	// 		tc.t.Errorf("%s: unexpected addresses -- got %d, want %d", prefix,
	// 			len(expectedAddrMap), 0)
	// 		return false
	// 	}

	return true
}

// testEncryptDecryptErrors ensures that errors which occur while encrypting and
// decrypting data return the expected errors.
func testEncryptDecryptErrors(tc *testContext) {
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
	if !errors.Is(errors.Locked, err) {
		tc.t.Fatal("encryption with private key should fail when manager" +
			" is locked")
	}

	_, err = tc.manager.Decrypt(CKTPrivate, []byte{})
	if !errors.Is(errors.Locked, err) {
		tc.t.Fatal("decryption with private key should fail when manager" +
			" is locked")

	}

	walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
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
	if !errors.Is(errors.Crypto, err) {
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
	plainText := []byte("this is a plaintext")

	walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
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

// testManagerAPI tests the functions provided by the Manager API as well as
// the ManagedAddress, ManagedPubKeyAddress, and ManagedScriptAddress
// interfaces.
func testManagerAPI(tc *testContext) {
	err := walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)

		testLocking(tc, ns)
		testImportPrivateKey(tc, ns)
		testImportScript(tc, ns)
		testChangePassphrase(tc, ns)

		// Reset default account
		tc.account = 0
		testNewAccount(tc, ns)
		testLookupAccount(tc, ns)
		testForEachAccount(tc, ns)
		// testForEachAccountAddress(tc)

		// Rename account 1 "acct-create"
		tc.account = 1
		testRenameAccount(tc, ns)

		return nil
	})
	if err != nil {
		tc.t.Errorf("unexpected error: %v", err)
	}
}

// testWatchingOnly tests various facets of a watching-only address
// manager such as running the full set of API tests against a newly converted
// copy as well as when it is opened from an existing namespace.
func testWatchingOnly(tc *testContext) bool {
	// Make a copy of the current database so the copy can be converted to
	// watching only.
	woMgrName := "mgrtestwo.bin"
	_ = os.Remove(woMgrName)
	fi, err := os.OpenFile(woMgrName, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		tc.t.Errorf("%v", err)
		return false
	}
	if err = tc.db.Copy(fi); err != nil {
		fi.Close()
		tc.t.Errorf("%v", err)
		return false
	}
	fi.Close()
	defer os.Remove(woMgrName)

	// Open the new database copy.
	db, err := walletdb.Open("bdb", woMgrName)
	if err != nil {
		tc.t.Errorf("%v", err)
		return false
	}

	// Open the manager using the namespace and convert it to watching-only.
	mgr, _, _, err := Open(db, &chaincfg.TestNet3Params, pubPassphrase)
	if err != nil {
		tc.t.Errorf("unexpected error: %v", err)
		return false
	}

	err = walletdb.Update(tc.db, func(tx walletdb.ReadWriteTx) error {
		ns := tx.ReadWriteBucket(waddrmgrBucketKey)
		if err = mgr.ConvertToWatchingOnly(ns); err != nil {
			return fmt.Errorf("unexpected error: %v", err)
		}
		return nil
	})
	if err != nil {
		tc.t.Error(err)
		return false
	}

	// Run all of the manager API tests against the converted manager and
	// close it.
	testManagerAPI(&testContext{
		t:            tc.t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: true,
	})
	mgr.Close()

	// Open the watching-only manager and run all the tests again.
	mgr, _, _, err = Open(db, &chaincfg.TestNet3Params, pubPassphrase)
	if err != nil {
		tc.t.Errorf("Open Watching-Only: unexpected error: %v", err)
		return false
	}
	defer mgr.Close()

	testManagerAPI(&testContext{
		t:            tc.t,
		db:           db,
		manager:      mgr,
		account:      0,
		create:       false,
		watchingOnly: true,
	})

	return true
}

// TestManager performs a full suite of tests against the address manager API.
// It makes use of a test context because the address manager is persistent and
// much of the testing involves having specific state.
func TestManager(t *testing.T) {
	t.Parallel()

	dbName := "mgrtest.bin"
	db, mgr, _, _, teardown, err := testDB(dbName)
	defer teardown()

	if err != nil {
		t.Fatal(err)
	}

	// Run all of the manager API tests in create mode and close the
	// manager after they've completed
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
	mgr, _, _, err = Open(db, &chaincfg.TestNet3Params, pubPassphrase)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
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
	testManagerAPI(tc)

	// Now that the address manager has been tested in both the newly
	// created and opened modes, test a watching-only version.
	testWatchingOnly(tc)
}
