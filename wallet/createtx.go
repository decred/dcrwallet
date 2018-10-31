// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/mempool"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/internal/walletdb"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"

	"crypto/elliptic"
	"crypto/rand"

	"net/http"

	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"

	pb "github.com/decred/dcrwallet/dcrtxclient/api/messages"
	"github.com/decred/dcrwallet/dcrtxclient/chacharng"
	"github.com/decred/dcrwallet/dcrtxclient/finitefield"
	"github.com/decred/dcrwallet/dcrtxclient/messages"
	"github.com/decred/dcrwallet/dcrtxclient/ripemd128"
	"github.com/decred/dcrwallet/dcrtxclient/util"
	"github.com/wsddn/go-ecdh"
)

// --------------------------------------------------------------------------------
// Constants and simple functions

const (
	// defaultTicketFeeLimits is the default byte string for the default
	// fee limits imposed on a ticket.
	defaultTicketFeeLimits = 0x5800

	// maxStandardTxSize is the maximum size allowed for transactions that
	// are considered standard and will therefore be relayed and considered
	// for mining.
	// TODO: import from dcrd.
	maxStandardTxSize = 100000

	// sanityVerifyFlags are the flags used to enable and disable features of
	// the txscript engine used for sanity checking of transactions signed by
	// the wallet.
	sanityVerifyFlags = mempool.BaseStandardVerifyFlags
)

var (
	// maxTxSize is the maximum size of a transaction we can
	// build with the wallet.
	maxTxSize = chaincfg.MainNetParams.MaxTxSize
)

// extendedOutPoint is a UTXO with an amount.
type extendedOutPoint struct {
	op       *wire.OutPoint
	amt      int64
	pkScript []byte
}

// --------------------------------------------------------------------------------
// Transaction creation

// OutputSelectionAlgorithm specifies the algorithm to use when selecting outputs
// to construct a transaction.
type OutputSelectionAlgorithm uint

const (
	// OutputSelectionAlgorithmDefault describes the default output selection
	// algorithm.  It is not optimized for any particular use case.
	OutputSelectionAlgorithmDefault = iota

	// OutputSelectionAlgorithmAll describes the output selection algorithm of
	// picking every possible availble output.  This is useful for sweeping.
	OutputSelectionAlgorithmAll
)

// NewUnsignedTransaction constructs an unsigned transaction using unspent
// account outputs.
//
// The changeSource parameter is optional and can be nil.  When nil, and if a
// change output should be added, an internal change address is created for the
// account.
func (w *Wallet) NewUnsignedTransaction(outputs []*wire.TxOut, relayFeePerKb dcrutil.Amount, account uint32, minConf int32,
	algo OutputSelectionAlgorithm, changeSource txauthor.ChangeSource) (*txauthor.AuthoredTx, error) {

	const op errors.Op = "wallet.NewUnsignedTransaction"

	var authoredTx *txauthor.AuthoredTx
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		if account != udb.ImportedAddrAccount {
			lastAcct, err := w.Manager.LastAccount(addrmgrNs)
			if err != nil {
				return err
			}
			if account > lastAcct {
				return errors.E(errors.NotExist, "missing account")
			}
		}

		sourceImpl := w.TxStore.MakeInputSource(txmgrNs, addrmgrNs, account,
			minConf, tipHeight)
		var inputSource txauthor.InputSource
		switch algo {
		case OutputSelectionAlgorithmDefault:
			inputSource = sourceImpl.SelectInputs
		case OutputSelectionAlgorithmAll:
			// Wrap the source with one that always fetches the max amount
			// available and ignores insufficient balance issues.
			inputSource = func(dcrutil.Amount) (*txauthor.InputDetail, error) {
				inputDetail, err := sourceImpl.SelectInputs(dcrutil.MaxAmount)
				if errors.Is(errors.InsufficientBalance, err) {
					err = nil
				}
				return inputDetail, err
			}
		default:
			return errors.E(errors.Invalid,
				errors.Errorf("unknown output selection algorithm %v", algo))
		}

		if changeSource == nil {
			changeSource = &p2PKHChangeSource{
				persist: w.deferPersistReturnedChild(&changeSourceUpdates),
				account: account,
				wallet:  w,
			}
		}

		var err error
		authoredTx, err = txauthor.NewUnsignedTransaction(outputs, relayFeePerKb,
			inputSource, changeSource)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	if len(changeSourceUpdates) != 0 {
		err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
			for _, up := range changeSourceUpdates {
				err := up(tx)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, errors.E(op, err)
		}
	}
	return authoredTx, nil
}

// secretSource is an implementation of txauthor.SecretSource for the wallet's
// address manager.
type secretSource struct {
	*udb.Manager
	addrmgrNs walletdb.ReadBucket
	doneFuncs []func()
}

func (s *secretSource) GetKey(addr dcrutil.Address) (chainec.PrivateKey, bool, error) {
	privKey, done, err := s.Manager.PrivateKey(s.addrmgrNs, addr)
	if err != nil {
		return nil, false, err
	}
	s.doneFuncs = append(s.doneFuncs, done)
	return privKey, true, nil
}

func (s *secretSource) GetScript(addr dcrutil.Address) ([]byte, error) {
	script, done, err := s.Manager.RedeemScript(s.addrmgrNs, addr)
	if err != nil {
		return nil, err
	}
	s.doneFuncs = append(s.doneFuncs, done)
	return script, nil
}

// CreatedTx holds the state of a newly-created transaction and the change
// output (if one was added).
type CreatedTx struct {
	MsgTx       *wire.MsgTx
	ChangeAddr  dcrutil.Address
	ChangeIndex int // negative if no change
	Fee         dcrutil.Amount
}

// insertIntoTxMgr inserts a newly created transaction into the tx store
// as unconfirmed.
func (w *Wallet) insertIntoTxMgr(ns walletdb.ReadWriteBucket, msgTx *wire.MsgTx) (*udb.TxRecord, error) {
	// Create transaction record and insert into the db.
	rec, err := udb.NewTxRecordFromMsgTx(msgTx, time.Now())
	if err != nil {
		return nil, err
	}
	err = w.TxStore.InsertMemPoolTx(ns, rec)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (w *Wallet) insertCreditsIntoTxMgr(op errors.Op, tx walletdb.ReadWriteTx, msgTx *wire.MsgTx, rec *udb.TxRecord) error {
	addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := tx.ReadWriteBucket(wtxmgrNamespaceKey)

	// Check every output to determine whether it is controlled by a wallet
	// key.  If so, mark the output as a credit.
	for i, output := range msgTx.TxOut {
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(output.Version,
			output.PkScript, w.chainParams)
		if err != nil {
			// Non-standard outputs are skipped.
			continue
		}
		for _, addr := range addrs {
			ma, err := w.Manager.Address(addrmgrNs, addr)
			if err == nil {
				// TODO: Credits should be added with the
				// account they belong to, so wtxmgr is able to
				// track per-account balances.
				err = w.TxStore.AddCredit(txmgrNs, rec, nil,
					uint32(i), ma.Internal(), ma.Account())
				if err != nil {
					return errors.E(op, err)
				}
				err = w.markUsedAddress(op, tx, ma)
				if err != nil {
					return err
				}
				log.Debugf("Marked address %v used", addr)
				continue
			}

			// Missing addresses are skipped.  Other errors should
			// be propagated.
			if !errors.Is(errors.NotExist, err) {
				return errors.E(op, err)
			}
		}
	}

	return nil
}

// insertMultisigOutIntoTxMgr inserts a multisignature output into the
// transaction store database.
func (w *Wallet) insertMultisigOutIntoTxMgr(ns walletdb.ReadWriteBucket, msgTx *wire.MsgTx, index uint32) error {
	// Create transaction record and insert into the db.
	rec, err := udb.NewTxRecordFromMsgTx(msgTx, time.Now())
	if err != nil {
		return err
	}

	return w.TxStore.AddMultisigOut(ns, rec, nil, index)
}

// checkHighFees performs a high fee check if enabled and possible, returning an
// error if the transaction pays high fees.
func (w *Wallet) checkHighFees(totalInput dcrutil.Amount, tx *wire.MsgTx) error {
	if w.AllowHighFees {
		return nil
	}
	if txrules.PaysHighFees(totalInput, tx) {
		return errors.E(errors.Policy, "high fee")
	}
	return nil
}

// txToOutputs creates a transaction, selecting previous outputs from an account
// with no less than minconf confirmations, and creates a signed transaction
// that pays to each of the outputs.
func (w *Wallet) txToOutputs(op errors.Op, outputs []*wire.TxOut, account uint32,
	minconf int32, randomizeChangeIdx bool) (*txauthor.AuthoredTx, error) {

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, errors.E(op, err)
	}

	return w.txToOutputsInternal(op, outputs, account, minconf, n,
		randomizeChangeIdx, w.RelayFee())
}

// txToOutputsSplitTx creates a input,output on each participants
// then each participant send to dcrtxmatcher server for create transaction.
// dcrtxmatcher will send back the transaction for sign
func (w *Wallet) txToOutputsSplitTx(outputs []*wire.TxOut, account uint32, minconf int32,
	n NetworkBackend, randomizeChangeIdx bool, txFee dcrutil.Amount) (*txauthor.AuthoredTx, *[]func(walletdb.ReadWriteTx) error, error) {

	const op errors.Op = "wallet.txToOutputsSplitTx"
	var atx *txauthor.AuthoredTx

	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Create the unsigned transaction.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		inputSource := w.TxStore.MakeInputSource(txmgrNs, addrmgrNs, account,
			minconf, tipHeight)
		changeSource := &p2PKHChangeSource{
			persist: w.deferPersistReturnedChild(&changeSourceUpdates),
			account: account,
			wallet:  w,
		}
		var err error
		atx, err = txauthor.NewUnsignedTransaction(outputs, txFee,
			inputSource.SelectInputs, changeSource)
		if err != nil {
			return err
		}

		// Randomize change position, if change exists, before signing.  This
		// doesn't affect the serialize size, so the change amount will still be
		// valid.
		if atx.ChangeIndex >= 0 && randomizeChangeIdx {
			atx.RandomizeChangePosition()
		}

		return err
	})
	if err != nil {
		return nil, nil, errors.E(op, err)
	}

	// Warn when spending UTXOs controlled by imported keys created change for
	// the default account.
	if atx.ChangeIndex >= 0 && account == udb.ImportedAddrAccount {
		changeAmount := dcrutil.Amount(atx.Tx.TxOut[atx.ChangeIndex].Value)
		log.Warnf("Spend from imported account produced change: moving"+
			" %v from imported account into default account.", changeAmount)
	}

	err = w.checkHighFees(atx.TotalInput, atx.Tx)
	if err != nil {
		return nil, nil, err
	}

	return atx, &changeSourceUpdates, nil
}

