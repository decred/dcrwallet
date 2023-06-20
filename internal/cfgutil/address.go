// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import (
	"decred.org/dcrwallet/v4/errors"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
)

// AddressFlag contains a stdaddr.Address and implements the flags.Marshaler and
// Unmarshaler interfaces so it can be used as a config struct field.
type AddressFlag struct {
	str string
}

// NewAddressFlag creates an AddressFlag with a default stdaddr.Address.
func NewAddressFlag() *AddressFlag {
	return new(AddressFlag)
}

// MarshalFlag satisfies the flags.Marshaler interface.
func (a *AddressFlag) MarshalFlag() (string, error) {
	return a.str, nil
}

// UnmarshalFlag satisfies the flags.Unmarshaler interface.
func (a *AddressFlag) UnmarshalFlag(addr string) error {
	a.str = addr
	return nil
}

// Address decodes the address flag for the network described by params.
// If the flag is the empty string, this returns a nil address.
func (a *AddressFlag) Address(params stdaddr.AddressParams) (stdaddr.Address, error) {
	if a.str == "" {
		return nil, nil
	}
	return stdaddr.DecodeAddress(a.str, params)
}

// StakeAddress decodes the address flag for the network described by
// params as a stake address.
// If the flag is the empty string, this returns a nil address.
func (a *AddressFlag) StakeAddress(params stdaddr.AddressParams) (stdaddr.StakeAddress, error) {
	addr, err := a.Address(params)
	if err != nil {
		return nil, err
	}
	if addr == nil {
		return nil, nil
	}
	if saddr, ok := addr.(stdaddr.StakeAddress); ok {
		return saddr, nil
	}
	return nil, errors.Errorf("address is not suitable for stake usage")
}
