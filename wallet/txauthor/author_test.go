// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txauthor_test

import (
	"testing"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/wallet/txauthor"
	. "decred.org/dcrwallet/v2/wallet/txauthor"
	"decred.org/dcrwallet/v2/wallet/txrules"
	"decred.org/dcrwallet/v2/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

type AuthorTestChangeSource struct{}

func (src AuthorTestChangeSource) Script() ([]byte, uint16, error) {
	// Only length matters for these tests.
	return make([]byte, txsizes.P2PKHPkScriptSize), 0, nil
}

func (src AuthorTestChangeSource) ScriptSize() int {
	return txsizes.P2PKHPkScriptSize
}

func p2pkhOutputs(amounts ...dcrutil.Amount) []*wire.TxOut {
	v := make([]*wire.TxOut, 0, len(amounts))
	for _, a := range amounts {
		outScript := make([]byte, txsizes.P2PKHOutputSize)
		v = append(v, wire.NewTxOut(int64(a), outScript))
	}
	return v
}

func makeInputSource(unspents []*wire.TxOut) InputSource {
	// Return outputs in order.
	currentTotal := dcrutil.Amount(0)
	currentInputs := make([]*wire.TxIn, 0, len(unspents))
	redeemScriptSizes := make([]int, 0, len(unspents))
	f := func(target dcrutil.Amount) (*InputDetail, error) {
		for currentTotal < target && len(unspents) != 0 {
			u := unspents[0]
			unspents = unspents[1:]
			nextInput := wire.NewTxIn(&wire.OutPoint{}, u.Value, nil)
			currentTotal += dcrutil.Amount(u.Value)
			currentInputs = append(currentInputs, nextInput)
			redeemScriptSizes = append(redeemScriptSizes, txsizes.RedeemP2PKHSigScriptSize)
		}

		inputDetail := txauthor.InputDetail{
			Amount:            currentTotal,
			Inputs:            currentInputs,
			Scripts:           make([][]byte, len(currentInputs)),
			RedeemScriptSizes: redeemScriptSizes,
		}
		return &inputDetail, nil
	}
	return InputSource(f)
}

func TestNewUnsignedTransaction(t *testing.T) {
	tests := []struct {
		UnspentOutputs   []*wire.TxOut
		Outputs          []*wire.TxOut
		RelayFee         dcrutil.Amount
		ChangeAmount     dcrutil.Amount
		InputSourceError bool
		InputCount       int
	}{
		0: {
			UnspentOutputs:   p2pkhOutputs(1e8),
			Outputs:          p2pkhOutputs(1e8),
			RelayFee:         1e3,
			InputSourceError: true,
		},
		1: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs:        p2pkhOutputs(1e6),
			RelayFee:       1e3,
			ChangeAmount: 1e8 - 1e6 - txrules.FeeForSerializeSize(1e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(1e6), txsizes.P2PKHPkScriptSize)),
			InputCount: 1,
		},
		2: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs:        p2pkhOutputs(1e6),
			RelayFee:       1e4,
			ChangeAmount: 1e8 - 1e6 - txrules.FeeForSerializeSize(1e4,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(1e6), txsizes.P2PKHPkScriptSize)),
			InputCount: 1,
		},
		3: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs:        p2pkhOutputs(1e6, 1e6, 1e6),
			RelayFee:       1e4,
			ChangeAmount: 1e8 - 3e6 - txrules.FeeForSerializeSize(1e4,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(1e6, 1e6, 1e6), txsizes.P2PKHPkScriptSize)),
			InputCount: 1,
		},
		4: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs:        p2pkhOutputs(1e6, 1e6, 1e6),
			RelayFee:       2.55e3,
			ChangeAmount: 1e8 - 3e6 - txrules.FeeForSerializeSize(2.55e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(1e6, 1e6, 1e6), txsizes.P2PKHPkScriptSize)),
			InputCount: 1,
		},

		// Test dust thresholds (603 for a 1e3 relay fee).
		5: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs: p2pkhOutputs(1e8 - 602 - txrules.FeeForSerializeSize(1e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(0), txsizes.P2PKHPkScriptSize))),
			RelayFee:     1e3,
			ChangeAmount: 0,
			InputCount:   1,
		},
		6: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs: p2pkhOutputs(1e8 - 603 - txrules.FeeForSerializeSize(1e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(0), txsizes.P2PKHPkScriptSize))),
			RelayFee:     1e3,
			ChangeAmount: 603,
			InputCount:   1,
		},

		// Test dust thresholds (1537.65 for a 2.55e3 relay fee).
		7: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs: p2pkhOutputs(1e8 - 1537 - txrules.FeeForSerializeSize(2.55e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(0), txsizes.P2PKHPkScriptSize))),
			RelayFee:     2.55e3,
			ChangeAmount: 0,
			InputCount:   1,
		},
		8: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs: p2pkhOutputs(1e8 - 1538 - txrules.FeeForSerializeSize(2.55e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(0), txsizes.P2PKHPkScriptSize))),
			RelayFee:     2.55e3,
			ChangeAmount: 1538,
			InputCount:   1,
		},

		// Test two unspent outputs available but only one needed
		// (tested fee only includes one input rather than using a
		// serialize size for each).
		9: {
			UnspentOutputs: p2pkhOutputs(1e8, 1e8),
			Outputs: p2pkhOutputs(1e8 - 603 - txrules.FeeForSerializeSize(1e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(0), txsizes.P2PKHPkScriptSize))),
			RelayFee:     1e3,
			ChangeAmount: 603,
			InputCount:   1,
		},

		// Test that second output is not included to make the change
		// output not dust and be included in the transaction.
		//
		// It's debatable whether or not this is a good idea, but it's
		// how the function was written, so test it anyways.
		10: {
			UnspentOutputs: p2pkhOutputs(1e8, 1e8),
			Outputs: p2pkhOutputs(1e8 - 545 - txrules.FeeForSerializeSize(1e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(0), txsizes.P2PKHPkScriptSize))),
			RelayFee:     1e3,
			ChangeAmount: 0,
			InputCount:   1,
		},

		// Test two unspent outputs available where both are needed.
		11: {
			UnspentOutputs: p2pkhOutputs(1e8, 1e8),
			Outputs:        p2pkhOutputs(1e8),
			RelayFee:       1e3,
			ChangeAmount: 1e8 - txrules.FeeForSerializeSize(1e3,
				txsizes.EstimateSerializeSize([]int{txsizes.RedeemP2PKHSigScriptSize, txsizes.RedeemP2PKHSigScriptSize}, p2pkhOutputs(1e8), txsizes.P2PKHPkScriptSize)),
			InputCount: 2,
		},

		// Test that zero change outputs are not included
		// (ChangeAmount=0 means don't include any change output).
		12: {
			UnspentOutputs: p2pkhOutputs(1e8),
			Outputs:        p2pkhOutputs(1e8),
			RelayFee:       0,
			ChangeAmount:   0,
			InputCount:     1,
		},
	}

	var changeSource AuthorTestChangeSource

	for i, test := range tests {
		inputSource := makeInputSource(test.UnspentOutputs)
		tx, err := NewUnsignedTransaction(test.Outputs, test.RelayFee, inputSource, changeSource, chaincfg.MainNetParams().MaxTxSize)
		if err != nil {
			insufficientBalance := errors.Is(err, errors.InsufficientBalance)
			if insufficientBalance != test.InputSourceError {
				if !test.InputSourceError {
					t.Errorf("Test %d: InsufficientBalance=%v expected %v", i, insufficientBalance, test.InputSourceError)
				}
				continue
			} else if !insufficientBalance {
				t.Errorf("Test %d: Unexpected error: %v", i, err)
				continue
			}
			continue
		}
		if tx.ChangeIndex < 0 {
			if test.ChangeAmount != 0 {
				t.Errorf("Test %d: No change output added but expected output with amount %v",
					i, test.ChangeAmount)
				continue
			}
		} else {
			changeAmount := dcrutil.Amount(tx.Tx.TxOut[tx.ChangeIndex].Value)
			if test.ChangeAmount == 0 {
				t.Errorf("Test %d: Included change output with value %v but expected no change",
					i, changeAmount)
				continue
			}
			if changeAmount != test.ChangeAmount {
				t.Errorf("Test %d: Got change amount %v, Expected %v",
					i, changeAmount, test.ChangeAmount)
				continue
			}
		}
		if len(tx.Tx.TxIn) != test.InputCount {
			t.Errorf("Test %d: Used %d outputs from input source, Expected %d",
				i, len(tx.Tx.TxIn), test.InputCount)
		}
	}
}

