// Copyright (c) 2015-2016 The btcsuite developers
// Copyright (c) 2016-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package cfgutil

import (
	"decred.org/dcrwallet/payments"
	"github.com/decred/dcrd/dcrutil/v3"
)

// AddressFlag implements the flags.Marshaler and Unmarshaler interfaces
// and returns parsed addresses from the configured values (if any).
type AddressFlag struct {
	str string
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
func (a *AddressFlag) Address(params dcrutil.AddressParams) (payments.Address, error) {
	if a.str == "" {
		return nil, nil
	}
	return payments.DecodeAddress(a.str, params)
}
