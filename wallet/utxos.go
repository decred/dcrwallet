package wallet

import (
	"fmt"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/walletdb"
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
func (w *Wallet) UnspentOutputs(policy OutputSelectionPolicy) ([]*TransactionOutput, error) {
	var outputResults []*TransactionOutput
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		// TODO: actually stream outputs from the db instead of fetching
		// all of them at once.
		outputs, err := w.TxStore.UnspentOutputs(txmgrNs)
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
				txscript.DefaultScriptVersion, output.PkScript,
				w.chainParams)
			if err != nil || len(addrs) == 0 {
				// Cannot determine which account this belongs
				// to without a valid address.  TODO: Fix this
				// by saving outputs per account, or accounts
				// per output.
				continue
			}
			outputAcct, err := w.Manager.AddrAccount(addrmgrNs, addrs[0])
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
					Version:  txscript.DefaultScriptVersion,
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
	return outputResults, err
}

// SelectInputs selects transaction inputs to redeem unspent outputs stored in
// the wallet.  It returns the total input amount referenced by the previous
// transaction outputs, a slice of transaction inputs referencing these outputs,
// and a slice of previous output scripts from each previous output referenced
// by the corresponding input.
func (w *Wallet) SelectInputs(targetAmount dcrutil.Amount, policy OutputSelectionPolicy) (total dcrutil.Amount,
	inputs []*wire.TxIn, prevScripts [][]byte, err error) {

	err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		if policy.Account != waddrmgr.ImportedAddrAccount {
			lastAcct, err := w.Manager.LastAccount(addrmgrNs)
			if err != nil {
				return err
			}
			if policy.Account > lastAcct {
				return waddrmgr.ManagerError{
					ErrorCode:   waddrmgr.ErrAccountNotFound,
					Description: "account not found",
				}
			}
		}

		sourceImpl := w.TxStore.MakeInputSource(txmgrNs, addrmgrNs, policy.Account,
			policy.RequiredConfirmations, tipHeight)
		var err error
		total, inputs, prevScripts, err = sourceImpl.SelectInputs(targetAmount)
		return err
	})
	return
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
func (w *Wallet) OutputInfo(op *wire.OutPoint) (OutputInfo, error) {
	var info OutputInfo
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		txDetails, err := w.TxStore.TxDetails(txmgrNs, &op.Hash)
		if err != nil {
			return err
		}
		if op.Index >= uint32(len(txDetails.TxRecord.MsgTx.TxOut)) {
			return fmt.Errorf("output %d not found, transaction only contains %d outputs",
				op.Index, len(txDetails.TxRecord.MsgTx.TxOut))
		}

		info.Received = txDetails.Received
		info.Amount = dcrutil.Amount(txDetails.TxRecord.MsgTx.TxOut[op.Index].Value)
		info.FromCoinbase = blockchain.IsCoinBaseTx(&txDetails.TxRecord.MsgTx)
		return nil
	})
	return info, err
}
