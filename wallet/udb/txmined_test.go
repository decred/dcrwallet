// Copyright (c) 2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"context"
	"testing"
	"time"

	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/crypto/rand"
	"github.com/decred/dcrd/dcrutil/v4"
)

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func randomHash() chainhash.Hash {
	hash := new(chainhash.Hash)
	err := hash.SetBytes(randomBytes(32))
	if err != nil {
		panic(err)
	}
	return *hash
}

func TestCreditCoinType(t *testing.T) {
	// Test Credit struct CoinType field functionality
	tests := []struct {
		name     string
		coinType dcrutil.CoinType
		want     dcrutil.CoinType
	}{
		{"VAR coin (default)", dcrutil.CoinTypeVAR, dcrutil.CoinTypeVAR},
		{"SKA-1 coin", dcrutil.CoinType(1), dcrutil.CoinType(1)},
		{"SKA-255 coin", dcrutil.CoinType(255), dcrutil.CoinType(255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Credit with specific CoinType
			credit := Credit{
				Amount:   dcrutil.Amount(100000000), // 1 DCR in atoms
				CoinType: tt.coinType,
				Received: time.Now(),
			}

			// Verify CoinType is stored correctly
			if credit.CoinType != tt.want {
				t.Errorf("Credit.CoinType = %v, want %v", credit.CoinType, tt.want)
			}
		})
	}
}

func TestCreditSerialization(t *testing.T) {
	// No database needed for direct serialization function testing

	// Test database serialization of CoinType field
	tests := []struct {
		name     string
		coinType dcrutil.CoinType
	}{
		{"VAR serialization", dcrutil.CoinTypeVAR},
		{"SKA-1 serialization", dcrutil.CoinType(1)},
		{"SKA-100 serialization", dcrutil.CoinType(100)},
		{"SKA-255 serialization", dcrutil.CoinType(255)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test fetchRawCreditCoinType function directly
			// Create sample serialized credit data with CoinType in byte 9
			testData := make([]byte, 10)
			testData[9] = byte(tt.coinType)

			// Test deserialization
			gotCoinType := fetchRawCreditCoinType(testData)
			if gotCoinType != tt.coinType {
				t.Errorf("fetchRawCreditCoinType() = %v, want %v", gotCoinType, tt.coinType)
			}
		})
	}

	// Test backward compatibility - old data without CoinType
	t.Run("backward compatibility", func(t *testing.T) {
		// Old serialized data (less than 10 bytes)
		oldData := make([]byte, 9)
		
		// Should default to VAR
		gotCoinType := fetchRawCreditCoinType(oldData)
		if gotCoinType != dcrutil.CoinTypeVAR {
			t.Errorf("fetchRawCreditCoinType(old data) = %v, want VAR", gotCoinType)
		}
	})
}

func TestSetBirthState(t *testing.T) {
	ctx := context.Background()
	db, _, _, teardown, err := cloneDB(ctx, "mgr_watching_only.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		birthState *BirthdayState
	}{{
		name: "ok",
		birthState: &BirthdayState{
			Hash:          randomHash(),
			Height:        uint32(rand.IntN(100000)),
			Time:          time.Unix(rand.Int64N(100000000), 0),
			SetFromHeight: rand.IntN(2) == 0,
			SetFromTime:   rand.IntN(2) == 0,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err = walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
				err := SetBirthState(dbtx, test.birthState)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}

			var bs *BirthdayState
			err = walletdb.View(ctx, db, func(dbtx walletdb.ReadTx) error {
				bs = BirthState(dbtx)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if bs.Hash != test.birthState.Hash ||
				bs.Height != test.birthState.Height ||
				bs.Time != test.birthState.Time ||
				bs.SetFromHeight != test.birthState.SetFromHeight ||
				bs.SetFromTime != test.birthState.SetFromTime {
				t.Fatalf("want birthday state %+v not equal to got %+v",
					test.birthState, bs)
			}
		})
	}
}
