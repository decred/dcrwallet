// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"decred.org/cspp"
	"decred.org/cspp/coinjoin"
	"github.com/decred/dcrd/blockchain/stake/v2"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/chaincfg/v2/chainec"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/v3/txauthor"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
	"github.com/decred/dcrwallet/wallet/v3/udb"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
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
	sanityVerifyFlags = txscript.ScriptDiscourageUpgradableNops |
		txscript.ScriptVerifyCleanStack |
		txscript.ScriptVerifyCheckLockTimeVerify |
		txscript.ScriptVerifyCheckSequenceVerify
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
	// picking every possible available output.  This is useful for sweeping.
	OutputSelectionAlgorithmAll
)

// NewUnsignedTransaction constructs an unsigned transaction using unspent
// account outputs.
//
// The changeSource parameter is optional and can be nil.  When nil, and if a
// change output should be added, an internal change address is created for the
// account.
func (w *Wallet) NewUnsignedTransaction(ctx context.Context, outputs []*wire.TxOut,
	relayFeePerKb dcrutil.Amount, account uint32, minConf int32,
	algo OutputSelectionAlgorithm, changeSource txauthor.ChangeSource) (*txauthor.AuthoredTx, error) {

	const op errors.Op = "wallet.NewUnsignedTransaction"

	var unlockOutpoints []*wire.OutPoint
	defer func() {
		if len(unlockOutpoints) != 0 {
			w.lockedOutpointMu.Lock()
			for _, op := range unlockOutpoints {
				delete(w.lockedOutpoints, *op)
			}
			w.lockedOutpointMu.Unlock()
		}
	}()
	ignoreInput := func(op *wire.OutPoint) bool {
		_, ok := w.lockedOutpoints[*op]
		return ok
	}

	var authoredTx *txauthor.AuthoredTx
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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

		sourceImpl := w.TxStore.MakeIgnoredInputSource(txmgrNs, addrmgrNs, account,
			minConf, tipHeight, ignoreInput)
		var inputSource txauthor.InputSource
		switch algo {
		case OutputSelectionAlgorithmDefault:
			inputSource = sourceImpl.SelectInputs
		case OutputSelectionAlgorithmAll:
			// Wrap the source with one that always fetches the max amount
			// available and ignores insufficient balance issues.
			inputSource = func(dcrutil.Amount) (*txauthor.InputDetail, error) {
				inputDetail, err := sourceImpl.SelectInputs(dcrutil.MaxAmount)
				if errors.Is(err, errors.InsufficientBalance) {
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
				persist: w.deferPersistReturnedChild(ctx, &changeSourceUpdates),
				account: account,
				wallet:  w,
				ctx:     context.Background(),
			}
		}

		defer w.lockedOutpointMu.Unlock()
		w.lockedOutpointMu.Lock()

		var err error
		authoredTx, err = txauthor.NewUnsignedTransaction(outputs, relayFeePerKb,
			inputSource, changeSource)
		if err != nil {
			return err
		}
		for _, in := range authoredTx.Tx.TxIn {
			w.lockedOutpoints[in.PreviousOutPoint] = struct{}{}
			unlockOutpoints = append(unlockOutpoints, &in.PreviousOutPoint)
		}
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	if len(changeSourceUpdates) != 0 {
		err := walletdb.Update(ctx, w.db, func(tx walletdb.ReadWriteTx) error {
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

// insertCreditsIntoTxMgr inserts the wallet credits from msgTx to the wallet's
// transaction store. It assumes msgTx is a regular transaction, which will
// cause balance issues if this is called from a code path where msgtx is not
// guaranteed to be a regular tx.
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
			if !errors.Is(err, errors.NotExist) {
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

// txToOutputs creates a signed transaction which includes each output
// from outputs.  Previous outputs to reedeem are chosen from the passed
// account's UTXO set and minconf policy. An additional output may be added to
// return change to the wallet.  An appropriate fee is included based on the
// wallet's current relay fee.  The wallet must be unlocked to create the
// transaction.
//
// Decred: This func also sends the transaction, and if successful, inserts it
// into the database, rather than delegating this work to the caller as
// btcwallet does.
func (w *Wallet) txToOutputs(ctx context.Context, op errors.Op, outputs []*wire.TxOut, account, changeAccount uint32, minconf int32,
	n NetworkBackend, randomizeChangeIdx bool, txFee dcrutil.Amount) (*txauthor.AuthoredTx, error) {

	if n == nil {
		var err error
		n, err = w.NetworkBackend()
		if err != nil {
			return nil, errors.E(op, err)
		}
	}

	var unlockOutpoints []*wire.OutPoint
	defer func() {
		if len(unlockOutpoints) != 0 {
			w.lockedOutpointMu.Lock()
			for _, op := range unlockOutpoints {
				delete(w.lockedOutpoints, *op)
			}
			w.lockedOutpointMu.Unlock()
		}
	}()
	ignoreInput := func(op *wire.OutPoint) bool {
		_, ok := w.lockedOutpoints[*op]
		return ok
	}

	var atx *txauthor.AuthoredTx
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		var once sync.Once
		defer once.Do(w.lockedOutpointMu.Unlock)
		w.lockedOutpointMu.Lock()

		// Create the unsigned transaction.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		inputSource := w.TxStore.MakeIgnoredInputSource(txmgrNs, addrmgrNs, account,
			minconf, tipHeight, ignoreInput)
		changeSource := &p2PKHChangeSource{
			persist: w.deferPersistReturnedChild(ctx, &changeSourceUpdates),
			account: changeAccount,
			wallet:  w,
			ctx:     ctx,
		}
		var err error
		atx, err = txauthor.NewUnsignedTransaction(outputs, txFee,
			inputSource.SelectInputs, changeSource)
		if err != nil {
			return err
		}
		for _, in := range atx.Tx.TxIn {
			w.lockedOutpoints[in.PreviousOutPoint] = struct{}{}
			unlockOutpoints = append(unlockOutpoints, &in.PreviousOutPoint)
		}
		once.Do(w.lockedOutpointMu.Unlock)

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
	var watch []wire.OutPoint
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, up := range changeSourceUpdates {
			err := up(dbtx)
			if err != nil {
				return err
			}
		}

		// TODO: this can be improved by not using the same codepath as notified
		// relevant transactions, since this does a lot of extra work.
		var err error
		watch, err = w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	err = n.PublishTransactions(ctx, atx.Tx)
	if err != nil {
		hash := atx.Tx.TxHash()
		log.Errorf("Abandoning transaction %v which failed to publish", &hash)
		if err := w.AbandonTransaction(ctx, &hash); err != nil {
			log.Errorf("Cannot abandon %v: %v", &hash, err)
		}
		return nil, errors.E(op, err)
	}

	// Watch for future relevant transactions.
	w.addressBuffersMu.Lock()
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		return w.watchFutureAddresses(ctx, dbtx)
	})
	w.addressBuffersMu.Unlock()
	if err != nil {
		log.Errorf("Failed to watch for future address usage after publishing "+
			"transaction: %v", err)
	}
	if len(watch) > 0 {
		err := n.LoadTxFilter(ctx, false, nil, watch)
		if err != nil {
			log.Errorf("Failed to watch outpoints: %v", err)
		}
	}
	return atx, nil
}

// txToMultisig spends funds to a multisig output, partially signs the
// transaction, then returns fund
func (w *Wallet) txToMultisig(ctx context.Context, op errors.Op, account uint32, amount dcrutil.Amount, pubkeys []*dcrutil.AddressSecpPubKey,
	nRequired int8, minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {

	var (
		created  *CreatedTx
		addr     dcrutil.Address
		msScript []byte
	)
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		created, addr, msScript, err = w.txToMultisigInternal(ctx, op, dbtx,
			account, amount, pubkeys, nRequired, minconf)
		return err
	})
	if err != nil {
		return nil, nil, nil, errors.E(op, err)
	}
	return created, addr, msScript, nil
}

func (w *Wallet) txToMultisigInternal(ctx context.Context, op errors.Op, dbtx walletdb.ReadWriteTx, account uint32, amount dcrutil.Amount,
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
	scriptSizes := make([]int, 0, len(eligible))
	// Fill out inputs.
	forSigning := make([]udb.Credit, 0, len(eligible))
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
		if !errors.Is(err, errors.Exist) {
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
	p2shScript, vers, err := addressScript(scAddr)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	txOut := &wire.TxOut{
		Value:    int64(amount),
		PkScript: p2shScript,
		Version:  vers,
	}
	msgtx.AddTxOut(txOut)

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
			persist: w.persistReturnedChild(ctx, dbtx),
			account: account,
			wallet:  w,
			ctx:     ctx,
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

	err = n.PublishTransactions(ctx, msgtx)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	// Request updates from dcrd for new transactions sent to this
	// script hash address.
	err = n.LoadTxFilter(ctx, false, []dcrutil.Address{scAddr}, nil)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	err = w.insertMultisigOutIntoTxMgr(txmgrNs, msgtx, 0)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	created := &CreatedTx{
		MsgTx:       msgtx,
		ChangeAddr:  nil,
		ChangeIndex: -1,
	}

	return created, scAddr, msScript, nil
}

// validateMsgTx verifies transaction input scripts for tx.  All previous output
// scripts from outputs redeemed by the transaction, in the same order they are
// spent, must be passed in the prevScripts slice.
func validateMsgTx(op errors.Op, tx *wire.MsgTx, prevScripts [][]byte) error {
	for i, prevScript := range prevScripts {
		vm, err := txscript.NewEngine(prevScript, tx, i,
			sanityVerifyFlags, 0, nil)
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
func (w *Wallet) compressWallet(ctx context.Context, op errors.Op, maxNumIns int, account uint32, changeAddr dcrutil.Address) (*chainhash.Hash, error) {
	var hash *chainhash.Hash
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		hash, err = w.compressWalletInternal(ctx, op, dbtx, maxNumIns, account, changeAddr)
		return err
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return hash, nil
}

func (w *Wallet) compressWalletInternal(ctx context.Context, op errors.Op, dbtx walletdb.ReadWriteTx, maxNumIns int, account uint32,
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

	// Check if output address is default, and generate a new address if needed
	if changeAddr == nil {
		changeAddr, err = w.newChangeAddress(ctx, op, w.persistReturnedChild(ctx, dbtx), account, gapPolicyIgnore)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}
	pkScript, vers, err := addressScript(changeAddr)
	if err != nil {
		return nil, errors.E(op, errors.Bug, err)
	}
	msgtx := wire.NewMsgTx()
	msgtx.AddTxOut(&wire.TxOut{
		Value:    0,
		PkScript: pkScript,
		Version:  vers,
	})
	maximumTxSize := w.chainParams.MaxTxSize
	if w.chainParams.Net == wire.MainNet {
		maximumTxSize = maxStandardTxSize
	}

	// Add the txins using all the eligible outputs.
	totalAdded := dcrutil.Amount(0)
	scriptSizes := make([]int, 0, maxNumIns)
	forSigning := make([]udb.Credit, 0, maxNumIns)
	count := 0
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

	err = n.PublishTransactions(ctx, msgtx)
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
	pkScript, vers, err := voteRightsScript(addrVote)
	if err != nil {
		return nil, errors.E(errors.Invalid, errors.Errorf("vote address %v", addrVote))
	}

	txOut := &wire.TxOut{
		Value:    ticketCost,
		PkScript: pkScript,
		Version:  vers,
	}
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
		pkScript, vers, err := rewardCommitment(addrPool,
			dcrutil.Amount(amountsCommitted[0]), limits)
		if err != nil {
			return nil, errors.E(errors.Invalid,
				errors.Errorf("pool commitment address %v", addrPool))
		}
		txout := &wire.TxOut{
			Value:    0,
			PkScript: pkScript,
			Version:  vers,
		}
		mtx.AddTxOut(txout)

		// Create a new script which pays to the provided address with an
		// SStx change tagged output.
		pkScript, vers, err = ticketChangeScript(addrZeroed)
		if err != nil {
			return nil, errors.E(errors.Bug,
				errors.Errorf("ticket change address %v", addrZeroed))
		}

		txOut = &wire.TxOut{
			Value:    0,
			PkScript: pkScript,
			Version:  vers,
		}
		mtx.AddTxOut(txOut)
	}

	// 3. Create the commitment and change output paying to the user.
	//
	// Create an OP_RETURN push containing the pubkeyhash to send rewards to.
	// Apply limits to revocations for fees while not allowing
	// fees for votes.
	pkScript, vers, err = rewardCommitment(addrSubsidy,
		dcrutil.Amount(amountsCommitted[userSubsidyNullIdx]), limits)
	if err != nil {
		return nil, errors.E(errors.Invalid,
			errors.Errorf("commitment address %v", addrSubsidy))
	}
	txout := &wire.TxOut{
		Value:    0,
		PkScript: pkScript,
		Version:  vers,
	}
	mtx.AddTxOut(txout)

	// Create a new script which pays to the provided address with an
	// SStx change tagged output.
	pkScript, vers, err = ticketChangeScript(addrZeroed)
	if err != nil {
		return nil, errors.E(errors.Bug,
			errors.Errorf("ticket change address %v", addrZeroed))
	}
	txOut = &wire.TxOut{
		Value:    0,
		PkScript: pkScript,
		Version:  vers,
	}
	mtx.AddTxOut(txOut)

	// Make sure we generated a valid SStx.
	if err := stake.CheckSStx(mtx); err != nil {
		return nil, errors.E(errors.Op("stake.CheckSStx"), errors.Bug, err)
	}

	return mtx, nil
}

var p2pkhSizedScript = make([]byte, 25)

func (w *Wallet) mixedSplit(ctx context.Context, req *PurchaseTicketsRequest, neededPerTicket dcrutil.Amount) (tx *wire.MsgTx, outIndexes []int, err error) {
	// Use txauthor to perform input selection and change amount
	// calculations for the unmixed portions of the coinjoin.
	mixOut := make([]*wire.TxOut, req.Count)
	for i := 0; i < req.Count; i++ {
		mixOut[i] = &wire.TxOut{Value: int64(neededPerTicket), Version: 0, PkScript: p2pkhSizedScript}
	}
	relayFee := req.txFee
	if relayFee == 0 {
		relayFee = w.RelayFee()
	}
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	defer func() {
		if err != nil {
			return
		}
		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			for _, f := range changeSourceUpdates {
				if err := f(dbtx); err != nil {
					return err
				}
			}
			return nil
		})
	}()
	var unlockOutpoints []*wire.OutPoint
	defer func() {
		if len(unlockOutpoints) != 0 {
			w.lockedOutpointMu.Lock()
			for _, op := range unlockOutpoints {
				delete(w.lockedOutpoints, *op)
			}
			w.lockedOutpointMu.Unlock()
		}
	}()
	ignoreInput := func(op *wire.OutPoint) bool {
		_, ok := w.lockedOutpoints[*op]
		return ok
	}
	var atx *txauthor.AuthoredTx
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		defer w.lockedOutpointMu.Unlock()
		w.lockedOutpointMu.Lock()

		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		inputSource := w.TxStore.MakeIgnoredInputSource(txmgrNs, addrmgrNs, req.SourceAccount,
			req.MinConf, tipHeight, ignoreInput)
		changeSource := &p2PKHChangeSource{
			persist:   w.deferPersistReturnedChild(ctx, &changeSourceUpdates),
			account:   req.ChangeAccount,
			wallet:    w,
			ctx:       ctx,
			gapPolicy: gapPolicyIgnore,
		}
		var err error
		atx, err = txauthor.NewUnsignedTransaction(mixOut, relayFee,
			randomInputSource(inputSource.SelectInputs), changeSource)
		if err != nil {
			return err
		}
		for _, in := range atx.Tx.TxIn {
			w.lockedOutpoints[in.PreviousOutPoint] = struct{}{}
			unlockOutpoints = append(unlockOutpoints, &in.PreviousOutPoint)
		}
		return nil
	})
	if err != nil {
		return
	}
	for _, in := range atx.Tx.TxIn {
		log.Infof("selected input %v (%v) for ticket purchase split transaction",
			in.PreviousOutPoint, dcrutil.Amount(in.ValueIn))
	}

	var change *wire.TxOut
	if atx.ChangeIndex >= 0 {
		change = atx.Tx.TxOut[atx.ChangeIndex]
	}
	const (
		txVersion = 1
		locktime  = 0
		expiry    = 0
	)
	pairing := coinjoin.EncodeDesc(coinjoin.P2PKHv0, int64(neededPerTicket), txVersion, locktime, expiry)
	cj := w.newCsppJoin(ctx, change, neededPerTicket, req.MixedSplitAccount, req.MixedAccountBranch, req.Count)
	for i, in := range atx.Tx.TxIn {
		cj.addTxIn(atx.PrevScripts[i], in)
	}

	csppSession, err := cspp.NewSession(rand.Reader, infoLog, pairing, req.Count)
	if err != nil {
		return
	}
	var conn net.Conn
	if req.DialCSPPServer != nil {
		conn, err = req.DialCSPPServer(ctx, "tcp", req.CSPPServer)
	} else {
		conn, err = tls.Dial("tcp", req.CSPPServer, nil)
	}
	if err != nil {
		return
	}
	log.Infof("Dialed CSPPServer %v -> %v", conn.LocalAddr(), conn.RemoteAddr())
	err = csppSession.DiceMix(ctx, conn, cj)
	if err != nil {
		return
	}
	splitTx := cj.tx
	splitTxHash := splitTx.TxHash()
	log.Infof("Completed CoinShuffle++ mix of ticket split transaction %v", &splitTxHash)
	return splitTx, cj.mixOutputIndexes(), nil
}

