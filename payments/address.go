// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package payments

import (
	"encoding/binary"

	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/blockchain/stake/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
)

// Address is a human-readable encoding of an output script.
//
// Address encodings may include a network identifier, to prevent misuse on an
// alternate Decred network.
type Address interface {
	String() string

	// PaymentScript returns the output script and script version to pay the
	// address.  The version is always returned with the script, as it is
	// not useful to use the script without the version.
	PaymentScript() (version uint16, script []byte)

	// ScriptLen returns the known length of the address output script.
	ScriptLen() int
}

// StakeAddress is an address kind which is allowed by consensus rules to appear
// in special positions in stake transactions.  These rules permit P2PKH and
// P2SH outputs only.  Stake scripts require a special opcode "tag" to appear at
// the start of the script to classify the type of stake transaction.
type StakeAddress interface {
	String() string

	VoteRights() (script []byte, version uint16)
	TicketChange() (script []byte, version uint16)
	RewardCommitment(amount dcrutil.Amount, limits uint16) (script []byte, version uint16)
	PayVoteCommitment() (script []byte, version uint16)
	PayRevokeCommitment() (script []byte, version uint16)
}

// PubKeyHashAddress is an address which pays to the hash of a public key.
type PubKeyHashAddress interface {
	Address

	PubKeyHash() []byte
}

// PubKeyAddress is an address which pays to a public key.  These addresses are
// typically avoided in favor of pubkey hash addresses, unless an operation
// (e.g. generating a multisig script) requires knowing the unhashed public key.
type PubKeyAddress interface {
	Address

	PubKey() []byte
}

// ScriptHashAddress is an address which pays to the hash of a redeem script
// (pay-to-script-hash, or P2SH).  The redeem script is provided by the
// redeeming input scripts as a final data push.
//
// An implementation of ScriptHashAddress does not necessarily know the unhashed
// redeem script, and may only know how to pay to the address.
type ScriptHashAddress interface {
	Address

	ScriptHash() []byte
}

// MultisigAddress is a P2SH address with a known multisig redeem script.  A
// multisig output requires M out of any N valid signatures from N different
// keypairs in order to redeem the output.
//
// Bare multisig output scripts are nonstandard, and so this is only able to
// implement Address as a P2SH address.  Any P2SH address with an unknown redeem
// script may be a multisig address, but it is impossible to know until the
// redeem script is revealed.
type MultisigAddress interface {
	ScriptHashAddress

	// RedeemScript returns the unhashed multisig script that redeemers must
	// provide as the final data push
	RedeemScript() []byte

	// PubKeys returns all N public keys for each keypair.
	PubKeys() [][]byte

	// M is the required number of unique signatures needed to redeem
	// outputs paying the address.
	M() int

	// N is the total number of public/private keypairs.  The return value
	// must always equal len(PubKeys()).
	N() int
}

// wrappedUtilAddr implements Address for any non-P2PKH and non-P2SH
// address.
type wrappedUtilAddr struct {
	addr   dcrutil.Address
	script []byte
}

var _ Address = (*wrappedUtilAddr)(nil)

func (x *wrappedUtilAddr) String() string {
	return x.addr.Address()
}
func (x *wrappedUtilAddr) PaymentScript() (version uint16, script []byte) {
	return 0, x.script
}
func (x *wrappedUtilAddr) ScriptLen() int {
	return len(x.script)
}
func (x *wrappedUtilAddr) utilAddr() dcrutil.Address {
	return x.addr
}

// WrapUtilAddress wraps a dcrutil.Address with a wallet implementation of the
// Address interface.
func WrapUtilAddress(addr dcrutil.Address) (Address, error) {
	switch addr := addr.(type) {
	case *dcrutil.AddressPubKeyHash:
		return &p2pkhAddress{addr}, nil
	case *dcrutil.AddressScriptHash:
		return &p2shAddress{addr}, nil
	default:
		script, err := txscript.PayToAddrScript(addr)
		if err != nil {
			return nil, err
		}
		return &wrappedUtilAddr{addr, script}, nil
	}
}

// AddressToUtilAddress unwraps or creates a new implementation of a
// dcrutil.Address from an Address.
func AddressToUtilAddress(a Address, params dcrutil.AddressParams) (dcrutil.Address, error) {
	switch a := a.(type) {
	case interface{ utilAddr() dcrutil.Address }:
		return a.utilAddr(), nil
	}
	return dcrutil.DecodeAddress(a.String(), params)
}