type ChangeSourceFunc func(walletdb.ReadWriteTx) error

// publishTx publishes the transaction to network.
func (w *Wallet) publishTx(tx *wire.MsgTx, changeSourceUpdates *[]func(walletdb.ReadWriteTx) error, n NetworkBackend) error {
	rec, err := udb.NewTxRecordFromMsgTx(tx, time.Now())
	if err != nil {
		return err
	}

	// Use a single DB update to store and publish the transaction.  If the
	// transaction is rejected, the update is rolled back.
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, up := range *changeSourceUpdates {
			err := up(dbtx)
			if err != nil {
				return err
			}
		}

		// TODO: this can be improved by not using the same codepath as notified
		// relevant transactions, since this does a lot of extra work.
		err = w.processTransactionRecord(dbtx, rec, nil, nil)
		if err != nil {
			return err
		}

		return n.PublishTransactions(context.TODO(), tx)
	})
	if err != nil {
		return err
	}

	// Watch for future address usage.
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		return w.watchFutureAddresses(dbtx)
	})
	if err != nil {
		log.Errorf("Failed to watch for future address usage after publishing "+
			"transaction: %v", err)
		return err
	}
	return nil
}

// txToOutputsInternal creates a signed transaction which includes each output
// from outputs.  Previous outputs to reedeem are chosen from the passed
// account's UTXO set and minconf policy. An additional output may be added to
// return change to the wallet.  An appropriate fee is included based on the
// wallet's current relay fee.  The wallet must be unlocked to create the
// transaction.  The address pool passed must be locked and engaged in an
// address pool batch call.
//
// Decred: This func also sends the transaction, and if successful, inserts it
// into the database, rather than delegating this work to the caller as
// btcwallet does.
func (w *Wallet) txToOutputsInternal(op errors.Op, outputs []*wire.TxOut, account uint32, minconf int32,
	n NetworkBackend, randomizeChangeIdx bool, txFee dcrutil.Amount) (*txauthor.AuthoredTx, error) {

	var atx *txauthor.AuthoredTx
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Create the unsigned transaction.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		inputSource := w.TxStore.MakeInputSource(txmgrNs, addrmgrNs, account,
			minconf, tipHeight)
		changeSource := &p2PKHChangeSource{
			persist: w.deferPersistReturnedChild(&changeSourceUpdates),
			account: account,
			wallet:  w,
		}
		var err error
		atx, err = txauthor.NewUnsignedTransaction(outputs, txFee,
			inputSource.SelectInputs, changeSource)
		if err != nil {
			return err
		}

		// Randomize change position, if change exists, before signing.  This
		// doesn't affect the serialize size, so the change amount will still be
		// valid.
		if atx.ChangeIndex >= 0 && randomizeChangeIdx {
			atx.RandomizeChangePosition()
		}

		// Sign the transaction
		secrets := &secretSource{Manager: w.Manager, addrmgrNs: addrmgrNs}
		err = atx.AddAllInputScripts(secrets)
		for _, done := range secrets.doneFuncs {
			done()
		}
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Ensure valid signatures were created.
	err = validateMsgTx(op, atx.Tx, atx.PrevScripts)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Warn when spending UTXOs controlled by imported keys created change for
	// the default account.
	if atx.ChangeIndex >= 0 && account == udb.ImportedAddrAccount {
		changeAmount := dcrutil.Amount(atx.Tx.TxOut[atx.ChangeIndex].Value)
		log.Warnf("Spend from imported account produced change: moving"+
			" %v from imported account into default account.", changeAmount)
	}

	err = w.checkHighFees(atx.TotalInput, atx.Tx)
	if err != nil {
		return nil, errors.E(op, err)
	}

	rec, err := udb.NewTxRecordFromMsgTx(atx.Tx, time.Now())
	if err != nil {
		return nil, errors.E(op, err)
	}

	// To avoid a race between publishing a transaction and potentially opening
	// a database view during PublishTransaction, the update must be committed
	// before publishing the transaction to the network.
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, up := range changeSourceUpdates {
			err := up(dbtx)
			if err != nil {
				return err
			}
		}

		// TODO: this can be improved by not using the same codepath as notified
		// relevant transactions, since this does a lot of extra work.
		return w.processTransactionRecord(dbtx, rec, nil, nil)
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	err = n.PublishTransactions(context.TODO(), atx.Tx)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Watch for future address usage.
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		return w.watchFutureAddresses(dbtx)
	})
	if err != nil {
		log.Errorf("Failed to watch for future address usage after publishing "+
			"transaction: %v", err)
	}
	return atx, nil
}

// txToMultisig spends funds to a multisig output, partially signs the
// transaction, then returns fund
func (w *Wallet) txToMultisig(op errors.Op, account uint32, amount dcrutil.Amount, pubkeys []*dcrutil.AddressSecpPubKey,
	nRequired int8, minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {

	var (
		ctx      *CreatedTx
		addr     dcrutil.Address
		msScript []byte
	)
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		ctx, addr, msScript, err = w.txToMultisigInternal(op, dbtx,
			account, amount, pubkeys, nRequired, minconf)
		return err
	})
	if err != nil {
		return nil, nil, nil, errors.E(op, err)
	}
	return ctx, addr, msScript, nil
}

func (w *Wallet) txToMultisigInternal(op errors.Op, dbtx walletdb.ReadWriteTx, account uint32, amount dcrutil.Amount,
	pubkeys []*dcrutil.AddressSecpPubKey, nRequired int8, minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	txToMultisigError := func(err error) (*CreatedTx, dcrutil.Address, []byte, error) {
		return nil, nil, nil, err
	}

	n, err := w.NetworkBackend()
	if err != nil {
		return txToMultisigError(err)
	}

	// Get current block's height and hash.
	_, topHeight := w.TxStore.MainChainTip(txmgrNs)

	// Add in some extra for fees. TODO In the future, make a better
	// fee estimator.
	var feeEstForTx dcrutil.Amount
	switch w.chainParams.Net {
	case wire.MainNet:
		feeEstForTx = 5e7
	case 0x48e7a065: // testnet2
		feeEstForTx = 5e7
	case wire.TestNet3:
		feeEstForTx = 5e7
	default:
		feeEstForTx = 3e4
	}
	amountRequired := amount + feeEstForTx

	// Instead of taking reward addresses by arg, just create them now  and
	// automatically find all eligible outputs from all current utxos.
	eligible, err := w.findEligibleOutputsAmount(dbtx, account, minconf,
		amountRequired, topHeight)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	if eligible == nil {
		return txToMultisigError(errors.E(op, "not enough funds to send to multisig address"))
	}

	msgtx := wire.NewMsgTx()
	scriptSizes := []int{}
	// Fill out inputs.
	var forSigning []udb.Credit
	totalInput := dcrutil.Amount(0)
	for _, e := range eligible {
		txIn := wire.NewTxIn(&e.OutPoint, int64(e.Amount), nil)
		msgtx.AddTxIn(txIn)
		totalInput += e.Amount
		forSigning = append(forSigning, e)
		scriptSizes = append(scriptSizes, txsizes.RedeemP2SHSigScriptSize)
	}

	// Insert a multi-signature output, then insert this P2SH
	// hash160 into the address manager and the transaction
	// manager.
	msScript, err := txscript.MultiSigScript(pubkeys, int(nRequired))
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	_, err = w.Manager.ImportScript(addrmgrNs, msScript)
	if err != nil {
		// We don't care if we've already used this address.
		if !errors.Is(errors.Exist, err) {
			return txToMultisigError(errors.E(op, err))
		}
	}
	err = w.TxStore.InsertTxScript(txmgrNs, msScript)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	scAddr, err := dcrutil.NewAddressScriptHash(msScript, w.chainParams)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	p2shScript, err := txscript.PayToAddrScript(scAddr)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	txout := wire.NewTxOut(int64(amount), p2shScript)
	msgtx.AddTxOut(txout)

	// Add change if we need it. The case in which
	// totalInput == amount+feeEst is skipped because
	// we don't need to add a change output in this
	// case.
	feeSize := txsizes.EstimateSerializeSize(scriptSizes, msgtx.TxOut, 0)
	feeEst := txrules.FeeForSerializeSize(w.RelayFee(), feeSize)

	if totalInput < amount+feeEst {
		return txToMultisigError(errors.E(op, errors.InsufficientBalance))
	}
	if totalInput > amount+feeEst {
		changeSource := p2PKHChangeSource{
			persist: w.persistReturnedChild(dbtx),
			account: account,
			wallet:  w,
		}

		pkScript, _, err := changeSource.Script()
		if err != nil {
			return txToMultisigError(err)
		}
		change := totalInput - (amount + feeEst)
		msgtx.AddTxOut(wire.NewTxOut(int64(change), pkScript))
	}

	err = w.signP2PKHMsgTx(msgtx, forSigning, addrmgrNs)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	err = w.checkHighFees(totalInput, msgtx)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	err = n.PublishTransactions(context.TODO(), msgtx)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	// Request updates from dcrd for new transactions sent to this
	// script hash address.
	err = n.LoadTxFilter(context.TODO(), false, []dcrutil.Address{scAddr}, nil)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	err = w.insertMultisigOutIntoTxMgr(txmgrNs, msgtx, 0)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	ctx := &CreatedTx{
		MsgTx:       msgtx,
		ChangeAddr:  nil,
		ChangeIndex: -1,
	}

	return ctx, scAddr, msScript, nil
}

// validateMsgTx verifies transaction input scripts for tx.  All previous output
// scripts from outputs redeemed by the transaction, in the same order they are
// spent, must be passed in the prevScripts slice.
func validateMsgTx(op errors.Op, tx *wire.MsgTx, prevScripts [][]byte) error {
	for i, prevScript := range prevScripts {
		vm, err := txscript.NewEngine(prevScript, tx, i,
			sanityVerifyFlags, txscript.DefaultScriptVersion, nil)
		if err != nil {
			return errors.E(op, err)
		}
		err = vm.Execute()
		if err != nil {
			prevOut := &tx.TxIn[i].PreviousOutPoint
			sigScript := tx.TxIn[i].SignatureScript

			log.Errorf("Script validation failed (outpoint %v pkscript %v sigscript %v): %v",
				prevOut, prevScript, sigScript, err)
			return errors.E(op, errors.ScriptFailure, err)
		}
	}
	return nil
}

func creditScripts(credits []udb.Credit) [][]byte {
	scripts := make([][]byte, 0, len(credits))
	for _, c := range credits {
		scripts = append(scripts, c.PkScript)
	}
	return scripts
}

// compressWallet compresses all the utxos in a wallet into a single change
// address. For use when it becomes dusty.
func (w *Wallet) compressWallet(op errors.Op, maxNumIns int, account uint32, changeAddr dcrutil.Address) (*chainhash.Hash, error) {
	var hash *chainhash.Hash
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		hash, err = w.compressWalletInternal(op, dbtx, maxNumIns, account, changeAddr)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return hash, nil
}

