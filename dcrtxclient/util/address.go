// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package util

import "github.com/decred/dcrd/dcrutil"

// AddressFlag embeds a dcrutil.Address and implements the flags.Marshaler and
// Unmarshaler interfaces so it can be used as a config struct field.
type AddressFlag struct {
	dcrutil.Address
}

// NewAddressFlag creates an AddressFlag with a default dcrutil.Address.
func NewAddressFlag(defaultValue dcrutil.Address) *AddressFlag {
	return &AddressFlag{defaultValue}
}

// MarshalFlag satisifes the flags.Marshaler interface.
func (a *AddressFlag) MarshalFlag() (string, error) {
	if a.Address != nil {
		return a.Address.String(), nil
	}

	return "", nil
}

// UnmarshalFlag satisifes the flags.Unmarshaler interface.
func (a *AddressFlag) UnmarshalFlag(addr string) error {
	if addr == "" {
		a.Address = nil
		return nil
	}
	address, err := dcrutil.DecodeAddress(addr)
	if err != nil {
		return err
	}
	a.Address = address
	return nil
}
