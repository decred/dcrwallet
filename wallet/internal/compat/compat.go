package compat

import (
	"fmt"

	chaincfg1 "github.com/decred/dcrd/chaincfg"
	chaincfg2 "github.com/decred/dcrd/chaincfg/v2"
	dcrec1 "github.com/decred/dcrd/dcrec"
	dcrutil2 "github.com/decred/dcrd/dcrutil/v2"
	hdkeychain2 "github.com/decred/dcrd/hdkeychain/v2"
	wire1 "github.com/decred/dcrd/wire"
)

func Params2to1(params *chaincfg2.Params) *chaincfg1.Params {
	switch params.Net {
	case wire1.MainNet:
		return &chaincfg1.MainNetParams
	case wire1.TestNet3:
		return &chaincfg1.TestNet3Params
	case wire1.SimNet:
		return &chaincfg1.SimNetParams
	default:
		panic(fmt.Sprintf("params2to1: unknown network %x\n", params.Net))
	}
}

func HD2Address(k *hdkeychain2.ExtendedKey, params dcrutil2.AddressParams) (*dcrutil2.AddressPubKeyHash, error) {
	pk, err := k.ECPubKey()
	if err != nil {
		return nil, err
	}
	hash := dcrutil2.Hash160(pk.SerializeCompressed())
	return dcrutil2.NewAddressPubKeyHash(hash, params, dcrec1.STEcdsaSecp256k1)
}