func (w *Wallet) individualSplit(ctx context.Context, req *PurchaseTicketsRequest, neededPerTicket dcrutil.Amount) (tx *wire.MsgTx, outIndexes []int, err error) {
	// Fetch the single use split address to break tickets into, to
	// immediately be consumed as tickets.
	//
	// This opens a write transaction.
	splitTxAddr, err := w.NewInternalAddress(ctx, req.SourceAccount, WithGapPolicyWrap())
	if err != nil {
		return
	}

	splitPkScript, vers, err := addressScript(splitTxAddr)
	if err != nil {
		err = errors.E(errors.Bug, errors.Errorf("split address %v", splitTxAddr))
		return
	}

	// Create the split transaction by using txToOutputs. This varies
	// based upon whether or not the user is using a stake pool or not.
	// For the default stake pool implementation, the user pays out the
	// first ticket commitment of a smaller amount to the pool, while
	// paying themselves with the larger ticket commitment.
	var splitOuts []*wire.TxOut
	for i := 0; i < req.Count; i++ {
		splitOuts = append(splitOuts, &wire.TxOut{
			Value:    int64(neededPerTicket),
			PkScript: splitPkScript,
			Version:  vers,
		})
		outIndexes = append(outIndexes, i)
	}

	txFeeIncrement := req.txFee
	if txFeeIncrement == 0 {
		txFeeIncrement = w.RelayFee()
	}
	splitTx, err := w.txToOutputs(ctx, "", splitOuts, req.SourceAccount, req.ChangeAccount, req.MinConf,
		nil, false, txFeeIncrement)
	if err != nil {
		return
	}
	tx = splitTx.Tx
	return
}

