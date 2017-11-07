// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txsizes

import (
	"fmt"

	"github.com/decred/dcrd/wire"
	h "github.com/decred/dcrwallet/internal/helpers"
)

// Worst case script and input/output size estimates.
const (
	// sig script descriptor flags
	P2SHSigScript  = "P2SH"
	P2PKHSigScript = "P2PKH"
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

	// P2SHScriptSize is the size of a transaction output script that
	// pays to a script hash.  It is calculated as:
	//
	//   - OP_DUP
	//   - OP_HASH160
	//   - OP_DATA_20
	//   - 20 bytes pubkey hash
	//   - OP_EQUAL
	P2SHScriptSize = 1 + 1 + 1 + 20 + 1

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

	// P2PKHOutputSize is the serialize size of a transaction output with a
	// P2PKH output script.  It is calculated as:
	//
	//   - 8 bytes output value
	//   - 2 bytes version
	//   - 1 byte compact int encoding value 25
	//   - 25 bytes P2PKH output script
	P2PKHOutputSize = 8 + 2 + 1 + P2PKHPkScriptSize
)

// EstimateSerializeSize returns a worst case serialize size estimate for a
// signed transaction that spends inputCount number of outputs,
// signed by their corresponding signature scripts descriped by txInSigDescs
// and contains each transaction output from txOuts.  The estimated size is
// incremented for an additional P2PKH change output if addChangeOutput is
// true.
func EstimateSerializeSize(inputCount int, txOuts []*wire.TxOut, addChangeOutput bool, txInSigDescs []string) (int, error) {
	// generate the estimated sizes of the inputs
	descCount := len(txInSigDescs)
	if inputCount != descCount {
		return 0, fmt.Errorf("txscript descriptor count (%d) does not match inputs count (%d)", descCount, inputCount)
	}

	txInsSize := 0
	for _, scriptDesc := range txInSigDescs {
		if scriptDesc == P2PKHSigScript {
			txInsSize += EstimateInputSize(RedeemP2SHSigScriptSize)
		} else if scriptDesc == P2SHSigScript {
			txInsSize += EstimateInputSize(RedeemP2PKHSigScriptSize)
		} else {
			return 0, fmt.Errorf("unknown sig script desc: %s", scriptDesc)
		}
	}

	outputCount := len(txOuts)
	changeSize := 0
	if addChangeOutput {
		changeSize = P2PKHOutputSize
		outputCount++
	}

	// 12 additional bytes are for version, locktime and expiry.
	return 12 + (2 * wire.VarIntSerializeSize(uint64(inputCount))) +
		wire.VarIntSerializeSize(uint64(outputCount)) +
		txInsSize +
		h.SumOutputSerializeSizes(txOuts) +
		changeSize, nil
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
