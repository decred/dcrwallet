// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
)

var dbUpgradeTests = [...]struct {
	verify   func(*testing.T, walletdb.DB)
	filename string // in testdata directory
}{
	{verifyV2Upgrade, "v1.db.gz"},
	{verifyV3Upgrade, "v2.db.gz"},
	{verifyV4Upgrade, "v3.db.gz"},
	{verifyV5Upgrade, "v4.db.gz"},
}

var pubPass = []byte("public")

func TestUpgrades(t *testing.T) {
	t.Parallel()

	d, err := ioutil.TempDir("", "dcrwallet_udb_TestUpgrades")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("group", func(t *testing.T) {
		for i, test := range dbUpgradeTests {
			test := test
			name := fmt.Sprintf("test%d", i)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
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
		}
	})

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
			{^uint32(0), ^uint32(0)},
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

func verifyV3Upgrade(t *testing.T, db walletdb.DB) {
	_, _, smgr, err := Open(db, &chaincfg.TestNet2Params, pubPass)
	if err != nil {
		t.Fatalf("Open after Upgrade failed: %v", err)
	}

	err = walletdb.View(db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wstakemgrBucketKey)

		const (
			ticketHashStr     = "4516ef1d548f3284c1a27b3e706c4677392031df7071ad2022050af376837033"
			votingAddrStr     = "Tcu5oEdEp1W93fRT9FGSwMin7LonfRjNYe4"
			ticketPurchaseHex = "01000000024bf0a303a7e6d174833d9eb761815b61f8ba8c6fa8852a6bf51c703daefc0ef60400000000ffffffff4bf0a303a7e6d174833d9eb761815b61f8ba8c6fa8852a6bf51c703daefc0ef60500000000ffffffff056f78d37a00000000000018baa914ec97b165a5f028b50fb12ae717c5f6c1b9057b5f8700000000000000000000206a1e7f686bc0e548bbb92f487db6da070e43a34117288ed59100000000000058000000000000000000001abd76a914000000000000000000000000000000000000000088ac00000000000000000000206a1e9d8e8bdc618035be32a14ab752af2e331f9abf3651074a7a000000000058000000000000000000001abd76a914000000000000000000000000000000000000000088ac00000000ad480000028ed59100000000009c480000010000006b483045022100c240bdd6a656c20e9035b839fc91faae6c766772f76149adb91a1fdcf20faf9c02203d68038b83263293f864b173c8f3f00e4371b67bf36fb9ec9f5132bdf68d2858012102adc226dec4de09a18c5a522f8f00917fb6d4eb2361a105218ac3f87d802ae3d451074a7a000000009c480000010000006a47304402205af53185f2662a30a22014b0d19760c1bfde8ec8f065b19cacab6a7abcec76a202204a2614cfcb4db3fc1c86eb0b1ca577f9039ec6db29e9c44ddcca2fe6e3c8bd5d012102adc226dec4de09a18c5a522f8f00917fb6d4eb2361a105218ac3f87d802ae3d4"

			// Stored timestamp uses time.Now().  The generated database test
			// artifact uses this time (2017-04-10 11:50:04 -0400 EDT).  If the
			// db is ever regenerated, this expected value be updated as well.
			timeStamp = 1491839404
		)

		// Verify ticket purchase is still present with correct info, and no
		// vote bits.
		ticketPurchaseHash, err := chainhash.NewHashFromStr(ticketHashStr)
		if err != nil {
			return err
		}
		rec, err := fetchSStxRecord(ns, ticketPurchaseHash, 3)
		if err != nil {
			return err
		}
		if rec.voteBitsSet || rec.voteBits != 0 || rec.voteBitsExt != nil {
			t.Errorf("Ticket purchase record still has vote bits")
		}
		votingAddr, err := smgr.SStxAddress(ns, ticketPurchaseHash)
		if err != nil {
			return err
		}
		if votingAddr.String() != votingAddrStr {
			t.Errorf("Unexpected voting address, got %v want %v",
				votingAddr.String(), votingAddrStr)
		}
		if rec.ts.Unix() != timeStamp {
			t.Errorf("Unexpected timestamp, got %v want %v", rec.ts.Unix(), timeStamp)
		}
		var buf bytes.Buffer
		err = rec.tx.MsgTx().Serialize(&buf)
		if err != nil {
			return err
		}
		expectedBytes, err := hex.DecodeString(ticketPurchaseHex)
		if err != nil {
			return err
		}
		if !bytes.Equal(buf.Bytes(), expectedBytes) {
			t.Errorf("Serialized transaction does not match expected")
		}

		// Verify that the agenda preferences bucket was created.
		if tx.ReadBucket(agendaPreferences.rootBucketKey()) == nil {
			t.Errorf("Agenda preferences bucket was not created")
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV4Upgrade(t *testing.T, db walletdb.DB) {
	err := walletdb.View(db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrBucketKey)
		mainBucket := ns.NestedReadBucket(mainBucketName)
		if mainBucket.Get(seedName) != nil {
			t.Errorf("Seed was not deleted")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}
}

func verifyV5Upgrade(t *testing.T, db walletdb.DB) {
	err := walletdb.View(db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(waddrmgrBucketKey)

		data := []struct {
			acct             uint32
			lastUsedExtChild uint32
			lastUsedIntChild uint32
		}{
			{0, ^uint32(0), ^uint32(0)},
			{1, 0, 0},
			{2, 9, 9},
			{3, 5, 15},
			{4, 19, 20},
			{5, 20, 19},
			{6, 29, 30},
			{7, 30, 29},
			{8, 1<<31 - 1, 1<<31 - 1},
			{ImportedAddrAccount, 0, 0},
		}

		const dbVersion = 5

		for _, d := range data {
			row, err := fetchAccountInfo(ns, d.acct, dbVersion)
			if err != nil {
				return err
			}
			if row.lastUsedExternalIndex != d.lastUsedExtChild {
				t.Errorf("Account %d last used ext child mismatch %d != %d",
					d.acct, row.lastUsedExternalIndex, d.lastUsedExtChild)
			}
			if row.lastReturnedExternalIndex != d.lastUsedExtChild {
				t.Errorf("Account %d last returned ext child mismatch %d != %d",
					d.acct, row.lastReturnedExternalIndex, d.lastUsedExtChild)
			}
			if row.lastUsedInternalIndex != d.lastUsedIntChild {
				t.Errorf("Account %d last used int child mismatch %d != %d",
					d.acct, row.lastUsedInternalIndex, d.lastUsedIntChild)
			}
			if row.lastReturnedInternalIndex != d.lastUsedIntChild {
				t.Errorf("Account %d last returned int child mismatch %d != %d",
					d.acct, row.lastReturnedInternalIndex, d.lastUsedIntChild)
			}
		}

		return nil
	})
	if err != nil {
		t.Error(err)
	}
}