func (w *Wallet) compressWalletInternal(op errors.Op, dbtx walletdb.ReadWriteTx, maxNumIns int, account uint32,
	changeAddr dcrutil.Address) (*chainhash.Hash, error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Get current block's height
	_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

	minconf := int32(1)
	eligible, err := w.findEligibleOutputs(dbtx, account, minconf, tipHeight)
	if err != nil {
		return nil, errors.E(op, err)
	}

	if len(eligible) <= 1 {
		return nil, errors.E(op, "too few outputs to consolidate")
	}

	// Check if output address is default, and generate a new adress if needed
	if changeAddr == nil {
		changeAddr, err = w.newChangeAddress(op, w.persistReturnedChild(dbtx), account)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}
	pkScript, err := txscript.PayToAddrScript(changeAddr)
	if err != nil {
		return nil, errors.E(op, errors.Bug, err)
	}
	msgtx := wire.NewMsgTx()
	msgtx.AddTxOut(wire.NewTxOut(0, pkScript))
	maximumTxSize := maxTxSize
	if w.chainParams.Net == wire.MainNet {
		maximumTxSize = maxStandardTxSize
	}

	// Add the txins using all the eligible outputs.
	totalAdded := dcrutil.Amount(0)
	scriptSizes := []int{}
	count := 0
	var forSigning []udb.Credit
	for _, e := range eligible {
		if count >= maxNumIns {
			break
		}
		// Add the size of a wire.OutPoint
		if msgtx.SerializeSize() > maximumTxSize {
			break
		}

		txIn := wire.NewTxIn(&e.OutPoint, int64(e.Amount), nil)
		msgtx.AddTxIn(txIn)
		totalAdded += e.Amount
		forSigning = append(forSigning, e)
		scriptSizes = append(scriptSizes, txsizes.RedeemP2PKHSigScriptSize)
		count++
	}

	// Get an initial fee estimate based on the number of selected inputs
	// and added outputs, with no change.
	szEst := txsizes.EstimateSerializeSize(scriptSizes, msgtx.TxOut, 0)
	feeEst := txrules.FeeForSerializeSize(w.RelayFee(), szEst)

	msgtx.TxOut[0].Value = int64(totalAdded - feeEst)

	err = w.signP2PKHMsgTx(msgtx, forSigning, addrmgrNs)
	if err != nil {
		return nil, errors.E(op, err)
	}
	err = validateMsgTx(op, msgtx, creditScripts(forSigning))
	if err != nil {
		return nil, errors.E(op, err)
	}

	err = w.checkHighFees(totalAdded, msgtx)
	if err != nil {
		return nil, errors.E(op, err)
	}

	err = n.PublishTransactions(context.TODO(), msgtx)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Insert the transaction and credits into the transaction manager.
	rec, err := w.insertIntoTxMgr(txmgrNs, msgtx)
	if err != nil {
		return nil, errors.E(op, err)
	}
	err = w.insertCreditsIntoTxMgr(op, dbtx, msgtx, rec)
	if err != nil {
		return nil, err
	}

	txHash := msgtx.TxHash()
	log.Infof("Successfully consolidated funds in transaction %v", &txHash)

	return &txHash, nil
}

// makeTicket creates a ticket from a split transaction output. It can optionally
// create a ticket that pays a fee to a pool if a pool input and pool address are
// passed.
func makeTicket(params *chaincfg.Params, inputPool *extendedOutPoint, input *extendedOutPoint, addrVote dcrutil.Address,
	addrSubsidy dcrutil.Address, ticketCost int64, addrPool dcrutil.Address) (*wire.MsgTx, error) {

	mtx := wire.NewMsgTx()

	if addrPool != nil && inputPool != nil {
		txIn := wire.NewTxIn(inputPool.op, inputPool.amt, []byte{})
		mtx.AddTxIn(txIn)
	}

	txIn := wire.NewTxIn(input.op, input.amt, []byte{})
	mtx.AddTxIn(txIn)

	// Create a new script which pays to the provided address with an
	// SStx tagged output.
	if addrVote == nil {
		return nil, errors.E(errors.Invalid, "nil vote address")
	}
	pkScript, err := txscript.PayToSStx(addrVote)
	if err != nil {
		return nil, errors.E(errors.Op("txscript.PayToSStx"), errors.Invalid,
			errors.Errorf("vote address %v", addrVote))
	}

	txOut := wire.NewTxOut(ticketCost, pkScript)
	txOut.Version = txscript.DefaultScriptVersion
	mtx.AddTxOut(txOut)

	// Obtain the commitment amounts.
	var amountsCommitted []int64
	userSubsidyNullIdx := 0
	if addrPool == nil {
		_, amountsCommitted, err = stake.SStxNullOutputAmounts(
			[]int64{input.amt}, []int64{0}, ticketCost)
		if err != nil {
			return nil, err
		}

	} else {
		_, amountsCommitted, err = stake.SStxNullOutputAmounts(
			[]int64{inputPool.amt, input.amt}, []int64{0, 0}, ticketCost)
		if err != nil {
			return nil, err
		}
		userSubsidyNullIdx = 1
	}

	// Zero value P2PKH addr.
	zeroed := [20]byte{}
	addrZeroed, err := dcrutil.NewAddressPubKeyHash(zeroed[:], params, 0)
	if err != nil {
		return nil, err
	}

	// 2. (Optional) If we're passed a pool address, make an extra
	// commitment to the pool.
	limits := uint16(defaultTicketFeeLimits)
	if addrPool != nil {
		pkScript, err = txscript.GenerateSStxAddrPush(addrPool,
			dcrutil.Amount(amountsCommitted[0]), limits)
		if err != nil {
			return nil, errors.E(errors.Op("txscript.GenerateSStxAddrPush"), errors.Invalid,
				errors.Errorf("pool commitment address %v", addrPool))
		}
		txout := wire.NewTxOut(int64(0), pkScript)
		mtx.AddTxOut(txout)

		// Create a new script which pays to the provided address with an
		// SStx change tagged output.
		pkScript, err = txscript.PayToSStxChange(addrZeroed)
		if err != nil {
			return nil, errors.E(errors.Op("txscript.PayToSStxChange"), errors.Bug,
				errors.Errorf("ticket change address %v", addrZeroed))
		}

		txOut = wire.NewTxOut(0, pkScript)
		txOut.Version = txscript.DefaultScriptVersion
		mtx.AddTxOut(txOut)
	}

	// 3. Create the commitment and change output paying to the user.
	//
	// Create an OP_RETURN push containing the pubkeyhash to send rewards to.
	// Apply limits to revocations for fees while not allowing
	// fees for votes.
	pkScript, err = txscript.GenerateSStxAddrPush(addrSubsidy,
		dcrutil.Amount(amountsCommitted[userSubsidyNullIdx]), limits)
	if err != nil {
		return nil, errors.E(errors.Op("txscript.GenerateSStxAddrPush"), errors.Invalid,
			errors.Errorf("commitment address %v", addrSubsidy))
	}
	txout := wire.NewTxOut(int64(0), pkScript)
	mtx.AddTxOut(txout)

	// Create a new script which pays to the provided address with an
	// SStx change tagged output.
	pkScript, err = txscript.PayToSStxChange(addrZeroed)
	if err != nil {
		return nil, errors.E(errors.Op("txscript.PayToSStxChange"), errors.Bug,
			errors.Errorf("ticket change address %v", addrZeroed))
	}

	txOut = wire.NewTxOut(0, pkScript)
	txOut.Version = txscript.DefaultScriptVersion
	mtx.AddTxOut(txOut)

	// Make sure we generated a valid SStx.
	if err := stake.CheckSStx(mtx); err != nil {
		return nil, errors.E(errors.Op("stake.CheckSStx"), errors.Bug, err)
	}

	return mtx, nil
}

type (
	PeerData struct {
		Id        uint32
		NumMsg    uint32
		Pk        []byte
		Vk        []byte
		DcExpSeed []byte
		DcExp     []byte
		DcXorSeed []byte
		DcXor     []byte
	}
)

func (peer PeerData) GetDcexpField() field.Field {
	n := field.FromBytes(peer.DcExp)
	return field.NewFF(n)
}

