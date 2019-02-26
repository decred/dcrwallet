// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package helpers provides convenience functions to simplify wallet code.  This
// package is intended for internal wallet use only.
package helpers

import (
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/wire"
)

// SumOutputValues sums up the list of TxOuts and returns an Amount.
func SumOutputValues(outputs []*wire.TxOut) (totalOutput dcrutil.Amount) {
	for _, txOut := range outputs {
		totalOutput += dcrutil.Amount(txOut.Value)
	}
	return totalOutput
}

// SumOutputValues sums up the list of TxOuts and returns an Amount.
func SumInputValues(inputs []*wire.TxIn) (totalInput dcrutil.Amount) {
	for _, txIn := range inputs {
		totalInput += dcrutil.Amount(txIn.ValueIn)
	}
	return totalInput
}

// SumOutputSerializeSizes sums up the serialized size of the supplied outputs.
func SumOutputSerializeSizes(outputs []*wire.TxOut) (serializeSize int) {
	for _, txOut := range outputs {
		serializeSize += txOut.SerializeSize()
	}
	return serializeSize
}
