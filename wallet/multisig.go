// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/txrules"
	"decred.org/dcrwallet/v5/wallet/txsizes"
	"decred.org/dcrwallet/v5/wallet/udb"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

// FetchP2SHMultiSigOutput fetches information regarding a wallet's P2SH
// multi-signature output.
func (w *Wallet) FetchP2SHMultiSigOutput(ctx context.Context, outPoint *wire.OutPoint) (*P2SHMultiSigOutput, error) {
	const op errors.Op = "wallet.FetchP2SHMultiSigOutput"

	var (
		mso          *udb.MultisigOut
		redeemScript []byte
	)
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		addrmgrNs := tx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error

		mso, err = w.txStore.GetMultisigOutput(txmgrNs, outPoint)
		if err != nil {
			return err
		}

		addr, _ := stdaddr.NewAddressScriptHashV0FromHash(mso.ScriptHash[:], w.chainParams)
		redeemScript, err = w.manager.RedeemScript(addrmgrNs, addr)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	p2shAddr, err := stdaddr.NewAddressScriptHashV0FromHash(
		mso.ScriptHash[:], w.chainParams)
	if err != nil {
		return nil, err
	}

	multiSigOutput := P2SHMultiSigOutput{
		OutPoint:     *mso.OutPoint,
		OutputAmount: mso.Amount,
		ContainingBlock: BlockIdentity{
			Hash:   mso.BlockHash,
			Height: int32(mso.BlockHeight),
		},
		P2SHAddress:  p2shAddr,
		RedeemScript: redeemScript,
		M:            mso.M,
		N:            mso.N,
		Redeemer:     nil,
	}

	if mso.Spent {
		multiSigOutput.Redeemer = &OutputRedeemer{
			TxHash:     mso.SpentBy,
			InputIndex: mso.SpentByIndex,
		}
	}

	return &multiSigOutput, nil
}

// PrepareRedeemMultiSigOutTxOutput estimates the tx value for a MultiSigOutTx
// output and adds it to msgTx.
func (w *Wallet) PrepareRedeemMultiSigOutTxOutput(msgTx *wire.MsgTx, p2shOutput *P2SHMultiSigOutput, pkScript *[]byte) error {
	const op errors.Op = "wallet.PrepareRedeemMultiSigOutTxOutput"

	scriptSizes := make([]int, 0, len(msgTx.TxIn))
	// generate the script sizes for the inputs
	for range msgTx.TxIn {
		scriptSizes = append(scriptSizes, txsizes.RedeemP2SHSigScriptSize)
	}

	// estimate the output fee
	txOut := wire.NewTxOut(0, *pkScript)
	feeSize := txsizes.EstimateSerializeSize(scriptSizes, []*wire.TxOut{txOut}, 0)
	feeEst := txrules.FeeForSerializeSize(w.RelayFee(), feeSize)
	if feeEst >= p2shOutput.OutputAmount {
		return errors.E(op, errors.Errorf("estimated fee %v is above output value %v",
			feeEst, p2shOutput.OutputAmount))
	}

	toReceive := p2shOutput.OutputAmount - feeEst
	// set the output value and add to the tx
	txOut.Value = int64(toReceive)
	msgTx.AddTxOut(txOut)
	return nil
}
