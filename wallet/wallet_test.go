// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"math"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
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
