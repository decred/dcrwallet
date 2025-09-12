// Copyright (c) 2016-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"time"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/internal/compat"
	"decred.org/dcrwallet/v5/wallet/txauthor"
	"decred.org/dcrwallet/v5/wallet/udb"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

// OutputSelectionPolicy describes the rules for selecting an output from the
// wallet.
type OutputSelectionPolicy struct {
	Account               uint32
	RequiredConfirmations int32
}

// SelectInputs selects transaction inputs to redeem unspent outputs stored in
// the wallet.  It returns an input detail summary.
func (w *Wallet) SelectInputs(ctx context.Context, targetAmount dcrutil.Amount, policy OutputSelectionPolicy) (inputDetail *txauthor.InputDetail, err error) {
	const op errors.Op = "wallet.SelectInputs"

	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		_, tipHeight := w.txStore.MainChainTip(dbtx)

		if policy.Account != udb.ImportedAddrAccount {
			lastAcct, err := w.manager.LastAccount(addrmgrNs)
			if err != nil {
				return err
			}
			if policy.Account > lastAcct {
				return errors.E(errors.NotExist, "account not found")
			}
		}

		sourceImpl := w.txStore.MakeInputSource(dbtx, policy.Account,
			policy.RequiredConfirmations, tipHeight, nil)
		var err error
		inputDetail, err = sourceImpl.SelectInputs(targetAmount)
		return err
	})
	if err != nil {
		err = errors.E(op, err)
	}
	return inputDetail, err
}

// OutputInfo describes additional info about an output which can be queried
// using an outpoint.
type OutputInfo struct {
	Received     time.Time
	Amount       dcrutil.Amount
	FromCoinbase bool
}

// OutputInfo queries the wallet for additional transaction output info
// regarding an outpoint.
func (w *Wallet) OutputInfo(ctx context.Context, out *wire.OutPoint) (OutputInfo, error) {
	const op errors.Op = "wallet.OutputInfo"
	var info OutputInfo
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		txDetails, err := w.txStore.TxDetails(txmgrNs, &out.Hash)
		if err != nil {
			return err
		}
		if out.Index >= uint32(len(txDetails.TxRecord.MsgTx.TxOut)) {
			return errors.Errorf("transaction has no output %d", out.Index)
		}

		info.Received = txDetails.Received
		info.Amount = dcrutil.Amount(txDetails.TxRecord.MsgTx.TxOut[out.Index].Value)
		info.FromCoinbase = compat.IsEitherCoinBaseTx(&txDetails.TxRecord.MsgTx)
		return nil
	})
	if err != nil {
		return info, errors.E(op, err)
	}
	return info, nil
}
