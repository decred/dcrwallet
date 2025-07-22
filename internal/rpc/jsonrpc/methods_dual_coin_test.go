// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// Note: These tests focus on functions that don't require a loaded wallet
// For full RPC testing, integration tests with a real wallet would be needed

// TestMakeOutputsWithCoinType tests the new coin-type aware output creation function
func TestMakeOutputsWithCoinType(t *testing.T) {
	chainParams := testChainParams()

	tests := []struct {
		name     string
		pairs    map[string]dcrutil.Amount
		coinType dcrutil.CoinType
		wantErr  bool
	}{
		{
			name: "Valid VAR outputs",
			pairs: map[string]dcrutil.Amount{
				"SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc": dcrutil.Amount(100000000), // 1 DCR
			},
			coinType: 0,
			wantErr:  false,
		},
		{
			name: "Valid SKA outputs",
			pairs: map[string]dcrutil.Amount{
				"SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc": dcrutil.Amount(50000000), // 0.5 DCR
			},
			coinType: 1,
			wantErr:  false,
		},
		{
			name: "Negative amount",
			pairs: map[string]dcrutil.Amount{
				"SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc": dcrutil.Amount(-100000000),
			},
			coinType: 0,
			wantErr:  true,
		},
		{
			name: "Invalid address",
			pairs: map[string]dcrutil.Amount{
				"invalid-address": dcrutil.Amount(100000000),
			},
			coinType: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputs, err := makeOutputsWithCoinType(tt.pairs, chainParams, tt.coinType)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				if len(outputs) != len(tt.pairs) {
					t.Errorf("Expected %d outputs, got %d", len(tt.pairs), len(outputs))
				}

				// Check that all outputs have the correct coin type
				for _, output := range outputs {
					if output.CoinType != wire.CoinType(tt.coinType) {
						t.Errorf("Expected coin type %d, got %d", tt.coinType, output.CoinType)
					}
				}
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func uint8Ptr(u uint8) *uint8 {
	return &u
}

// testChainParams returns test chain parameters
func testChainParams() *chaincfg.Params {
	return chaincfg.SimNetParams()
}