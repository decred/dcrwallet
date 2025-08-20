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

func TestMissingCFiltersHeight(t *testing.T) {
	ctx := context.Background()
	db, _, s, teardown, err := cloneDB(ctx, "mgr_watching_only.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	fakeFilter := [16]byte{}

	g := makeBlockGenerator()
	b1H := g.generate(dcrutil.BlockValid)
	b2H := g.generate(dcrutil.BlockValid)
	b3H := g.generate(dcrutil.BlockValid)
	b4H := g.generate(dcrutil.BlockValid)
	b5H := g.generate(dcrutil.BlockValid)
	headerData := makeHeaderDataSlice(b1H, b2H, b3H, b4H, b5H)
	filters := emptyFilters(5)

	err = walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
		err = insertMainChainHeaders(s, dbtx, headerData, filters)
		if err != nil {
			return err
		}
		// Delete filters for block 3 and 4.
		ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)
		b3Hash := b3H.BlockHash()
		if err := ns.NestedReadWriteBucket(bucketCFilters).Delete(b3Hash[:]); err != nil {
			return err
		}
		b4Hash := b4H.BlockHash()
		if err := ns.NestedReadWriteBucket(bucketCFilters).Delete(b4Hash[:]); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name            string
		missingNo, from int32
		wantErr         bool
		do              func()
	}{{
		name:      "ok from 0",
		missingNo: 3,
	}, {
		name:      "ok from mid",
		from:      4,
		missingNo: 4,
	}, {
		name: "ok from 1 after adding",
		from: 1,
		do: func() {
			if err := walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
				ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)
				b3Hash := b3H.BlockHash()
				err := putRawCFilter(ns, b3Hash[:], fakeFilter[:])
				if err != nil {
					return err
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		},
		missingNo: 4,
	}, {
		name: "error once all filters full",
		do: func() {
			if err := walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
				ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)
				b4Hash := b4H.BlockHash()
				err := putRawCFilter(ns, b4Hash[:], fakeFilter[:])
				if err != nil {
					return err
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		},
		wantErr: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var missingNo int32
			if test.do != nil {
				test.do()
			}
			err = walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
				var err error
				missingNo, err = MissingCFiltersHeight(dbtx, test.from)
				return err
			})
			if test.wantErr {
				if err == nil {
					t.Fatal("wanted error but got none")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if missingNo != test.missingNo {
				t.Fatalf("wanted missing number %v but got %v", test.missingNo, missingNo)
			}
		})
	}
}
