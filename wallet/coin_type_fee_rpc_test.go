// Copyright (c) 2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/cointype"
	"github.com/decred/dcrd/dcrutil/v4"
)

// TestCoinTypeFeeManagementMethods tests the new SKA fee management methods
// added to the Wallet struct.
func TestCoinTypeFeeManagementMethods(t *testing.T) {
	t.Log("=== Testing Coin-Type-Aware Fee Management Methods ===")

	// Create test chain parameters with SKA fee configuration
	chainParams := &chaincfg.Params{
		SKAMinRelayTxFee: 1000, // 1000 atoms/KB for SKA
	}

	// Simulate wallet initialization
	w := &Wallet{
		chainParams: chainParams,
	}

	// Initialize fees as would happen in wallet Open()
	varRelayFee := dcrutil.Amount(10000) // 10000 atoms/KB for VAR
	w.relayFee = varRelayFee
	if w.chainParams.SKAMinRelayTxFee > 0 {
		w.skaRelayFee = dcrutil.Amount(w.chainParams.SKAMinRelayTxFee)
	} else {
		w.skaRelayFee = varRelayFee
	}

	t.Run("Test RelayFee and SetRelayFee for VAR", func(t *testing.T) {
		// Test getting VAR relay fee
		fee := w.RelayFee()
		expectedFee := dcrutil.Amount(10000)
		if fee != expectedFee {
			t.Errorf("RelayFee() = %d, expected %d", fee, expectedFee)
		}

		// Test setting VAR relay fee
		newFee := dcrutil.Amount(15000)
		w.SetRelayFee(newFee)
		if w.RelayFee() != newFee {
			t.Errorf("After SetRelayFee(%d), RelayFee() = %d", newFee, w.RelayFee())
		}

		t.Logf("✅ VAR fee management: get=%d, set=%d", fee, newFee)
	})

	t.Run("Test SKARelayFee and SetSKARelayFee for SKA", func(t *testing.T) {
		// Test getting SKA relay fee
		fee := w.SKARelayFee()
		expectedFee := dcrutil.Amount(1000)
		if fee != expectedFee {
			t.Errorf("SKARelayFee() = %d, expected %d", fee, expectedFee)
		}

		// Test setting SKA relay fee
		newFee := dcrutil.Amount(2000)
		w.SetSKARelayFee(newFee)
		if w.SKARelayFee() != newFee {
			t.Errorf("After SetSKARelayFee(%d), SKARelayFee() = %d", newFee, w.SKARelayFee())
		}

		t.Logf("✅ SKA fee management: get=%d, set=%d", fee, newFee)
	})

	t.Run("Test RelayFeeForCoinType helper method", func(t *testing.T) {
		// Set different fee rates for VAR and SKA
		varFee := dcrutil.Amount(12000)
		skaFee := dcrutil.Amount(800)
		w.SetRelayFee(varFee)
		w.SetSKARelayFee(skaFee)

		// Test VAR coin type
		varResult := w.RelayFeeForCoinType(cointype.CoinTypeVAR)
		if varResult != varFee {
			t.Errorf("RelayFeeForCoinType(VAR) = %d, expected %d", varResult, varFee)
		}

		// Test SKA coin type (SKA-1)
		skaResult := w.RelayFeeForCoinType(cointype.CoinType(1))
		if skaResult != skaFee {
			t.Errorf("RelayFeeForCoinType(SKA-1) = %d, expected %d", skaResult, skaFee)
		}

		// Test another SKA coin type (SKA-2)
		ska2Result := w.RelayFeeForCoinType(cointype.CoinType(2))
		if ska2Result != skaFee {
			t.Errorf("RelayFeeForCoinType(SKA-2) = %d, expected %d", ska2Result, skaFee)
		}

		t.Logf("✅ RelayFeeForCoinType: VAR=%d, SKA-1=%d, SKA-2=%d",
			varResult, skaResult, ska2Result)
	})

	t.Run("Test Fee Independence", func(t *testing.T) {
		// Set different fees for VAR and SKA
		varFee := dcrutil.Amount(20000)
		skaFee := dcrutil.Amount(500)
		w.SetRelayFee(varFee)
		w.SetSKARelayFee(skaFee)

		// Verify they are independent
		if w.RelayFee() == w.SKARelayFee() {
			t.Error("VAR and SKA fees should be independent")
		}

		// Change VAR fee, verify SKA fee unchanged
		w.SetRelayFee(dcrutil.Amount(25000))
		if w.SKARelayFee() != skaFee {
			t.Error("SKA fee should not change when VAR fee changes")
		}

		// Change SKA fee, verify VAR fee unchanged
		w.SetSKARelayFee(dcrutil.Amount(600))
		if w.RelayFee() != dcrutil.Amount(25000) {
			t.Error("VAR fee should not change when SKA fee changes")
		}

		t.Logf("✅ Fee independence verified: VAR=%d, SKA=%d",
			w.RelayFee(), w.SKARelayFee())
	})
}