func (w *Wallet) vspSplit(ctx context.Context, req *PurchaseTicketsRequest, neededPerTicket, vspFee dcrutil.Amount) (tx *wire.MsgTx, outIndexes []int, err error) {
	// Fetch the single use split address to break tickets into, to
	// immediately be consumed as tickets.
	//
	// This opens a write transaction.
	splitTxAddr, err := w.NewInternalAddress(ctx, req.SourceAccount, WithGapPolicyWrap())
	if err != nil {
		return
	}

	splitPkScript, vers, err := addressScript(splitTxAddr)
	if err != nil {
		err = errors.E(errors.Bug, errors.Errorf("split address %v", splitTxAddr))
		return
	}

	// Create the split transaction by using txToOutputs. This varies
	// based upon whether or not the user is using a stake pool or not.
	// For the default stake pool implementation, the user pays out the
	// first ticket commitment of a smaller amount to the pool, while
	// paying themselves with the larger ticket commitment.
	var splitOuts []*wire.TxOut
	for i := 0; i < req.Count; i++ {
		userAmt := neededPerTicket - vspFee
		splitOuts = append(splitOuts, &wire.TxOut{
			Value:    int64(vspFee),
			PkScript: splitPkScript,
			Version:  vers,
		})
		splitOuts = append(splitOuts, &wire.TxOut{
			Value:    int64(userAmt),
			PkScript: splitPkScript,
			Version:  vers,
		})
		outIndexes = append(outIndexes, i*2)
	}

	txFeeIncrement := req.txFee
	if txFeeIncrement == 0 {
		txFeeIncrement = w.RelayFee()
	}
	splitTx, err := w.txToOutputs(ctx, "", splitOuts, req.SourceAccount, req.ChangeAccount, req.MinConf,
		nil, false, txFeeIncrement)
	if err != nil {
		return
	}
	tx = splitTx.Tx
	return
}

