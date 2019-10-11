// Copyright (c) 2018-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// expectedAddr is used to house the expected return values from a managed
// address.  Not all fields for used for all managed address types.
type expectedAddr struct {
	address     string
	addressHash []byte
	internal    bool
	pubKey      []byte
}

// testContext is used to store context information about a running test which
// is passed into helper functions.
type testContext struct {
	t            *testing.T
	account      uint32
	watchingOnly bool
}

// hexToBytes is a wrapper around hex.DecodeString that panics if there is an
// error.  It MUST only be used with hard coded values in the tests.
func hexToBytes(origHex string) []byte {
	buf, err := hex.DecodeString(origHex)
	if err != nil {
		panic(err)
	}
	return buf
}

var (
	// seed is the master seed used throughout the tests.
	seed = []byte{
		0xb4, 0x6b, 0xc6, 0x50, 0x2a, 0x30, 0xbe, 0xb9, 0x2f,
		0x0a, 0xeb, 0xc7, 0x76, 0x40, 0x3c, 0x3d, 0xbf, 0x11,
		0xbf, 0xb6, 0x83, 0x05, 0x96, 0x7c, 0x36, 0xda, 0xc9,
		0xef, 0x8d, 0x64, 0x15, 0x67,
	}

	pubPassphrase  = []byte("_DJr{fL4H0O}*-0\n:V1izc)(6BomK")
	privPassphrase = []byte("81lUHXnOMZ@?XXd7O9xyDIWIbXX-lj")

	walletConfig = Config{
		PubPassphrase: pubPassphrase,
		GapLimit:      20,
		RelayFee:      dcrutil.Amount(1e5).ToCoin(),
		Params:        chaincfg.SimNetParams(),
	}

	defaultAccount = uint32(0)

	waddrmgrBucketKey = []byte("waddrmgr")

	expectedInternalAddrs = []expectedAddr{
		{
			address:     "SsrFKd8aX4KHabWSQfbmEaDv3BJCpSH2ySj",
			addressHash: hexToBytes("f032b89ec075ab2847e2ec186ad000be16cf354b"),
			internal:    true,
			pubKey:      hexToBytes("03d1ad44eeac8eb59e9598f7e530a1cbe2c1684c0aa5f45ab24d33d38a2102dd1a"),
		},
		{
			address:     "SsW4roiFKWkbbhiAeEV5byet1pLKAP4xRks",
			addressHash: hexToBytes("12d5a8e19b9a070d6d5e6e425b593c2c137285e3"),
			internal:    true,
			pubKey:      hexToBytes("02cbcf5c1aa84bf8e6d04412d867eccbaa6cc12ebb792f3f1eaf4d2887f8e884f3"),
		},
		{
			address:     "SscaK4A6V94dawc6ZBRCGUxPjdf7um1GJgD",
			addressHash: hexToBytes("5a38638f09937214b07481c656d0c9c73020f8bf"),
			internal:    true,
			pubKey:      hexToBytes("0392735a0eee9026425556ef5c5ae23ad3e54598132a1ca0d74dbcac7bfe31bfa4"),
		},
		{
			address:     "Ssm4BeTKgwKGTqNR63WiGtP1FJaKCRJsN1S",
			addressHash: hexToBytes("b73edb8f32957800e2e3b9424c3b659acac51b7f"),
			internal:    true,
			pubKey:      hexToBytes("037c1e500884c6c3cb044390b52525d324fd67c031fdd9a47d742d0323fe5de73f"),
		},
		{
			address:     "SssBoVxTkCUb6xs7vph3BHdPmki3weVvRsF",
			addressHash: hexToBytes("fa8073fcb670ba7312a1ef0d908cfb05c59b70b9"),
			internal:    true,
			pubKey:      hexToBytes("0327540e546f9cfac45f51699e2656732727507971060641ead554d78eeea88aa6"),
		},
	}

	expectedExternalAddrs = []expectedAddr{
		{
			address:     "SsfPTmZmaXGkXfcNGjftRPmoGGCqtNPCHKx",
			addressHash: hexToBytes("791376f67fb3becf392b071d25d7c99c82139ee3"),
			internal:    false,
			pubKey:      hexToBytes("031634efb3e83c834a82cdc898000f85215a09dc742d5b3b82ace7221ca1bb0938"),
		},
		{
			address:     "SsXhSHBiaEan7Ls36bvhLspZ3LC1NKzuwQz",
			addressHash: hexToBytes("24b8b3d89f987bf3cd80a8c16d9368d683217fa4"),
			internal:    false,
			pubKey:      hexToBytes("0280beb72c6ef42ce3133fd6d340fd5bedcfccaded5a6eabb6d2430e3958bf7c85"),
		},
		{
			address:     "SspSfaWDNwc9TA31Q9iR2jot2eV1hk2ix6U",
			addressHash: hexToBytes("dc67b3d95adb1789efe4aa73607d8a8c57eee2bb"),
			internal:    false,
			pubKey:      hexToBytes("03b120e0e073a12a1957680251a1562c5c6e30e547797fb5411107eac19699f601"),
		},
		{
			address:     "SsrSYTB9MQQ1czAfPmWE66ZFqv7NrwzqfQT",
			addressHash: hexToBytes("f252015c8e0059c21cae623704d8588d12ca5c74"),
			internal:    false,
			pubKey:      hexToBytes("0350822c9bd61f524f4d68fa605e850c34c5e8ccc9b5cf278782131c1e21dd261b"),
		},
		{
			address:     "SsqBcGBre8SZrG61cd5M5e2GaMJbK2CMdEa",
			addressHash: hexToBytes("e486d22d1244becac5a30b38dda1c8c4c1b3bdeb"),
			internal:    false,
			pubKey:      hexToBytes("031c494068c9c454bef7de35fa4717f21c07dec4471bd8500650b133d57e49a81d"),
		},
	}
)

