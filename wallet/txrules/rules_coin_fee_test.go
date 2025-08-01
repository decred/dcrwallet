// Copyright (c) 2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txrules

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// TestFeeForSerializeSizeWithChainParams tests the new coin-type-aware fee calculation.
func TestFeeForSerializeSizeWithChainParams(t *testing.T) {
	// Test parameters similar to simnet
	chainParams := &chaincfg.Params{
		SKAMinRelayTxFee: 1000, // 1000 atoms/KB for SKA (lower than VAR)
	}
	
	varRelayFee := dcrutil.Amount(10000) // 10000 atoms/KB for VAR
	txSize := 250 // 250 byte transaction
	
	tests := []struct {
		name         string
		coinType     dcrutil.CoinType
		expectedFee  dcrutil.Amount
		description  string
	}{
		{
			name:         "VAR transaction fee",
			coinType:     dcrutil.CoinTypeVAR,
			expectedFee:  varRelayFee * dcrutil.Amount(txSize) / 1000, // 2500 atoms
			description:  "VAR transactions should use provided relay fee rate",
		},
		{
			name:         "SKA transaction fee",
			coinType:     dcrutil.CoinTypeSKA,
			expectedFee:  1000 * dcrutil.Amount(txSize) / 1000, // 250 atoms
			description:  "SKA transactions should use chain-specific fee rate",
		},
		{
			name:         "Unknown coin type",
			coinType:     dcrutil.CoinType(99),
			expectedFee:  1000 * dcrutil.Amount(txSize) / 1000, // 250 atoms (uses SKA rate)
			description:  "Unknown coin types should use SKA fee rate",
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actualFee := FeeForSerializeSizeWithChainParams(varRelayFee, txSize, test.coinType, chainParams)
			
			if actualFee != test.expectedFee {
				t.Errorf("%s: expected fee %d atoms, got %d atoms", 
					test.description, test.expectedFee, actualFee)
			}
			
			t.Logf("%s: calculated fee %d atoms for %d byte transaction", 
				test.name, actualFee, txSize)
		})
	}
}

// TestFeeForSerializeSizeWithChainParamsNoSKAFee tests fallback behavior when no SKA fee is configured.
func TestFeeForSerializeSizeWithChainParamsNoSKAFee(t *testing.T) {
	// Test parameters with no SKA fee configured
	chainParams := &chaincfg.Params{
		SKAMinRelayTxFee: 0, // No SKA fee configured
	}
	
	varRelayFee := dcrutil.Amount(10000)
	txSize := 250
	expectedFee := varRelayFee * dcrutil.Amount(txSize) / 1000 // Should fallback to VAR fee
	
	actualFee := FeeForSerializeSizeWithChainParams(varRelayFee, txSize, dcrutil.CoinTypeSKA, chainParams)
	
	if actualFee != expectedFee {
		t.Errorf("SKA transaction with no configured fee should fallback to VAR fee: expected %d, got %d",
			expectedFee, actualFee)
	}
}

// TestGetPrimaryCoinTypeFromOutputs tests coin type detection from transaction outputs.
func TestGetPrimaryCoinTypeFromOutputs(t *testing.T) {
	tests := []struct {
		name     string
		outputs  []*wire.TxOut
		expected dcrutil.CoinType
	}{
		{
			name: "All VAR outputs",
			outputs: []*wire.TxOut{
				{CoinType: wire.CoinType(dcrutil.CoinTypeVAR), Value: 1000},
				{CoinType: wire.CoinType(dcrutil.CoinTypeVAR), Value: 2000},
			},
			expected: dcrutil.CoinTypeVAR,
		},
		{
			name: "Mixed outputs - SKA first",
			outputs: []*wire.TxOut{
				{CoinType: wire.CoinType(dcrutil.CoinTypeSKA), Value: 1000},
				{CoinType: wire.CoinType(dcrutil.CoinTypeVAR), Value: 2000},
			},
			expected: dcrutil.CoinTypeSKA,
		},
		{
			name: "Mixed outputs - VAR first",
			outputs: []*wire.TxOut{
				{CoinType: wire.CoinType(dcrutil.CoinTypeVAR), Value: 1000},
				{CoinType: wire.CoinType(dcrutil.CoinTypeSKA), Value: 2000},
			},
			expected: dcrutil.CoinTypeSKA, // Should return first non-VAR coin type
		},
		{
			name: "Multiple SKA types",
			outputs: []*wire.TxOut{
				{CoinType: wire.CoinType(1), Value: 1000}, // SKA-1
				{CoinType: wire.CoinType(2), Value: 2000}, // SKA-2
			},
			expected: dcrutil.CoinType(1), // Should return first non-VAR coin type
		},
		{
			name:     "No outputs",
			outputs:  nil,
			expected: dcrutil.CoinTypeVAR, // Default to VAR
		},
	}
	
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := GetPrimaryCoinTypeFromOutputs(test.outputs)
			if actual != test.expected {
				t.Errorf("Expected coin type %d, got %d", test.expected, actual)
			}
		})
	}
}

// TestSKAFeeDesign verifies that SKA transactions pay fees in their own coin type.
func TestSKAFeeDesign(t *testing.T) {
	relayFee := dcrutil.Amount(10000)
	txSize := 250
	
	// Test that SKA transactions pay fees in their own coin type
	skaFee := FeeForSerializeSizeDualCoin(relayFee, txSize, dcrutil.CoinTypeSKA)
	
	// Should be same calculation as VAR (fee paid in SKA coins)
	expectedFee := relayFee * dcrutil.Amount(txSize) / 1000
	if expectedFee == 0 && relayFee > 0 {
		expectedFee = relayFee
	}
	
	if skaFee != expectedFee {
		t.Errorf("SKA transactions should pay fees in SKA coins using same calculation as VAR, expected %d, got %d", expectedFee, skaFee)
	}
	
	// VAR should have same calculation
	varFee := FeeForSerializeSizeDualCoin(relayFee, txSize, dcrutil.CoinTypeVAR)
	if varFee != expectedFee {
		t.Errorf("VAR and SKA should use same fee calculation, VAR=%d, SKA=%d",
			varFee, skaFee)
	}
	
	t.Logf("Fixed fee calculation: VAR=%d atoms, SKA=%d atoms (no longer zero)", varFee, skaFee)
}