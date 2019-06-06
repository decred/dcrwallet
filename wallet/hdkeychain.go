// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	hdkeychain1 "github.com/decred/dcrd/hdkeychain"
	hdkeychain2 "github.com/decred/dcrd/hdkeychain/v2"
)

// hd2to1 converts a hdkeychain/v2 extended key to the v1 API.
// An error check during string conversion is intentionally dropped for brevity.
func hd2to1(k2 *hdkeychain2.ExtendedKey) *hdkeychain1.ExtendedKey {
	k, _ := hdkeychain1.NewKeyFromString(k2.String())
	return k
}

// hd1to2 converts a v1 extended key to the v2 API.
// An error check during string conversion is intentionally dropped for brevity.
func hd1to2(k *hdkeychain1.ExtendedKey, params hdkeychain2.NetworkParams) *hdkeychain2.ExtendedKey {
	k2, _ := hdkeychain2.NewKeyFromString(k.String(), params)
	return k2
}
