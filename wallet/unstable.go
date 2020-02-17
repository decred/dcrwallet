// Copyright (c) 2016-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/wallet/udb"
	"decred.org/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
)

type unstableAPI struct {
	w *Wallet
}

// UnstableAPI exposes additional unstable public APIs for a Wallet.  These APIs
// may be changed or removed at any time.  Currently this type exists to ease
// the transition (particularly for the legacy JSON-RPC server) from using
// exported manager packages to a unified wallet package that exposes all
// functionality by itself.  New code should not be written using this API.
func UnstableAPI(w *Wallet) unstableAPI { return unstableAPI{w} }

// TxDetails calls udb.Store.TxDetails under a single database view transaction.
func (u unstableAPI) TxDetails(ctx context.Context, txHash *chainhash.Hash) (*udb.TxDetails, error) {
	const op errors.Op = "wallet.TxDetails"

	var details *udb.TxDetails
	err := walletdb.View(ctx, u.w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		details, err = u.w.TxStore.TxDetails(txmgrNs, txHash)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return details, nil
}

// RangeTransactions calls udb.Store.RangeTransactions under a single
// database view tranasction.
func (u unstableAPI) RangeTransactions(ctx context.Context, begin, end int32, f func([]udb.TxDetails) (bool, error)) error {
	const op errors.Op = "wallet.RangeTransactions"
	err := walletdb.View(ctx, u.w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		return u.w.TxStore.RangeTransactions(txmgrNs, begin, end, f)
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// UnspentMultisigCreditsForAddress calls
// udb.Store.UnspentMultisigCreditsForAddress under a single database view
// transaction.
func (u unstableAPI) UnspentMultisigCreditsForAddress(ctx context.Context, p2shAddr *dcrutil.AddressScriptHash) ([]*udb.MultisigCredit, error) {
	const op errors.Op = "wallet.UnspentMultisigCreditsForAddress"
	var multisigCredits []*udb.MultisigCredit
	err := walletdb.View(ctx, u.w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error
		multisigCredits, err = u.w.TxStore.UnspentMultisigCreditsForAddress(
			txmgrNs, p2shAddr)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return multisigCredits, nil
}
