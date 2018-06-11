// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txsizes

import (
	"github.com/decred/dcrd/wire"
	h "github.com/decred/dcrwallet/internal/helpers"
)

// ScriptSizer signature script sizing interface
type ScriptSizer interface {
	ScriptSize() int
}

type sigScriptSize int

func (s sigScriptSize) ScriptSize() int { return int(s) }

// TODO: add multi sig sizer type

// Worst case script and input/output size estimates.
const (
	// RedeemP2PKHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a compressed P2PKH output.
	// It is calculated as:
	//
	//   - OP_DATA_73
	//   - 72 bytes DER signature + 1 byte sighash
	//   - OP_DATA_33
	//   - 33 bytes serialized compressed pubkey
	RedeemP2PKHSigScriptSize = 1 + 73 + 1 + 33

	// RedeemP2SHSigScriptSize is the worst case (largest) serialize size
	// of a transaction input script that redeems a P2SH output.
	// It is calculated as:
	//
	//  - OP_DATA_73
	//  - 73-byte signature
	//  - OP_DATA_35
	//  - OP_DATA_33
	//  - 33 bytes serialized compressed pubkey
	//  - OP_CHECKSIG
	RedeemP2SHSigScriptSize = 1 + 73 + 1 + 1 + 33 + 1

	// RedeemP2PKHInputSize is the worst case (largest) serialize size of a
	// transaction input redeeming a compressed P2PKH output.  It is
	// calculated as:
	//
	//   - 32 bytes previous tx
	//   - 4 bytes output index
	//   - 1 byte tree
	//   - 8 bytes amount
	//   - 4 bytes block height
	//   - 4 bytes block index
	//   - 1 byte compact int encoding value 107
	//   - 107 bytes signature script
	//   - 4 bytes sequence
	RedeemP2PKHInputSize = 32 + 4 + 1 + 8 + 4 + 4 + 1 + RedeemP2PKHSigScriptSize + 4

	// P2PKHPkScriptSize is the size of a transaction output script that
	// pays to a compressed pubkey hash.  It is calculated as:
	//
	//   - OP_DUP
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUALVERIFY
	//   - OP_CHECKSIG
	P2PKHPkScriptSize = 1 + 1 + 1 + 20 + 1 + 1

	// P2PKHOutputSize is the serialize size of a transaction output with a
	// P2PKH output script.  It is calculated as:
	//
	//   - 8 bytes output value
	//   - 2 bytes version
	//   - 1 byte compact int encoding value 25
	//   - 25 bytes P2PKH output script
	P2PKHOutputSize = 8 + 2 + 1 + 25

	// signature script definitions
	P2SHScriptSize  = sigScriptSize(RedeemP2SHSigScriptSize)
	P2PKHScriptSize = sigScriptSize(RedeemP2PKHSigScriptSize)
)

// EstimateSerializeSize returns a worst case serialize size estimate for a
// signed transaction that spends a number of outputs and contains each
// transaction output from txOuts. The estimated size is incremented for an
// additional change output if changeScriptSize is greater than 0. Passing 0
// does not add a change output.
func EstimateSerializeSize(scriptSizers []ScriptSizer, txOuts []*wire.TxOut, changeScriptSize int) int {
	// generate the estimated sizes of the inputs
	txInsSize := 0
	for _, sizer := range scriptSizers {
		txInsSize += EstimateInputSize(sizer.ScriptSize())
	}

	inputCount := len(scriptSizers)
	outputCount := len(txOuts)
	changeSize := 0
	if changeScriptSize > 0 {
		changeSize = EstimateOutputSize(changeScriptSize)
		outputCount++
	}

	// 12 additional bytes are for version, locktime and expiry.
	return 12 + (2 * wire.VarIntSerializeSize(uint64(inputCount))) +
		wire.VarIntSerializeSize(uint64(outputCount)) +
		txInsSize +
		h.SumOutputSerializeSizes(txOuts) +
		changeSize
}

// EstimateInputSize returns the worst case serialize size estimate for a tx input
//   - 32 bytes previous tx
//   - 4 bytes output index
//   - 1 byte tree
//   - 8 bytes amount
//   - 4 bytes block height
//   - 4 bytes block index
//   - the compact int representation of the script size
//   - the supplied script size
//   - 4 bytes sequence
func EstimateInputSize(scriptSize int) int {
	return 32 + 4 + 1 + 8 + 4 + 4 + wire.VarIntSerializeSize(uint64(scriptSize)) + scriptSize + 4
}

// EstimateOutputSize returns the worst case serialize size estimate for a tx output
//   - 8 bytes amount
//   - 2 bytes version
//   - the compact int representation of the script size
//   - the supplied script size
func EstimateOutputSize(scriptSize int) int {
	return 8 + 2 + wire.VarIntSerializeSize(uint64(scriptSize)) + scriptSize
}
