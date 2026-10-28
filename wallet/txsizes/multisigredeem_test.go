// Copyright (c) 2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txsizes_test

import (
	"testing"

	"decred.org/dcrwallet/v5/wallet/txrules"
	"decred.org/dcrwallet/v5/wallet/txsizes"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/wire"
)

// redeemScript2of2 is the real 2-of-2 multisig redeem script from dcrwallet
// issue #2273 (71 bytes).
var redeemScript2of2 = []byte{
	0x52, // OP_2
	0x21, 0x03, 0x0e, 0x4d, 0xb4, 0xd3, 0x7c, 0xfa, 0x43, 0x55, 0x3c, 0x64,
	0x5a, 0xd2, 0x0c, 0xa7, 0x9a, 0xe7, 0x9e, 0xef, 0x96, 0x6f, 0x41, 0x24,
	0x36, 0x28, 0x31, 0x0e, 0x76, 0x24, 0xd3, 0x3a, 0x41, 0x45, // pubkey 1
	0x21, 0x02, 0xdc, 0x7a, 0xae, 0xb5, 0x75, 0xd3, 0x17, 0x07, 0x60, 0xf3,
	0xc7, 0x19, 0xbe, 0xfd, 0x52, 0x80, 0x5a, 0x03, 0x01, 0xb2, 0x00, 0xa1,
	0xe4, 0xef, 0xb7, 0x00, 0xeb, 0xcc, 0x37, 0x9a, 0x9a, 0xf5, // pubkey 2
	0x52, // OP_2
	0xae, // OP_CHECKMULTISIG
}

// buildRedeemTxOut returns a transaction output paying to a P2PKH script, as
// redeemmultisigout constructs for the redeeming transaction.
func buildRedeemTxOut() *wire.TxOut {
	// A P2PKH pkScript is 25 bytes.
	return wire.NewTxOut(0, make([]byte, txsizes.P2PKHPkScriptSize))
}

// workingRequiredFee returns the minimum fee the network would require for a
// fully-signed m-of-n redeeming transaction, using an independently built
// worst-case signature script as the oracle (OP_0 + m max-size signatures +
// the pushed redeem script).
func workingRequiredFee(t *testing.T, relayFee dcrutil.Amount, m int) dcrutil.Amount {
	t.Helper()

	b := txscript.NewScriptBuilder()
	b.AddOp(txscript.OP_0) // CHECKMULTISIG off-by-one dummy
	for i := 0; i < m; i++ {
		b.AddData(make([]byte, 73)) // max DER signature + sighash byte
	}
	b.AddData(redeemScript2of2)
	sigScript, err := b.Script()
	if err != nil {
		t.Fatalf("build signed sigScript: %v", err)
	}

	tx := wire.NewMsgTx()
	tx.AddTxIn(&wire.TxIn{SignatureScript: sigScript})
	tx.AddTxOut(buildRedeemTxOut())

	return txrules.FeeForSerializeSize(relayFee, tx.SerializeSize())
}

// estimatedRedeemFee mirrors wallet.PrepareRedeemMultiSigOutTxOutput's fee
// estimation: one input whose signature script is estimated at sigScriptSize,
// plus a single P2PKH output.
func estimatedRedeemFee(relayFee dcrutil.Amount, sigScriptSize int) dcrutil.Amount {
	feeSize := txsizes.EstimateSerializeSize([]int{sigScriptSize},
		[]*wire.TxOut{buildRedeemTxOut()}, 0)
	return txrules.FeeForSerializeSize(relayFee, feeSize)
}

func TestRedeemMultiSigOutFeeMeetsMinimum(t *testing.T) {
	const relayFee = txrules.DefaultRelayFeePerKb // 10000 atoms/kB

	requiredFee := workingRequiredFee(t, relayFee, 2)
	t.Logf("network-required fee for fully-signed 2-of-2 redeem: %d atoms", requiredFee)

	// Fixed path: estimate the signature script with the m-of-n multisig
	// redeem size, which includes every required signature and the pushed
	// redeem script.
	fixedSize := txsizes.RedeemP2SHMultiSigSigScriptSize(2, redeemScript2of2)
	fixedFee := estimatedRedeemFee(relayFee, fixedSize)
	t.Logf("fixed redeemmultisigout estimated fee: %d atoms (sigScript est %d bytes)",
		fixedFee, fixedSize)
	if fixedFee < requiredFee {
		t.Fatalf("redeemmultisigout underpays fee: estimated %d atoms < required %d atoms",
			fixedFee, requiredFee)
	}

	// Regression guard: the previous single-signature P2SH estimate
	// (RedeemP2SHSigScriptSize) undercounted the size and produced a fee
	// below the required minimum, which is the bug reported in issue #2273.
	buggyFee := estimatedRedeemFee(relayFee, txsizes.RedeemP2SHSigScriptSize)
	t.Logf("legacy single-sig estimate fee: %d atoms", buggyFee)
	if buggyFee >= requiredFee {
		t.Fatalf("expected legacy single-sig estimate %d atoms to underpay required %d atoms; "+
			"reproduction no longer demonstrates the bug", buggyFee, requiredFee)
	}
}
