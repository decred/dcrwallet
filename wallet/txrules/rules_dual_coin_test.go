// Copyright (c) 2024 The Monetarium developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txrules_test

import (
	"testing"

	"decred.org/dcrwallet/v5/wallet/txrules"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// TestFeeForSerializeSizeDualCoin tests coin-type aware fee calculation
func TestFeeForSerializeSizeDualCoin(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e3)
	txSize := 250 // bytes

	testCases := []struct {
		name        string
		coinType    dcrutil.CoinType
		expectedFee dcrutil.Amount
		description string
	}{
		{
			name:        "VAR transaction",
			coinType:    dcrutil.CoinTypeVAR,
			expectedFee: txrules.FeeForSerializeSize(relayFeePerKb, txSize), // Normal fee
			description: "VAR should use standard fee calculation",
		},
		{
			name:        "SKA-1 transaction",
			coinType:    dcrutil.CoinType(1),
			expectedFee: 0, // Zero fee
			description: "SKA transactions should have zero fees",
		},
		{
			name:        "SKA-255 transaction",
			coinType:    dcrutil.CoinType(255),
			expectedFee: 0, // Zero fee
			description: "All SKA types should have zero fees",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fee := txrules.FeeForSerializeSizeDualCoin(relayFeePerKb, txSize, tc.coinType)
			if fee != tc.expectedFee {
				t.Errorf("FeeForSerializeSizeDualCoin(%v, %d, %v) = %v, want %v. %s",
					relayFeePerKb, txSize, tc.coinType, fee, tc.expectedFee, tc.description)
			}
		})
	}
}

// TestIsDustAmountDualCoin tests dual-coin dust amount logic
func TestIsDustAmountDualCoin(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e3)
	scriptSize := 25 // P2PKH script size

	testCases := []struct {
		name         string
		amount       dcrutil.Amount
		coinType     dcrutil.CoinType
		expectedDust bool
		description  string
	}{
		// VAR tests - should use normal dust calculation
		{
			name:         "VAR normal amount",
			amount:       dcrutil.Amount(1e6), // 0.01 DCR
			coinType:     dcrutil.CoinTypeVAR,
			expectedDust: false,
			description:  "Normal VAR amount should not be dust",
		},
		{
			name:         "VAR dust amount",
			amount:       dcrutil.Amount(1), // 1 atom
			coinType:     dcrutil.CoinTypeVAR,
			expectedDust: true,
			description:  "Very small VAR amount should be dust",
		},
		{
			name:         "VAR zero amount",
			amount:       dcrutil.Amount(0),
			coinType:     dcrutil.CoinTypeVAR,
			expectedDust: true,
			description:  "Zero VAR amount should be dust",
		},
		// SKA tests - simplified dust logic (only check > 0)
		{
			name:         "SKA normal amount",
			amount:       dcrutil.Amount(1e6),
			coinType:     dcrutil.CoinType(1),
			expectedDust: false,
			description:  "Normal SKA amount should not be dust",
		},
		{
			name:         "SKA small amount",
			amount:       dcrutil.Amount(1), // Would be dust for VAR but not SKA
			coinType:     dcrutil.CoinType(1),
			expectedDust: false,
			description:  "Small SKA amount should not be dust (no fee-based dust)",
		},
		{
			name:         "SKA zero amount",
			amount:       dcrutil.Amount(0),
			coinType:     dcrutil.CoinType(1),
			expectedDust: true,
			description:  "Zero SKA amount should be dust",
		},
		{
			name:         "SKA negative amount",
			amount:       dcrutil.Amount(-1),
			coinType:     dcrutil.CoinType(2),
			expectedDust: true,
			description:  "Negative SKA amount should be dust",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isDust := txrules.IsDustAmountDualCoin(tc.amount, scriptSize, relayFeePerKb, tc.coinType)
			if isDust != tc.expectedDust {
				t.Errorf("IsDustAmountDualCoin(%v, %d, %v, %v) = %v, want %v. %s",
					tc.amount, scriptSize, relayFeePerKb, tc.coinType, isDust, tc.expectedDust, tc.description)
			}
		})
	}
}

// TestIsDustOutputDualCoin tests dual-coin output dust validation
func TestIsDustOutputDualCoin(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e3)
	scriptSize := 25

	testCases := []struct {
		name         string
		output       *wire.TxOut
		expectedDust bool
		description  string
	}{
		// VAR outputs
		{
			name: "VAR normal output",
			output: &wire.TxOut{
				Value:    int64(1e6),
				CoinType: wire.CoinType(dcrutil.CoinTypeVAR),
				PkScript: make([]byte, scriptSize),
			},
			expectedDust: false,
			description:  "Normal VAR output should not be dust",
		},
		{
			name: "VAR dust output",
			output: &wire.TxOut{
				Value:    int64(1),
				CoinType: wire.CoinType(dcrutil.CoinTypeVAR),
				PkScript: make([]byte, scriptSize),
			},
			expectedDust: true,
			description:  "Small VAR output should be dust",
		},
		// SKA outputs
		{
			name: "SKA normal output",
			output: &wire.TxOut{
				Value:    int64(1e6),
				CoinType: wire.CoinType(dcrutil.CoinType(1)),
				PkScript: make([]byte, scriptSize),
			},
			expectedDust: false,
			description:  "Normal SKA output should not be dust",
		},
		{
			name: "SKA small output",
			output: &wire.TxOut{
				Value:    int64(1), // Would be dust for VAR
				CoinType: wire.CoinType(dcrutil.CoinType(1)),
				PkScript: make([]byte, scriptSize),
			},
			expectedDust: false,
			description:  "Small SKA output should not be dust (no fee-based dust)",
		},
		{
			name: "SKA zero output",
			output: &wire.TxOut{
				Value:    int64(0),
				CoinType: wire.CoinType(dcrutil.CoinType(2)),
				PkScript: make([]byte, scriptSize),
			},
			expectedDust: true,
			description:  "Zero SKA output should be dust",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isDust := txrules.IsDustOutputDualCoin(tc.output, relayFeePerKb)
			if isDust != tc.expectedDust {
				t.Errorf("IsDustOutputDualCoin(%v, %v) = %v, want %v. %s",
					tc.output.Value, tc.output.CoinType, isDust, tc.expectedDust, tc.description)
			}
		})
	}
}