// DecodeAddress decodes an address in string encoding for the network described
// by params.  The wallet depends on addresses implementing Address
// returned by this func, rather than external implementations of the interface.
func DecodeAddress(s string, params dcrutil.AddressParams) (Address, error) {
	utilAddr, err := dcrutil.DecodeAddress(s, params)
	if err != nil {
		return nil, err
	}
	return WrapUtilAddress(utilAddr)
}

// ParseAddress parses an address (if the script form matches a known address
// type) from a transaction's output script.  This function works with both
// regular and stake-tagged output scripts.
//
// If the script is multisig, the returned address implements the
// MultisigAddress P2SH type.
func ParseAddress(vers uint16, script []byte, params dcrutil.AddressParams) (Address, error) {
	_, addrs, m, err := txscript.ExtractPkScriptAddrs(vers, script, params)
	if err != nil {
		return nil, err
	}
	if m >= 2 && m <= len(addrs) { // multisig
		n := len(addrs)
		pubkeyAddrs := make([]*dcrutil.AddressSecpPubKey, 0, n)
		pubkeys := make([][]byte, 0, n)
		for i := range addrs {
			addr := addrs[i]
			pubkeyAddr, ok := addr.(*dcrutil.AddressSecpPubKey)
			if !ok {
				err := errors.Errorf("unexpected address type %T", addr)
				return nil, err
			}
			pubkey := pubkeyAddr.PubKey().SerializeCompressed()

			pubkeyAddrs = append(pubkeyAddrs, pubkeyAddr)
			pubkeys = append(pubkeys, pubkey)
		}
		redeemScript, err := txscript.MultiSigScript(pubkeyAddrs, m)
		if err != nil {
			return nil, errors.Errorf("create multisig redeem script: %v", err)
		}
		p2shUtilAddr, err := dcrutil.NewAddressScriptHash(redeemScript, params)
		if err != nil {
			return nil, errors.Errorf("create multisig P2SH: %v", err)
		}

		addr := new(multisigAddress)
		addr.p2shAddress = p2shAddress{p2shUtilAddr}
		addr.redeemScript = redeemScript
		addr.pubkeys = pubkeys
		addr.m = m
		return addr, nil
	}
	if len(addrs) != 1 {
		return nil, errors.E("expected one address")
	}
	addr := addrs[0]
	return WrapUtilAddress(addr)
}

// ParseTicketCommitmentAddress parses a P2PKH or P2SH address from a ticket's
// commitment output script.
func ParseTicketCommitmentAddress(script []byte, params dcrutil.AddressParams) (Address, error) {
	utilAddr, err := stake.AddrFromSStxPkScrCommitment(script, params)
	if err != nil {
		return nil, err
	}
	switch a := utilAddr.(type) {
	case *dcrutil.AddressPubKeyHash:
		return &p2pkhAddress{a}, nil
	case *dcrutil.AddressScriptHash:
		return &p2shAddress{a}, nil
	default:
		return nil, errors.Errorf("unknown address type %T", utilAddr)
	}
}

type p2pkhAddress struct {
	*dcrutil.AddressPubKeyHash
}

var _ interface {
	Address
	PubKeyHashAddress
	StakeAddress
} = (*p2pkhAddress)(nil)

// P2PKHAddress creates an address which is paid to by the hash of its public
// key.
func P2PKHAddress(pubkeyHash []byte, params dcrutil.AddressParams) (PubKeyHashAddress, error) {
	utilAddr, err := dcrutil.NewAddressPubKeyHash(pubkeyHash, params, dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, err
	}
	return &p2pkhAddress{utilAddr}, nil
}

func (x *p2pkhAddress) PubKeyHash() []byte        { return x.Hash160()[:] }
func (x *p2pkhAddress) utilAddr() dcrutil.Address { return x.AddressPubKeyHash }
func (x *p2pkhAddress) ScriptLen() int            { return txsizes.P2PKHPkScriptSize }

func (x *p2pkhAddress) PaymentScript() (uint16, []byte) {
	s := []byte{
		0:  txscript.OP_DUP,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
		24: txscript.OP_CHECKSIG,
	}
	copy(s[3:23], x.Hash160()[:])
	return 0, s
}