// purchaseTickets indicates to the wallet that a ticket should be purchased
// using all currently available funds.  The ticket address parameter in the
// request can be nil in which case the ticket address associated with the
// wallet instance will be used.  Also, when the spend limit in the request is
// greater than or equal to 0, tickets that cost more than that limit will
// return an error that not enough funds are available.
func (w *Wallet) purchaseTickets(op errors.Op, req purchaseTicketRequest) ([]*chainhash.Hash, error) {
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Ensure the minimum number of required confirmations is positive.
	if req.minConf < 0 {
		return nil, errors.E(op, errors.Invalid, "negative minconf")
	}
	// Need a positive or zero expiry that is higher than the next block to
	// generate.
	if req.expiry < 0 {
		return nil, errors.E(op, errors.Invalid, "negative expiry")
	}

	// Perform a sanity check on expiry.
	var tipHeight int32
	err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight = w.TxStore.MainChainTip(ns)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if req.expiry <= tipHeight+1 && req.expiry > 0 {
		return nil, errors.E(op, errors.Invalid, "expiry height must be above next block height")
	}

	// addrFunc returns a change address.
	addrFunc := w.newChangeAddress
	if w.addressReuse {
		xpub := w.addressBuffers[udb.DefaultAccountNum].albExternal.branchXpub
		addr, err := deriveChildAddress(xpub, 0, w.chainParams)
		if err != nil {
			err = errors.E(op, err)
		}
		addrFunc = func(errors.Op, persistReturnedChildFunc, uint32) (dcrutil.Address, error) {
			return addr, err
		}
	}

	// Fetch a new address for creating a split transaction. Then,
	// make a split transaction that contains exact outputs for use
	// in ticket generation. Cache its hash to use below when
	// generating a ticket. The account balance is checked first
	// in case there is not enough money to generate the split
	// even without fees.
	// TODO This can still sometimes fail if the split amount
	// required plus fees for the split is larger than the
	// balance we have, wasting an address. In the future,
	// address this better and prevent address burning.
	account := req.account

	// Calculate the current ticket price.  If the DCP0001 deployment is not
	// active, fallback to querying the ticket price over RPC.
	ticketPrice, err := w.NextStakeDifficulty()
	if errors.Is(errors.Deployment, err) {
		ticketPrice, err = n.StakeDifficulty(context.TODO())
	}
	if err != nil {
		return nil, err
	}

	// Ensure the ticket price does not exceed the spend limit if set.
	if req.spendLimit >= 0 && ticketPrice > req.spendLimit {
		return nil, errors.E(op, errors.Invalid,
			errors.Errorf("ticket price %v above spend limit %v", ticketPrice, req.spendLimit))
	}

	// Try to get the pool address from the request. If none exists
	// in the request, try to get the global pool address. Then do
	// the same for pool fees, but check sanity too.
	poolAddress := req.poolAddress
	if poolAddress == nil {
		poolAddress = w.PoolAddress()
	}
	poolFees := req.poolFees
	if poolFees == 0.0 {
		poolFees = w.PoolFees()
	}
	if poolAddress != nil && poolFees == 0.0 {
		return nil, errors.E(op, errors.Invalid, "stakepool fee percent unset")
	}

	var stakeSubmissionPkScriptSize int

	// The stake submission pkScript is tagged by an OP_SSTX.
	switch req.ticketAddr.(type) {
	case *dcrutil.AddressScriptHash:
		stakeSubmissionPkScriptSize = txsizes.P2SHPkScriptSize + 1
	case *dcrutil.AddressPubKeyHash, nil:
		stakeSubmissionPkScriptSize = txsizes.P2PKHPkScriptSize + 1
	default:
		return nil, errors.E(op, errors.Invalid,
			"ticket address must either be P2SH or P2PKH")
	}

	// Make sure that we have enough funds. Calculate different
	// ticket required amounts depending on whether or not a
	// pool output is needed. If the ticket fee increment is
	// unset in the request, use the global ticket fee increment.
	var neededPerTicket, ticketFee dcrutil.Amount
	var estSize int
	ticketFeeIncrement := req.ticketFee
	if ticketFeeIncrement == 0 {
		ticketFeeIncrement = w.TicketFeeIncrement()
	}

	inSizes := make([]int, 0)
	outSizes := make([]int, 0)
	if poolAddress == nil {
		// A solo ticket has:
		//   - a single input redeeming a P2PKH for the worst case size
		//   - a P2PKH or P2SH stake submission output
		//   - a ticket commitment output
		//   - an OP_SSTXCHANGE tagged P2PKH or P2SH change output
		//
		//   NB: The wallet currently only supports P2PKH change addresses.
		//   The network supports both P2PKH and P2SH change addresses however.
		inSizes = append(inSizes, txsizes.RedeemP2PKHSigScriptSize)
		outSizes = append(outSizes, stakeSubmissionPkScriptSize,
			txsizes.TicketCommitmentScriptSize, txsizes.P2PKHPkScriptSize+1)
		estSize = txsizes.EstimateSerializeSizeFromScriptSizes(inSizes,
			outSizes, 0)
	} else {
		// A pool ticket has:
		//   - two inputs redeeming a P2PKH for the worst case size
		//   - a P2PKH or P2SH stake submission output
		//   - two ticket commitment outputs
		//   - two OP_SSTXCHANGE tagged P2PKH or P2SH change outputs
		//
		//   NB: The wallet currently only supports P2PKH change addresses.
		//   The network supports both P2PKH and P2SH change addresses however.
		inSizes = append(inSizes, txsizes.RedeemP2PKHSigScriptSize,
			txsizes.RedeemP2PKHSigScriptSize)
		outSizes = append(outSizes, stakeSubmissionPkScriptSize,
			txsizes.TicketCommitmentScriptSize, txsizes.TicketCommitmentScriptSize,
			txsizes.P2PKHPkScriptSize+1, txsizes.P2PKHPkScriptSize+1)
		estSize = txsizes.EstimateSerializeSizeFromScriptSizes(inSizes,
			outSizes, 0)
	}

	ticketFee = txrules.FeeForSerializeSize(ticketFeeIncrement, estSize)
	neededPerTicket = ticketFee + ticketPrice

	// Make sure this doesn't over spend based on the balance to
	// maintain. This component of the API is inaccessible to the
	// end user through the legacy RPC, so it should only ever be
	// set by internal calls e.g. automatic ticket purchase.
	if req.minBalance > 0 {
		bal, err := w.CalculateAccountBalance(account, req.minConf)
		if err != nil {
			return nil, err
		}

		estimatedFundsUsed := neededPerTicket * dcrutil.Amount(req.numTickets)
		if req.minBalance+estimatedFundsUsed > bal.Spendable {
			return nil, errors.E(op, errors.InsufficientBalance, errors.Errorf(
				"estimated ending balance %v is below minimum requested balance %v",
				bal.Spendable-estimatedFundsUsed, req.minBalance))
		}
	}

	// Separate purchase ticket code to a func for using in many part of code.
	// The first one is used to purchase when got joined tx from dcrtxmatcher server.
	// The secode is used when dcrtxmatcher is not available and purchase will work as usually.
	purchaseFn := func(tx *wire.MsgTx, numberTickets int, outputIds []int32) ([]*chainhash.Hash, error) {

		log.Info("OutputIndex will be used for purchase", outputIds)
		ticketHashes := make([]*chainhash.Hash, 0)
		for i := 0; i < req.numTickets; i++ {

			outputId := outputIds[i]

			var eop *extendedOutPoint
			txOut := tx.TxOut[outputId]
			eop = &extendedOutPoint{
				op: &wire.OutPoint{
					Hash:  tx.TxHash(),
					Index: uint32(outputId),
					Tree:  wire.TxTreeRegular,
				},
				amt:      txOut.Value,
				pkScript: txOut.PkScript,
			}
			votingAddress, subsidyAddress, err := w.fetchAddresses(req.ticketAddr, req.account, addrFunc)

			// Generate the ticket msgTx and sign it.
			ticket, err := makeTicket(w.chainParams, nil, eop, votingAddress,
				subsidyAddress, int64(ticketPrice), poolAddress)
			if err != nil {
				return ticketHashes, err
			}

			var forSigning []udb.Credit
			eopCredit := udb.Credit{
				OutPoint:     *eop.op,
				BlockMeta:    udb.BlockMeta{},
				Amount:       dcrutil.Amount(eop.amt),
				PkScript:     eop.pkScript,
				Received:     time.Now(),
				FromCoinBase: false,
			}
			forSigning = append(forSigning, eopCredit)

			// Set ticket expiry if greater than zero
			if req.expiry > 0 {
				ticket.Expiry = uint32(req.expiry)
			}

			err = walletdb.View(w.db, func(tx walletdb.ReadTx) error {
				ns := tx.ReadBucket(waddrmgrNamespaceKey)
				// signMsgTx set signaturescript to txin
				return w.signP2PKHMsgTx(ticket, forSigning, ns)
			})
			if err != nil {
				return ticketHashes, err
			}

			//			err = w.processTxRecordAndPublish(ticket, n)
			//			if err != nil {
			//				return ticketHashes, err
			//			}

			ticketHash := ticket.TxHash()
			ticketHashes = append(ticketHashes, &ticketHash)
			log.Infof("Successfully sent SStx purchase transaction %v", ticketHash)
		}

		return ticketHashes, nil
	}

	// We purchase ticket by coinshuffle++ method.
	// If dcrTxClient is not enable, we purchase by coinjoin method.
	// If server is not available, purchase ticket locally.
	if req.dcrTxClient.Cfg.Enable {
		var splitOuts []*wire.TxOut

		//create slice of pkscripts
		pkScripts := make([][]byte, req.numTickets)

		for i := 0; i < req.numTickets; i++ {
			splitTxAddr, err := w.NewInternalAddress(req.account, WithGapPolicyWrap())
			if err != nil {
				return nil, err
			}
			pkScripts[i], err = txscript.PayToAddrScript(splitTxAddr)
			if err != nil {
				return nil, errors.E(op, err)
			}

			splitOuts = append(splitOuts, wire.NewTxOut(int64(neededPerTicket), pkScripts[i]))
			log.Debugf("Pkscript %x", pkScripts[i])
		}

		txFeeIncrement := req.txFee
		if txFeeIncrement == 0 {
			txFeeIncrement = w.RelayFee()
		}

		splitTx, changeSourceFuncs, err := w.txToOutputsSplitTx(splitOuts, req.account, req.minConf,
			n, false, txFeeIncrement)
		if err != nil {
			return nil, errors.E(op, errors.Bug, errors.Errorf("split address %v", ""))
		}

		// Remove all txout to perform dicemix output address, keep only change output
		if len(splitTx.Tx.TxOut) > 1 {
			splitTx.Tx.TxOut = []*wire.TxOut{splitTx.Tx.TxOut[len(splitTx.Tx.TxOut)-1]}
		} else {
			splitTx.Tx.TxOut = []*wire.TxOut{}
		}

		// Build outputs index in case communicate with server fails
		localOutputIndex := make([]int32, 0)
		for i := 0; i < req.numTickets; i++ {
			localOutputIndex = append(localOutputIndex, int32(i))
		}

		peers := make([]PeerData, 0)
		var PeerId, SessionId uint32

		// Connect to dcrtxmatcher server via websocket.
		// In case dcrtxmacher is not available, we will purchase locally as usual
		dialer := websocket.Dialer{}
		ws, _, err := dialer.Dial("ws://"+req.dcrTxClient.Cfg.Address+"/ws", http.Header{})
		if err != nil {
			log.Errorf("Connecting Error: %v", err)
			// Will purchase locally

		}
		log.Info("Connected to dcrtxmatcher server for joining transaction!")
		defer ws.Close()

		// Generate public/private keypair using Elliptic Curve Diffe Huffman
		ecp256 := ecdh.NewEllipticECDH(elliptic.P256())
		vk, pk, err := ecp256.GenerateKey(rand.Reader)
		if err != nil {
			return nil, errors.E(op, err)
		}

		pkbytes := ecp256.Marshal(pk)

		//vkbytes := ecp256.Marshal(vk)
		var numMsg uint32 = 0
		var allMsgBytes [][]byte
		var allMsgHashes []field.Uint128
		myMsgsHash := make([][]byte, 0)

		// Will need to record the input and output index of peer in joined transaction.
		// Know input index to sign peer's transaction input.
		// Know output index to purchase ticket.
		var outputIndex, inputIndex []int32
		var joinTx wire.MsgTx

		// Read websocket continuously for incoming message
		for {

			_, msg, err := ws.ReadMessage()
			if err != nil {
				return nil, errors.E(op, err)
			}

			message, err := messages.ParseMessage(msg)
			if err != nil {
				return nil, errors.E(op, err)
			}

			// We need to check the type of the message and having proper process
			switch message.MsgType {
			case messages.S_JOIN_RESPONSE:
				// Peer receives this message when server has received all join transaction request from all peers.
				// Server will generate session id and peer id and send back data to every peers
				joinRes := &pb.CoinJoinRes{}
				err := proto.Unmarshal(message.Data, joinRes)
				if err != nil {
					return nil, errors.E(op, err)
				}

				PeerId = joinRes.PeerId
				SessionId = joinRes.SessionId

				// Send key exchange cmd
				keyex := &pb.KeyExchangeReq{
					SessionId: SessionId,
					PeerId:    PeerId,
					NumMsg:    uint32(req.numTickets),
					Pk:        pkbytes,
				}

				data, err := proto.Marshal(keyex)
				if err != nil {
					return nil, errors.E(op, err)
				}

				message := messages.NewMessage(messages.C_KEY_EXCHANGE, data)
				if err := ws.WriteMessage(websocket.BinaryMessage, message.ToBytes()); err != nil {
					return nil, errors.E(op, err)
				}

			case messages.S_KEY_EXCHANGE:
				// After server has received the public key of all peers, each peer received  public keys of all peers
				keyex := &pb.KeyExchangeRes{}
				err := proto.Unmarshal(message.Data, keyex)
				if err != nil {
					return nil, errors.E(op, err)
				}
				log.Debug("Generate sharedkey with each peer")
				log.Debug("Uses shared key as seed to generate padding bytes for dc-net exponential and dc-net xor")

				// We will create peer random bytes for dc-net exponential and dc-net xor
				for _, peer := range keyex.Peers {
					peerPk, _ := ecp256.Unmarshal(peer.Pk)
					// Will generate shared key with other peer from private key and other peer public key
					sharedKey, err := ecp256.GenerateSharedSecret(vk, peerPk)
					if err != nil {
						log.Errorf("error GenerateSharedSecret: %v", err)
						return nil, err
					}
					// Generate random byte with shared key and random size is 12.
					// Choose 12 because with bigger random size will cause the power sum
					// with padding random bytes overflow max value of prime finite field (1<<127)
					dcexpRng, err := chacharng.RandBytes(sharedKey, messages.ExpRandSize)
					if err != nil {
						return nil, errors.E(op, err)
					}
					dcexpRng = append([]byte{0, 0, 0, 0}, dcexpRng...)

					// For random byte of Xor vector, we get the same size of pkscript is 25 bytes
					dcXorRng, err := chacharng.RandBytes(sharedKey, messages.PkScriptSize)
					if err != nil {
						return nil, err
					}
					peers = append(peers, PeerData{Id: peer.PeerId, Pk: peer.Pk, NumMsg: peer.NumMsg, DcExp: dcexpRng,
						DcXor: dcXorRng})

					// Calculate total number of messsages from all peers
					numMsg += peer.NumMsg
				}

				for _, p := range peers {
					if p.Id != PeerId {
						log.Debugf("Peerid: %d, expopential padding: %x, xor padding:%x", p.Id, p.DcExp, p.DcXor)
					}
				}

				// Create dc-net exponential vector with size is total number message of all peers in join session.
				// We choose 127 instead of 128 because as the same reason choosing 12 byte of random byte size in dc-net exponential
				myDcexp := make([]field.Field, numMsg)
				for _, msg := range pkScripts {
					md := ripemd128.New()
					_, err = md.Write(msg)
					if err != nil {
						return nil, errors.E(op, err)
					}

					pkscripthash := md.Sum127(nil)
					myMsgsHash = append(myMsgsHash, pkscripthash)

					ff := field.NewFF(field.FromBytes(pkscripthash))

					// With N message we will create vector with size N,
					// create power of each message hash and sum all messages with the same power i
					for i := 0; i < int(numMsg); i++ {
						myDcexp[i] = myDcexp[i].Add(ff.Exp(uint64(i + 1)))
					}
				}

				// Padding with random number generated with secret key seed
				for j := 0; j < int(numMsg); j++ {
					// Padding with other peers
					for _, p := range peers {
						if PeerId > p.Id {
							myDcexp[j] = myDcexp[j].Add(p.GetDcexpField())
						} else if PeerId < p.Id {
							myDcexp[j] = myDcexp[j].Sub(p.GetDcexpField())
						}
					}
				}

				// Submit dc-net exponential vector of peer to server
				// the server will combine the vectors of all peers to resolve polynomial
				dcExpVector := &pb.DcExpVector{PeerId: PeerId, Len: uint32(numMsg)}
				vector := make([]byte, 0)
				for i := 0; i < int(numMsg); i++ {
					vector = append(vector, myDcexp[i].N.GetBytes()...)
					log.Debugf("Exponential vector %x", myDcexp[i].N.GetBytes())
				}
				log.Debug("Sent dc-net exponential vector to server")

				md := ripemd128.New()
				_, err = md.Write(vector)
				if err != nil {
					return nil, errors.E(op, err)
				}

				commit := md.Sum(nil)
				dcExpVector.Vector = vector
				dcExpVector.Commit = commit

				data, err := proto.Marshal(dcExpVector)
				if err != nil {
					return nil, errors.E(op, err)
				}

				message := messages.NewMessage(messages.C_DC_EXP_VECTOR, data)
				if err := ws.WriteMessage(websocket.BinaryMessage, message.ToBytes()); err != nil {
					return nil, errors.E(op, err)
				}
			case messages.S_DC_EXP_VECTOR:
				// After server received all peers exponential vector and resolved polynomial,
				// server will sent back all resolved pkscripst hash.
				// Each peer has to sort all pkscripts hash and sort to find their index of pkscripts hash in sorted list.
				log.Debug("Received all hash of pkscript from peers")
				allMsgs := &pb.AllMessages{}

				err := proto.Unmarshal(message.Data, allMsgs)
				if err != nil {
					return nil, errors.E(op, err)
				}

				allMsgBytes = make([][]byte, allMsgs.Len)
				allMsgHashes = make([]field.Uint128, allMsgs.Len)
				for i := 0; i < int(allMsgs.Len); i++ {
					allMsgBytes[i] = allMsgs.Msgs[i*messages.PkScriptHashSize : (i+1)*messages.PkScriptHashSize]
					log.Debugf("Pkscripts hash received from server %x", allMsgBytes[i])
					allMsgHashes[i] = field.FromBytes(allMsgBytes[i])
				}

				// Sort all messages and find pkscripts hash slot index.
				sort.Slice(allMsgHashes, func(i, j int) bool {
					return allMsgHashes[i].Compare(allMsgHashes[j]) < 0
				})

				// Find my messages index in messages hash.
				slotReserveds := make([]int, len(myMsgsHash))
				for j := 0; j < len(myMsgsHash); j++ {
					for i := 0; i < int(allMsgs.Len); i++ {
						if bytes.Equal(myMsgsHash[j], allMsgHashes[i].GetBytes()) {
							slotReserveds[j] = i
						}
					}
				}
				log.Debugf("Find my pkscripts from all hashed messages returned from server. My slot reserved index %v", slotReserveds)

				// After found slot index of pkscripts hash.
				// Peer will create dc-net xor vector.
				// Base on equation: (Pkscript ^ P ^ P1 ^ P2...) ^ (P ^ P1 ^ P2...) = Pkscript
				// Each peer will send Pkscript ^ P ^ P1 ^ P2... bytes to server.
				// Server combine all dc-net xor vector and will have Pkscript ^ P ^ P1 ^ P2... ^ (P ^ P1 ^ P2...) = Pkscript
				// But server could not know which Pkscript belongs to any peer because only peer know it's slot index.
				// And each peer only knows it's Pkscript itself.
				myDcXor := make([][]byte, numMsg)
				for i := 0; i < int(req.numTickets); i++ {
					myDcXor[slotReserveds[i]] = pkScripts[i]
				}
				for _, peer := range peers {
					if peer.Id == PeerId {
						continue
					}
					for i := 0; i < int(numMsg); i++ {
						myDcXor[i], err = util.XorBytes(myDcXor[i], peer.DcXor)
						if err != nil {
							return nil, errors.E(op, err)
						}
					}
				}

				for _, item := range myDcXor {
					log.Debugf("My dc-net Xor vector %x", item)
				}

				xorData := make([]byte, 0)
				for _, dc := range myDcXor {
					xorData = append(xorData, dc...)
				}

				dcXor := &pb.DcXorVector{PeerId: PeerId, Vector: xorData, Len: numMsg}
				data, err := proto.Marshal(dcXor)
				if err != nil {
					return nil, errors.E(op, err)
				}
				// Submit dc-net xor vector to server
				message := messages.NewMessage(messages.C_DC_XOR_VECTOR, data)
				if err := ws.WriteMessage(websocket.BinaryMessage, message.ToBytes()); err != nil {
					return nil, errors.E(op, err)
				}
				log.Debug("Sent dc-net xor vector to server")

			case messages.S_DC_XOR_VECTOR:
				// Will check whether there is malicious peers.
				// If all pkscripts sent from server not contain at least one peer's pkscript.
				// We need to find the malicious peer to remove.
				// And we trust on server terminating malicious peers.
				dcxorRet := &pb.DcXorVectorResult{}
				err := proto.Unmarshal(message.Data, dcxorRet)
				if err != nil {
					return nil, errors.E(op, err)
				}

				buffTx := bytes.NewBuffer(nil)
				buffTx.Grow(splitTx.Tx.SerializeSize())
				err = splitTx.Tx.BtcEncode(buffTx, 0)
				if err != nil {
					return nil, errors.E(op, err)
				}
				log.Info("Will submit tx inputs and change amount to server")
				txInputs := &pb.TxInputs{PeerId: PeerId, TicketPrice: int64(neededPerTicket), Txins: buffTx.Bytes()}
				txInsData, err := proto.Marshal(txInputs)
				if err != nil {
					return nil, errors.E(op, err)
				}

				message := messages.NewMessage(messages.C_TX_INPUTS, txInsData)
				if err := ws.WriteMessage(websocket.BinaryMessage, message.ToBytes()); err != nil {
					return nil, errors.E(op, err)
				}
				log.Info("Sent transaction inputs and change amount")
			case messages.S_JOINED_TX:
				// With joined transaction returns from server.
				// The key is finding correct peer's transaction input and output index
				// and record for later use.
				joinTxData := &pb.JoinTx{}
				err := proto.Unmarshal(message.Data, joinTxData)
				if err != nil {
					return nil, errors.E(op, err)
				}
				log.Info("Received joined tx. Will find my txin, txout index from joined tx and sign")
				var joinTx wire.MsgTx
				buffTx := bytes.NewReader(joinTxData.Tx)
				err = joinTx.BtcDecode(buffTx, 0)
				for _, msg := range pkScripts {
					for i, txout := range joinTx.TxOut {
						if bytes.Equal(txout.PkScript, msg) {
							outputIndex = append(outputIndex, int32(i))
							break
						}
					}
				}

				for _, txin := range splitTx.Tx.TxIn {
					for i, joinTxin := range joinTx.TxIn {
						if (&joinTxin.PreviousOutPoint.Hash).IsEqual(&txin.PreviousOutPoint.Hash) &&
							joinTxin.PreviousOutPoint.Index == txin.PreviousOutPoint.Index {
							inputIndex = append(inputIndex, int32(i))
							break
						}
					}
				}

				// Sign the transaction with peer's transaction inputs only, not others
				err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
					addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
					secrets := &secretSource{Manager: w.Manager, addrmgrNs: addrmgrNs}
					err = txauthor.AddInputScripts(&joinTx, splitTx.PrevScripts, secrets, inputIndex)
					for _, done := range secrets.doneFuncs {
						done()
					}
					return err
				})
				if err != nil {
					return nil, err
				}

				// Encode signed transaction and submit to server.
				signedTx := &pb.JoinTx{PeerId: PeerId}
				buff := bytes.NewBuffer(nil)
				buff.Grow(joinTx.SerializeSize())
				err = joinTx.BtcEncode(buff, 0)
				if err != nil {
					return nil, errors.E(op, err)
				}

				signedTx.Tx = buff.Bytes()
				signedTxData, err := proto.Marshal(signedTx)
				if err != nil {
					return nil, errors.E(op, err)
				}

				message := messages.NewMessage(messages.C_TX_SIGN, signedTxData)
				if err := ws.WriteMessage(websocket.BinaryMessage, message.ToBytes()); err != nil {
					return nil, errors.E(op, err)
				}
				log.Debug("Sent signed joined tx")

			case messages.S_TX_SIGN:
				// With peer is selected to publish transaction
				// then will be received this message data.
				joinTxData := &pb.JoinTx{}

				err := proto.Unmarshal(message.Data, joinTxData)
				if err != nil {
					return nil, errors.E(op, err)
				}

				buffTx := bytes.NewReader(joinTxData.Tx)
				err = joinTx.BtcDecode(buffTx, 0)
				if err != nil {
					return nil, errors.E(op, err)
				}

				log.Info("Will publish the transaction")
				err = w.publishTx(&joinTx, changeSourceFuncs, w.networkBackend)
				var msg *messages.Message
				if err != nil {
					return nil, errors.E(op, err)
				}
				log.Info("Published and sent the transaction to server", joinTx.TxHash().String())

				// Encode the published transaction and send to server.
				var buffTx1 = bytes.NewBuffer(nil)
				buffTx1.Grow(joinTx.SerializeSize())
				err = joinTx.BtcEncode(buffTx1, 0)
				if err != nil {
					return nil, errors.E(op, err)
				}

				publishRet := &pb.PublishResult{}
				publishRet.Tx = buffTx1.Bytes()

				data, err := proto.Marshal(publishRet)
				if err != nil {
					log.Errorf("Error Marshal PublishResult %v", err)
					msg = messages.NewMessage(messages.C_TX_PUBLISH_RESULT, []byte{0x0})
					return nil, err
				}

				msg = messages.NewMessage(messages.C_TX_PUBLISH_RESULT, data)
				if err := ws.WriteMessage(websocket.BinaryMessage, msg.ToBytes()); err != nil {
					return nil, errors.E(op, err)
				}
			case messages.S_TX_PUBLISH_RESULT:
				// Will use transaction to pruchase ticket with peer's output index.
				if len(message.Data) == 1 {
					return nil, errors.New("error when publish joined transaction")
				}

				var tx wire.MsgTx
				buffTx := bytes.NewBuffer(message.Data)
				err := tx.BtcDecode(buffTx, 0)
				if err != nil {
					return nil, errors.E(op, err)
				}
				log.Info("Received published transaction, will use to purchase tickets")
				return purchaseFn(&tx, req.numTickets, outputIndex)
			case messages.S_MALICIOUS_PEERS:
				mailiciousPeers := &pb.MaliciousPeers{}
				err := proto.Unmarshal(message.Data, mailiciousPeers)
				if err != nil {
					return nil, err
				}

				log.Debug("mailicious peer ids ", mailiciousPeers.PeerIds)
				log.Debug("new PeerId ", mailiciousPeers.PeerId)
				log.Debug("new SessionId ", mailiciousPeers.SessionId)

				vk, pk, err = ecp256.GenerateKey(rand.Reader)
				if err != nil {
					log.Errorf("error ecdh GenerateKey: %v", err)
					return nil, err
				}

				pkbytes = ecp256.Marshal(pk)

				// Start join session from beginning.
				PeerId = mailiciousPeers.PeerId
				SessionId = mailiciousPeers.SessionId

				// Send key exchange command.
				keyex := &pb.KeyExchangeReq{
					SessionId: SessionId,
					PeerId:    PeerId,
					NumMsg:    uint32(req.numTickets),
					Pk:        pkbytes,
				}

				data, err := proto.Marshal(keyex)
				if err != nil {
					log.Errorf("error Unmarshal joinRes: %v", err)
					return nil, err
				}
				// Reset peer's data from previous session
				splitOuts = []*wire.TxOut{}
				//create slice of pkscripts
				pkScripts = make([][]byte, req.numTickets)

				for i := 0; i < req.numTickets; i++ {
					splitTxAddr, err := w.NewInternalAddress(req.account, WithGapPolicyWrap())
					if err != nil {
						return nil, err
					}
					pkScripts[i], err = txscript.PayToAddrScript(splitTxAddr)
					if err != nil {
						log.Errorf("cannot create txout script: %s", err)
						return nil, err
					}

					splitOuts = append(splitOuts, wire.NewTxOut(int64(neededPerTicket), pkScripts[i]))
					log.Debugf("Pkscript %x", pkScripts[i])
				}

				txFeeIncrement = req.txFee
				if txFeeIncrement == 0 {
					txFeeIncrement = w.RelayFee()
				}

				splitTx, changeSourceFuncs, err = w.txToOutputsSplitTx(splitOuts, req.account, req.minConf,
					n, false, txFeeIncrement)
				if err != nil {
					return nil, errors.E(op, errors.Bug, errors.Errorf("split address %v", ""))
				}

				//remove all txout to perform dicemix output address, keep only change output
				if len(splitTx.Tx.TxOut) > 1 {
					splitTx.Tx.TxOut = []*wire.TxOut{splitTx.Tx.TxOut[len(splitTx.Tx.TxOut)-1]}
				} else {
					splitTx.Tx.TxOut = []*wire.TxOut{}
				}

				// build outputs index in case communicate with server fails
				localOutputIndex = make([]int32, 0)
				for i := 0; i < req.numTickets; i++ {
					localOutputIndex = append(localOutputIndex, int32(i))
				}

				peers = []PeerData{}
				numMsg = 0
				allMsgBytes = [][]byte{}
				allMsgHashes = []field.Uint128{}
				myMsgsHash = make([][]byte, 0)
				outputIndex = []int32{}
				inputIndex = []int32{}
				joinTx = wire.MsgTx{}

				message := messages.NewMessage(messages.C_KEY_EXCHANGE, data)
				if err := ws.WriteMessage(websocket.BinaryMessage, message.ToBytes()); err != nil {
					log.Errorf("error WriteMessage: %v", err)
					return nil, err
				}

				log.Debug("Sent C_KEY_EXCHANGE to server")

			}
		}

		return nil, nil

	} else {
		// This opens a write transaction.
		splitTxAddr, err := w.NewInternalAddress(req.account, WithGapPolicyWrap())
		if err != nil {
			return nil, err
		}
		var splitOuts []*wire.TxOut
		pkScript, err := txscript.PayToAddrScript(splitTxAddr)
		if err != nil {
			return nil, errors.E(op, err)
		}
		for i := 0; i < req.numTickets; i++ {
			splitOuts = append(splitOuts, wire.NewTxOut(int64(neededPerTicket), pkScript))
		}

		txFeeIncrement := req.txFee
		if txFeeIncrement == 0 {
			txFeeIncrement = w.RelayFee()
		}
		// Need to do join split transaction from this with dcrtxmatcher server.
		splitTx, changeSourceFuncs, err := w.txToOutputsSplitTx(splitOuts, req.account, req.minConf,
			n, false, txFeeIncrement)
		if err != nil {
			return nil, errors.E(op, errors.Bug, errors.Errorf("split address %v", splitTxAddr))
		}

		// Purchase tickets individually.
		ticketHashes := make([]*chainhash.Hash, 0, req.numTickets)

		// Build outputs index in case communicate with server fails
		localOutputIndex := make([]int32, 0)
		for i := 0; i < req.numTickets; i++ {
			localOutputIndex = append(localOutputIndex, int32(i))
		}

		// Connect to dcrtxmatcher server.
		err = req.dcrTxClient.StartSession()
		if err != nil {
			if !req.dcrTxClient.IsShutdown {
				log.Infof("Error %v in communication with dcrtxmatcher server", err)
				log.Infof("Will buy ticket locally")
				localSplitTx, err := w.txToOutputsInternal(op, splitOuts, req.account, req.minConf,
					n, false, txFeeIncrement)
				if err != nil {
					return nil, errors.E(op, err)
				}

				return purchaseFn(localSplitTx.Tx, req.numTickets, localOutputIndex)
			}
			return nil, err
		}

		// Disconnect after purchase completed.
		defer func() {
			req.dcrTxClient.Disconnect()
		}()

		// Call join split tx request with timeout.
		tx, sesID, inputIds, outputIds, joinId, err := req.dcrTxClient.TxService.JoinSplitTx(splitTx.Tx, req.dcrTxClient.Cfg.Timeout)
		log.Debugf("JoinSessionId %v", joinId)
		if err != nil {
			if !req.dcrTxClient.IsShutdown {
				log.Infof("Error %v in communication with dcrtxmatcher server", err)
				log.Infof("Will buy ticket locally")
				localSplitTx, err := w.txToOutputsInternal(op, splitOuts, req.account, req.minConf,
					n, false, txFeeIncrement)
				if err != nil {
					return nil, errors.E(op, err)
				}

				return purchaseFn(localSplitTx.Tx, req.numTickets, localOutputIndex)
			}
			return nil, err
		}

		// Verify input, output from server before signing.
		for _, i := range inputIds {
			valid := false
			for _, txin := range splitTx.Tx.TxIn {
				if tx.TxIn[i].BlockHeight == txin.BlockHeight &&
					reflect.DeepEqual(tx.TxIn[i].PreviousOutPoint.Hash, txin.PreviousOutPoint.Hash) &&
					tx.TxIn[i].PreviousOutPoint.Index == txin.PreviousOutPoint.Index {
					valid = true
					break
				}
			}

			if !valid {
				if !req.dcrTxClient.IsShutdown {
					log.Warn("Server returns invalid splittx output, will buy locally")
					localSplitTx, err := w.txToOutputsInternal(op, splitOuts, req.account, req.minConf,
						n, false, txFeeIncrement)
					if err != nil {
						return nil, errors.E(op, err)
					}

					return purchaseFn(localSplitTx.Tx, req.numTickets, localOutputIndex)
				}
				return nil, nil
			}
		}

		for _, i := range outputIds {
			valid := false
			for _, txout := range splitTx.Tx.TxOut {
				if reflect.DeepEqual(tx.TxOut[i].PkScript, txout.PkScript) &&
					tx.TxOut[i].Value == txout.Value {
					valid = true
					break
				}
			}
			if !valid {
				if !req.dcrTxClient.IsShutdown {
					log.Warn("Server returns invalid splittx output, will buy locally")
					localSplitTx, err := w.txToOutputsInternal(op, splitOuts, req.account, req.minConf,
						n, false, txFeeIncrement)
					if err != nil {
						return nil, errors.E(op, err)
					}

					return purchaseFn(localSplitTx.Tx, req.numTickets, localOutputIndex)
				}
				return nil, nil
			}
		}

		// Sign the tx
		err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
			addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
			secrets := &secretSource{Manager: w.Manager, addrmgrNs: addrmgrNs}
			err = txauthor.AddInputScripts(tx, splitTx.PrevScripts, secrets, inputIds)
			for _, done := range secrets.doneFuncs {
				done()
			}
			return err
		})
		if err != nil {
			return ticketHashes, err
		}

		// Submit signed input to server.
		signedTx, publisher, err := req.dcrTxClient.TxService.SubmitSignedTx(tx, sesID, joinId)
		if err != nil {

			if !req.dcrTxClient.IsShutdown {

				log.Info("Error in communication with dcrtxmatcher server, will buy locally")
				localSplitTx, err := w.txToOutputsInternal(op, splitOuts, req.account, req.minConf,
					n, false, txFeeIncrement)
				if err != nil {
					log.Errorf("failed to send split transaction: %v", err)
					return nil, err
				}

				return purchaseFn(localSplitTx.Tx, req.numTickets, localOutputIndex)
			}

			return nil, err
		}

		var publishedTx *wire.MsgTx
		if publisher {
			log.Info("Will publish the transaction")
			err = w.publishTx(signedTx, changeSourceFuncs, w.networkBackend)
			if err != nil {
				_, err := req.dcrTxClient.TxService.PublishResult(nil, sesID, joinId)
				return ticketHashes, err
			}

			_, err := req.dcrTxClient.TxService.PublishResult(signedTx, sesID, joinId)
			if err != nil {
				return ticketHashes, err
			}
			publishedTx = signedTx
			log.Info("Published and sent the joined transaction %v to server", publishedTx.TxHash().String())
		} else {
			publishedTx, err = req.dcrTxClient.TxService.PublishResult(nil, sesID, joinId)
			if err != nil {
				return ticketHashes, err
			}
			log.Infof("Joined transaction hash %v", publishedTx.TxHash().String())
		}

		return purchaseFn(publishedTx, req.numTickets, outputIds)
	}
}