func TestNewUnsignedTransactionRecipientPaysFee(t *testing.T) {
	const defaultRelayFeePerKb = 1e3
	const defaultFeeOneInputOneOutput = 228
	const defaultFeeOneInputTwoOutputs = 264

	tests := []struct {
		name             string
		unspentOutputs   []*wire.TxOut
		output           *wire.TxOut
		wantErr          error
		wantOutputAmount dcrutil.Amount
		wantChangeAmount dcrutil.Amount
	}{{
		name:             "output spends all input value",
		unspentOutputs:   p2pkhOutputs(1e8),
		output:           p2pkhOutputs(1e8)[0],
		wantOutputAmount: 1e8 - defaultFeeOneInputOneOutput,
	}, {
		name:             "output with dust change",
		unspentOutputs:   p2pkhOutputs(1e8),
		output:           p2pkhOutputs(1e8 - 1)[0],
		wantOutputAmount: 1e8 - defaultFeeOneInputOneOutput,
	}, {
		name:             "output with non-dust change",
		unspentOutputs:   p2pkhOutputs(2e8),
		output:           p2pkhOutputs(1e8)[0],
		wantOutputAmount: 1e8 - defaultFeeOneInputTwoOutputs,
		wantChangeAmount: 1e8,
	}, {
		name:           "output overspends input value",
		unspentOutputs: p2pkhOutputs(1e8),
		output:         p2pkhOutputs(1e8 + 1)[0],
		wantErr:        errors.InsufficientBalance,
	}, {
		name:           "output is dust",
		unspentOutputs: p2pkhOutputs(defaultFeeOneInputOneOutput),
		output:         p2pkhOutputs(defaultFeeOneInputOneOutput)[0],
		wantErr:        errors.Policy,
	}}

	var changeSource AuthorTestChangeSource
	maxTxSize := chaincfg.MainNetParams().MaxTxSize
	for _, test := range tests {
		testName := test.name
		inputSource := makeInputSource(test.unspentOutputs)
		tx, err := NewUnsignedTransactionRecipientPaysFee(test.output,
			defaultRelayFeePerKb, inputSource, changeSource, maxTxSize)

		// Ensure the error returned matches what is expected from the
		// respective test.
		if test.wantErr != nil {
			if err == nil {
				t.Errorf("%s: error expected, but one was not returned",
					testName)
				return
			} else if !errors.Is(err, test.wantErr) {
				t.Errorf("%s: got error: %v, expected error: %v",
					testName, err, test.wantErr)
				return
			}
			continue
		} else if err != nil {
			t.Errorf("%s: got unexpected error: %v", testName, err)
			return
		}

		// Ensure the change output matches what is expected from the
		// respective test.
		if test.wantChangeAmount > 0 && tx.ChangeIndex < 0 {
			t.Errorf("%s: expected change output does not exist", testName)
			return
		}
		if test.wantChangeAmount == 0 && tx.ChangeIndex >= 0 {
			t.Errorf("%s: unexpected change output created", testName)
			return
		}

		outputIndex := 0
		if tx.ChangeIndex >= 0 {
			outputIndex = tx.ChangeIndex ^ 1
		}
		actualOutputValue := tx.Tx.TxOut[outputIndex].Value

		// Ensure the returned output reference is not the same as the provided
		// output argument.  Since the transaction is modified, it should not
		// be the same.
		if test.output == tx.Tx.TxOut[outputIndex] {
			t.Errorf("%s: expected returned output reference to not equal"+
				" test output reference", testName)
			return
		}

		if actualOutputValue != int64(test.wantOutputAmount) {
			t.Errorf("%s: unexpected output value. got %v, want %v ",
				testName, actualOutputValue, test.wantOutputAmount)
			return
		}

		// If there is change, make sure it's the expected value.
		if tx.ChangeIndex >= 0 {
			changeValue := tx.Tx.TxOut[tx.ChangeIndex].Value
			if changeValue != int64(test.wantChangeAmount) {
				t.Errorf("%s: Got change amount %v, Expected %v",
					testName, changeValue, test.wantChangeAmount)
				return
			}
		}
	}
}
