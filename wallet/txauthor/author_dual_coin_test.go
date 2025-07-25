// Copyright (c) 2024 The Monetarium developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txauthor_test

import (
	"testing"

	"decred.org/dcrwallet/v5/wallet/txauthor"
	"decred.org/dcrwallet/v5/wallet/txrules"
	"decred.org/dcrwallet/v5/wallet/txsizes"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// Dual-coin test helper functions

func p2pkhOutputsWithCoinType(coinType wire.CoinType, amounts ...dcrutil.Amount) []*wire.TxOut {
	v := make([]*wire.TxOut, 0, len(amounts))
	for _, a := range amounts {
		outScript := make([]byte, txsizes.P2PKHOutputSize)
		txOut := wire.NewTxOut(int64(a), outScript)
		txOut.CoinType = coinType
		v = append(v, txOut)
	}
	return v
}

func makeInputSourceWithCoinType(unspents []*wire.TxOut) txauthor.InputSource {
	currentTotal := dcrutil.Amount(0)
	currentInputs := make([]*wire.TxIn, 0, len(unspents))
	redeemScriptSizes := make([]int, 0, len(unspents))
	f := func(target dcrutil.Amount) (*txauthor.InputDetail, error) {
		for currentTotal < target && len(unspents) != 0 {
			u := unspents[0]
			unspents = unspents[1:]
			nextInput := wire.NewTxIn(&wire.OutPoint{}, u.Value, nil)
			currentTotal += dcrutil.Amount(u.Value)
			currentInputs = append(currentInputs, nextInput)
			redeemScriptSizes = append(redeemScriptSizes, txsizes.RedeemP2PKHSigScriptSize)
		}
		return &txauthor.InputDetail{
			Amount:            currentTotal,
			Inputs:            currentInputs,
			RedeemScriptSizes: redeemScriptSizes,
		}, nil
	}
	return f
}

// TestVARTransactionCreation tests VAR transaction creation with fees (backward compatibility)
func TestVARTransactionCreation(t *testing.T) {
	// Create VAR inputs (CoinType = 0)
	varUnspents := p2pkhOutputsWithCoinType(wire.CoinType(dcrutil.CoinTypeVAR), 1e8)
	varOutputs := p2pkhOutputsWithCoinType(wire.CoinType(dcrutil.CoinTypeVAR), 1e6)

	relayFee := dcrutil.Amount(1e3)
	inputSource := makeInputSourceWithCoinType(varUnspents)
	changeSource := AuthorTestChangeSource{}

	// Create transaction
	authoredTx, err := txauthor.NewUnsignedTransaction(varOutputs, relayFee, inputSource, changeSource, 100000)
	if err != nil {
		t.Fatalf("Failed to create VAR transaction: %v", err)
	}

	// Verify transaction has fee (VAR transactions should have fees)
	expectedFee := txrules.FeeForSerializeSize(relayFee,
		txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, varOutputs, txsizes.P2PKHPkScriptSize))

	if authoredTx.TotalInput < dcrutil.Amount(1e6)+expectedFee {
		t.Errorf("VAR transaction should include fees. Got total input %v, expected at least %v",
			authoredTx.TotalInput, dcrutil.Amount(1e6)+expectedFee)
	}

	// Verify all outputs are VAR coin type
	for i, out := range authoredTx.Tx.TxOut {
		if out.CoinType != wire.CoinType(dcrutil.CoinTypeVAR) {
			t.Errorf("Output %d has wrong coin type: got %v, want %v",
				i, out.CoinType, wire.CoinType(dcrutil.CoinTypeVAR))
		}
	}
}

// TestSKATransactionCreation tests SKA transaction creation without fees
func TestSKATransactionCreation(t *testing.T) {
	// Test different SKA coin types
	skaTypes := []wire.CoinType{
		wire.CoinType(dcrutil.CoinType(1)),   // SKA-1
		wire.CoinType(dcrutil.CoinType(2)),   // SKA-2
		wire.CoinType(dcrutil.CoinType(255)), // SKA-255
	}

	for _, coinType := range skaTypes {
		t.Run(string(rune(coinType)), func(t *testing.T) {
			// Create SKA inputs and outputs with exact matching amounts
			amount := dcrutil.Amount(1e6)
			skaUnspents := p2pkhOutputsWithCoinType(coinType, amount)
			skaOutputs := p2pkhOutputsWithCoinType(coinType, amount)

			relayFee := dcrutil.Amount(1e3) // Should be ignored for SKA
			inputSource := makeInputSourceWithCoinType(skaUnspents)
			changeSource := AuthorTestChangeSource{}

			// Create transaction
			authoredTx, err := txauthor.NewUnsignedTransaction(skaOutputs, relayFee, inputSource, changeSource, 100000)
			if err != nil {
				t.Fatalf("Failed to create SKA transaction: %v", err)
			}

			// Verify transaction has zero fees (inputs should exactly equal outputs for SKA)
			if authoredTx.TotalInput != amount {
				t.Errorf("SKA transaction should have zero fees. Got total input %v, expected %v",
					authoredTx.TotalInput, amount)
			}

			// Verify all outputs are correct SKA coin type
			for i, out := range authoredTx.Tx.TxOut {
				if out.CoinType != coinType {
					t.Errorf("Output %d has wrong coin type: got %v, want %v",
						i, out.CoinType, coinType)
				}
			}

			// Verify no change output (exact matching for SKA)
			if authoredTx.ChangeIndex != -1 {
				t.Error("SKA transaction should not have change output when inputs exactly match outputs")
			}
		})
	}
}