func (x *p2pkhAddress) VoteRights() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTX,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.Hash160()[:])
	return s, 0
}

func (x *p2pkhAddress) TicketChange() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTXCHANGE,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.Hash160()[:])
	return s, 0
}

func (x *p2pkhAddress) RewardCommitment(amount dcrutil.Amount, limits uint16) ([]byte, uint16) {
	s := make([]byte, 32)
	s[0] = txscript.OP_RETURN
	s[1] = txscript.OP_DATA_30
	copy(s[2:22], x.Hash160()[:])
	binary.LittleEndian.PutUint64(s[22:30], uint64(amount))
	binary.LittleEndian.PutUint16(s[30:32], limits)
	return s, 0
}

func (x *p2pkhAddress) PayVoteCommitment() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSGEN,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.Hash160()[:])
	return s, 0
}

func (x *p2pkhAddress) PayRevokeCommitment() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSRTX,
		1:  txscript.OP_DUP,
		2:  txscript.OP_HASH160,
		3:  txscript.OP_DATA_20,
		24: txscript.OP_EQUALVERIFY,
		25: txscript.OP_CHECKSIG,
	}
	copy(s[4:24], x.Hash160()[:])
	return s, 0
}

type p2shAddress struct {
	*dcrutil.AddressScriptHash
}

var _ interface {
	Address
	ScriptHashAddress
	StakeAddress
} = (*p2shAddress)(nil)

// P2SHAddress creates an address which is paid to by the hash of its redeem
// script.
func P2SHAddress(scriptHash []byte, params dcrutil.AddressParams) (ScriptHashAddress, error) {
	utilAddr, err := dcrutil.NewAddressScriptHash(scriptHash, params)
	if err != nil {
		return nil, err
	}
	return &p2shAddress{utilAddr}, nil
}

func (x *p2shAddress) ScriptHash() []byte        { return x.Hash160()[:] }
func (x *p2shAddress) utilAddr() dcrutil.Address { return x.AddressScriptHash }
func (x *p2shAddress) ScriptLen() int            { return txsizes.P2SHPkScriptSize }

func (x *p2shAddress) PaymentScript() (uint16, []byte) {
	s := []byte{
		0:  txscript.OP_HASH160,
		1:  txscript.OP_DATA_20,
		22: txscript.OP_EQUALVERIFY,
	}
	copy(s[2:22], x.Hash160()[:])
	return 0, s
}

func (x *p2shAddress) VoteRights() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTX,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
	}
	copy(s[3:23], x.Hash160()[:])
	return s, 0
}

func (x *p2shAddress) TicketChange() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSTXCHANGE,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
	}
	copy(s[3:23], x.Hash160()[:])
	return s, 0
}

func (x *p2shAddress) RewardCommitment(amount dcrutil.Amount, limits uint16) ([]byte, uint16) {
	s := make([]byte, 32)
	s[0] = txscript.OP_RETURN
	s[1] = txscript.OP_DATA_30
	copy(s[2:22], x.Hash160()[:])
	binary.LittleEndian.PutUint64(s[22:30], uint64(amount))
	binary.LittleEndian.PutUint16(s[30:32], limits)
	s[29] |= 0x80 // mark the hash160 as a script hash
	return s, 0
}

func (x *p2shAddress) PayVoteCommitment() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSGEN,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
	}
	copy(s[3:23], x.Hash160()[:])
	return s, 0
}

func (x *p2shAddress) PayRevokeCommitment() (script []byte, version uint16) {
	s := []byte{
		0:  txscript.OP_SSRTX,
		1:  txscript.OP_HASH160,
		2:  txscript.OP_DATA_20,
		23: txscript.OP_EQUALVERIFY,
	}
	copy(s[3:23], x.Hash160()[:])
	return s, 0
}

type multisigAddress struct {
	p2shAddress
	redeemScript []byte
	pubkeys      [][]byte
	m            int
}

func (a *multisigAddress) RedeemScript() []byte { return a.redeemScript }
func (a *multisigAddress) PubKeys() [][]byte    { return a.pubkeys }
func (a *multisigAddress) M() int               { return a.m }
func (a *multisigAddress) N() int               { return len(a.pubkeys) }
