// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package txauthor provides transaction creation code for wallets.
package txauthor

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/txrules"

	h "github.com/decred/dcrwallet/internal/helpers"
	"github.com/decred/dcrwallet/wallet/internal/txsizes"
)

const (
	// generatedTxVersion is the version of the transaction being generated.
	// It is defined as a constant here rather than using the wire.TxVersion
	// constant since a change in the transaction version will potentially
	// require changes to the generated transaction.  Thus, using the wire
	// constant for the generated transaction version could allow creation
	// of invalid transactions for the updated version.
	generatedTxVersion = 1
)

// InputSource provides transaction inputs referencing spendable outputs to
// construct a transaction outputting some target amount.  If the target amount
// can not be satisified, this can be signaled by returning a total amount less
// than the target or by returning a more detailed error.
type InputSource func(target dcrutil.Amount) (total dcrutil.Amount,
	inputs []*wire.TxIn, scripts [][]byte, err error)

// AuthoredTx holds the state of a newly-created transaction and the change
// output (if one was added).
type AuthoredTx struct {
	Tx                           *wire.MsgTx
	PrevScripts                  [][]byte
	TotalInput                   dcrutil.Amount
	ChangeIndex                  int // negative if no change
	EstimatedSignedSerializeSize int
}

// ChangeSource provides change output scripts and versions for
// transaction creation.
type ChangeSource interface {
	Script() (script []byte, version uint16, err error)
	ScriptSize() int
}

// NewUnsignedTransaction creates an unsigned transaction paying to one or more
// non-change outputs.  An appropriate transaction fee is included based on the
// transaction size.
//
// Transaction inputs are chosen from repeated calls to fetchInputs with
// increasing targets amounts.
//
// If any remaining output value can be returned to the wallet via a change
// output without violating mempool dust rules, a P2PKH change output is
// appended to the transaction outputs.  Since the change output may not be
// necessary, fetchChange is called zero or one times to generate this script.
// This function must return a P2PKH script or smaller, otherwise fee estimation
// will be incorrect.
//
// If successful, the transaction, total input value spent, and all previous
// output scripts are returned.  If the input source was unable to provide
// enough input value to pay for every output any any necessary fees, an
// InputSourceError is returned.
func NewUnsignedTransaction(outputs []*wire.TxOut, relayFeePerKb dcrutil.Amount,
	fetchInputs InputSource, fetchChange ChangeSource) (*AuthoredTx, error) {

	const op errors.Op = "txauthor.NewUnsignedTransaction"

	targetAmount := h.SumOutputValues(outputs)
	scriptSizers := []txsizes.ScriptSizer{txsizes.P2PKHScriptSize}
	changeScript, changeScriptVersion, err := fetchChange.Script()
	if err != nil {
		return nil, errors.E(op, err)
	}
	changeScriptSize := fetchChange.ScriptSize()
	maxSignedSize := txsizes.EstimateSerializeSize(scriptSizers, outputs, changeScriptSize)
	targetFee := txrules.FeeForSerializeSize(relayFeePerKb, maxSignedSize)

	for {
		inputAmount, inputs, scripts, err := fetchInputs(targetAmount + targetFee)
		if err != nil {
			return nil, errors.E(op, err)
		}

		if inputAmount < targetAmount+targetFee {
			return nil, errors.E(op, errors.InsufficientBalance)
		}

		scriptSizers := []txsizes.ScriptSizer{}
		for range inputs {
			scriptSizers = append(scriptSizers, txsizes.P2PKHScriptSize)
		}

		maxSignedSize = txsizes.EstimateSerializeSize(scriptSizers, outputs, changeScriptSize)
		maxRequiredFee := txrules.FeeForSerializeSize(relayFeePerKb, maxSignedSize)
		remainingAmount := inputAmount - targetAmount
		if remainingAmount < maxRequiredFee {
			targetFee = maxRequiredFee
			continue
		}

		unsignedTransaction := &wire.MsgTx{
			SerType:  wire.TxSerializeFull,
			Version:  generatedTxVersion,
			TxIn:     inputs,
			TxOut:    outputs,
			LockTime: 0,
			Expiry:   0,
		}
		changeIndex := -1
		changeAmount := inputAmount - targetAmount - maxRequiredFee
		if changeAmount != 0 && !txrules.IsDustAmount(changeAmount,
			changeScriptSize, relayFeePerKb) {
			if len(changeScript) > txscript.MaxScriptElementSize {
				return nil, errors.E(errors.Invalid, "script size exceed maximum bytes "+
					"pushable to the stack")
			}
			change := &wire.TxOut{
				Value:    int64(changeAmount),
				Version:  changeScriptVersion,
				PkScript: changeScript,
			}
			l := len(outputs)
			unsignedTransaction.TxOut = append(outputs[:l:l], change)
			changeIndex = l
		} else {
			maxSignedSize = txsizes.EstimateSerializeSize(scriptSizers,
				unsignedTransaction.TxOut, 0)
		}
		return &AuthoredTx{
			Tx:                           unsignedTransaction,
			PrevScripts:                  scripts,
			TotalInput:                   inputAmount,
			ChangeIndex:                  changeIndex,
			EstimatedSignedSerializeSize: maxSignedSize,
		}, nil
	}
}

