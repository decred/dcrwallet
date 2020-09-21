package compat

import (
	"github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	"github.com/decred/dcrd/wire"
)

func HD2Address(k *hdkeychain.ExtendedKey, params dcrutil.AddressParams) (*dcrutil.AddressPubKeyHash, error) {
	pk := k.SerializedPubKey()
	hash := dcrutil.Hash160(pk)
	return dcrutil.NewAddressPubKeyHash(hash, params, dcrec.STEcdsaSecp256k1)
}

// IsEitherCoinBaseTx verifies if a transaction is either a coinbase prior to
// the treasury agenda activation or a coinbse after treasury agenda
// activation.
func IsEitherCoinBaseTx(tx *wire.MsgTx) bool {
	if standalone.IsCoinBaseTx(tx, false) {
		return true
	}
	if standalone.IsCoinBaseTx(tx, true) {
		return true
	}
	return false
}
