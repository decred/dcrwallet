package compat

import (
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
)

func HD2Address(k *hdkeychain.ExtendedKey, params dcrutil.AddressParams) (*dcrutil.AddressPubKeyHash, error) {
	pk := k.SerializedPubKey()
	hash := dcrutil.Hash160(pk)
	return dcrutil.NewAddressPubKeyHash(hash, params, dcrec.STEcdsaSecp256k1)
}
