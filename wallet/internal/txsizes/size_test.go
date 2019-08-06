package txsizes_test

import (
	"testing"

	"github.com/decred/dcrd/wire"
	. "github.com/decred/dcrwallet/wallet/v3/internal/txsizes"
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
		0: {[]int{RedeemP2PKHSigScriptSize}, []int{}, 0, 181},
		1: {[]int{RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, 0, 217},
		2: {[]int{RedeemP2PKHSigScriptSize}, []int{}, p2pkhScriptSize, 217},
		3: {[]int{RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, p2pkhScriptSize, 253},
		4: {[]int{RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, 0, 215},
		5: {[]int{RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, p2pkhScriptSize, 251},

		6:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{}, 0, 347},
		7:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, 0, 383},
		8:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{}, p2pkhScriptSize, 383},
		9:  {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2pkhScriptSize}, p2pkhScriptSize, 419},
		10: {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, 0, 381},
		11: {[]int{RedeemP2PKHSigScriptSize, RedeemP2PKHSigScriptSize}, []int{p2shScriptSize}, p2pkhScriptSize, 417},

		// 0xfd is discriminant for 16-bit compact ints, compact int
		// total size increases from 1 byte to 3.
		12: {[]int{RedeemP2PKHSigScriptSize}, makeInts(p2pkhScriptSize, 0xfc), 0, 9253},
		13: {[]int{RedeemP2PKHSigScriptSize}, makeInts(p2pkhScriptSize, 0xfd), 0, 9253 + P2PKHOutputSize + 2},
		14: {[]int{RedeemP2PKHSigScriptSize}, makeInts(p2pkhScriptSize, 0xfc), p2pkhScriptSize, 9253 + P2PKHOutputSize + 2},
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