// processTxRecordAndPublish creates a transaction record, inserts the record
// in the walletdb and publishes the transaction over the decred network.
func (w *Wallet) processTxRecordAndPublish(tx *wire.MsgTx, n NetworkBackend) error {
	// Create transaction record
	rec, err := udb.NewTxRecordFromMsgTx(tx, time.Now())
	if err != nil {
		return err
	}

	// Open a DB update to insert and publish the transaction.  If
	// publishing fails, the update is rolled back.
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		err := w.processTransactionRecord(dbtx, rec, nil, nil)
		if err != nil {
			log.Error("[processTxRecordAndPublish.processTxRecordAndPublish] - error ", err)
			return err
		}
		return n.PublishTransactions(context.TODO(), tx)
	})

	return err
}

// signAndValidateTicket signs the passed msgtx and performs validation checks
func (w *Wallet) signAndValidateTicket(ticket *wire.MsgTx, forSigning []udb.Credit, eop *extendedOutPoint) error {
	const op errors.Op = "wallet.signAndValidateTicket"
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
		return w.signP2PKHMsgTx(ticket, forSigning, ns)
	})
	if err != nil {
		return err
	}

	err = validateMsgTx(op, ticket, creditScripts(forSigning))
	if err != nil {
		return err
	}

	if eop != nil {
		err = w.checkHighFees(dcrutil.Amount(eop.amt), ticket)
	}

	return err
}

