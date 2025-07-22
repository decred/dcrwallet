package txsizes_test

import (
	"testing"

	. "decred.org/dcrwallet/v5/wallet/txsizes"
	"github.com/decred/dcrd/wire"
)

const (
	p2pkhScriptSize = P2PKHPkScriptSize
	p2shScriptSize  = 23
)

func makeScriptSizes(count int, size int) *[]int {
	scriptSizes := make([]int, count)
	for idx := 0; idx < count; idx++ {
		scriptSizes[idx] = size
	}
	return &scriptSizes
}

func makeInts(value int, n int) []int {
	v := make([]int, n)
	for i := range v {
		v[i] = value
	}
	return v
}

func TestEstimateSerializeSize(t *testing.T) {
	tests := []struct {
		InputScriptSizes     []int
		OutputScriptLengths  []int
		ChangeScriptSize     int
		ExpectedSizeEstimate int
	}{
		// Updated expected values to account for 1-byte CoinType field per output
		0: {[]int{RedeemP2PKHSigScriptSize}, []int{}, 0, 181},
		1: {[]int{RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, 0, 218},         // +1 for CoinType
		2: {[]int{RedeemP2PKHSigScriptSize}, []int{}, p2pkhScriptSize, 218},          // +1 for CoinType in change
		3: {[]int{RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, p2pkhScriptSize, 255}, // +2 for CoinType in both outputs
		4: {[]int{RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, 0, 216},          // +1 for CoinType
		5: {[]int{RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, p2pkhScriptSize, 253}, // +2 for CoinType

		6:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{}, 0, 347},
		7:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, 0, 384},     // +1 for CoinType
		8:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{}, p2pkhScriptSize, 384},      // +1 for CoinType in change
		9:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, p2pkhScriptSize, 421}, // +2 for CoinType
		10: {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, 0, 382},      // +1 for CoinType
		11: {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, p2pkhScriptSize, 419}, // +2 for CoinType

		// 0xfd is discriminant for 16-bit compact ints, compact int
		// total size increases from 1 byte to 3.
		12: {[]int{RedeemP2PKHSigScriptSize}, makeInts(p2pkhScriptSize, 0xfc), 0, 9505},                              // +252 (0xfc outputs * 1 byte CoinType each)
		13: {[]int{RedeemP2PKHSigScriptSize}, makeInts(p2pkhScriptSize, 0xfd), 0, 9544},                              // +291 (0xfd outputs * 1 byte CoinType each) 
		14: {[]int{RedeemP2PKHSigScriptSize}, makeInts(p2pkhScriptSize, 0xfc), p2pkhScriptSize, 9544},                // +253 (0xfc=252 outputs + 1 change, all with CoinType)
		15: {*makeScriptSizes(0xfc, RedeemP2PKHSigScriptSize), []int{}, 0, 41847},
		16: {*makeScriptSizes(0xfd, RedeemP2PKHSigScriptSize), []int{}, 0, 41847 + RedeemP2PKHInputSize + 4}, // 4 not 2, varint encoded twice.
	}
	for i, test := range tests {
		outputs := make([]*wire.TxOut, 0, len(test.OutputScriptLengths))
		for _, l := range test.OutputScriptLengths {
			outputs = append(outputs, &wire.TxOut{PkScript: make([]byte, l)})
		}
		actualEstimate := EstimateSerializeSize(test.InputScriptSizes, outputs, test.ChangeScriptSize)
		if actualEstimate != test.ExpectedSizeEstimate {
			t.Errorf("Test %d: Got %v: Expected %v", i, actualEstimate, test.ExpectedSizeEstimate)
		}
	}
}