// TestVARFeeCalculation ensures VAR maintains standard fee logic
func TestVARFeeCalculation(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e4) // 0.0001 DCR/kB

	testSizes := []int{250, 500, 1000, 2000}

	for _, size := range testSizes {
		t.Run(string(rune(size)), func(t *testing.T) {
			// Calculate fee using both methods
			standardFee := txrules.FeeForSerializeSize(relayFeePerKb, size)
			dualCoinFee := txrules.FeeForSerializeSizeDualCoin(relayFeePerKb, size, dcrutil.CoinTypeVAR)

			// Should be identical for VAR
			if standardFee != dualCoinFee {
				t.Errorf("VAR fee calculation mismatch for size %d: standard=%v, dual-coin=%v",
					size, standardFee, dualCoinFee)
			}

			// Verify fee is reasonable (not zero unless relay fee is zero)
			if relayFeePerKb > 0 && dualCoinFee == 0 {
				t.Errorf("VAR fee should not be zero when relay fee is %v", relayFeePerKb)
			}
		})
	}
}

// TestSKANoFeeLogic verifies SKA has no fee requirements
func TestSKANoFeeLogic(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e4) // High fee rate

	skaTypes := []dcrutil.CoinType{
		dcrutil.CoinType(1),   // SKA-1
		dcrutil.CoinType(2),   // SKA-2
		dcrutil.CoinType(128), // SKA-128
		dcrutil.CoinType(255), // SKA-255
	}

	testSizes := []int{100, 250, 500, 1000, 5000}

	for _, coinType := range skaTypes {
		for _, size := range testSizes {
			t.Run(string(rune(coinType))+"/size"+string(rune(size)), func(t *testing.T) {
				fee := txrules.FeeForSerializeSizeDualCoin(relayFeePerKb, size, coinType)

				// SKA should always have zero fees regardless of size or relay fee
				if fee != 0 {
					t.Errorf("SKA-%d fee should be zero for size %d, got %v",
						coinType, size, fee)
				}
			})
		}
	}
}

// TestDustEdgeCases tests edge cases in dust calculation
func TestDustEdgeCases(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e3)

	testCases := []struct {
		name         string
		amount       dcrutil.Amount
		scriptSize   int
		coinType     dcrutil.CoinType
		expectedDust bool
		description  string
	}{
		{
			name:         "Large script VAR",
			amount:       dcrutil.Amount(1000),
			scriptSize:   1000, // Very large script
			coinType:     dcrutil.CoinTypeVAR,
			expectedDust: true, // Large script increases dust threshold
			description:  "VAR with large script should be dust due to high cost",
		},
		{
			name:         "Large script SKA",
			amount:       dcrutil.Amount(1000),
			scriptSize:   1000, // Very large script
			coinType:     dcrutil.CoinType(1),
			expectedDust: false, // SKA ignores script size for dust
			description:  "SKA with large script should not be dust",
		},
		{
			name:         "Zero relay fee VAR",
			amount:       dcrutil.Amount(1),
			scriptSize:   25,
			coinType:     dcrutil.CoinTypeVAR,
			expectedDust: false, // No relay fee means no dust threshold
			description:  "VAR with zero relay fee should not have dust threshold",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testRelayFee := relayFeePerKb
			if tc.name == "Zero relay fee VAR" {
				testRelayFee = 0
			}

			isDust := txrules.IsDustAmountDualCoin(tc.amount, tc.scriptSize, testRelayFee, tc.coinType)
			if isDust != tc.expectedDust {
				t.Errorf("IsDustAmountDualCoin(%v, %d, %v, %v) = %v, want %v. %s",
					tc.amount, tc.scriptSize, testRelayFee, tc.coinType, isDust, tc.expectedDust, tc.description)
			}
		})
	}
}

// TestBackwardCompatibility ensures existing VAR behavior is unchanged
func TestBackwardCompatibility(t *testing.T) {
	relayFeePerKb := dcrutil.Amount(1e3)
	scriptSize := 25

	// Test amounts that should have same behavior for VAR in both old and new logic
	testAmounts := []dcrutil.Amount{
		0, 1, 100, 1000, 1e4, 1e6, 1e8,
	}

	for _, amount := range testAmounts {
		t.Run(string(rune(amount)), func(t *testing.T) {
			// Old dust calculation (standard)
			oldIsDust := txrules.IsDustAmount(amount, scriptSize, relayFeePerKb)

			// New dual-coin calculation for VAR
			newIsDust := txrules.IsDustAmountDualCoin(amount, scriptSize, relayFeePerKb, dcrutil.CoinTypeVAR)

			if oldIsDust != newIsDust {
				t.Errorf("Backward compatibility broken for VAR amount %v: old=%v, new=%v",
					amount, oldIsDust, newIsDust)
			}
		})
	}
}