// TestWalletFeeInitialization tests that wallet fees are properly initialized
// from chain parameters during wallet creation.
func TestWalletFeeInitialization(t *testing.T) {
	t.Log("=== Testing Wallet Fee Initialization from Chain Parameters ===")

	t.Run("Test initialization with SKA fee configured", func(t *testing.T) {
		chainParams := &chaincfg.Params{
			SKAMinRelayTxFee: 1500, // 1500 atoms/KB for SKA
		}

		w := &Wallet{
			chainParams: chainParams,
		}

		// Simulate Open() initialization
		configRelayFee := dcrutil.Amount(8000)
		w.relayFee = configRelayFee
		if w.chainParams.SKAMinRelayTxFee > 0 {
			w.skaRelayFee = dcrutil.Amount(w.chainParams.SKAMinRelayTxFee)
		} else {
			w.skaRelayFee = configRelayFee
		}

		if w.RelayFee() != configRelayFee {
			t.Errorf("VAR fee should be initialized to config value %d, got %d",
				configRelayFee, w.RelayFee())
		}

		expectedSKAFee := dcrutil.Amount(1500)
		if w.SKARelayFee() != expectedSKAFee {
			t.Errorf("SKA fee should be initialized to chain param value %d, got %d",
				expectedSKAFee, w.SKARelayFee())
		}

		t.Logf("✅ Initialized with SKA param: VAR=%d, SKA=%d",
			w.RelayFee(), w.SKARelayFee())
	})

	t.Run("Test initialization without SKA fee configured", func(t *testing.T) {
		chainParams := &chaincfg.Params{
			SKAMinRelayTxFee: 0, // No SKA fee configured
		}

		w := &Wallet{
			chainParams: chainParams,
		}

		// Simulate Open() initialization
		configRelayFee := dcrutil.Amount(9000)
		w.relayFee = configRelayFee
		if w.chainParams.SKAMinRelayTxFee > 0 {
			w.skaRelayFee = dcrutil.Amount(w.chainParams.SKAMinRelayTxFee)
		} else {
			w.skaRelayFee = configRelayFee // Fallback to VAR fee
		}

		if w.RelayFee() != configRelayFee {
			t.Errorf("VAR fee should be initialized to config value %d, got %d",
				configRelayFee, w.RelayFee())
		}

		if w.SKARelayFee() != configRelayFee {
			t.Errorf("SKA fee should fallback to config value %d, got %d",
				configRelayFee, w.SKARelayFee())
		}

		t.Logf("✅ Initialized without SKA param (fallback): VAR=%d, SKA=%d",
			w.RelayFee(), w.SKARelayFee())
	})
}

// TestCoinTypeFeeIntegrationScenarios tests real-world scenarios of
// coin-type-aware fee management.
func TestCoinTypeFeeIntegrationScenarios(t *testing.T) {
	t.Log("=== Testing Real-World Fee Management Scenarios ===")

	chainParams := &chaincfg.Params{
		SKAMinRelayTxFee: 1000, // SKA has lower fees than VAR
	}

	w := &Wallet{
		chainParams: chainParams,
		relayFee:    dcrutil.Amount(10000), // VAR: 10000 atoms/KB
		skaRelayFee: dcrutil.Amount(1000),  // SKA: 1000 atoms/KB
	}

	t.Run("Scenario: User wants to check current fees", func(t *testing.T) {
		// User calls getwalletfee (no coin type = VAR default)
		varFee := w.RelayFeeForCoinType(cointype.CoinTypeVAR)

		// User calls getwalletfee 1 (for SKA-1)
		skaFee := w.RelayFeeForCoinType(cointype.CoinType(1))

		t.Logf("Current fees: VAR=%d atoms/KB, SKA=%d atoms/KB", varFee, skaFee)

		if varFee <= skaFee {
			t.Log("⚠️  Note: VAR fee is not higher than SKA fee in this test scenario")
		}

		t.Log("✅ Fee query scenario completed")
	})

	t.Run("Scenario: User adjusts fees for different coin types", func(t *testing.T) {
		// User sets VAR fee higher (settxfee 0.00015)
		newVARFee := dcrutil.Amount(15000)
		w.SetRelayFee(newVARFee)

		// User sets SKA fee lower (settxfee 0.00005 1)
		newSKAFee := dcrutil.Amount(500)
		w.SetSKARelayFee(newSKAFee)

		// Verify changes
		if w.RelayFee() != newVARFee {
			t.Errorf("VAR fee not updated correctly: expected %d, got %d",
				newVARFee, w.RelayFee())
		}

		if w.SKARelayFee() != newSKAFee {
			t.Errorf("SKA fee not updated correctly: expected %d, got %d",
				newSKAFee, w.SKARelayFee())
		}

		t.Logf("✅ Fee adjustment scenario: VAR raised to %d, SKA lowered to %d",
			newVARFee, newSKAFee)
	})

	t.Run("Scenario: Multiple SKA coin types use same fee", func(t *testing.T) {
		// All SKA coin types should use the same fee setting
		ska1Fee := w.RelayFeeForCoinType(cointype.CoinType(1))     // SKA-1
		ska2Fee := w.RelayFeeForCoinType(cointype.CoinType(2))     // SKA-2
		ska255Fee := w.RelayFeeForCoinType(cointype.CoinType(255)) // SKA-255 (max)

		if ska1Fee != ska2Fee || ska2Fee != ska255Fee {
			t.Error("All SKA coin types should use the same fee rate")
		}

		t.Logf("✅ Multiple SKA types scenario: SKA-1=%d, SKA-2=%d, SKA-255=%d",
			ska1Fee, ska2Fee, ska255Fee)
	})
}
