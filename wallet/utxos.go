// Copyright (c) 2016-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"time"

	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/wallet/txauthor"
	"decred.org/dcrwallet/wallet/udb"
	"decred.org/dcrwallet/wallet/walletdb"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

// OutputSelectionPolicy describes the rules for selecting an output from the
// wallet.
type OutputSelectionPolicy struct {
	Account               uint32
	RequiredConfirmations int32
}

func (p *OutputSelectionPolicy) meetsRequiredConfs(txHeight, curHeight int32) bool {
	return confirmed(p.RequiredConfirmations, txHeight, curHeight)
}

// UnspentOutputs fetches all unspent outputs from the wallet that match rules
// described in the passed policy.
func (w *Wallet) UnspentOutputs(ctx context.Context, policy OutputSelectionPolicy) ([]*TransactionOutput, error) {
	const op errors.Op = "wallet.UnspentOutputs"

	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	var outputResults []*TransactionOutput
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.txStore.MainChainTip(txmgrNs)

		// TODO: actually stream outputs from the db instead of fetching
		// all of them at once.
		outputs, err := w.txStore.UnspentOutputs(txmgrNs)
		if err != nil {
			return err
		}

		for _, output := range outputs {
			// Ignore outputs that haven't reached the required
			// number of confirmations.
			if !policy.meetsRequiredConfs(output.Height, tipHeight) {
				continue
			}

			// Ignore outputs that are not controlled by the account.
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(
				0, output.PkScript,
				w.chainParams)
			if err != nil || len(addrs) == 0 {
				// Cannot determine which account this belongs
				// to without a valid address.  TODO: Fix this
				// by saving outputs per account, or accounts
				// per output.
				continue
			}
			outputAcct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
			if err != nil {
				return err
			}
			if outputAcct != policy.Account {
				continue
			}

			// Stakebase isn't exposed by wtxmgr so those will be
			// OutputKindNormal for now.
			outputSource := OutputKindNormal
			if output.FromCoinBase {
				outputSource = OutputKindCoinbase
			}

			result := &TransactionOutput{
				OutPoint: output.OutPoint,
				Output: wire.TxOut{
					Value: int64(output.Amount),
					// TODO: version is bogus but there is
					// only version 0 at time of writing.
					Version:  0,
					PkScript: output.PkScript,
				},
				OutputKind:      outputSource,
				ContainingBlock: BlockIdentity(output.Block),
				ReceiveTime:     output.Received,
			}
			outputResults = append(outputResults, result)
		}

		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return outputResults, nil
}

// SelectInputs selects transaction inputs to redeem unspent outputs stored in
// the wallet.  It returns an input detail summary.
func (w *Wallet) SelectInputs(ctx context.Context, targetAmount dcrutil.Amount, policy OutputSelectionPolicy) (inputDetail *txauthor.InputDetail, err error) {
	const op errors.Op = "wallet.SelectInputs"

	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.txStore.MainChainTip(txmgrNs)

		if policy.Account != udb.ImportedAddrAccount {
			lastAcct, err := w.manager.LastAccount(addrmgrNs)
			if err != nil {
				return err
			}
			if policy.Account > lastAcct {
				return errors.E(errors.NotExist, "account not found")
			}
		}

		sourceImpl := w.txStore.MakeInputSource(txmgrNs, addrmgrNs, policy.Account,
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
		info.FromCoinbase = blockchain.IsCoinBaseTx(&txDetails.TxRecord.MsgTx)
		return nil
	})
	if err != nil {
		return info, errors.E(op, err)
	}
	return info, nil
}
