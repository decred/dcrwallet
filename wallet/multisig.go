// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"runtime/trace"

	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// MakeSecp256k1MultiSigScript creates a multi-signature script that can be
// redeemed with nRequired signatures of the passed keys and addresses.  If the
// address is a P2PKH address, the associated pubkey is looked up by the wallet
// if possible, otherwise an error is returned for a missing pubkey.
//
// This function only works with secp256k1 pubkeys and P2PKH addresses derived
// from them.
func (w *Wallet) MakeSecp256k1MultiSigScript(ctx context.Context, secp256k1Addrs []dcrutil.Address, nRequired int) ([]byte, error) {
	const op errors.Op = "wallet.MakeSecp256k1MultiSigScript"

	secp256k1PubKeys := make([]*dcrutil.AddressSecpPubKey, len(secp256k1Addrs))

	var dbtx walletdb.ReadTx
	var addrmgrNs walletdb.ReadBucket
	defer func() {
		if dbtx != nil {
			dbtx.Rollback()
		}
	}()

	// The address list will made up either of addreseses (pubkey hash), for
	// which we need to look up the keys in wallet, straight pubkeys, or a
	// mixture of the two.
	for i, addr := range secp256k1Addrs {
		switch addr := addr.(type) {
		default:
			return nil, errors.E(op, errors.Invalid, "address key is not secp256k1")

		case *dcrutil.AddressSecpPubKey:
			secp256k1PubKeys[i] = addr

		case *dcrutil.AddressPubKeyHash:
			if addr.DSA() != dcrec.STEcdsaSecp256k1 {
				return nil, errors.E(op, errors.Invalid, "address key is not secp256k1")
			}

			if dbtx == nil {
				var err error
				defer trace.StartRegion(ctx, "db.View").End()
				dbtx, err = w.db.BeginReadTx()
				if err != nil {
					return nil, err
				}
				defer trace.StartRegion(ctx, "db.ReadTx").End()
				addrmgrNs = dbtx.ReadBucket(waddrmgrNamespaceKey)
			}
			addrInfo, err := w.Manager.Address(addrmgrNs, addr)
			if err != nil {
				return nil, err
			}
			serializedPubKey := addrInfo.(udb.ManagedPubKeyAddress).
				PubKey().Serialize()

			pubKeyAddr, err := dcrutil.NewAddressSecpPubKey(
				serializedPubKey, w.chainParams)
			if err != nil {
				return nil, err
			}
			secp256k1PubKeys[i] = pubKeyAddr
		}
	}

	script, err := txscript.MultiSigScript(secp256k1PubKeys, nRequired)
	if err != nil {
		return nil, errors.E(op, errors.E(errors.Op("txscript.MultiSigScript"), err))
	}
	return script, nil
}

// ImportP2SHRedeemScript adds a P2SH redeem script to the wallet.
func (w *Wallet) ImportP2SHRedeemScript(ctx context.Context, script []byte) (*dcrutil.AddressScriptHash, error) {
	const op errors.Op = "wallet.ImportP2SHRedeemScript"

	var p2shAddr *dcrutil.AddressScriptHash
	err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadWriteBucket(wtxmgrNamespaceKey)

		err := w.TxStore.InsertTxScript(txmgrNs, script)
		if err != nil {
			return err
		}

		addrInfo, err := w.Manager.ImportScript(addrmgrNs, script)
		if err != nil {
			// Don't care if it's already there, but still have to
			// set the p2shAddr since the address manager didn't
			// return anything useful.
			if errors.Is(err, errors.Exist) {
				// This function will never error as it always
				// hashes the script to the correct length.
				p2shAddr, _ = dcrutil.NewAddressScriptHash(script,
					w.chainParams)
				return nil
			}
			return err
		}

		p2shAddr = addrInfo.Address().(*dcrutil.AddressScriptHash)
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return p2shAddr, nil
}

// FetchP2SHMultiSigOutput fetches information regarding a wallet's P2SH
// multi-signature output.
func (w *Wallet) FetchP2SHMultiSigOutput(ctx context.Context, outPoint *wire.OutPoint) (*P2SHMultiSigOutput, error) {
	const op errors.Op = "wallet.FetchP2SHMultiSigOutput"

	var (
		mso          *udb.MultisigOut
		redeemScript []byte
	)
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		var err error

		mso, err = w.TxStore.GetMultisigOutput(txmgrNs, outPoint)
		if err != nil {
			return err
		}

		redeemScript, err = w.TxStore.GetTxScript(txmgrNs, mso.ScriptHash[:])
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	p2shAddr, err := dcrutil.NewAddressScriptHashFromHash(
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

// FetchAllRedeemScripts returns all P2SH redeem scripts saved by the wallet.
func (w *Wallet) FetchAllRedeemScripts(ctx context.Context) ([][]byte, error) {
	const op errors.Op = "wallet.FetchAllRedeemScripts"

	var redeemScripts [][]byte
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		redeemScripts = w.TxStore.StoredTxScripts(txmgrNs)
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return redeemScripts, nil
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
