// Copyright (c) 2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var r = rand.New(rand.NewSource(time.Now().Unix()))

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	r.Read(bytes)
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

func TestSetBirthState(t *testing.T) {
	ctx := context.Background()
	db, _, _, _, teardown, err := cloneDB(ctx, "mgr_watching_only.kv")
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
			Height:        uint32(r.Intn(100000)),
			Time:          time.Unix(r.Int63n(100000000), 0),
			SetFromHeight: r.Intn(2) == 0,
			SetFromTime:   r.Intn(2) == 0,
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
