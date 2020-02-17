// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/wallet/udb"
	"decred.org/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrd/dcrutil/v3"
)

// StakePoolUserInfo returns the stake pool user information for a user
// identified by their P2SH voting address.
func (w *Wallet) StakePoolUserInfo(ctx context.Context, userAddress dcrutil.Address) (*udb.StakePoolUser, error) {
	const op errors.Op = "wallet.StakePoolUserInfo"

	switch userAddress.(type) {
	case *dcrutil.AddressPubKeyHash: // ok
	case *dcrutil.AddressScriptHash: // ok
	default:
		return nil, errors.E(op, errors.Invalid, "address must be P2PKH or P2SH")
	}

	var user *udb.StakePoolUser
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		var err error
		user, err = w.StakeMgr.StakePoolUserInfo(stakemgrNs, userAddress)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return user, nil
}