// purchaseTickets indicates to the wallet that a ticket should be purchased
// using all currently available funds.  The ticket address parameter in the
// request can be nil in which case the ticket address associated with the
// wallet instance will be used.  Also, when the spend limit in the request is
// greater than or equal to 0, tickets that cost more than that limit will
// return an error that not enough funds are available.
func (w *Wallet) purchaseTickets(ctx context.Context, op errors.Op, n NetworkBackend, req *PurchaseTicketsRequest) ([]*chainhash.Hash, error) {
	// Ensure the minimum number of required confirmations is positive.
	if req.MinConf < 0 {
		return nil, errors.E(op, errors.Invalid, "negative minconf")
	}
	// Need a positive or zero expiry that is higher than the next block to
	// generate.
	if req.Expiry < 0 {
		return nil, errors.E(op, errors.Invalid, "negative expiry")
	}

	// Perform a sanity check on expiry.
	var tipHeight int32
	err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
		ns := tx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight = w.TxStore.MainChainTip(ns)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if req.Expiry <= tipHeight+1 && req.Expiry > 0 {
		return nil, errors.E(op, errors.Invalid, "expiry height must be above next block height")
	}

	addrFunc := func(op errors.Op, maybeDBTX walletdb.ReadWriteTx, account, branch uint32) (dcrutil.Address, error) {
		return w.nextAddress(ctx, op, w.persistReturnedChild(ctx, maybeDBTX), account, branch, WithGapPolicyIgnore())
	}

	if w.addressReuse && req.CSPPServer == "" {
		xpub := w.addressBuffers[udb.DefaultAccountNum].albExternal.branchXpub
		addr, err := deriveChildAddress(xpub, 0, w.chainParams)
		if err != nil {
			err = errors.E(op, err)
		}
		addrFunc = func(errors.Op, walletdb.ReadWriteTx, uint32, uint32) (dcrutil.Address, error) {
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
	account := req.SourceAccount

	// Calculate the current ticket price.  If the DCP0001 deployment is not
	// active, fallback to querying the ticket price over RPC.
	ticketPrice, err := w.NextStakeDifficulty(ctx)
	if errors.Is(err, errors.Deployment) {
		ticketPrice, err = n.StakeDifficulty(ctx)
	}
	if err != nil {
		return nil, err
	}

	// Ensure the ticket price does not exceed the spend limit if set.
	if req.spendLimit > 0 && ticketPrice > req.spendLimit {
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
	switch req.VotingAddress.(type) {
	case *dcrutil.AddressScriptHash:
		stakeSubmissionPkScriptSize = txsizes.P2SHPkScriptSize + 1
	case interface {
		V0Scripter
		SecpPubKeyHash160er
	}:
		stakeSubmissionPkScriptSize = txsizes.P2PKHPkScriptSize + 1
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

	if poolAddress == nil {
		// A solo ticket has:
		//   - a single input redeeming a P2PKH for the worst case size
		//   - a P2PKH or P2SH stake submission output
		//   - a ticket commitment output
		//   - an OP_SSTXCHANGE tagged P2PKH or P2SH change output
		//
		//   NB: The wallet currently only supports P2PKH change addresses.
		//   The network supports both P2PKH and P2SH change addresses however.
		inSizes := []int{txsizes.RedeemP2PKHSigScriptSize}
		outSizes := []int{stakeSubmissionPkScriptSize,
			txsizes.TicketCommitmentScriptSize, txsizes.P2PKHPkScriptSize + 1}
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
		inSizes := []int{txsizes.RedeemP2PKHSigScriptSize,
			txsizes.RedeemP2PKHSigScriptSize}
		outSizes := []int{stakeSubmissionPkScriptSize,
			txsizes.TicketCommitmentScriptSize, txsizes.TicketCommitmentScriptSize,
			txsizes.P2PKHPkScriptSize + 1, txsizes.P2PKHPkScriptSize + 1}
		estSize = txsizes.EstimateSerializeSizeFromScriptSizes(inSizes,
			outSizes, 0)
	}

	ticketFee = txrules.FeeForSerializeSize(ticketFeeIncrement, estSize)
	neededPerTicket = ticketFee + ticketPrice

	// If we need to calculate the amount for a pool fee percentage,
	// do so now.
	var vspFee dcrutil.Amount
	if poolAddress != nil {
		vspFee = txrules.StakePoolTicketFee(ticketPrice, ticketFee,
			tipHeight, poolFees, w.ChainParams())
	}

	// Make sure this doesn't over spend based on the balance to
	// maintain. This component of the API is inaccessible to the
	// end user through the legacy RPC, so it should only ever be
	// set by internal calls e.g. automatic ticket purchase.
	if req.minBalance > 0 {
		bal, err := w.CalculateAccountBalance(ctx, account, req.MinConf)
		if err != nil {
			return nil, err
		}

		estimatedFundsUsed := neededPerTicket * dcrutil.Amount(req.Count)
		if req.minBalance+estimatedFundsUsed > bal.Spendable {
			return nil, errors.E(op, errors.InsufficientBalance, errors.Errorf(
				"estimated ending balance %v is below minimum requested balance %v",
				bal.Spendable-estimatedFundsUsed, req.minBalance))
		}
	}

	// After tickets are created and published, watch for future
	// relevant transactions
	var watchOutPoints []wire.OutPoint
	defer func() {
		w.addressBuffersMu.Lock()
		err := walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
			return w.watchFutureAddresses(ctx, tx)
		})
		w.addressBuffersMu.Unlock()
		if err != nil {
			log.Errorf("Failed to watch for future addresses after ticket "+
				"purchases: %v", err)
		}
		if len(watchOutPoints) > 0 {
			err := n.LoadTxFilter(ctx, false, nil, watchOutPoints)
			if err != nil {
				log.Errorf("Failed to watch outpoints: %v", err)
			}
		}
	}()

	var splitTx *wire.MsgTx
	var splitOutputIndexes []int
	switch {
	case req.CSPPServer != "":
		splitTx, splitOutputIndexes, err = w.mixedSplit(ctx, req, neededPerTicket)
	case req.poolAddress != nil:
		splitTx, splitOutputIndexes, err = w.vspSplit(ctx, req, neededPerTicket, vspFee)
	default:
		splitTx, splitOutputIndexes, err = w.individualSplit(ctx, req, neededPerTicket)
	}
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Process and publish split tx.
	rec, err := udb.NewTxRecordFromMsgTx(splitTx, time.Now())
	if err != nil {
		return nil, err
	}
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		watch, err := w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
		watchOutPoints = append(watchOutPoints, watch...)
		return err
	})
	if err != nil {
		return nil, err
	}
	w.recentlyPublishedMu.Lock()
	w.recentlyPublished[rec.Hash] = struct{}{}
	w.recentlyPublishedMu.Unlock()
	err = n.PublishTransactions(ctx, splitTx)
	if err != nil {
		return nil, err
	}

	// Create each ticket
	ticketHashes := make([]*chainhash.Hash, 0, req.Count)
	outpoint := wire.OutPoint{Hash: splitTx.TxHash()}
	for _, index := range splitOutputIndexes {
		// Generate the extended outpoints that we need to use for ticket
		// inputs. There are two inputs for pool tickets corresponding to the
		// fees and the user subsidy, while user-handled tickets have only one
		// input.
		var eopPool, eop *extendedOutPoint
		if poolAddress == nil {
			op := outpoint
			op.Index = uint32(index)
			log.Infof("Split output is %v", &op)
			txOut := splitTx.TxOut[index]
			eop = &extendedOutPoint{
				op:       &op,
				amt:      txOut.Value,
				pkScript: txOut.PkScript,
			}
		} else {
			vspOutPoint := outpoint
			vspOutPoint.Index = uint32(index)
			vspOutput := splitTx.TxOut[vspOutPoint.Index]
			eopPool = &extendedOutPoint{
				op:       &vspOutPoint,
				amt:      vspOutput.Value,
				pkScript: vspOutput.PkScript,
			}
			myOutPoint := outpoint
			myOutPoint.Index = uint32(index + 1)
			myOutput := splitTx.TxOut[myOutPoint.Index]
			eop = &extendedOutPoint{
				op:       &myOutPoint,
				amt:      myOutput.Value,
				pkScript: myOutput.PkScript,
			}
		}

		// If the user hasn't specified a voting address
		// to delegate voting to, just use an address from
		// this wallet. Check the passed address from the
		// request first, then check the ticket address
		// stored from the configuation. Finally, generate
		// an address.
		var addrVote, addrSubsidy dcrutil.Address
		err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			addrVote = req.VotingAddress
			if addrVote == nil && req.CSPPServer == "" {
				addrVote = w.ticketAddress
			}
			if addrVote == nil {
				addrVote, err = addrFunc(op, dbtx, req.VotingAccount, 1)
				if err != nil {
					return err
				}
			}

			var subsidyAccount = req.SourceAccount
			var branch uint32 = 1
			if req.CSPPServer != "" {
				subsidyAccount = req.MixedAccount
				branch = req.MixedAccountBranch
			}
			addrSubsidy, err = addrFunc(op, dbtx, subsidyAccount, branch)
			return err
		})
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}

		// Generate the ticket msgTx and sign it.
		ticket, err := makeTicket(w.chainParams, eopPool, eop, addrVote,
			addrSubsidy, int64(ticketPrice), poolAddress)
		if err != nil {
			return ticketHashes, err
		}
		var forSigning []udb.Credit
		if eopPool != nil {
			eopPoolCredit := udb.Credit{
				OutPoint:     *eopPool.op,
				BlockMeta:    udb.BlockMeta{},
				Amount:       dcrutil.Amount(eopPool.amt),
				PkScript:     eopPool.pkScript,
				Received:     time.Now(),
				FromCoinBase: false,
			}
			forSigning = append(forSigning, eopPoolCredit)
		}
		eopCredit := udb.Credit{
			OutPoint:     *eop.op,
			BlockMeta:    udb.BlockMeta{},
			Amount:       dcrutil.Amount(eop.amt),
			PkScript:     eop.pkScript,
			Received:     time.Now(),
			FromCoinBase: false,
		}
		forSigning = append(forSigning, eopCredit)

		// Set the expiry.
		ticket.Expiry = uint32(req.Expiry)

		err = walletdb.View(ctx, w.db, func(tx walletdb.ReadTx) error {
			ns := tx.ReadBucket(waddrmgrNamespaceKey)
			return w.signP2PKHMsgTx(ticket, forSigning, ns)
		})
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}
		err = validateMsgTx(op, ticket, creditScripts(forSigning))
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}

		err = w.checkHighFees(dcrutil.Amount(eop.amt), ticket)
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}

		rec, err := udb.NewTxRecordFromMsgTx(ticket, time.Now())
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}

		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			watch, err := w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
			watchOutPoints = append(watchOutPoints, watch...)
			return err
		})
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}

		w.recentlyPublishedMu.Lock()
		w.recentlyPublished[rec.Hash] = struct{}{}
		w.recentlyPublishedMu.Unlock()

		// TODO: Send all tickets, and all split transactions, together.  Purge
		// transactions from DB if tickets cannot be sent.
		err = n.PublishTransactions(ctx, ticket)
		if err != nil {
			return ticketHashes, errors.E(op, err)
		}
		ticketHash := ticket.TxHash()
		ticketHashes = append(ticketHashes, &ticketHash)
		log.Infof("Published ticket purchase %v", &ticketHash)
	}

	return ticketHashes, nil
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
	// dependency and requesting unspent outputs for a single account.
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
			0, output.PkScript, w.chainParams)
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
func (w *Wallet) FindEligibleOutputs(ctx context.Context, account uint32, minconf int32, currentHeight int32) ([]udb.Credit, error) {
	const op errors.Op = "wallet.FindEligibleOutputs"

	var unspentOutputs []udb.Credit
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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
			0, output.PkScript, w.chainParams)
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
			0, output.PkScript, w.chainParams)
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
	copy(b[2:], voteBits.ExtendedBits)
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
	subsidy := subsidyCache.CalcStakeVoteSubsidy(int64(blockHeight))

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

	// Reduce the output value of one of the outputs to accommodate for the relay
	// fee.  To avoid creating dust outputs, a suitable output value is reduced
	// by the fee estimate only if it is large enough to not create dust.  This
	// code does not currently handle reducing the output values of multiple
	// commitment outputs to accommodate for the fee.
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