func setupWallet(t *testing.T, cfg *Config) (*Wallet, walletdb.DB, func()) {
	f, err := ioutil.TempFile("", "testwallet.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	db, err := walletdb.Create("bdb", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = Create(ctx, opaqueDB{db}, pubPassphrase, privPassphrase, seed, cfg.Params)
	if err != nil {
		db.Close()
		os.Remove(f.Name())
		t.Fatal(err)
	}
	cfg.DB = opaqueDB{db}

	w, err := Open(ctx, cfg)
	if err != nil {
		db.Close()
		os.Remove(f.Name())
		t.Fatal(err)
	}

	teardown := func() {
		db.Close()
		os.Remove(f.Name())
	}

	return w, db, teardown
}

func testExternalAddresses(tc *testContext) {
	w, db, teardown := setupWallet(tc.t, &walletConfig)
	defer teardown()

	prefix := "testExternalAddresses"
	ctx := context.Background()

	if tc.watchingOnly {
		err := walletdb.Update(ctx, db, func(tx walletdb.ReadWriteTx) error {
			ns := tx.ReadWriteBucket(waddrmgrBucketKey)
			return w.Manager.ConvertToWatchingOnly(ns)
		})
		if err != nil {
			tc.t.Fatalf("%s: failed to convert wallet to watching only: %v",
				prefix, err)
		}
	}

	for i := 0; i < len(expectedExternalAddrs); i++ {
		addr, err := w.NewExternalAddress(context.Background(), defaultAccount)
		if err != nil {
			tc.t.Fatalf("%s: failed to generate external address: %v",
				prefix, err)
		}

		maddr, err := w.AddressInfo(ctx, addr)
		if err != nil {
			tc.t.Errorf("Unexpected error: %v", err)
		}

		if maddr.Account() != defaultAccount {
			tc.t.Fatalf("%s: expected account %v got %v", prefix,
				defaultAccount, maddr.Account())
		}

		if strings.Compare(maddr.Address().String(),
			expectedExternalAddrs[i].address) != 0 {
			tc.t.Fatalf("%s: expected address %v got %v", prefix,
				expectedExternalAddrs[i].address, maddr.Address().String())
		}

		if !bytes.Equal(maddr.AddrHash(), expectedExternalAddrs[i].addressHash) {
			tc.t.Fatalf("%s: expected address hash %v got %v", prefix,
				hex.EncodeToString(expectedExternalAddrs[i].addressHash),
				hex.EncodeToString(maddr.AddrHash()))
		}

		if maddr.Internal() != expectedExternalAddrs[i].internal {
			tc.t.Fatalf("%s: expected internal status of %v got %v", prefix,
				expectedExternalAddrs[i].internal, maddr.Internal())
		}

		pubKey, err := w.PubKeyForAddress(ctx, addr)
		if err != nil {
			tc.t.Fatalf("%s: failed to get public key for address %s: %v",
				prefix, addr.String(), err)
		}

		if !bytes.Equal(pubKey.Serialize(), expectedExternalAddrs[i].pubKey) {
			tc.t.Fatalf("%s: expected pubkey %v got %v",
				prefix, hex.EncodeToString(expectedExternalAddrs[i].pubKey),
				hex.EncodeToString(pubKey.Serialize()))
		}
	}
}

func testInternalAddresses(tc *testContext) {
	w, db, teardown := setupWallet(tc.t, &walletConfig)
	defer teardown()

	prefix := "testInternalAddresses"
	ctx := context.Background()

	if tc.watchingOnly {
		err := walletdb.Update(ctx, db, func(tx walletdb.ReadWriteTx) error {
			ns := tx.ReadWriteBucket(waddrmgrBucketKey)
			return w.Manager.ConvertToWatchingOnly(ns)
		})
		if err != nil {
			tc.t.Fatalf("%s: failed to convert wallet to watching only: %v",
				prefix, err)
		}
	}

	for i := 0; i < len(expectedInternalAddrs); i++ {
		addr, err := w.NewInternalAddress(context.Background(), defaultAccount)
		if err != nil {
			tc.t.Fatalf("%s: failed to generate internal address: %v",
				prefix, err)
		}

		maddr, err := w.AddressInfo(ctx, addr)
		if err != nil {
			tc.t.Errorf("Unexpected error: %v", err)
		}

		if maddr.Account() != defaultAccount {
			tc.t.Fatalf("%s: expected account %v got %v", prefix,
				defaultAccount, maddr.Account())
		}

		if strings.Compare(maddr.Address().String(),
			expectedInternalAddrs[i].address) != 0 {
			tc.t.Fatalf("%s: expected address %v got %v", prefix,
				expectedExternalAddrs[i].address, maddr.Address().String())
		}

		if !bytes.Equal(maddr.AddrHash(), expectedInternalAddrs[i].addressHash) {
			tc.t.Fatalf("%s: expected address hash %v got %v", prefix,
				hex.EncodeToString(expectedInternalAddrs[i].addressHash),
				hex.EncodeToString(maddr.AddrHash()))
		}

		if maddr.Internal() != expectedInternalAddrs[i].internal {
			tc.t.Fatalf("%s: expected internal status of %v got %v", prefix,
				expectedExternalAddrs[i].internal, maddr.Internal())
		}

		pubKey, err := w.PubKeyForAddress(ctx, addr)
		if err != nil {
			tc.t.Fatalf("%s: failed to get public key for address %s: %v",
				prefix, addr.String(), err)
		}

		if !bytes.Equal(pubKey.Serialize(), expectedInternalAddrs[i].pubKey) {
			tc.t.Fatalf("%s: expected pubkey %v got %v",
				prefix, hex.EncodeToString(expectedInternalAddrs[i].pubKey),
				hex.EncodeToString(pubKey.Serialize()))
		}
	}
}

func TestAddresses(t *testing.T) {
	testInternalAddresses(&testContext{
		t:            t,
		account:      defaultAccount,
		watchingOnly: false,
	})

	testInternalAddresses(&testContext{
		t:            t,
		account:      defaultAccount,
		watchingOnly: true,
	})

	testExternalAddresses(&testContext{
		t:            t,
		account:      defaultAccount,
		watchingOnly: false,
	})

	testExternalAddresses(&testContext{
		t:            t,
		account:      defaultAccount,
		watchingOnly: true,
	})
}

func TestAccountIndexes(t *testing.T) {
	cfg := basicWalletConfig
	w, teardown := testWallet(t, &cfg)
	defer teardown()

	w.SetNetworkBackend(mockNetwork{})

	tests := []struct {
		f       func(t *testing.T, w *Wallet)
		indexes accountIndexes
	}{
		{nil, accountIndexes{{^uint32(0), 0}, {^uint32(0), 0}}},
		{nextAddresses(1), accountIndexes{{^uint32(0), 1}, {^uint32(0), 0}}},
		{nextAddresses(19), accountIndexes{{^uint32(0), 20}, {^uint32(0), 0}}},
		{watchFutureAddresses, accountIndexes{{^uint32(0), 20}, {^uint32(0), 0}}},
		{useAddress(10), accountIndexes{{10, 9}, {^uint32(0), 0}}},
		{nextAddresses(1), accountIndexes{{10, 10}, {^uint32(0), 0}}},
		{nextAddresses(10), accountIndexes{{10, 20}, {^uint32(0), 0}}},
		{useAddress(30), accountIndexes{{30, 0}, {^uint32(0), 0}}},
		{useAddress(31), accountIndexes{{31, 0}, {^uint32(0), 0}}},
	}
	for i, test := range tests {
		if test.f != nil {
			test.f(t, w)
		}
		w.addressBuffersMu.Lock()
		b := w.addressBuffers[0]
		check := func(what string, a, b uint32) {
			if a != b {
				t.Fatalf("%d: %s do not match: %d != %d", i, what, a, b)
			}
		}
		check("external last indexes", b.albExternal.lastUsed, test.indexes[0].last)
		check("external cursors", b.albExternal.cursor, test.indexes[0].cursor)
		check("internal last indexes", b.albInternal.lastUsed, test.indexes[1].last)
		check("internal cursors", b.albInternal.cursor, test.indexes[1].cursor)
		w.addressBuffersMu.Unlock()
	}
}

type accountIndexes [2]struct {
	last, cursor uint32
}

func nextAddresses(n int) func(t *testing.T, w *Wallet) {
	return func(t *testing.T, w *Wallet) {
		for i := 0; i < n; i++ {
			_, err := w.NewExternalAddress(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func watchFutureAddresses(t *testing.T, w *Wallet) {
	ctx := context.Background()
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		return w.watchFutureAddresses(context.Background(), dbtx)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func useAddress(child uint32) func(t *testing.T, w *Wallet) {
	ctx := context.Background()
	return func(t *testing.T, w *Wallet) {
		w.addressBuffersMu.Lock()
		xbranch := w.addressBuffers[0].albExternal.branchXpub
		w.addressBuffersMu.Unlock()
		addr, err := deriveChildAddress(xbranch, child, basicWalletConfig.Params)
		if err != nil {
			t.Fatal(err)
		}
		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			ns := dbtx.ReadWriteBucket(waddrmgrBucketKey)
			ma, err := w.Manager.Address(ns, addr)
			if err != nil {
				return err
			}
			return w.markUsedAddress("", dbtx, ma)
		})
		if err != nil {
			t.Fatal(err)
		}
		watchFutureAddresses(t, w)
	}
}