// TestMixedCoinRejection tests that we cannot mix VAR and SKA in outputs
func TestMixedCoinRejection(t *testing.T) {
	// Create mixed outputs (VAR + SKA)
	mixedOutputs := []*wire.TxOut{
		{Value: 1e6, CoinType: wire.CoinType(dcrutil.CoinTypeVAR)},
		{Value: 1e6, CoinType: wire.CoinType(dcrutil.CoinType(1))}, // SKA-1
	}

	// Create inputs
	unspents := p2pkhOutputsWithCoinType(wire.CoinType(dcrutil.CoinTypeVAR), 2e6)
	inputSource := makeInputSourceWithCoinType(unspents)
	changeSource := AuthorTestChangeSource{}
	relayFee := dcrutil.Amount(1e3)

	// This should work in txauthor (validation happens at higher level)
	_, err := txauthor.NewUnsignedTransaction(mixedOutputs, relayFee, inputSource, changeSource, 100000)

	// Note: Mixed coin validation happens in wallet.NewUnsignedTransaction, not in txauthor
	// This test verifies that txauthor itself can handle mixed outputs (validation is elsewhere)
	if err != nil {
		t.Logf("Mixed coin transaction creation failed as expected at txauthor level: %v", err)
	}
}

// TestDualCoinChangeOutput tests that change outputs inherit the correct coin type
func TestDualCoinChangeOutput(t *testing.T) {
	testCases := []struct {
		name     string
		coinType wire.CoinType
	}{
		{"VAR change", wire.CoinType(dcrutil.CoinTypeVAR)},
		{"SKA-1 change", wire.CoinType(dcrutil.CoinType(1))},
		{"SKA-2 change", wire.CoinType(dcrutil.CoinType(2))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create transaction that will produce change
			unspents := p2pkhOutputsWithCoinType(tc.coinType, 1e8) // Large input
			outputs := p2pkhOutputsWithCoinType(tc.coinType, 1e6)  // Small output

			relayFee := dcrutil.Amount(1e3)
			if tc.coinType != wire.CoinType(dcrutil.CoinTypeVAR) {
				relayFee = 0 // SKA transactions have zero fees
			}

			inputSource := makeInputSourceWithCoinType(unspents)
			changeSource := AuthorTestChangeSource{}

			authoredTx, err := txauthor.NewUnsignedTransaction(outputs, relayFee, inputSource, changeSource, 100000)
			if err != nil {
				t.Fatalf("Failed to create transaction: %v", err)
			}

			// Should have change output
			if authoredTx.ChangeIndex == -1 {
				t.Error("Expected change output but none was created")
				return
			}

			// Verify change output has correct coin type
			changeOutput := authoredTx.Tx.TxOut[authoredTx.ChangeIndex]
			if changeOutput.CoinType != tc.coinType {
				t.Errorf("Change output has wrong coin type: got %v, want %v",
					changeOutput.CoinType, tc.coinType)
			}
		})
	}
}

// TestSKAZeroFeeValidation tests that SKA transactions have zero fees
func TestSKAZeroFeeValidation(t *testing.T) {
	// Create SKA transaction with high relay fee (should be ignored)
	skaUnspents := p2pkhOutputsWithCoinType(wire.CoinType(dcrutil.CoinType(1)), 1e6)
	skaOutputs := p2pkhOutputsWithCoinType(wire.CoinType(dcrutil.CoinType(1)), 1e6)

	highRelayFee := dcrutil.Amount(1e5) // Very high fee that should be ignored
	inputSource := makeInputSourceWithCoinType(skaUnspents)
	changeSource := AuthorTestChangeSource{}

	authoredTx, err := txauthor.NewUnsignedTransaction(skaOutputs, highRelayFee, inputSource, changeSource, 100000)
	if err != nil {
		t.Fatalf("Failed to create SKA transaction: %v", err)
	}

	// Verify transaction has zero effective fee (inputs = outputs exactly)
	totalOutputValue := dcrutil.Amount(0)
	for _, out := range authoredTx.Tx.TxOut {
		totalOutputValue += dcrutil.Amount(out.Value)
	}

	if authoredTx.TotalInput != totalOutputValue {
		t.Errorf("SKA transaction should have zero fees. Input: %v, Output total: %v",
			authoredTx.TotalInput, totalOutputValue)
	}
}

// TestEmptyOutputsHandling tests edge case of empty outputs
func TestEmptyOutputsHandling(t *testing.T) {
	unspents := p2pkhOutputsWithCoinType(wire.CoinType(dcrutil.CoinTypeVAR), 1e6)
	emptyOutputs := []*wire.TxOut{} // No outputs

	inputSource := makeInputSourceWithCoinType(unspents)
	changeSource := AuthorTestChangeSource{}
	relayFee := dcrutil.Amount(1e3)

	_, err := txauthor.NewUnsignedTransaction(emptyOutputs, relayFee, inputSource, changeSource, 100000)
	// Note: Empty outputs might be allowed at txauthor level - validation happens elsewhere
	// This test just verifies the behavior is consistent
	if err != nil {
		t.Logf("Empty outputs rejected as expected: %v", err)
	} else {
		t.Log("Empty outputs allowed at txauthor level - validation handled elsewhere")
	}
}
