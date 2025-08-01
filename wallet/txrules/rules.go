// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txrules

import (
	"decred.org/dcrwallet/v5/errors"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

// StakeSubScriptType potentially transforms the provided script type by
// converting the various stake-specific script types to their associated sub
// type.  It will be returned unmodified otherwise.
func StakeSubScriptType(scriptType stdscript.ScriptType) (stdscript.ScriptType, bool) {
	switch scriptType {
	case stdscript.STStakeSubmissionPubKeyHash, stdscript.STStakeChangePubKeyHash,
		stdscript.STStakeGenPubKeyHash, stdscript.STStakeRevocationPubKeyHash,
		stdscript.STTreasuryGenPubKeyHash:

		return stdscript.STPubKeyHashEcdsaSecp256k1, true

	case stdscript.STStakeSubmissionScriptHash, stdscript.STStakeChangeScriptHash,
		stdscript.STStakeGenScriptHash, stdscript.STStakeRevocationScriptHash,
		stdscript.STTreasuryGenScriptHash:

		return stdscript.STScriptHash, true
	}

	return scriptType, false
}

// DefaultRelayFeePerKb is the default minimum relay fee policy for a mempool.
const DefaultRelayFeePerKb dcrutil.Amount = 0.0001 * 1e8

// IsDustAmount determines whether a transaction output value and script length would
// cause the output to be considered dust.  Transactions with dust outputs are
// not standard and are rejected by mempools with default policies.
func IsDustAmount(amount dcrutil.Amount, scriptSize int, relayFeePerKb dcrutil.Amount) bool {
	// Calculate the total (estimated) cost to the network.  This is
	// calculated using the serialize size of the output plus the serial
	// size of a transaction input which redeems it.  The output is assumed
	// to be compressed P2PKH as this is the most common script type.  Use
	// the average size of a compressed P2PKH redeem input (165) rather than
	// the largest possible (txsizes.RedeemP2PKHInputSize).
	totalSize := 8 + 2 + wire.VarIntSerializeSize(uint64(scriptSize)) +
		scriptSize + 165

	// Dust is defined as an output value where the total cost to the network
	// (output size + input size) is greater than 1/3 of the relay fee.
	return int64(amount)*1000/(3*int64(totalSize)) < int64(relayFeePerKb)
}

// IsDustOutput determines whether a transaction output is considered dust.
// Transactions with dust outputs are not standard and are rejected by mempools
// with default policies.
func IsDustOutput(output *wire.TxOut, relayFeePerKb dcrutil.Amount) bool {
	// Unspendable outputs which solely carry data are not checked for dust.
	if stdscript.IsNullDataScript(output.Version, output.PkScript) {
		return false
	}

	// All other unspendable outputs are considered dust.
	if txscript.IsUnspendable(output.Value, output.PkScript) {
		return true
	}

	return IsDustAmount(dcrutil.Amount(output.Value), len(output.PkScript),
		relayFeePerKb)
}

// CheckOutput performs simple consensus and policy tests on a transaction
// output.  Returns with errors.Invalid if output violates consensus rules, and
// errors.Policy if the output violates a non-consensus policy.
func CheckOutput(output *wire.TxOut, relayFeePerKb dcrutil.Amount) error {
	if output.Value < 0 {
		return errors.E(errors.Invalid, "transaction output amount is negative")
	}
	if output.Value > dcrutil.MaxAmount {
		return errors.E(errors.Invalid, "transaction output amount exceeds maximum value")
	}
	if IsDustOutput(output, relayFeePerKb) {
		return errors.E(errors.Policy, "transaction output is dust")
	}
	return nil
}

// FeeForSerializeSize calculates the required fee for a transaction of some
// arbitrary size given a mempool's relay fee policy.
func FeeForSerializeSize(relayFeePerKb dcrutil.Amount, txSerializeSize int) dcrutil.Amount {
	fee := relayFeePerKb * dcrutil.Amount(txSerializeSize) / 1000

	if fee == 0 && relayFeePerKb > 0 {
		fee = relayFeePerKb
	}

	if fee < 0 || fee > dcrutil.MaxAmount {
		fee = dcrutil.MaxAmount
	}

	return fee
}

func sumOutputValues(outputs []*wire.TxOut) (totalOutput dcrutil.Amount) {
	for _, txOut := range outputs {
		totalOutput += dcrutil.Amount(txOut.Value)
	}
	return totalOutput
}

// FeeForSerializeSizeDualCoin calculates the required fee for a transaction
// based on coin type. Each coin type pays fees in its own coin.
// VAR transactions: Pay fees in VAR
// SKA transactions: Pay fees in their respective SKA coin type (SKA-1 pays in SKA-1, etc.)
// SKA emission transactions: Zero fees (handled separately - coin doesn't exist yet)
func FeeForSerializeSizeDualCoin(relayFeePerKb dcrutil.Amount, txSerializeSize int, coinType dcrutil.CoinType) dcrutil.Amount {
	// All coin types (VAR and SKA) pay fees using the same calculation
	// The fee is paid in the same coin type as the transaction
	return FeeForSerializeSize(relayFeePerKb, txSerializeSize)
}

// FeeForSerializeSizeWithChainParams calculates the required fee for a transaction
// based on coin type using proper chain parameters for SKA fee rates.
func FeeForSerializeSizeWithChainParams(relayFeePerKb dcrutil.Amount, txSerializeSize int, coinType dcrutil.CoinType, chainParams *chaincfg.Params) dcrutil.Amount {
	switch coinType {
	case dcrutil.CoinTypeVAR:
		// VAR transactions use the provided relay fee rate
		return FeeForSerializeSize(relayFeePerKb, txSerializeSize)
	
	default:
		// SKA and other coin types: use chain-specific fee rates
		if chainParams != nil && chainParams.SKAMinRelayTxFee > 0 {
			// Use SKA-specific fee rate from chain parameters
			skaFeePerKb := dcrutil.Amount(chainParams.SKAMinRelayTxFee)
			return FeeForSerializeSize(skaFeePerKb, txSerializeSize)
		}
		// Fallback to VAR fee rate if no SKA rate is configured
		return FeeForSerializeSize(relayFeePerKb, txSerializeSize)
	}
}

// GetPrimaryCoinTypeFromOutputs determines the primary coin type of transaction outputs.
// Returns the first non-VAR coin type found, or VAR if all outputs are VAR.
// This matches the logic used in the blockchain fee collection.
func GetPrimaryCoinTypeFromOutputs(outputs []*wire.TxOut) dcrutil.CoinType {
	for _, output := range outputs {
		if dcrutil.CoinType(output.CoinType) != dcrutil.CoinTypeVAR {
			return dcrutil.CoinType(output.CoinType)
		}
	}
	return dcrutil.CoinTypeVAR
}

// IsDustAmountDualCoin determines dust for dual-coin system.
// All coin types use the same dust calculation since all pay fees.
func IsDustAmountDualCoin(amount dcrutil.Amount, scriptSize int, relayFeePerKb dcrutil.Amount, coinType dcrutil.CoinType) bool {
	// All coin types (VAR and SKA) use the same dust calculation
	// since they all pay fees using the same calculation
	return IsDustAmount(amount, scriptSize, relayFeePerKb)
}

// IsDustOutputDualCoin determines whether a transaction output is considered dust
// in the dual-coin system.
func IsDustOutputDualCoin(output *wire.TxOut, relayFeePerKb dcrutil.Amount) bool {
	// Unspendable outputs which solely carry data are not checked for dust.
	if stdscript.IsNullDataScript(output.Version, output.PkScript) {
		return false
	}

	// All other unspendable outputs are considered dust.
	if txscript.IsUnspendable(output.Value, output.PkScript) {
		return true
	}

	return IsDustAmountDualCoin(dcrutil.Amount(output.Value), len(output.PkScript),
		relayFeePerKb, dcrutil.CoinType(output.CoinType))
}

// PaysHighFees checks whether the signed transaction pays insanely high fees.
// Transactons are defined to have a high fee if they have pay a fee rate that
// is 1000 time higher than the default fee.
func PaysHighFees(totalInput dcrutil.Amount, tx *wire.MsgTx) bool {
	fee := totalInput - sumOutputValues(tx.TxOut)
	if fee <= 0 {
		// Impossible to determine
		return false
	}

	maxFee := FeeForSerializeSize(1000*DefaultRelayFeePerKb, tx.SerializeSize())
	return fee > maxFee
}

// TxPaysHighFees checks whether the signed transaction pays insanely high fees.
// Transactons are defined to have a high fee if they have pay a fee rate that
// is 1000 time higher than the default fee.  Total transaction input value is
// determined by summing the ValueIn fields of each input, and an error is returned
// if any input values were the null value.
func TxPaysHighFees(tx *wire.MsgTx) (bool, error) {
	var input dcrutil.Amount
	for i, in := range tx.TxIn {
		if in.ValueIn < 0 {
			err := errors.Errorf("transaction input %d does not "+
				"specify the input value", i)
			return false, errors.E(errors.Invalid, err)
		}
		input += dcrutil.Amount(in.ValueIn)
	}
	return PaysHighFees(input, tx), nil
}