// addSStxChange adds a new output with the given amount and address, and
// randomizes the index (and returns it) of the newly added output.
func addSStxChange(msgtx *wire.MsgTx, change dcrutil.Amount,
	changeAddr dcrutil.Address) error {
	pkScript, err := txscript.PayToSStxChange(changeAddr)
	if err != nil {
		return fmt.Errorf("cannot create txout script: %s", err)
	}
	msgtx.AddTxOut(wire.NewTxOut(int64(change), pkScript))

	return nil
}

func (w *Wallet) findEligibleOutputs(dbtx walletdb.ReadTx, account uint32, minconf int32,
	currentHeight int32) ([]udb.Credit, error) {

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

	unspent, err := w.TxStore.UnspentOutputs(txmgrNs)
	if err != nil {
		return nil, err
	}

	// TODO: Eventually all of these filters (except perhaps output locking)
	// should be handled by the call to UnspentOutputs (or similar).
	// Because one of these filters requires matching the output script to
	// the desired account, this change depends on making wtxmgr a waddrmgr
	// dependancy and requesting unspent outputs for a single account.
	eligible := make([]udb.Credit, 0, len(unspent))
	for i := range unspent {
		output := unspent[i]

		// Only include this output if it meets the required number of
		// confirmations.  Coinbase transactions must have have reached
		// maturity before their outputs may be spent.
		if !confirmed(minconf, output.Height, currentHeight) {
			continue
		}

		// Locked unspent outputs are skipped.
		if w.LockedOutpoint(output.OutPoint) {
			continue
		}

		// Filter out unspendable outputs, that is, remove those that
		// (at this time) are not P2PKH outputs.  Other inputs must be
		// manually included in transactions and sent (for example,
		// using createrawtransaction, signrawtransaction, and
		// sendrawtransaction).
		class, addrs, _, err := txscript.ExtractPkScriptAddrs(
			txscript.DefaultScriptVersion, output.PkScript, w.chainParams)
		if err != nil || len(addrs) != 1 {
			continue
		}

		// Make sure everything we're trying to spend is actually mature.
		switch {
		case class == txscript.StakeSubmissionTy:
			continue
		case class == txscript.StakeGenTy:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case class == txscript.StakeRevocationTy:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case class == txscript.StakeSubChangeTy:
			if !ticketChangeMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case class == txscript.PubKeyHashTy:
			if output.FromCoinBase {
				if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
					continue
				}
			}
		default:
			continue
		}

		// Only include the output if it is associated with the passed
		// account.
		//
		// TODO: Handle multisig outputs by determining if enough of the
		// addresses are controlled.
		addrAcct, err := w.Manager.AddrAccount(addrmgrNs, addrs[0])
		if err != nil || addrAcct != account {
			continue
		}

		eligible = append(eligible, *output)
	}
	return eligible, nil
}

