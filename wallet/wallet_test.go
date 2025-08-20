// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
	"time"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/udb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
)

func TestCoinbaseMatured(t *testing.T) {
	t.Parallel()
	params := chaincfg.MainNetParams()
	maturity := int32(params.CoinbaseMaturity)
	tests := []struct {
		txHeight, tipHeight int32
		matured             bool
	}{
		{0, 0, false},
		{0, maturity - 1, false},
		{0, maturity, true},
		{0, maturity + 1, true},
		{1, 0, false},
		{maturity, 0, false},
		{0, -1, false},
		{-1, maturity, false},
		{-1, math.MaxInt32, false},
	}

	for i, test := range tests {
		result := coinbaseMatured(params, test.txHeight, test.tipHeight)
		if result != test.matured {
			t.Errorf("test %d: result (%v) != expected (%v)", i, result, test.matured)
		}
	}
}

func TestTicketMatured(t *testing.T) {
	t.Parallel()
	params := chaincfg.MainNetParams()
	maturity := int32(params.TicketMaturity)
	tests := []struct {
		txHeight, tipHeight int32
		matured             bool
	}{
		{0, 0, false},
		{0, maturity - 1, false},
		{0, maturity, false}, // dcrd off-by-one results in this being false
		{0, maturity + 1, true},
		{1, 0, false},
		{maturity, 0, false},
		{0, -1, false},
		{-1, maturity, false},
		{-1, math.MaxInt32, false},
	}

	for i, test := range tests {
		result := ticketMatured(params, test.txHeight, test.tipHeight)
		if result != test.matured {
			t.Errorf("test %d: result (%v) != expected (%v)", i, result, test.matured)
		}
	}
}

func TestTicketExpired(t *testing.T) {
	t.Parallel()
	params := chaincfg.MainNetParams()
	expiry := int32(params.TicketMaturity) + int32(params.TicketExpiry)
	tests := []struct {
		txHeight, tipHeight int32
		expired             bool
	}{
		{0, 0, false},
		{0, expiry - 1, false},
		{0, expiry, false}, // dcrd off-by-one results in this being false
		{0, expiry + 1, true},
		{1, 0, false},
		{expiry, 0, false},
		{0, -1, false},
		{-1, expiry, false},
		{-1, math.MaxInt32, false},
	}

	for i, test := range tests {
		result := ticketExpired(params, test.txHeight, test.tipHeight)
		if result != test.expired {
			t.Errorf("test %d: result (%v) != expected (%v)", i, result, test.expired)
		}
	}
}

// TestVotingXprivFromSeed tests that creating voting xprivs works properly.
func TestVotingXprivFromSeed(t *testing.T) {
	seed, err := hex.DecodeString("0000000000000000000000000000000000000" +
		"000000000000000000000000000")
	if err != nil {
		t.Fatalf("unable to create seed: %v", err)
	}
	wantXpriv := "dprv3oaf1h1hLxLZNHn6Gn9AaUaMJxhpHAj2KGMMHTi2AnoiHdRhCc" +
		"iKbwf3dB6zpPEq8ffdT4NZ7gjtrZBQhSWDm2RVXbphmpdPnbq299ddB8a"

	tests := []struct {
		name, want string
		seed       []byte
		wantErr    *errors.Error
	}{{
		name: "ok",
		seed: seed,
		want: wantXpriv,
	}, {
		name:    "bad seed",
		seed:    seed[:1],
		wantErr: &errors.Error{Kind: errors.Invalid},
	}}

	for _, test := range tests {
		got, err := votingXprivFromSeed(test.seed, chaincfg.MainNetParams())
		if test.wantErr != nil {
			if err == nil {
				t.Fatalf("wanted error %v but got none", test.wantErr)
			}
			kind := err.(*errors.Error).Kind
			if !test.wantErr.Is(kind) {
				t.Fatalf("wanted error %v but got %v", test.wantErr, kind)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.String() != test.want {
			t.Fatalf("wanted xpriv %v but got %v", test.want, got)
		}
	}
}

func TestSetBirthStateAndScan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := basicWalletConfig
	w, teardown := testWallet(ctx, t, &cfg, nil)
	defer teardown()

	before := time.Now().Add(-time.Minute)

	tg := maketg(t, cfg.Params)
	tw := &tw{t, w}
	forest := new(SidechainForest)

	for i := 1; i < 10; i++ {
		name := fmt.Sprintf("%va", i)
		b := tg.nextBlock(name, nil, nil)
		mustAddBlockNode(t, forest, b.BlockNode)
		t.Logf("Generated block %v name %q", b.Hash, name)
	}
	b9aHash := tg.blockHashByName("9a")
	bestChain := tw.evaluateBestChain(ctx, forest, 9, b9aHash)
	tw.chainSwitch(ctx, forest, bestChain)
	tw.assertNoBetterChain(ctx, forest)

	tests := []struct {
		name      string
		bs        *udb.BirthdayState
		wantBHash *chainhash.Hash
		wantErr   bool
	}{{
		name: "ok middle",
		bs: &udb.BirthdayState{
			SetFromHeight: true,
			Height:        6,
		},
		wantBHash: tg.blockHashByName("6a"),
	}, {
		name: "ok genesis",
		bs: &udb.BirthdayState{
			SetFromHeight: true,
			Height:        0,
		},
		wantBHash: &cfg.Params.GenesisHash,
	}, {
		name: "ok from now",
		bs: &udb.BirthdayState{
			SetFromTime: true,
			Time:        time.Now().Add(time.Minute),
		},
		wantBHash: b9aHash,
	}, {
		name: "ok from before",
		bs: &udb.BirthdayState{
			SetFromTime: true,
			Time:        before,
		},
		wantBHash: &cfg.Params.GenesisHash, // genesis is timestamped 2014
	}, {
		name:    "nothing to set",
		bs:      new(udb.BirthdayState),
		wantErr: true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := w.SetBirthStateAndScan(ctx, test.bs)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			bs, err := w.BirthState(ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bs.Hash != *test.wantBHash {
				t.Fatalf("wanted birthday hash %v but got %v", test.wantBHash, bs.Hash)
			}
		})
	}
}

func TestRescanFromHeight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cfg := basicWalletConfig
	w, teardown := testWallet(ctx, t, &cfg, nil)
	defer teardown()

	tg := maketg(t, cfg.Params)
	tw := &tw{t, w}
	forest := new(SidechainForest)

	for i := 1; i < 10; i++ {
		name := fmt.Sprintf("%va", i)
		b := tg.nextBlock(name, nil, nil)
		mustAddBlockNode(t, forest, b.BlockNode)
		t.Logf("Generated block %v name %q", b.Hash, name)
	}
	b9aHash := tg.blockHashByName("9a")
	bestChain := tw.evaluateBestChain(ctx, forest, 9, b9aHash)
	tw.chainSwitch(ctx, forest, bestChain)
	tw.assertNoBetterChain(ctx, forest)

	tests := []struct {
		name string
		bs   *udb.BirthdayState
	}{{
		name: "ok no birthday",
	}, {
		name: "ok birthday",
		bs: &udb.BirthdayState{
			SetFromHeight: true,
			Height:        5,
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.bs != nil {
				err := w.SetBirthState(ctx, test.bs)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			err := w.RescanFromHeight(ctx, mockNetwork{}, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
