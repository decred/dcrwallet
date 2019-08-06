// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import (
	"github.com/decred/dcrd/dcrutil/v2"
)

// AddressFlag contains a dcrutil.Address and implements the flags.Marshaler and
// Unmarshaler interfaces so it can be used as a config struct field.
type AddressFlag struct {
	str string
}

// NewAddressFlag creates an AddressFlag with a default dcrutil.Address.
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
func (a *AddressFlag) Address(params dcrutil.AddressParams) (dcrutil.Address, error) {
	if a.str == "" {
		return nil, nil
	}
	return dcrutil.DecodeAddress(a.str, params)
}
