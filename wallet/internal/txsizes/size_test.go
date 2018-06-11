package txsizes_test

import (
	"testing"

	"github.com/decred/dcrd/wire"
	. "github.com/decred/dcrwallet/wallet/internal/txsizes"
)

const (
	p2pkhScriptSize = P2PKHPkScriptSize
	p2shScriptSize  = 23
)

func makeScriptSizers(count int, sizer ScriptSizer) *[]ScriptSizer {
	scriptSizers := make([]ScriptSizer, count)
	for idx := 0; idx < count; idx++ {
		scriptSizers[idx] = sizer
	}
	return &scriptSizers
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
		InputScriptSizers    []ScriptSizer
		OutputScriptLengths  []int
		ChangeScriptSize     int
		ExpectedSizeEstimate int
	}{
		0: {[]ScriptSizer{P2PKHScriptSize}, []int{}, 0, 181},
		1: {[]ScriptSizer{P2PKHScriptSize}, []int{p2pkhScriptSize}, 0, 217},
		2: {[]ScriptSizer{P2PKHScriptSize}, []int{}, p2pkhScriptSize, 217},
		3: {[]ScriptSizer{P2PKHScriptSize}, []int{p2pkhScriptSize}, p2pkhScriptSize, 253},
		4: {[]ScriptSizer{P2PKHScriptSize}, []int{p2shScriptSize}, 0, 215},
		5: {[]ScriptSizer{P2PKHScriptSize}, []int{p2shScriptSize}, p2pkhScriptSize, 251},

		6:  {[]ScriptSizer{P2PKHScriptSize, P2PKHScriptSize}, []int{}, 0, 347},
		7:  {[]ScriptSizer{P2PKHScriptSize, P2PKHScriptSize}, []int{p2pkhScriptSize}, 0, 383},
		8:  {[]ScriptSizer{P2PKHScriptSize, P2PKHScriptSize}, []int{}, p2pkhScriptSize, 383},
		9:  {[]ScriptSizer{P2PKHScriptSize, P2PKHScriptSize}, []int{p2pkhScriptSize}, p2pkhScriptSize, 419},
		10: {[]ScriptSizer{P2PKHScriptSize, P2PKHScriptSize}, []int{p2shScriptSize}, 0, 381},
		11: {[]ScriptSizer{P2PKHScriptSize, P2PKHScriptSize}, []int{p2shScriptSize}, p2pkhScriptSize, 417},

		// 0xfd is discriminant for 16-bit compact ints, compact int
		// total size increases from 1 byte to 3.
		12: {[]ScriptSizer{P2PKHScriptSize}, makeInts(p2pkhScriptSize, 0xfc), 0, 9253},
		13: {[]ScriptSizer{P2PKHScriptSize}, makeInts(p2pkhScriptSize, 0xfd), 0, 9253 + P2PKHOutputSize + 2},
		14: {[]ScriptSizer{P2PKHScriptSize}, makeInts(p2pkhScriptSize, 0xfc), p2pkhScriptSize, 9253 + P2PKHOutputSize + 2},
		15: {*makeScriptSizers(0xfc, P2PKHScriptSize), []int{}, 0, 41847},
		16: {*makeScriptSizers(0xfd, P2PKHScriptSize), []int{}, 0, 41847 + RedeemP2PKHInputSize + 4}, // 4 not 2, varint encoded twice.
	}
	for i, test := range tests {
		outputs := make([]*wire.TxOut, 0, len(test.OutputScriptLengths))
		for _, l := range test.OutputScriptLengths {
			outputs = append(outputs, &wire.TxOut{PkScript: make([]byte, l)})
		}
		actualEstimate := EstimateSerializeSize(test.InputScriptSizers, outputs, test.ChangeScriptSize)
		if actualEstimate != test.ExpectedSizeEstimate {
			t.Errorf("Test %d: Got %v: Expected %v", i, actualEstimate, test.ExpectedSizeEstimate)
		}
	}
}