// RandomizeOutputPosition randomizes the position of a transaction's output by
// swapping it with a random output.  The new index is returned.  This should be
// done before signing.
func RandomizeOutputPosition(outputs []*wire.TxOut, index int) int {
	r := cprng.Int31n(int32(len(outputs)))
	outputs[r], outputs[index] = outputs[index], outputs[r]
	return int(r)
}

// RandomizeChangePosition randomizes the position of an authored transaction's
// change output.  This should be done before signing.
func (tx *AuthoredTx) RandomizeChangePosition() {
	tx.ChangeIndex = RandomizeOutputPosition(tx.Tx.TxOut, tx.ChangeIndex)
}

// SecretsSource provides private keys and redeem scripts necessary for
// constructing transaction input signatures.  Secrets are looked up by the
// corresponding Address for the previous output script.  Addresses for lookup
// are created using the source's blockchain parameters and means a single
// SecretsSource can only manage secrets for a single chain.
//
// TODO: Rewrite this interface to look up private keys and redeem scripts for
// pubkeys, pubkey hashes, script hashes, etc. as separate interface methods.
// This would remove the ChainParams requirement of the interface and could
// avoid unnecessary conversions from previous output scripts to Addresses.
// This can not be done without modifications to the txscript package.
type SecretsSource interface {
	txscript.KeyDB
	txscript.ScriptDB
	ChainParams() *chaincfg.Params
}

// AddAllInputScripts modifies transaction a transaction by adding inputs
// scripts for each input.  Previous output scripts being redeemed by each input
// are passed in prevPkScripts and the slice length must match the number of
// inputs.  Private keys and redeem scripts are looked up using a SecretsSource
// based on the previous output script.
func AddAllInputScripts(tx *wire.MsgTx, prevPkScripts [][]byte, secrets SecretsSource) error {
	inputs := tx.TxIn
	chainParams := secrets.ChainParams()

	if len(inputs) != len(prevPkScripts) {
		return errors.New("tx.TxIn and prevPkScripts slices must " +
			"have equal length")
	}

	for i := range inputs {
		pkScript := prevPkScripts[i]
		sigScript := inputs[i].SignatureScript
		script, err := txscript.SignTxOutput(chainParams, tx, i,
			pkScript, txscript.SigHashAll, secrets, secrets,
			sigScript, chainec.ECTypeSecp256k1)
		if err != nil {
			return err
		}
		inputs[i].SignatureScript = script
	}

	return nil
}

// AddAllInputScripts modifies an authored transaction by adding inputs scripts
// for each input of an authored transaction.  Private keys and redeem scripts
// are looked up using a SecretsSource based on the previous output script.
func (tx *AuthoredTx) AddAllInputScripts(secrets SecretsSource) error {
	return AddAllInputScripts(tx.Tx, tx.PrevScripts, secrets)
}