// FindEligibleOutputs is the exported version of findEligibleOutputs (which
// tried to find unspent outputs that pass a maturity check).
func (w *Wallet) FindEligibleOutputs(account uint32, minconf int32, currentHeight int32) ([]udb.Credit, error) {
	const op errors.Op = "wallet.FindEligibleOutputs"

	var unspentOutputs []udb.Credit
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		unspentOutputs, err = w.findEligibleOutputs(dbtx, account, minconf, currentHeight)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return unspentOutputs, nil
}

// findEligibleOutputsAmount uses wtxmgr to find a number of unspent outputs
// while doing maturity checks there.
func (w *Wallet) findEligibleOutputsAmount(dbtx walletdb.ReadTx, account uint32, minconf int32,
	amount dcrutil.Amount, currentHeight int32) ([]udb.Credit, error) {
	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
	var outTotal dcrutil.Amount

	unspent, err := w.TxStore.UnspentOutputsForAmount(txmgrNs, addrmgrNs,
		amount, currentHeight, minconf, false, account)
	if err != nil {
		return nil, err
	}

	eligible := make([]udb.Credit, 0, len(unspent))
	for i := range unspent {
		output := unspent[i]

		// Locked unspent outputs are skipped.
		if w.LockedOutpoint(output.OutPoint) {
			continue
		}

		// Filter out unspendable outputs, that is, remove those that
		// (at this time) are not P2PKH outputs.  Other inputs must be
		// manually included in transactions and sent (for example,
		// using createrawtransaction, signrawtransaction, and
		// sendrawtransaction).
		class, addrs, _, err := txscript.ExtractPkScriptAddrs(
			txscript.DefaultScriptVersion, output.PkScript, w.chainParams)
		if err != nil ||
			!(class == txscript.PubKeyHashTy ||
				class == txscript.StakeGenTy ||
				class == txscript.StakeRevocationTy ||
				class == txscript.StakeSubChangeTy) {
			continue
		}

		// Only include the output if it is associated with the passed
		// account.  There should only be one address since this is a
		// P2PKH script.
		addrAcct, err := w.Manager.AddrAccount(addrmgrNs, addrs[0])
		if err != nil || addrAcct != account {
			continue
		}

		eligible = append(eligible, *output)
		outTotal += output.Amount
	}

	if outTotal < amount {
		return nil, nil
	}

	return eligible, nil
}

// signP2PKHMsgTx sets the SignatureScript for every item in msgtx.TxIn.
// It must be called every time a msgtx is changed.
// Only P2PKH outputs are supported at this point.
func (w *Wallet) signP2PKHMsgTx(msgtx *wire.MsgTx, prevOutputs []udb.Credit, addrmgrNs walletdb.ReadBucket) error {
	if len(prevOutputs) != len(msgtx.TxIn) {
		return errors.Errorf(
			"Number of prevOutputs (%d) does not match number of tx inputs (%d)",
			len(prevOutputs), len(msgtx.TxIn))
	}
	for i, output := range prevOutputs {
		// Errors don't matter here, as we only consider the
		// case where len(addrs) == 1.
		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(
			txscript.DefaultScriptVersion, output.PkScript, w.chainParams)
		if len(addrs) != 1 {
			continue
		}
		apkh, ok := addrs[0].(*dcrutil.AddressPubKeyHash)
		if !ok {
			return errors.E(errors.Bug, "previous output address is not P2PKH")
		}

		privKey, done, err := w.Manager.PrivateKey(addrmgrNs, apkh)
		if err != nil {
			return err
		}
		defer done()

		sigscript, err := txscript.SignatureScript(msgtx, i, output.PkScript,
			txscript.SigHashAll, privKey, true)
		if err != nil {
			return errors.E(errors.Op("txscript.SignatureScript"), err)
		}
		msgtx.TxIn[i].SignatureScript = sigscript
	}

	return nil
}

