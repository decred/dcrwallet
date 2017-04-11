// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
)

var dbUpgradeTests = [...]struct {
	verify   func(*testing.T, walletdb.DB)
	filename string // in testdata directory
}{
	{verifyV2Upgrade, "v1.db.gz"},
}

var pubPass = []byte("public")

func TestUpgrades(t *testing.T) {
	t.Parallel()

	d, err := ioutil.TempDir("", "dcrwallet_udb_TestUpgrades")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(len(dbUpgradeTests))
	for i, test := range dbUpgradeTests {
		test := test
		name := fmt.Sprintf("test%d", i)
		go func() {
			t.Run(name, func(t *testing.T) {
				defer wg.Done()
				testFile, err := os.Open(filepath.Join("testdata", test.filename))
				if err != nil {
					t.Fatal(err)
				}
				defer testFile.Close()
				r, err := gzip.NewReader(testFile)
				if err != nil {
					t.Fatal(err)
				}
				dbPath := filepath.Join(d, name+".db")
				fi, err := os.Create(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(fi, r)
				fi.Close()
				if err != nil {
					t.Fatal(err)
				}
				db, err := walletdb.Open("bdb", dbPath)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				err = Upgrade(db, pubPass)
				if err != nil {
					t.Fatalf("Upgrade failed: %v", err)
				}
				test.verify(t, db)
			})
		}()
	}

	wg.Wait()
	os.RemoveAll(d)
}

func verifyV2Upgrade(t *testing.T, db walletdb.DB) {
	amgr, _, _, err := Open(db, &chaincfg.TestNet2Params, pubPass)
	if err != nil {
		t.Fatalf("Open after Upgrade failed: %v", err)
	}

	err = walletdb.View(db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrBucketKey)
		nsMetaBucket := ns.NestedReadBucket(metaBucketName)

		accounts := []struct {
			totalAddrs uint32
			lastUsed   uint32
		}{
			{0, 0},
			{20, 18},
			{20, 19},
			{20, 19},
			{30, 25},
			{30, 29},
			{30, 29},
			{200, 185},
			{200, 199},
		}

		switch lastAccount, err := fetchLastAccount(ns); {
		case err != nil:
			t.Errorf("fetchLastAccount: %v", err)
		case lastAccount != uint32(len(accounts)-1):
			t.Errorf("Number of BIP0044 accounts got %v want %v",
				lastAccount+1, uint32(len(accounts)))
		}

		for i, a := range accounts {
			account := uint32(i)

			if nsMetaBucket.Get(accountNumberToAddrPoolKey(false, account)) != nil {
				t.Errorf("Account %v external address pool bucket still exists", account)
			}
			if nsMetaBucket.Get(accountNumberToAddrPoolKey(true, account)) != nil {
				t.Errorf("Account %v external address pool bucket still exists", account)
			}

			props, err := amgr.AccountProperties(ns, account)
			if err != nil {
				t.Errorf("AccountProperties: %v", err)
				continue
			}
			if props.LastUsedExternalIndex != a.lastUsed {
				t.Errorf("Account %v last used ext index got %v want %v",
					account, props.LastUsedExternalIndex, a.lastUsed)
			}
			if props.LastUsedInternalIndex != a.lastUsed {
				t.Errorf("Account %v last used int index got %v want %v",
					account, props.LastUsedInternalIndex, a.lastUsed)
			}
		}

		if ns.NestedReadBucket(usedAddrBucketName) != nil {
			t.Error("Used address bucket still exists")
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
