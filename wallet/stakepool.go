// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/wallet/udb"
	"decred.org/dcrwallet/v3/wallet/walletdb"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
)

// StakePoolUserInfo returns the stake pool user information for a user
// identified by their voting address.
func (w *Wallet) StakePoolUserInfo(ctx context.Context, userAddress stdaddr.StakeAddress) (*udb.StakePoolUser, error) {
	const op errors.Op = "wallet.StakePoolUserInfo"

	if !w.stakePoolEnabled {
		return nil, errors.E(op, errors.Invalid, "VSP features are disabled")
	}

	var user *udb.StakePoolUser
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		stakemgrNs := tx.ReadBucket(wstakemgrNamespaceKey)
		var err error
		user, err = w.stakeMgr.StakePoolUserInfo(stakemgrNs, userAddress)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return user, nil
}