// signVoteOrRevocation signs a vote or revocation, specified by the isVote
// argument.  This signs the transaction by modifying tx's input scripts.
func (w *Wallet) signVoteOrRevocation(addrmgrNs walletdb.ReadBucket, ticketPurchase, tx *wire.MsgTx, isVote bool) error {
	// Create a slice of functions to run after the retreived secrets are no
	// longer needed.
	doneFuncs := make([]func(), 0, len(tx.TxIn))
	defer func() {
		for _, done := range doneFuncs {
			done()
		}
	}()

	// Prepare functions to look up private key and script secrets so signing
	// can be performed.
	var getKey txscript.KeyClosure = func(addr dcrutil.Address) (chainec.PrivateKey, bool, error) {
		key, done, err := w.Manager.PrivateKey(addrmgrNs, addr)
		if err != nil {
			return nil, false, err
		}
		doneFuncs = append(doneFuncs, done)

		return key, true, nil // secp256k1 pubkeys are always compressed in Decred
	}
	var getScript txscript.ScriptClosure = func(addr dcrutil.Address) ([]byte, error) {
		script, done, err := w.Manager.RedeemScript(addrmgrNs, addr)
		if err != nil {
			return nil, err
		}
		doneFuncs = append(doneFuncs, done)
		return script, nil
	}

	// Revocations only contain one input, which is the input that must be
	// signed.  The first input for a vote is the stakebase and the second input
	// must be signed.
	inputToSign := 0
	if isVote {
		inputToSign = 1
	}

	// Sign the input.
	redeemTicketScript := ticketPurchase.TxOut[0].PkScript
	signedScript, err := txscript.SignTxOutput(w.chainParams, tx, inputToSign,
		redeemTicketScript, txscript.SigHashAll, getKey, getScript,
		tx.TxIn[inputToSign].SignatureScript, dcrec.STEcdsaSecp256k1)
	if err != nil {
		return errors.E(errors.Op("txscript.SignTxOutput"), errors.ScriptFailure, err)
	}
	if isVote {
		tx.TxIn[0].SignatureScript = w.chainParams.StakeBaseSigScript
	}
	tx.TxIn[inputToSign].SignatureScript = signedScript

	return nil
}

// signVote signs a vote transaction.  This modifies the input scripts pointed
// to by the vote transaction.
func (w *Wallet) signVote(addrmgrNs walletdb.ReadBucket, ticketPurchase, vote *wire.MsgTx) error {
	return w.signVoteOrRevocation(addrmgrNs, ticketPurchase, vote, true)
}

// signRevocation signs a revocation transaction.  This modifes the input
// scripts pointed to by the revocation transaction.
func (w *Wallet) signRevocation(addrmgrNs walletdb.ReadBucket, ticketPurchase, revocation *wire.MsgTx) error {
	return w.signVoteOrRevocation(addrmgrNs, ticketPurchase, revocation, false)
}

// newVoteScript generates a voting script from the passed VoteBits, for
// use in a vote.
func newVoteScript(voteBits stake.VoteBits) ([]byte, error) {
	b := make([]byte, 2+len(voteBits.ExtendedBits))
	binary.LittleEndian.PutUint16(b[0:2], voteBits.Bits)
	copy(b[2:], voteBits.ExtendedBits[:])
	return txscript.GenerateProvablyPruneableOut(b)
}

// createUnsignedVote creates an unsigned vote transaction that votes using the
// ticket specified by a ticket purchase hash and transaction with the provided
// vote bits.  The block height and hash must be of the previous block the vote
// is voting on.
func createUnsignedVote(ticketHash *chainhash.Hash, ticketPurchase *wire.MsgTx,
	blockHeight int32, blockHash *chainhash.Hash, voteBits stake.VoteBits,
	subsidyCache *blockchain.SubsidyCache, params *chaincfg.Params) (*wire.MsgTx, error) {

	// Parse the ticket purchase transaction to determine the required output
	// destinations for vote rewards or revocations.
	ticketPayKinds, ticketHash160s, ticketValues, _, _, _ :=
		stake.TxSStxStakeOutputInfo(ticketPurchase)

	// Calculate the subsidy for votes at this height.
	subsidy := blockchain.CalcStakeVoteSubsidy(subsidyCache, int64(blockHeight),
		params)

	// Calculate the output values from this vote using the subsidy.
	voteRewardValues := stake.CalculateRewards(ticketValues,
		ticketPurchase.TxOut[0].Value, subsidy)

	// Begin constructing the vote transaction.
	vote := wire.NewMsgTx()

	// Add stakebase input to the vote.
	stakebaseOutPoint := wire.NewOutPoint(&chainhash.Hash{}, ^uint32(0),
		wire.TxTreeRegular)
	stakebaseInput := wire.NewTxIn(stakebaseOutPoint, subsidy, nil)
	vote.AddTxIn(stakebaseInput)

	// Votes reference the ticket purchase with the second input.
	ticketOutPoint := wire.NewOutPoint(ticketHash, 0, wire.TxTreeStake)
	ticketInput := wire.NewTxIn(ticketOutPoint,
		ticketPurchase.TxOut[ticketOutPoint.Index].Value, nil)
	vote.AddTxIn(ticketInput)

	// The first output references the previous block the vote is voting on.
	// This function never errors.
	blockRefScript, _ := txscript.GenerateSSGenBlockRef(*blockHash,
		uint32(blockHeight))
	vote.AddTxOut(wire.NewTxOut(0, blockRefScript))

	// The second output contains the votebits encode as a null data script.
	voteScript, err := newVoteScript(voteBits)
	if err != nil {
		return nil, err
	}
	vote.AddTxOut(wire.NewTxOut(0, voteScript))

	// All remaining outputs pay to the output destinations and amounts tagged
	// by the ticket purchase.
	for i, hash160 := range ticketHash160s {
		scriptFn := txscript.PayToSSGenPKHDirect
		if ticketPayKinds[i] { // P2SH
			scriptFn = txscript.PayToSSGenSHDirect
		}
		// Error is checking for a nil hash160, just ignore it.
		script, _ := scriptFn(hash160)
		vote.AddTxOut(wire.NewTxOut(voteRewardValues[i], script))
	}

	return vote, nil
}

// createUnsignedRevocation creates an unsigned revocation transaction that
// revokes a missed or expired ticket.  Revocations must carry a relay fee and
// this function can error if the revocation contains no suitable output to
// decrease the estimated relay fee from.
func createUnsignedRevocation(ticketHash *chainhash.Hash, ticketPurchase *wire.MsgTx, feePerKB dcrutil.Amount) (*wire.MsgTx, error) {
	// Parse the ticket purchase transaction to determine the required output
	// destinations for vote rewards or revocations.
	ticketPayKinds, ticketHash160s, ticketValues, _, _, _ :=
		stake.TxSStxStakeOutputInfo(ticketPurchase)

	// Calculate the output values for the revocation.  Revocations do not
	// contain any subsidy.
	revocationValues := stake.CalculateRewards(ticketValues,
		ticketPurchase.TxOut[0].Value, 0)

	// Begin constructing the revocation transaction.
	revocation := wire.NewMsgTx()

	// Revocations reference the ticket purchase with the first (and only)
	// input.
	ticketOutPoint := wire.NewOutPoint(ticketHash, 0, wire.TxTreeStake)
	ticketInput := wire.NewTxIn(ticketOutPoint,
		ticketPurchase.TxOut[ticketOutPoint.Index].Value, nil)
	revocation.AddTxIn(ticketInput)
	scriptSizes := []int{txsizes.RedeemP2SHSigScriptSize}

	// All remaining outputs pay to the output destinations and amounts tagged
	// by the ticket purchase.
	for i, hash160 := range ticketHash160s {
		scriptFn := txscript.PayToSSRtxPKHDirect
		if ticketPayKinds[i] { // P2SH
			scriptFn = txscript.PayToSSRtxSHDirect
		}
		// Error is checking for a nil hash160, just ignore it.
		script, _ := scriptFn(hash160)
		revocation.AddTxOut(wire.NewTxOut(revocationValues[i], script))
	}

	// Revocations must pay a fee but do so by decreasing one of the output
	// values instead of increasing the input value and using a change output.
	// Calculate the estimated signed serialize size.
	sizeEstimate := txsizes.EstimateSerializeSize(scriptSizes, revocation.TxOut, 0)
	feeEstimate := txrules.FeeForSerializeSize(feePerKB, sizeEstimate)

	// Reduce the output value of one of the outputs to accomodate for the relay
	// fee.  To avoid creating dust outputs, a suitable output value is reduced
	// by the fee estimate only if it is large enough to not create dust.  This
	// code does not currently handle reducing the output values of multiple
	// commitment outputs to accomodate for the fee.
	for _, output := range revocation.TxOut {
		if dcrutil.Amount(output.Value) > feeEstimate {
			amount := dcrutil.Amount(output.Value) - feeEstimate
			if !txrules.IsDustAmount(amount, len(output.PkScript), feePerKB) {
				output.Value = int64(amount)
				return revocation, nil
			}
		}
	}
	return nil, errors.New("missing suitable revocation output to pay relay fee")
}

// sanityCheckExpiry performs a sanity check on expiry
// returns  tipHeight and error
func (w *Wallet) sanityCheckExpiry(expiry int32) (int32, error) {
	// Need a positive or zero expiry that is higher than the next block to
	// generate.
	if expiry < 0 {
		return 0, fmt.Errorf("need positive expiry")
	}
	// Perform a sanity check on expiry.
	var tipHeight int32
	err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight = w.TxStore.MainChainTip(ns)
		return nil
	})
	if err != nil {
		return tipHeight, err
	}
	if expiry <= tipHeight+1 && expiry > 0 {
		return tipHeight, fmt.Errorf("need expiry that is beyond next height ("+
			"given: %v, next height %v)", expiry, tipHeight+1)
	}
	return tipHeight, err
}

// fetchAddresses returns a voting address. It checks if an address
// was passed along with the request. if none was passed, it checks
// for an address stored in configuration. If it finds none, it generates
// an address from user wallet
func (w *Wallet) fetchAddresses(ticketAddress dcrutil.Address, account uint32,
	addrFunc func(errors.Op, persistReturnedChildFunc, uint32) (dcrutil.Address, error)) (dcrutil.Address, dcrutil.Address, error) {
	const op errors.Op = "wallet.fetchAddresses"
	// If the user hasn't specified a voting address
	// to delegate voting to, just use an address from
	// this wallet. Check the passed address from the
	// request first, then check the ticket address
	// stored from the configuation. Finally, generate
	// an address.
	var addrVote, addrSubsidy dcrutil.Address
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		addrVote = ticketAddress
		var err error
		if addrVote == nil {
			addrVote = w.ticketAddress
			if addrVote == nil {
				addrVote, err = addrFunc(op, w.persistReturnedChild(dbtx), account)
				if err != nil {
					return err
				}
			}
		}

		addrSubsidy, err = addrFunc(op, w.persistReturnedChild(dbtx), account)
		if err != nil {
			return err
		}
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return addrVote, addrSubsidy, nil
}
