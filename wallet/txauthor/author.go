// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package txauthor provides transaction creation code for wallets.
package txauthor

import (
	"errors"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
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
// than the target or by returning a more detailed error implementing
// InputSourceError.
type InputSource func(target dcrutil.Amount) (total dcrutil.Amount,
	inputs []*wire.TxIn, scripts [][]byte, err error)

// InputSourceError describes the failure to provide enough input value from
// unspent transaction outputs to meet a target amount.  A typed error is used
// so input sources can provide their own implementations describing the reason
// for the error, for example, due to spendable policies or locked coins rather
// than the wallet not having enough available input value.
type InputSourceError interface {
	error
	InputSourceError()
}

// InsufficientFundsError is the default implementation of InputSourceError.
type InsufficientFundsError struct{}

// InputSourceError generates the InputSourceError interface for an
// InsufficientFundsError.
func (InsufficientFundsError) InputSourceError() {}

// Error satistifies the error interface.
func (InsufficientFundsError) Error() string {
	return "insufficient funds available to construct transaction"
}

// AuthoredTx holds the state of a newly-created transaction and the change
// output (if one was added).
type AuthoredTx struct {
	Tx                           *wire.MsgTx
	PrevScripts                  [][]byte
	TotalInput                   dcrutil.Amount
	ChangeIndex                  int // negative if no change
	EstimatedSignedSerializeSize int
}

// ChangeSource provides P2PKH change output scripts and versions for
// transaction creation.
type ChangeSource func() ([]byte, uint16, error)

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
//
// BUGS: Fee estimation may be off when redeeming non-compressed P2PKH outputs.
func NewUnsignedTransaction(outputs []*wire.TxOut, relayFeePerKb dcrutil.Amount,
	fetchInputs InputSource, fetchChange ChangeSource) (*AuthoredTx, error) {

	targetAmount := h.SumOutputValues(outputs)
	scriptSizers := []txsizes.ScriptSizer{txsizes.P2PKHScriptSize}
	estimatedSize := txsizes.EstimateSerializeSize(scriptSizers, outputs, true)
	targetFee := txrules.FeeForSerializeSize(relayFeePerKb, estimatedSize)

	for {
		inputAmount, inputs, scripts, err := fetchInputs(targetAmount + targetFee)
		if err != nil {
			return nil, err
		}

		if inputAmount < targetAmount+targetFee {
			return nil, InsufficientFundsError{}
		}

		scriptSizers := []txsizes.ScriptSizer{}
		for range inputs {
			scriptSizers = append(scriptSizers, txsizes.P2PKHScriptSize)
		}

		maxSignedSize := txsizes.EstimateSerializeSize(scriptSizers, outputs, true)
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
			txsizes.P2PKHPkScriptSize, relayFeePerKb) {
			changeScript, changeScriptVersion, err := fetchChange()
			if err != nil {
				return nil, err
			}
			if len(changeScript) > txsizes.P2PKHPkScriptSize {
				return nil, errors.New("fee estimation requires change " +
					"scripts no larger than P2PKH output scripts")
			}
			change := &wire.TxOut{
				Value:    int64(changeAmount),
				Version:  changeScriptVersion,
				PkScript: changeScript,
			}
			l := len(outputs)
			unsignedTransaction.TxOut = append(outputs[:l:l], change)
			changeIndex = l
		}

		estSignedSize := txsizes.EstimateSerializeSize(scriptSizers,
			unsignedTransaction.TxOut, false)
		return &AuthoredTx{
			Tx:                           unsignedTransaction,
			PrevScripts:                  scripts,
			TotalInput:                   inputAmount,
			ChangeIndex:                  changeIndex,
			EstimatedSignedSerializeSize: estSignedSize,
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

// AddInputScripts modifies transaction a transaction by adding inputs
// scripts for each input with index is specified in indes.  Previous output scripts being redeemed by each input
// are passed in prevPkScripts and the slice length must match the number of
// inputs.  Private keys and redeem scripts are looked up using a SecretsSource
// based on the previous output script.
func AddInputScripts(tx *wire.MsgTx, prevPkScripts [][]byte, secrets SecretsSource, indes []int32) error {
	inputs := tx.TxIn
	chainParams := secrets.ChainParams()

	if len(indes) != len(prevPkScripts) {
		return errors.New("indes and prevPkScripts slices must " +
			"have equal length")
	}
	k := 0
	for _, i := range indes {
		pkScript := prevPkScripts[k]
		sigScript := inputs[i].SignatureScript
		script, err := txscript.SignTxOutput(chainParams, tx, int(i),
			pkScript, txscript.SigHashAll, secrets, secrets,
			sigScript, chainec.ECTypeSecp256k1)
		if err != nil {
			return err
		}
		inputs[i].SignatureScript = script
		k++
	}

	return nil
}

// AddAllInputScripts modifies an authored transaction by adding inputs scripts
// for each input of an authored transaction.  Private keys and redeem scripts
// are looked up using a SecretsSource based on the previous output script.
func (tx *AuthoredTx) AddAllInputScripts(secrets SecretsSource) error {
	return AddAllInputScripts(tx.Tx, tx.PrevScripts, secrets)
}
