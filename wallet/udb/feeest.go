// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

const (
	// All transactions have 4 bytes for version, 4 bytes of locktime,
	// 4 bytes of expiry, and 2 varints for the number of inputs and
	// outputs, and 1 varint for the witnesses.
	txOverheadEstimate = 4 + 4 + 4 + 1 + 1 + 1

	// A worst case signature script to redeem a P2PKH output for a
	// compressed pubkey has 73 bytes of the possible DER signature
	// (with no leading 0 bytes for R and S), 65 bytes of serialized pubkey,
	// and data push opcodes for both, plus one byte for the hash type flag
	// appended to the end of the signature.
	sigScriptEstimate = 1 + 73 + 1 + 65 + 1

	// A best case tx input serialization cost is 32 bytes of sha, 4 bytes
	// of output index, 1 byte for tree, 4 bytes of sequence, 12 bytes for
	// fraud proof, one byte for both the txin signature size (0) and the
	// witness signature script size, and the estimated signature script
	// size.
	txInEstimate = 32 + 4 + 1 + 12 + 4 + 1 + 1 + sigScriptEstimate

	// An SSRtx P2PKH pkScript contains the following bytes:
	//  - OP_SSRTX
	//  - OP_DUP
	//  - OP_HASH160
	//  - OP_DATA_20 + 20 bytes of pubkey hash
	//  - OP_EQUALVERIFY
	//  - OP_CHECKSIG
	ssrtxPkScriptEstimate = 1 + 1 + 1 + 1 + 20 + 1 + 1

	// txOutEstimate is a best case tx output serialization cost is 8 bytes
	// of value, two bytes of version, one byte of varint, and the pkScript
	// size.
	txOutEstimate = 8 + 2 + 1 + ssrtxPkScriptEstimate
)

func estimateSSRtxTxSize(numInputs, numOutputs int) int {
	return txOverheadEstimate + txInEstimate*numInputs +
		txOutEstimate*numOutputs
}
