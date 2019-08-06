// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"encoding/hex"

	"github.com/decred/dcrd/chaincfg/v2/chainec"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
)

// ManagedAddress is an interface that provides acces to information regarding
// an address managed by an address manager. Concrete implementations of this
// type may provide further fields to provide information specific to that type
// of address.
type ManagedAddress interface {
	// Account returns the account the address is associated with.
	Account() uint32

	// Address returns a dcrutil.Address for the backing address.
	Address() dcrutil.Address

	// AddrHash returns the key or script hash related to the address
	AddrHash() []byte

	// Imported returns true if the backing address was imported instead
	// of being part of an address chain.
	Imported() bool

	// Internal returns true if the backing address was created for internal
	// use such as a change output of a transaction.
	Internal() bool

	// Multisig returns true if the backing address was created for multisig
	// use.
	Multisig() bool

	// Compressed returns true if the backing address is compressed.
	Compressed() bool
}

// ManagedPubKeyAddress extends ManagedAddress and additionally provides the
// public and private keys for pubkey-based addresses.
type ManagedPubKeyAddress interface {
	ManagedAddress

	// PubKey returns the public key associated with the address.
	PubKey() chainec.PublicKey

	// ExportPubKey returns the public key associated with the address
	// serialized as a hex encoded string.
	ExportPubKey() string

	// Index returns the child number used to derive this public key address
	Index() uint32
}

// ManagedScriptAddress extends ManagedAddress and represents a pay-to-script-hash
// style of addresses.
type ManagedScriptAddress interface {
	ManagedAddress

	// isScriptAddress tags the interface to the concrete type in this package.
	// This prevents non-script types from being mistakenly type asserted as
	// a ManagedScriptAddress.
	isScriptAddress()
}

// managedAddress represents a public key address.  It also may or may not have
// the private key associated with the public key.
type managedAddress struct {
	manager    *Manager
	account    uint32
	address    *dcrutil.AddressPubKeyHash
	imported   bool
	internal   bool
	multisig   bool
	compressed bool
	pubKey     chainec.PublicKey
	index      uint32
}

// Enforce managedAddress satisfies the ManagedPubKeyAddress interface.
var _ ManagedPubKeyAddress = (*managedAddress)(nil)

// Account returns the account number the address is associated with.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) Account() uint32 {
	return a.account
}

// Address returns the dcrutil.Address which represents the managed address.
// This will be a pay-to-pubkey-hash address.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) Address() dcrutil.Address {
	return a.address
}

// AddrHash returns the public key hash for the address.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) AddrHash() []byte {
	return a.address.Hash160()[:]
}

// Imported returns true if the address was imported instead of being part of an
// address chain.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) Imported() bool {
	return a.imported
}

// Internal returns true if the address was created for internal use such as a
// change output of a transaction.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) Internal() bool {
	return a.internal
}

// Multisig returns true if the address was created for multisig use.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) Multisig() bool {
	return a.multisig
}

// Compressed returns true if the address is compressed.
//
// This is part of the ManagedAddress interface implementation.
func (a *managedAddress) Compressed() bool {
	return a.compressed
}

// PubKey returns the public key associated with the address.
//
// This is part of the ManagedPubKeyAddress interface implementation.
func (a *managedAddress) PubKey() chainec.PublicKey {
	return a.pubKey
}

// Index returns the child number used to derive this key.
//
// This is part of the ManagedPubKeyAddress interface implementation.
func (a *managedAddress) Index() uint32 {
	return a.index
}

// pubKeyBytes returns the serialized public key bytes for the managed address
// based on whether or not the managed address is marked as compressed.
func (a *managedAddress) pubKeyBytes() []byte {
	if a.compressed {
		return a.pubKey.SerializeCompressed()
	}
	return a.pubKey.SerializeUncompressed()
}

// ExportPubKey returns the public key associated with the address
// serialized as a hex encoded string.
//
// This is part of the ManagedPubKeyAddress interface implementation.
func (a *managedAddress) ExportPubKey() string {
	return hex.EncodeToString(a.pubKeyBytes())
}

// newManagedAddressWithoutPrivKey returns a new managed address based on the
// passed account, public key, and whether or not the public key should be
// compressed.
func newManagedAddressWithoutPrivKey(m *Manager, account uint32, pubKey chainec.PublicKey, compressed bool) (*managedAddress, error) {
	// Create a pay-to-pubkey-hash address from the public key.
	var pubKeyHash []byte
	if compressed {
		pubKeyHash = dcrutil.Hash160(pubKey.SerializeCompressed())
	} else {
		pubKeyHash = dcrutil.Hash160(pubKey.SerializeUncompressed())
	}
	address, err := dcrutil.NewAddressPubKeyHash(pubKeyHash, m.chainParams,
		dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, err
	}

	return &managedAddress{
		manager:    m,
		address:    address,
		account:    account,
		imported:   false,
		internal:   false,
		multisig:   false,
		compressed: compressed,
		pubKey:     pubKey,
	}, nil
}

// newManagedAddressFromExtKey returns a new managed address based on the passed
// account and extended key.  The managed address will have access to the
// private and public keys if the provided extended key is private, otherwise it
// will only have access to the public key.
func newManagedAddressFromExtKey(m *Manager, account uint32, key *hdkeychain.ExtendedKey) (*managedAddress, error) {
	pubKey, err := key.ECPubKey()
	if err != nil {
		return nil, err
	}

	return newManagedAddressWithoutPrivKey(m, account, pubKey, true)
}

// scriptAddress represents a pay-to-script-hash address.
type scriptAddress struct {
	manager *Manager
	account uint32
	address *dcrutil.AddressScriptHash
}

// Enforce scriptAddress satisfies the ManagedScriptAddress interface.
var _ ManagedScriptAddress = (*scriptAddress)(nil)

// Account returns the account the address is associated with.  This will always
// be the ImportedAddrAccount constant for script addresses.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) Account() uint32 {
	return a.account
}

// Address returns the dcrutil.Address which represents the managed address.
// This will be a pay-to-script-hash address.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) Address() dcrutil.Address {
	return a.address
}

// AddrHash returns the script hash for the address.
//
// This is part of the ManagedAddress interface implementation.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) AddrHash() []byte {
	return a.address.Hash160()[:]
}

// Imported always returns true since script addresses are always imported
// addresses and not part of any chain.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) Imported() bool {
	return true
}

// Internal always returns false since script addresses are always imported
// addresses and not part of any chain in order to be for internal use.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) Internal() bool {
	return false
}

// Multisig always returns false since script addresses are always imported
// addresses and not part of any chain in order to be for multisig use.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) Multisig() bool {
	return false
}

// Compressed returns false since script addresses are never compressed.
//
// This is part of the ManagedAddress interface implementation.
func (a *scriptAddress) Compressed() bool {
	return false
}

func (*scriptAddress) isScriptAddress() {}

// newScriptAddress initializes and returns a new pay-to-script-hash address.
func newScriptAddress(m *Manager, account uint32, scriptHash []byte) (*scriptAddress, error) {
	address, err := dcrutil.NewAddressScriptHashFromHash(scriptHash,
		m.chainParams)
	if err != nil {
		return nil, err
	}

	return &scriptAddress{
		manager: m,
		account: account,
		address: address,
	}, nil
}
