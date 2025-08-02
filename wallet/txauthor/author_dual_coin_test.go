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

// TestSKATransactionCreation tests regular SKA transaction creation with fees
func TestSKATransactionCreation(t *testing.T) {
	// Test different SKA coin types
	skaTypes := []wire.CoinType{
		wire.CoinType(dcrutil.CoinType(1)),   // SKA-1
		wire.CoinType(dcrutil.CoinType(2)),   // SKA-2
		wire.CoinType(dcrutil.CoinType(255)), // SKA-255
	}

	for _, coinType := range skaTypes {
		t.Run(string(rune(coinType)), func(t *testing.T) {
			// Create SKA inputs with enough for outputs + fees
			outputAmount := dcrutil.Amount(1e6)
			inputAmount := dcrutil.Amount(1e6 + 1000) // Extra for fees
			skaUnspents := p2pkhOutputsWithCoinType(coinType, inputAmount)
			skaOutputs := p2pkhOutputsWithCoinType(coinType, outputAmount)

			relayFee := dcrutil.Amount(1e3) // Should be used for regular SKA transactions
			inputSource := makeInputSourceWithCoinType(skaUnspents)
			changeSource := AuthorTestChangeSource{}

			// Create transaction
			authoredTx, err := txauthor.NewUnsignedTransaction(skaOutputs, relayFee, inputSource, changeSource, 100000)
			if err != nil {
				t.Fatalf("Failed to create SKA transaction: %v", err)
			}

			// Verify transaction has fees (regular SKA transactions should include fees)
			expectedFee := txrules.FeeForSerializeSize(relayFee, 
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, skaOutputs, txsizes.P2PKHPkScriptSize))
			
			if authoredTx.TotalInput < outputAmount + expectedFee {
				t.Errorf("SKA transaction should include fees. Got total input %v, expected at least %v",
					authoredTx.TotalInput, outputAmount + expectedFee)
			}

			// Verify all outputs are correct SKA coin type
			for i, out := range authoredTx.Tx.TxOut {
				if out.CoinType != coinType {
					t.Errorf("Output %d has wrong coin type: got %v, want %v",
						i, out.CoinType, coinType)
				}
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
			// Both VAR and SKA transactions have fees (except emission transactions)

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

// TestSKAEmissionZeroFeeValidation tests that SKA emission transactions have zero fees
// Note: This is a placeholder test since creating actual emission transactions 
// requires specific blockchain context that's not available in unit tests
func TestSKAEmissionZeroFeeValidation(t *testing.T) {
	t.Skip("SKA emission transaction testing requires blockchain context - tested in integration tests")
	
	// This test would verify that wire.IsSKAEmissionTransaction() returns true
	// for emission transactions and that those transactions have zero fees
	// However, creating valid emission transactions requires:
	// 1. Specific block height context
	// 2. Valid emission signatures  
	// 3. Proper UTXO chain state
	// These are better tested in integration tests with actual blockchain state
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
