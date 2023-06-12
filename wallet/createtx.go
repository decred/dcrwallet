// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"net"
	"sort"
	"time"

	"decred.org/cspp/v2"
	"decred.org/cspp/v2/coinjoin"
	"decred.org/dcrwallet/v3/deployments"
	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/internal/uniformprng"
	"decred.org/dcrwallet/v3/rpc/client/dcrd"
	"decred.org/dcrwallet/v3/wallet/txauthor"
	"decred.org/dcrwallet/v3/wallet/txrules"
	"decred.org/dcrwallet/v3/wallet/txsizes"
	"decred.org/dcrwallet/v3/wallet/udb"
	"decred.org/dcrwallet/v3/wallet/walletdb"
	"github.com/decred/dcrd/blockchain/stake/v5"
	blockchain "github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

// --------------------------------------------------------------------------------
// Constants and simple functions

const (
	revocationFeeLimit = 1 << 14

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
		txscript.ScriptVerifyCheckSequenceVerify |
		txscript.ScriptVerifyTreasury
)

// Input provides transaction inputs referencing spendable outputs.
type Input struct {
	OutPoint wire.OutPoint
	PrevOut  wire.TxOut
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
// The changeSource and inputSource parameters are optional and can be nil.
// When the changeSource is nil and change output should be added, an internal
// change address is created for the account.  When the inputSource is nil,
// the inputs will be selected by the wallet.
func (w *Wallet) NewUnsignedTransaction(ctx context.Context, outputs []*wire.TxOut,
	relayFeePerKb dcrutil.Amount, account uint32, minConf int32,
	algo OutputSelectionAlgorithm, changeSource txauthor.ChangeSource, inputSource txauthor.InputSource) (*txauthor.AuthoredTx, error) {

	const op errors.Op = "wallet.NewUnsignedTransaction"

	ignoreInput := func(op *wire.OutPoint) bool {
		_, ok := w.lockedOutpoints[outpoint{op.Hash, op.Index}]
		return ok
	}
	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	var authoredTx *txauthor.AuthoredTx
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		_, tipHeight := w.txStore.MainChainTip(dbtx)

		if account != udb.ImportedAddrAccount {
			lastAcct, err := w.manager.LastAccount(addrmgrNs)
			if err != nil {
				return err
			}
			if account > lastAcct {
				return errors.E(errors.NotExist, "missing account")
			}
		}

		if inputSource == nil {
			sourceImpl := w.txStore.MakeInputSource(dbtx, account,
				minConf, tipHeight, ignoreInput)
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
		}

		if changeSource == nil {
			changeSource = &p2PKHChangeSource{
				persist: w.deferPersistReturnedChild(ctx, &changeSourceUpdates),
				account: account,
				wallet:  w,
				ctx:     context.Background(),
			}
		}

		var err error
		authoredTx, err = txauthor.NewUnsignedTransaction(outputs, relayFeePerKb,
			inputSource, changeSource, w.chainParams.MaxTxSize)
		if err != nil {
			return err
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

func (s *secretSource) GetKey(addr stdaddr.Address) ([]byte, dcrec.SignatureType, bool, error) {
	privKey, done, err := s.Manager.PrivateKey(s.addrmgrNs, addr)
	if err != nil {
		return nil, 0, false, err
	}
	s.doneFuncs = append(s.doneFuncs, done)
	return privKey.Serialize(), dcrec.STEcdsaSecp256k1, true, nil
}

func (s *secretSource) GetScript(addr stdaddr.Address) ([]byte, error) {
	return s.Manager.RedeemScript(s.addrmgrNs, addr)
}

// CreatedTx holds the state of a newly-created transaction and the change
// output (if one was added).
type CreatedTx struct {
	MsgTx       *wire.MsgTx
	ChangeAddr  stdaddr.Address
	ChangeIndex int // negative if no change
	Fee         dcrutil.Amount
}

// insertIntoTxMgr inserts a newly created transaction into the tx store
// as unconfirmed.
func (w *Wallet) insertIntoTxMgr(dbtx walletdb.ReadWriteTx, msgTx *wire.MsgTx) (*udb.TxRecord, error) {
	// Create transaction record and insert into the db.
	rec, err := udb.NewTxRecordFromMsgTx(msgTx, time.Now())
	if err != nil {
		return nil, err
	}

	err = w.txStore.InsertMemPoolTx(dbtx, rec)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// insertCreditsIntoTxMgr inserts the wallet credits from msgTx to the wallet's
// transaction store. It assumes msgTx is a regular transaction, which will
// cause balance issues if this is called from a code path where msgtx is not
// guaranteed to be a regular tx.
func (w *Wallet) insertCreditsIntoTxMgr(op errors.Op, dbtx walletdb.ReadWriteTx, msgTx *wire.MsgTx, rec *udb.TxRecord) error {
	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)

	// Check every output to determine whether it is controlled by a wallet
	// key.  If so, mark the output as a credit.
	for i, output := range msgTx.TxOut {
		_, addrs := stdscript.ExtractAddrs(output.Version, output.PkScript, w.chainParams)
		for _, addr := range addrs {
			ma, err := w.manager.Address(addrmgrNs, addr)
			if err == nil {
				// TODO: Credits should be added with the
				// account they belong to, so wtxmgr is able to
				// track per-account balances.
				err = w.txStore.AddCredit(dbtx, rec, nil,
					uint32(i), ma.Internal(), ma.Account())
				if err != nil {
					return errors.E(op, err)
				}
				err = w.markUsedAddress(op, dbtx, ma)
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
func (w *Wallet) insertMultisigOutIntoTxMgr(dbtx walletdb.ReadWriteTx, msgTx *wire.MsgTx, index uint32) error {
	// Create transaction record and insert into the db.
	rec, err := udb.NewTxRecordFromMsgTx(msgTx, time.Now())
	if err != nil {
		return err
	}

	return w.txStore.AddMultisigOut(dbtx, rec, nil, index)
}

// checkHighFees performs a high fee check if enabled and possible, returning an
// error if the transaction pays high fees.
func (w *Wallet) checkHighFees(totalInput dcrutil.Amount, tx *wire.MsgTx) error {
	if w.allowHighFees {
		return nil
	}
	if txrules.PaysHighFees(totalInput, tx) {
		return errors.E(errors.Policy, "high fee")
	}
	return nil
}

// publishAndWatch publishes an authored transaction to the network and begins watching for
// relevant transactions.
func (w *Wallet) publishAndWatch(ctx context.Context, op errors.Op, n NetworkBackend, tx *wire.MsgTx,
	watch []wire.OutPoint) error {

	if n == nil {
		var err error
		n, err = w.NetworkBackend()
		if err != nil {
			return errors.E(op, err)
		}
	}

	err := n.PublishTransactions(ctx, tx)
	if err != nil {
		hash := tx.TxHash()
		log.Errorf("Abandoning transaction %v which failed to publish", &hash)
		if err := w.AbandonTransaction(ctx, &hash); err != nil {
			log.Errorf("Cannot abandon %v: %v", &hash, err)
		}
		return errors.E(op, err)
	}

	// Watch for future relevant transactions.
	_, err = w.watchHDAddrs(ctx, false, n)
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
	return nil
}

type authorTx struct {
	outputs            []*wire.TxOut
	account            uint32
	changeAccount      uint32
	minconf            int32
	randomizeChangeIdx bool
	txFee              dcrutil.Amount
	dontSignTx         bool
	isTreasury         bool

	atx                 *txauthor.AuthoredTx
	changeSourceUpdates []func(walletdb.ReadWriteTx) error
	watch               []wire.OutPoint
}

// authorTx creates a (typically signed) transaction which includes each output
// from outputs.  Previous outputs to redeem are chosen from the passed
// account's UTXO set and minconf policy. An additional output may be added to
// return change to the wallet.  An appropriate fee is included based on the
// wallet's current relay fee.  The wallet must be unlocked to create the
// transaction.
func (w *Wallet) authorTx(ctx context.Context, op errors.Op, a *authorTx) error {
	var unlockOutpoints []*wire.OutPoint
	defer func() {
		for _, op := range unlockOutpoints {
			delete(w.lockedOutpoints, outpoint{op.Hash, op.Index})
		}
		w.lockedOutpointMu.Unlock()
	}()
	ignoreInput := func(op *wire.OutPoint) bool {
		_, ok := w.lockedOutpoints[outpoint{op.Hash, op.Index}]
		return ok
	}
	w.lockedOutpointMu.Lock()

	var atx *txauthor.AuthoredTx
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

		// Create the unsigned transaction.
		_, tipHeight := w.txStore.MainChainTip(dbtx)
		inputSource := w.txStore.MakeInputSource(dbtx, a.account,
			a.minconf, tipHeight, ignoreInput)
		var changeSource txauthor.ChangeSource
		if a.isTreasury {
			changeSource = &p2PKHTreasuryChangeSource{
				persist: w.deferPersistReturnedChild(ctx,
					&changeSourceUpdates),
				account: a.changeAccount,
				wallet:  w,
				ctx:     ctx,
			}
		} else {
			changeSource = &p2PKHChangeSource{
				persist: w.deferPersistReturnedChild(ctx,
					&changeSourceUpdates),
				account:   a.changeAccount,
				wallet:    w,
				ctx:       ctx,
				gapPolicy: gapPolicyWrap,
			}
		}
		var err error
		atx, err = txauthor.NewUnsignedTransaction(a.outputs, a.txFee,
			inputSource.SelectInputs, changeSource,
			w.chainParams.MaxTxSize)
		if err != nil {
			return err
		}
		for _, in := range atx.Tx.TxIn {
			prev := &in.PreviousOutPoint
			w.lockedOutpoints[outpoint{prev.Hash, prev.Index}] = struct{}{}
			unlockOutpoints = append(unlockOutpoints, prev)
		}

		// Randomize change position, if change exists, before signing.
		// This doesn't affect the serialize size, so the change amount
		// will still be valid.
		if atx.ChangeIndex >= 0 && a.randomizeChangeIdx {
			atx.RandomizeChangePosition()
		}

		// TADDs need to use version 3 txs.
		if a.isTreasury {
			// This check ensures that if NewUnsignedTransaction is
			// updated to generate a different transaction version
			// we error out loudly instead of failing to validate
			// in some obscure way.
			//
			// TODO: maybe isTreasury should be passed into
			// NewUnsignedTransaction?
			if atx.Tx.Version != wire.TxVersion {
				return errors.E(op, "violated assumption: "+
					"expected unsigned tx to be version 1")
			}
			atx.Tx.Version = wire.TxVersionTreasury
		}

		if !a.dontSignTx {
			// Sign the transaction.
			secrets := &secretSource{Manager: w.manager, addrmgrNs: addrmgrNs}
			err = atx.AddAllInputScripts(secrets)
			for _, done := range secrets.doneFuncs {
				done()
			}
		}
		return err
	})
	if err != nil {
		return errors.E(op, err)
	}

	// Warn when spending UTXOs controlled by imported keys created change for
	// the default account.
	if atx.ChangeIndex >= 0 && a.account == udb.ImportedAddrAccount {
		changeAmount := dcrutil.Amount(atx.Tx.TxOut[atx.ChangeIndex].Value)
		log.Warnf("Spend from imported account produced change: moving"+
			" %v from imported account into default account.", changeAmount)
	}

	err = w.checkHighFees(atx.TotalInput, atx.Tx)
	if err != nil {
		return errors.E(op, err)
	}

	if !a.dontSignTx {
		// Ensure valid signatures were created.
		err = validateMsgTx(op, atx.Tx, atx.PrevScripts)
		if err != nil {
			return errors.E(op, err)
		}
	}

	a.atx = atx
	a.changeSourceUpdates = changeSourceUpdates
	return nil
}

// recordAuthoredTx records an authored transaction to the wallet's database.  It
// also updates the database for change addresses used by the new transaction.
//
// As a side effect of recording the transaction to the wallet, clients
// subscribed to new tx notifications will also be notified of the new
// transaction.
func (w *Wallet) recordAuthoredTx(ctx context.Context, op errors.Op, a *authorTx) error {
	rec, err := udb.NewTxRecordFromMsgTx(a.atx.Tx, time.Now())
	if err != nil {
		return errors.E(op, err)
	}

	w.lockedOutpointMu.Lock()
	defer w.lockedOutpointMu.Unlock()

	// To avoid a race between publishing a transaction and potentially opening
	// a database view during PublishTransaction, the update must be committed
	// before publishing the transaction to the network.
	var watch []wire.OutPoint
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, up := range a.changeSourceUpdates {
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
		return errors.E(op, err)
	}

	a.watch = watch
	return nil
}

// txToMultisig spends funds to a multisig output, partially signs the
// transaction, then returns fund
func (w *Wallet) txToMultisig(ctx context.Context, op errors.Op, account uint32, amount dcrutil.Amount, pubkeys [][]byte,
	nRequired int8, minconf int32) (*CreatedTx, stdaddr.Address, []byte, error) {

	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	var created *CreatedTx
	var addr stdaddr.Address
	var msScript []byte
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
	pubkeys [][]byte, nRequired int8, minconf int32) (*CreatedTx, stdaddr.Address, []byte, error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)

	txToMultisigError := func(err error) (*CreatedTx, stdaddr.Address, []byte, error) {
		return nil, nil, nil, err
	}

	n, err := w.NetworkBackend()
	if err != nil {
		return txToMultisigError(err)
	}

	// Get current block's height and hash.
	_, topHeight := w.txStore.MainChainTip(dbtx)

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
	const minAmount = 0
	const maxResults = 0
	eligible, err := w.findEligibleOutputsAmount(dbtx, account, minconf,
		amountRequired, topHeight, minAmount, maxResults)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	if eligible == nil {
		return txToMultisigError(errors.E(op, "not enough funds to send to multisig address"))
	}
	for i := range eligible {
		op := &eligible[i].OutPoint
		w.lockedOutpoints[outpoint{op.Hash, op.Index}] = struct{}{}
	}
	defer func() {
		for i := range eligible {
			op := &eligible[i].OutPoint
			delete(w.lockedOutpoints, outpoint{op.Hash, op.Index})
		}
	}()

	msgtx := wire.NewMsgTx()
	scriptSizes := make([]int, 0, len(eligible))
	// Fill out inputs.
	forSigning := make([]Input, 0, len(eligible))
	totalInput := dcrutil.Amount(0)
	for _, e := range eligible {
		txIn := wire.NewTxIn(&e.OutPoint, e.PrevOut.Value, nil)
		msgtx.AddTxIn(txIn)
		totalInput += dcrutil.Amount(e.PrevOut.Value)
		forSigning = append(forSigning, e)
		scriptSizes = append(scriptSizes, txsizes.RedeemP2SHSigScriptSize)
	}

	// Insert a multi-signature output, then insert this P2SH
	// hash160 into the address manager and the transaction
	// manager.
	msScript, err := stdscript.MultiSigScriptV0(int(nRequired), pubkeys...)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	_, err = w.manager.ImportScript(addrmgrNs, msScript)
	if err != nil {
		// We don't care if we've already used this address.
		if !errors.Is(err, errors.Exist) {
			return txToMultisigError(errors.E(op, err))
		}
	}
	scAddr, err := stdaddr.NewAddressScriptHashV0(msScript, w.chainParams)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}
	vers, p2shScript := scAddr.PaymentScript()
	txOut := &wire.TxOut{
		Value:    int64(amount),
		PkScript: p2shScript,
		Version:  vers,
	}
	msgtx.AddTxOut(txOut)

	// Add change if we need it.
	changeSize := 0
	if totalInput > amount+feeEstForTx {
		changeSize = txsizes.P2PKHPkScriptSize
	}
	feeSize := txsizes.EstimateSerializeSize(scriptSizes, msgtx.TxOut, changeSize)
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

		pkScript, vers, err := changeSource.Script()
		if err != nil {
			return txToMultisigError(err)
		}
		change := totalInput - (amount + feeEst)
		msgtx.AddTxOut(&wire.TxOut{
			Value:    int64(change),
			Version:  vers,
			PkScript: pkScript,
		})
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
	err = n.LoadTxFilter(ctx, false, []stdaddr.Address{scAddr}, nil)
	if err != nil {
		return txToMultisigError(errors.E(op, err))
	}

	err = w.insertMultisigOutIntoTxMgr(dbtx, msgtx, 0)
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
			sanityVerifyFlags, scriptVersionAssumed, nil)
		if err != nil {
			return errors.E(op, err)
		}
		err = vm.Execute()
		if err != nil {
			prevOut := &tx.TxIn[i].PreviousOutPoint
			sigScript := tx.TxIn[i].SignatureScript

			log.Errorf("Script validation failed (outpoint %v pkscript %x sigscript %x): %v",
				prevOut, prevScript, sigScript, err)
			return errors.E(op, errors.ScriptFailure, err)
		}
	}
	return nil
}

func creditScripts(credits []Input) [][]byte {
	scripts := make([][]byte, 0, len(credits))
	for _, c := range credits {
		scripts = append(scripts, c.PrevOut.PkScript)
	}
	return scripts
}

// compressWallet compresses all the utxos in a wallet into a single change
// address. For use when it becomes dusty.
func (w *Wallet) compressWallet(ctx context.Context, op errors.Op, maxNumIns int, account uint32, changeAddr stdaddr.Address) (*chainhash.Hash, error) {
	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

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
	changeAddr stdaddr.Address) (*chainhash.Hash, error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Get current block's height
	_, tipHeight := w.txStore.MainChainTip(dbtx)

	minconf := int32(1)
	eligible, err := w.findEligibleOutputs(dbtx, account, minconf, tipHeight)
	if err != nil {
		return nil, errors.E(op, err)
	}
	if len(eligible) <= 1 {
		return nil, errors.E(op, "too few outputs to consolidate")
	}
	for i := range eligible {
		op := eligible[i].OutPoint
		w.lockedOutpoints[outpoint{op.Hash, op.Index}] = struct{}{}
	}

	defer func() {
		for i := range eligible {
			op := &eligible[i].OutPoint
			delete(w.lockedOutpoints, outpoint{op.Hash, op.Index})
		}
	}()

	// Check if output address is default, and generate a new address if needed
	if changeAddr == nil {
		const accountName = "" // not used, so can be faked.
		changeAddr, err = w.newChangeAddress(ctx, op, w.persistReturnedChild(ctx, dbtx),
			accountName, account, gapPolicyIgnore)
		if err != nil {
			return nil, errors.E(op, err)
		}
	}
	vers, pkScript := changeAddr.PaymentScript()
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
	forSigning := make([]Input, 0, maxNumIns)
	count := 0
	for _, e := range eligible {
		if count >= maxNumIns {
			break
		}
		// Add the size of a wire.OutPoint
		if msgtx.SerializeSize() > maximumTxSize {
			break
		}

		txIn := wire.NewTxIn(&e.OutPoint, e.PrevOut.Value, nil)
		msgtx.AddTxIn(txIn)
		totalAdded += dcrutil.Amount(e.PrevOut.Value)
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
	rec, err := w.insertIntoTxMgr(dbtx, msgtx)
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
func makeTicket(params *chaincfg.Params, inputPool *Input, input *Input, addrVote stdaddr.StakeAddress,
	addrSubsidy stdaddr.StakeAddress, ticketCost int64, addrPool stdaddr.StakeAddress) (*wire.MsgTx, error) {

	mtx := wire.NewMsgTx()

	if addrPool != nil && inputPool != nil {
		txIn := wire.NewTxIn(&inputPool.OutPoint, inputPool.PrevOut.Value, []byte{})
		mtx.AddTxIn(txIn)
	}

	txIn := wire.NewTxIn(&input.OutPoint, input.PrevOut.Value, []byte{})
	mtx.AddTxIn(txIn)

	// Create a new script which pays to the provided address with an
	// SStx tagged output.
	if addrVote == nil {
		return nil, errors.E(errors.Invalid, "nil vote address")
	}
	vers, pkScript := addrVote.VotingRightsScript()

	txOut := &wire.TxOut{
		Value:    ticketCost,
		PkScript: pkScript,
		Version:  vers,
	}
	mtx.AddTxOut(txOut)

	// Obtain the commitment amounts.
	var amountsCommitted []int64
	userSubsidyNullIdx := 0
	var err error
	if addrPool == nil {
		_, amountsCommitted, err = stake.SStxNullOutputAmounts(
			[]int64{input.PrevOut.Value}, []int64{0}, ticketCost)
		if err != nil {
			return nil, err
		}

	} else {
		_, amountsCommitted, err = stake.SStxNullOutputAmounts(
			[]int64{inputPool.PrevOut.Value, input.PrevOut.Value}, []int64{0, 0}, ticketCost)
		if err != nil {
			return nil, err
		}
		userSubsidyNullIdx = 1
	}

	// Zero value P2PKH addr.
	zeroed := [20]byte{}
	addrZeroed, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(zeroed[:], params)
	if err != nil {
		return nil, err
	}

	// 2. (Optional) If we're passed a pool address, make an extra
	// commitment to the pool.
	if addrPool != nil {
		vers, pkScript := addrPool.RewardCommitmentScript(
			amountsCommitted[0], 0, revocationFeeLimit)
		txout := &wire.TxOut{
			Value:    0,
			PkScript: pkScript,
			Version:  vers,
		}
		mtx.AddTxOut(txout)

		// Create a new script which pays to the provided address with an
		// SStx change tagged output.
		vers, pkScript = addrZeroed.StakeChangeScript()

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
	vers, pkScript = addrSubsidy.RewardCommitmentScript(
		amountsCommitted[userSubsidyNullIdx], 0, revocationFeeLimit)
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
	vers, pkScript = addrZeroed.StakeChangeScript()
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
	relayFee := w.RelayFee()
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
				delete(w.lockedOutpoints, outpoint{op.Hash, op.Index})
			}
			w.lockedOutpointMu.Unlock()
		}
	}()
	ignoreInput := func(op *wire.OutPoint) bool {
		_, ok := w.lockedOutpoints[outpoint{op.Hash, op.Index}]
		return ok
	}

	w.lockedOutpointMu.Lock()
	var atx *txauthor.AuthoredTx
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		_, tipHeight := w.txStore.MainChainTip(dbtx)
		inputSource := w.txStore.MakeInputSource(dbtx, req.SourceAccount,
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
			inputSource.SelectInputs, changeSource,
			w.chainParams.MaxTxSize)
		if err != nil {
			return err
		}
		for _, in := range atx.Tx.TxIn {
			prev := &in.PreviousOutPoint
			w.lockedOutpoints[outpoint{prev.Hash, prev.Index}] = struct{}{}
			unlockOutpoints = append(unlockOutpoints, prev)
		}
		return nil
	})
	w.lockedOutpointMu.Unlock()
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
	if change != nil && dcrutil.Amount(change.Value) < smallestMixChange(relayFee) {
		change = nil
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

	csppSession, err := cspp.NewSession(rand.Reader, debugLog, pairing, req.Count)
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
	defer conn.Close()
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

	vers, splitPkScript := splitTxAddr.PaymentScript()

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

	const op errors.Op = "individualSplit"
	a := &authorTx{
		outputs:            splitOuts,
		account:            req.SourceAccount,
		changeAccount:      req.ChangeAccount,
		minconf:            req.MinConf,
		randomizeChangeIdx: false,
		txFee:              w.RelayFee(),
		dontSignTx:         req.DontSignTx,
		isTreasury:         false,
	}
	err = w.authorTx(ctx, op, a)
	if err != nil {
		return
	}
	err = w.recordAuthoredTx(ctx, op, a)
	if err != nil {
		return
	}
	if !req.DontSignTx {
		err = w.publishAndWatch(ctx, op, nil, a.atx.Tx, a.watch)
		if err != nil {
			return
		}
	}

	tx = a.atx.Tx
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

	vers, splitPkScript := splitTxAddr.PaymentScript()

	// Create the split transaction by using authorTx. This varies
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

	const op errors.Op = "vspSplit"
	a := &authorTx{
		outputs:            splitOuts,
		account:            req.SourceAccount,
		changeAccount:      req.ChangeAccount,
		minconf:            req.MinConf,
		randomizeChangeIdx: false,
		txFee:              w.RelayFee(),
		dontSignTx:         req.DontSignTx,
		isTreasury:         false,
	}
	err = w.authorTx(ctx, op, a)
	if err != nil {
		return
	}
	err = w.recordAuthoredTx(ctx, op, a)
	if err != nil {
		return
	}
	if !req.DontSignTx {
		err = w.publishAndWatch(ctx, op, nil, a.atx.Tx, a.watch)
		if err != nil {
			return
		}
	}

	tx = a.atx.Tx
	return
}

var errVSPFeeRequiresUTXOSplit = errors.New("paying VSP fee requires UTXO split")

// purchaseTickets indicates to the wallet that a ticket should be purchased
// using all currently available funds.  The ticket address parameter in the
// request can be nil in which case the ticket address associated with the
// wallet instance will be used.  Also, when the spend limit in the request is
// greater than or equal to 0, tickets that cost more than that limit will
// return an error that not enough funds are available.
func (w *Wallet) purchaseTickets(ctx context.Context, op errors.Op,
	n NetworkBackend, req *PurchaseTicketsRequest) (*PurchaseTicketsResponse, error) {
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
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		_, tipHeight = w.txStore.MainChainTip(dbtx)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if req.Expiry <= tipHeight+1 && req.Expiry > 0 {
		return nil, errors.E(op, errors.Invalid, "expiry height must be above next block height")
	}

	stakeAddrFunc := func(op errors.Op, account, branch uint32) (stdaddr.StakeAddress, uint32, error) {
		const accountName = "" // not used, so can be faked.
		a, err := w.nextAddress(ctx, op, w.persistReturnedChild(ctx, nil), accountName,
			account, branch, WithGapPolicyIgnore())
		if err != nil {
			return nil, 0, err
		}
		var idx uint32
		if xpa, ok := a.(*xpubAddress); ok {
			idx = xpa.child
		}
		switch a := a.(type) {
		case stdaddr.StakeAddress:
			return a, idx, nil
		default:
			return nil, 0, errors.E(errors.Invalid, "account does "+
				"not return compatible stake addresses")
		}
	}

	if w.addressReuse && req.CSPPServer == "" {
		xpub := w.addressBuffers[udb.DefaultAccountNum].albExternal.branchXpub
		addr, err := deriveChildAddress(xpub, 0, w.chainParams)
		if err != nil {
			err = errors.E(op, err)
		}
		stakeAddr, ok := addr.(stdaddr.StakeAddress)
		if !ok {
			err = errors.E(op, errors.Invalid, "account does not return "+
				"compatible stake addresses")
		}
		stakeAddrFunc = func(errors.Op, uint32, uint32) (stdaddr.StakeAddress, uint32, error) {
			return stakeAddr, 0, err
		}
	}

	// Calculate the current ticket price.  If the DCP0001 deployment is not
	// active, fallback to querying the ticket price over RPC.
	ticketPrice, err := w.NextStakeDifficulty(ctx)
	if errors.Is(err, errors.Deployment) {
		ticketPrice, err = n.StakeDifficulty(ctx)
	}
	if err != nil {
		return nil, err
	}

	// Try to get the pool address from the request. If none exists
	// in the request, try to get the global pool address. Then do
	// the same for pool fees, but check sanity too.
	poolAddress := req.VSPAddress
	if poolAddress == nil {
		poolAddress = w.poolAddress
	}
	poolFees := req.VSPFees
	if poolFees == 0.0 {
		poolFees = w.poolFees
	}
	if poolAddress != nil && poolFees == 0.0 {
		return nil, errors.E(op, errors.Invalid, "stakepool fee percent unset")
	}

	var stakeSubmissionPkScriptSize int

	// The stake submission pkScript is tagged by an OP_SSTX.
	switch req.VotingAddress.(type) {
	case *stdaddr.AddressScriptHashV0:
		stakeSubmissionPkScriptSize = txsizes.P2SHPkScriptSize + 1
	case *stdaddr.AddressPubKeyHashEcdsaSecp256k1V0, PubKeyHashAddress, nil:
		stakeSubmissionPkScriptSize = txsizes.P2PKHPkScriptSize + 1
	default:
		return nil, errors.E(op, errors.Invalid,
			"ticket address must either be P2SH or P2PKH")
	}

	// Make sure that we have enough funds. Calculate different
	// ticket required amounts depending on whether or not a
	// pool output is needed. If the ticket fee increment is
	// unset in the request, use the global ticket fee increment.
	var neededPerTicket dcrutil.Amount
	var estSize int
	ticketRelayFee := w.RelayFee()

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

	ticketFee := txrules.FeeForSerializeSize(ticketRelayFee, estSize)
	neededPerTicket = ticketFee + ticketPrice

	// If we need to calculate the amount for a pool fee percentage,
	// do so now.
	var vspFee dcrutil.Amount
	if poolAddress != nil {
		// poolAddress is only used with the legacy stakepool
		const dcp0010Active = false
		vspFee = txrules.StakePoolTicketFee(ticketPrice, ticketFee,
			tipHeight, poolFees, w.ChainParams(), dcp0010Active)
	}

	// After tickets are created and published, watch for future
	// relevant transactions
	var watchOutPoints []wire.OutPoint
	defer func() {
		// Cancellation of the request context should not prevent the
		// watching of addresses and outpoints that need to be watched.
		// A better solution would be to watch for the data first,
		// before publishing transactions.
		ctx := context.TODO()
		_, err := w.watchHDAddrs(ctx, false, n)
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

	var vspFeeCredits, ticketCredits [][]Input
	unlockCredits := true
	total := func(ins []Input) (v int64) {
		for _, in := range ins {
			v += in.PrevOut.Value
		}
		return
	}
	if req.VSPFeePaymentProcess != nil {
		if req.VSPFeeProcess == nil {
			return nil, errors.E(op, errors.Bug, "VSPFeeProcess "+
				"may not be nil if VSPServerProcess is non-nil")
		}
		feePrice, err := req.VSPFeeProcess(ctx)
		if err != nil {
			return nil, err
		}
		// In SPV mode, DCP0010 is assumed to have activated.  This
		// results in a larger fee calculation for the purposes of UTXO
		// selection.  In RPC mode the actual activation can be
		// determined.
		dcp0010Active := true
		switch n := n.(type) {
		case *dcrd.RPC:
			dcp0010Active, err = deployments.DCP0010Active(ctx,
				tipHeight, w.chainParams, n)
			if err != nil {
				return nil, err
			}
		}
		fee := txrules.StakePoolTicketFee(ticketPrice, ticketFee,
			tipHeight, feePrice, w.chainParams,
			dcp0010Active)

		// Reserve outputs for number of buys.
		vspFeeCredits = make([][]Input, 0, req.Count)
		ticketCredits = make([][]Input, 0, req.Count)
		defer func() {
			if unlockCredits {
				for _, credit := range vspFeeCredits {
					for _, c := range credit {
						log.Debugf("unlocked unneeded credit for vsp fee tx: %v",
							c.OutPoint.String())
						w.UnlockOutpoint(&c.OutPoint.Hash, c.OutPoint.Index)
					}
				}
			}
		}()
		if req.extraSplitOutput != nil {
			vspFeeCredits = make([][]Input, 1)
			vspFeeCredits[0] = []Input{*req.extraSplitOutput}
			op := &req.extraSplitOutput.OutPoint
			w.LockOutpoint(&op.Hash, op.Index)
		}
		var lowBalance bool
		for i := 0; i < req.Count; i++ {
			if req.extraSplitOutput == nil {
				credits, err := w.ReserveOutputsForAmount(ctx,
					req.SourceAccount, fee, req.MinConf)

				if errors.Is(err, errors.InsufficientBalance) {
					lowBalance = true
					break
				}
				if err != nil {
					log.Errorf("ReserveOutputsForAmount failed: %v", err)
					return nil, err
				}
				vspFeeCredits = append(vspFeeCredits, credits)
			}

			credits, err := w.ReserveOutputsForAmount(ctx, req.SourceAccount,
				ticketPrice, req.MinConf)
			if errors.Is(err, errors.InsufficientBalance) {
				lowBalance = true
				credits, _ = w.reserveOutputs(ctx, req.SourceAccount,
					req.MinConf)
				if len(credits) != 0 {
					ticketCredits = append(ticketCredits, credits)
				}
				break
			}
			if err != nil {
				log.Errorf("ReserveOutputsForAmount failed: %v", err)
				return nil, err
			}
			ticketCredits = append(ticketCredits, credits)
		}
		for _, credits := range ticketCredits {
			for _, c := range credits {
				log.Debugf("unlocked credit for ticket tx: %v",
					c.OutPoint.String())
				w.UnlockOutpoint(&c.OutPoint.Hash, c.OutPoint.Index)
			}
		}
		if lowBalance {
			// When there is UTXO contention between reserved fee
			// UTXOs and the tickets that can be purchased, UTXOs
			// which were selected for paying VSP fees are instead
			// allocated towards purchasing tickets.  We sort the
			// UTXOs picked for fees and tickets by decreasing
			// amounts and incrementally reserve them for ticket
			// purchases while reducing the total number of fees
			// (and therefore tickets) that will be purchased.  The
			// final UTXOs chosen for ticket purchases must be
			// unlocked for UTXO selection to work, while all inputs
			// for fee payments must be locked.
			credits := vspFeeCredits[:len(vspFeeCredits):len(vspFeeCredits)]
			credits = append(credits, ticketCredits...)
			sort.Slice(credits, func(i, j int) bool {
				return total(credits[i]) > total(credits[j])
			})
			if len(credits) == 0 {
				return nil, errors.E(errors.InsufficientBalance)
			}
			if req.Count > len(credits)-1 {
				req.Count = len(credits) - 1
			}
			var freedBalance int64
			extraSplit := true
			for req.Count > 1 {
				for _, c := range credits[0] {
					freedBalance += c.PrevOut.Value
					w.UnlockOutpoint(&c.OutPoint.Hash, c.OutPoint.Index)
				}
				credits = credits[1:]
				// XXX this is a bad estimate because it doesn't
				// consider the transaction fees
				if freedBalance > int64(ticketPrice)*int64(req.Count) {
					extraSplit = false
					break
				}
				req.Count--
			}
			vspFeeCredits = credits
			var remaining int64
			for _, c := range vspFeeCredits {
				remaining += total(c)
				for i := range c {
					w.LockOutpoint(&c[i].OutPoint.Hash, c[i].OutPoint.Index)
				}
			}

			if req.Count < 2 && extraSplit {
				// XXX still a bad estimate
				if int64(ticketPrice) > freedBalance+remaining {
					return nil, errors.E(errors.InsufficientBalance)
				}
				// A new transaction may need to be created to
				// split a single UTXO into two: one to pay the
				// VSP fee, and a second to fund the ticket
				// purchase.  This error condition is left to
				// the caller to detect and perform.
				return nil, errVSPFeeRequiresUTXOSplit
			}
		}
		log.Infof("Reserved credits for %d tickets: total fee: %v", req.Count, fee)
		for _, credit := range vspFeeCredits {
			for _, c := range credit {
				log.Debugf("%s reserved for vsp fee transaction", c.OutPoint.String())
			}
		}
	}

	purchaseTicketsResponse := &PurchaseTicketsResponse{}
	var splitTx *wire.MsgTx
	var splitOutputIndexes []int
	for {
		switch {
		case req.CSPPServer != "":
			splitTx, splitOutputIndexes, err = w.mixedSplit(ctx, req, neededPerTicket)
		case req.VSPAddress != nil:
			splitTx, splitOutputIndexes, err = w.vspSplit(ctx, req, neededPerTicket, vspFee)
		default:
			splitTx, splitOutputIndexes, err = w.individualSplit(ctx, req, neededPerTicket)
		}
		if errors.Is(err, errors.InsufficientBalance) && req.Count > 1 {
			req.Count--
			if len(vspFeeCredits) > 0 {
				for _, in := range vspFeeCredits[0] {
					w.UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
				}
				vspFeeCredits = vspFeeCredits[1:]
			}
			continue
		}
		if err != nil {
			return nil, errors.E(op, err)
		}
		break
	}
	purchaseTicketsResponse.SplitTx = splitTx

	// Process and publish split tx.
	if !req.DontSignTx {
		rec, err := udb.NewTxRecordFromMsgTx(splitTx, time.Now())
		if err != nil {
			return nil, err
		}
		w.lockedOutpointMu.Lock()
		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			watch, err := w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
			watchOutPoints = append(watchOutPoints, watch...)
			return err
		})
		w.lockedOutpointMu.Unlock()
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
	}

	// Calculate trickle times for published mixed tickets.
	// Random times between 20s to 1m from now are chosen for each ticket,
	// and tickets will not be published until their trickle time is reached.
	var trickleTickets []time.Time
	if req.CSPPServer != "" {
		now := time.Now()
		trickleTickets = make([]time.Time, 0, len(splitOutputIndexes))
		for range splitOutputIndexes {
			delay, err := uniformprng.Int63n(rand.Reader,
				int64(40*time.Second))
			if err != nil {
				return nil, err
			}
			t := now.Add(time.Duration(delay) + 20*time.Second)
			trickleTickets = append(trickleTickets, t)
		}
		sort.Slice(trickleTickets, func(i, j int) bool {
			t1 := trickleTickets[i]
			t2 := trickleTickets[j]
			return t1.Before(t2)
		})
	}

	// Create each ticket.
	ticketHashes := make([]*chainhash.Hash, 0, req.Count)
	tickets := make([]*wire.MsgTx, 0, req.Count)
	outpoint := wire.OutPoint{Hash: splitTx.TxHash()}
	for _, index := range splitOutputIndexes {
		// Generate the extended outpoints that we need to use for ticket
		// inputs. There are two inputs for pool tickets corresponding to the
		// fees and the user subsidy, while user-handled tickets have only one
		// input.
		var eopPool, eop *Input
		if poolAddress == nil {
			op := outpoint
			op.Index = uint32(index)
			log.Infof("Split output is %v", &op)
			txOut := splitTx.TxOut[index]
			eop = &Input{
				OutPoint: op,
				PrevOut:  *txOut,
			}
		} else {
			vspOutPoint := outpoint
			vspOutPoint.Index = uint32(index)
			vspOutput := splitTx.TxOut[vspOutPoint.Index]
			eopPool = &Input{
				OutPoint: vspOutPoint,
				PrevOut:  *vspOutput,
			}
			myOutPoint := outpoint
			myOutPoint.Index = uint32(index + 1)
			myOutput := splitTx.TxOut[myOutPoint.Index]
			eop = &Input{
				OutPoint: myOutPoint,
				PrevOut:  *myOutput,
			}
		}

		// If the user hasn't specified a voting address
		// to delegate voting to, just use an address from
		// this wallet. Check the passed address from the
		// request first, then check the ticket address
		// stored from the configuation. Finally, generate
		// an address.
		var addrVote stdaddr.StakeAddress

		// If req.UseVotingAccount is true, always take the submission
		// script's address from the voting account. This is intended
		// to be used with a special account type. The signing address
		// for the same index is saved to the database. That address is
		// later used to sign messages sent to a vspd related to this
		// ticket.
		if req.UseVotingAccount {
			var idx uint32
			addrVote, idx, err = stakeAddrFunc(op, req.VotingAccount, 1)
			if err != nil {
				return nil, err
			}
			_, err := w.signingAddressAtIdx(ctx, op, w.persistReturnedChild(ctx, nil),
				req.VotingAccount, idx)
			if err != nil {
				return nil, err
			}
		} else {
			addrVote = req.VotingAddress
			if addrVote == nil && req.CSPPServer == "" {
				addrVote = w.ticketAddress
			}
			if addrVote == nil {
				addrVote, _, err = stakeAddrFunc(op, req.VotingAccount, 1)
				if err != nil {
					return nil, err
				}
			}
		}
		subsidyAccount := req.SourceAccount
		var branch uint32 = 1
		if req.CSPPServer != "" {
			subsidyAccount = req.MixedAccount
			branch = req.MixedAccountBranch
		}
		addrSubsidy, _, err := stakeAddrFunc(op, subsidyAccount, branch)
		if err != nil {
			return nil, err
		}

		var ticket *wire.MsgTx
		w.lockedOutpointMu.Lock()
		err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			// Generate the ticket msgTx and sign it if DontSignTx is false.
			ticket, err = makeTicket(w.chainParams, eopPool, eop, addrVote,
				addrSubsidy, int64(ticketPrice), poolAddress)
			if err != nil {
				return err
			}
			// Set the expiry.
			ticket.Expiry = uint32(req.Expiry)

			ticketHash := ticket.TxHash()
			ticketHashes = append(ticketHashes, &ticketHash)
			tickets = append(tickets, ticket)

			purchaseTicketsResponse.Tickets = tickets
			purchaseTicketsResponse.TicketHashes = ticketHashes

			if req.DontSignTx {
				return nil
			}
			// Sign and publish tx if DontSignTx is false
			var forSigning []Input
			if eopPool != nil {
				forSigning = append(forSigning, *eopPool)
			}
			forSigning = append(forSigning, *eop)

			ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
			err = w.signP2PKHMsgTx(ticket, forSigning, ns)
			if err != nil {
				return err
			}
			err = validateMsgTx(op, ticket, creditScripts(forSigning))
			if err != nil {
				return err
			}

			err = w.checkHighFees(dcrutil.Amount(eop.PrevOut.Value), ticket)
			if err != nil {
				return err
			}

			rec, err := udb.NewTxRecordFromMsgTx(ticket, time.Now())
			if err != nil {
				return err
			}

			watch, err := w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
			watchOutPoints = append(watchOutPoints, watch...)
			if err != nil {
				return err
			}

			w.recentlyPublishedMu.Lock()
			w.recentlyPublished[rec.Hash] = struct{}{}
			w.recentlyPublishedMu.Unlock()

			return nil
		})
		w.lockedOutpointMu.Unlock()
		if err != nil {
			return purchaseTicketsResponse, errors.E(op, err)
		}
	}

	for i, ticket := range tickets {
		// Wait for trickle time if this was a mixed buy.
		if len(trickleTickets) > 0 {
			t := trickleTickets[0]
			trickleTickets = trickleTickets[1:]
			timer := time.NewTimer(time.Until(t))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return purchaseTicketsResponse, errors.E(op, ctx.Err())
			case <-timer.C:
			}
		}

		// Publish transaction
		err = n.PublishTransactions(ctx, ticket)
		if err != nil {
			return purchaseTicketsResponse, errors.E(op, err)
		}
		log.Infof("Published ticket purchase %v", ticket.TxHash())

		// Pay VSP fee when configured to do so.
		if req.VSPFeePaymentProcess == nil {
			continue
		}
		unlockCredits = false
		feeTx := wire.NewMsgTx()
		for j := range vspFeeCredits[i] {
			in := &vspFeeCredits[i][j]
			feeTx.AddTxIn(wire.NewTxIn(&in.OutPoint, in.PrevOut.Value, nil))
		}
		ticketHash := purchaseTicketsResponse.TicketHashes[i]
		err = req.VSPFeePaymentProcess(ctx, ticketHash, feeTx)
		if err != nil {
			// unlock outpoints in case of error
			for _, outpoint := range vspFeeCredits[i] {
				w.UnlockOutpoint(&outpoint.OutPoint.Hash,
					outpoint.OutPoint.Index)
			}
			continue
		}
		// watch for outpoints change.
		_, err = udb.NewTxRecordFromMsgTx(feeTx, time.Now())
		if err != nil {
			return nil, err
		}
	}

	return purchaseTicketsResponse, err
}

// ReserveOutputsForAmount returns locked spendable outpoints from the given
// account.  It is the responsibility of the caller to unlock the outpoints.
func (w *Wallet) ReserveOutputsForAmount(ctx context.Context, account uint32, amount dcrutil.Amount, minconf int32) ([]Input, error) {
	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	var outputs []Input
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		// Get current block's height
		_, tipHeight := w.txStore.MainChainTip(dbtx)

		var err error
		const minAmount = 0
		const maxResults = 0
		outputs, err = w.findEligibleOutputsAmount(dbtx, account, minconf, amount, tipHeight,
			minAmount, maxResults)
		if err != nil {
			return err
		}

		for _, output := range outputs {
			w.lockedOutpoints[outpoint{output.OutPoint.Hash, output.OutPoint.Index}] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return outputs, nil
}

func (w *Wallet) reserveOutputs(ctx context.Context, account uint32, minconf int32) ([]Input, error) {
	defer w.lockedOutpointMu.Unlock()
	w.lockedOutpointMu.Lock()

	var outputs []Input
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		// Get current block's height
		_, tipHeight := w.txStore.MainChainTip(dbtx)

		var err error
		outputs, err = w.findEligibleOutputs(dbtx, account, minconf, tipHeight)
		if err != nil {
			return err
		}

		for _, output := range outputs {
			w.lockedOutpoints[outpoint{output.OutPoint.Hash, output.OutPoint.Index}] = struct{}{}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return outputs, nil
}

// This can't be optimized to use the random selection because it must read all
// outputs.  Prefer to use findEligibleOutputsAmount with various filter options
// instead.
func (w *Wallet) findEligibleOutputs(dbtx walletdb.ReadTx, account uint32, minconf int32,
	currentHeight int32) ([]Input, error) {

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

	unspent, err := w.txStore.UnspentOutputs(dbtx)
	if err != nil {
		return nil, err
	}

	// TODO: Eventually all of these filters (except perhaps output locking)
	// should be handled by the call to UnspentOutputs (or similar).
	// Because one of these filters requires matching the output script to
	// the desired account, this change depends on making wtxmgr a waddrmgr
	// dependency and requesting unspent outputs for a single account.
	eligible := make([]Input, 0, len(unspent))
	for i := range unspent {
		output := unspent[i]

		// Locked unspent outputs are skipped.
		if _, locked := w.lockedOutpoints[outpoint{output.Hash, output.Index}]; locked {
			continue
		}

		// Only include this output if it meets the required number of
		// confirmations.  Coinbase transactions must have have reached
		// maturity before their outputs may be spent.
		if !confirmed(minconf, output.Height, currentHeight) {
			continue
		}

		// Filter out unspendable outputs, that is, remove those that
		// (at this time) are not P2PKH outputs.  Other inputs must be
		// manually included in transactions and sent (for example,
		// using createrawtransaction, signrawtransaction, and
		// sendrawtransaction).
		class, addrs := stdscript.ExtractAddrs(scriptVersionAssumed, output.PkScript, w.chainParams)
		if len(addrs) != 1 {
			continue
		}

		// Make sure everything we're trying to spend is actually mature.
		switch class {
		case stdscript.STStakeGenPubKeyHash, stdscript.STStakeGenScriptHash:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case stdscript.STStakeRevocationPubKeyHash, stdscript.STStakeRevocationScriptHash:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case stdscript.STTreasuryAdd, stdscript.STTreasuryGenPubKeyHash, stdscript.STTreasuryGenScriptHash:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case stdscript.STStakeChangePubKeyHash, stdscript.STStakeChangeScriptHash:
			if !ticketChangeMatured(w.chainParams, output.Height, currentHeight) {
				continue
			}
		case stdscript.STPubKeyHashEcdsaSecp256k1:
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
		addrAcct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
		if err != nil || addrAcct != account {
			continue
		}

		txOut := &wire.TxOut{
			Value:    int64(output.Amount),
			Version:  wire.DefaultPkScriptVersion, // XXX
			PkScript: output.PkScript,
		}
		eligible = append(eligible, Input{
			OutPoint: output.OutPoint,
			PrevOut:  *txOut,
		})
	}

	return eligible, nil
}

// findEligibleOutputsAmount uses wtxmgr to find a number of unspent outputs
// while doing maturity checks there.
func (w *Wallet) findEligibleOutputsAmount(dbtx walletdb.ReadTx, account uint32, minconf int32,
	amount dcrutil.Amount, currentHeight int32, minAmount dcrutil.Amount, maxResults int) ([]Input, error) {

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

	var eligible []Input
	var outTotal dcrutil.Amount
	seen := make(map[outpoint]struct{})
	skip := func(output *udb.Credit) bool {
		if _, ok := seen[outpoint{output.Hash, output.Index}]; ok {
			return true
		}

		// Locked unspent outputs are skipped.
		if _, locked := w.lockedOutpoints[outpoint{output.Hash, output.Index}]; locked {
			return true
		}

		// Only include this output if it meets the required number of
		// confirmations.  Coinbase transactions must have have reached
		// maturity before their outputs may be spent.
		if !confirmed(minconf, output.Height, currentHeight) {
			return true
		}

		// When a minumum amount is required, skip when it is less.
		if minAmount != 0 && output.Amount < minAmount {
			return true
		}

		// Filter out unspendable outputs, that is, remove those that
		// (at this time) are not P2PKH outputs.  Other inputs must be
		// manually included in transactions and sent (for example,
		// using createrawtransaction, signrawtransaction, and
		// sendrawtransaction).
		class, addrs := stdscript.ExtractAddrs(scriptVersionAssumed, output.PkScript, w.chainParams)
		if len(addrs) != 1 {
			return true
		}

		// Make sure everything we're trying to spend is actually mature.
		switch class {
		case stdscript.STStakeGenPubKeyHash, stdscript.STStakeGenScriptHash:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				return true
			}
		case stdscript.STStakeRevocationPubKeyHash, stdscript.STStakeRevocationScriptHash:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				return true
			}
		case stdscript.STTreasuryAdd, stdscript.STTreasuryGenPubKeyHash, stdscript.STTreasuryGenScriptHash:
			if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
				return true
			}
		case stdscript.STStakeChangePubKeyHash, stdscript.STStakeChangeScriptHash:
			if !ticketChangeMatured(w.chainParams, output.Height, currentHeight) {
				return true
			}
		case stdscript.STPubKeyHashEcdsaSecp256k1:
			if output.FromCoinBase {
				if !coinbaseMatured(w.chainParams, output.Height, currentHeight) {
					return true
				}
			}
		default:
			return true
		}

		// Only include the output if it is associated with the passed
		// account.  There should only be one address since this is a
		// P2PKH script.
		addrAcct, err := w.manager.AddrAccount(addrmgrNs, addrs[0])
		if err != nil || addrAcct != account {
			return true
		}

		return false
	}

	randTries := 0
	maxTries := 0
	if (amount != 0 || maxResults != 0) && minconf > 0 {
		numUnspent := w.txStore.UnspentOutputCount(dbtx)
		log.Debugf("Unspent bucket k/v count: %v", numUnspent)
		maxTries = numUnspent / 2
	}
	for ; randTries < maxTries; randTries++ {
		output, err := w.txStore.RandomUTXO(dbtx, minconf, currentHeight)
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		if skip(output) {
			continue
		}
		seen[outpoint{output.Hash, output.Index}] = struct{}{}

		txOut := &wire.TxOut{
			Value:    int64(output.Amount),
			Version:  wire.DefaultPkScriptVersion, // XXX
			PkScript: output.PkScript,
		}
		eligible = append(eligible, Input{
			OutPoint: output.OutPoint,
			PrevOut:  *txOut,
		})
		outTotal += output.Amount
		if amount != 0 && outTotal >= amount {
			return eligible, nil
		}
		if maxResults != 0 && len(eligible) == maxResults {
			return eligible, nil
		}
	}
	if randTries > 0 {
		log.Debugf("Abandoned random UTXO selection "+
			"attempts after %v tries", randTries)
	}

	eligible = eligible[:0]
	seen = nil
	outTotal = 0
	unspent, err := w.txStore.UnspentOutputs(dbtx)
	if err != nil {
		return nil, err
	}
	shuffle(len(unspent), func(i, j int) {
		unspent[i], unspent[j] = unspent[j], unspent[i]
	})

	for i := range unspent {
		output := unspent[i]
		if skip(output) {
			continue
		}

		txOut := &wire.TxOut{
			Value:    int64(output.Amount),
			Version:  wire.DefaultPkScriptVersion, // XXX
			PkScript: output.PkScript,
		}
		eligible = append(eligible, Input{
			OutPoint: output.OutPoint,
			PrevOut:  *txOut,
		})
		outTotal += output.Amount
		if amount != 0 && outTotal >= amount {
			return eligible, nil
		}
		if maxResults != 0 && len(eligible) == maxResults {
			return eligible, nil
		}
	}
	if amount != 0 && outTotal < amount {
		return nil, errors.InsufficientBalance
	}

	return eligible, nil
}

// signP2PKHMsgTx sets the SignatureScript for every item in msgtx.TxIn.
// It must be called every time a msgtx is changed.
// Only P2PKH outputs are supported at this point.
func (w *Wallet) signP2PKHMsgTx(msgtx *wire.MsgTx, prevOutputs []Input, addrmgrNs walletdb.ReadBucket) error {
	if len(prevOutputs) != len(msgtx.TxIn) {
		return errors.Errorf(
			"Number of prevOutputs (%d) does not match number of tx inputs (%d)",
			len(prevOutputs), len(msgtx.TxIn))
	}
	for i, output := range prevOutputs {
		_, addrs := stdscript.ExtractAddrs(output.PrevOut.Version, output.PrevOut.PkScript, w.chainParams)
		if len(addrs) != 1 {
			continue // not error? errors.E(errors.Bug, "previous output address is not P2PKH")
		}
		apkh, ok := addrs[0].(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0)
		if !ok {
			return errors.E(errors.Bug, "previous output address is not P2PKH")
		}

		privKey, done, err := w.manager.PrivateKey(addrmgrNs, apkh)
		if err != nil {
			return err
		}
		defer done()

		sigscript, err := sign.SignatureScript(msgtx, i, output.PrevOut.PkScript,
			txscript.SigHashAll, privKey.Serialize(), dcrec.STEcdsaSecp256k1, true)
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
	var getKey sign.KeyClosure = func(addr stdaddr.Address) ([]byte, dcrec.SignatureType, bool, error) {
		key, done, err := w.manager.PrivateKey(addrmgrNs, addr)
		if err != nil {
			return nil, 0, false, err
		}
		doneFuncs = append(doneFuncs, done)

		// secp256k1 pubkeys are always compressed in Decred
		return key.Serialize(), dcrec.STEcdsaSecp256k1, true, nil
	}
	var getScript sign.ScriptClosure = func(addr stdaddr.Address) ([]byte, error) {
		return w.manager.RedeemScript(addrmgrNs, addr)
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
	signedScript, err := sign.SignTxOutput(w.chainParams, tx, inputToSign,
		redeemTicketScript, txscript.SigHashAll, getKey, getScript,
		tx.TxIn[inputToSign].SignatureScript, true) // Yes treasury
	if err != nil {
		return errors.E(errors.Op("txscript.SignTxOutput"), errors.ScriptFailure, err)
	}
	tx.TxIn[inputToSign].SignatureScript = signedScript

	return nil
}

// signVote signs a vote transaction.  This modifies the input scripts pointed
// to by the vote transaction.
func (w *Wallet) signVote(addrmgrNs walletdb.ReadBucket, ticketPurchase, vote *wire.MsgTx) error {
	return w.signVoteOrRevocation(addrmgrNs, ticketPurchase, vote, true)
}

// newVoteScript generates a voting script from the passed VoteBits, for
// use in a vote.
func newVoteScript(voteBits stake.VoteBits) ([]byte, error) {
	b := make([]byte, 2+len(voteBits.ExtendedBits))
	binary.LittleEndian.PutUint16(b[0:2], voteBits.Bits)
	copy(b[2:], voteBits.ExtendedBits)
	return stdscript.ProvablyPruneableScriptV0(b)
}

// createUnsignedVote creates an unsigned vote transaction that votes using the
// ticket specified by a ticket purchase hash and transaction with the provided
// vote bits.  The block height and hash must be of the previous block the vote
// is voting on.
func createUnsignedVote(ticketHash *chainhash.Hash, ticketPurchase *wire.MsgTx,
	blockHeight int32, blockHash *chainhash.Hash, voteBits stake.VoteBits,
	subsidyCache *blockchain.SubsidyCache, params *chaincfg.Params,
	dcp0010Active, dcp0012Active bool) (*wire.MsgTx, error) {

	// Parse the ticket purchase transaction to determine the required output
	// destinations for vote rewards or revocations.
	ticketPayKinds, ticketHash160s, ticketValues, _, _, _ :=
		stake.TxSStxStakeOutputInfo(ticketPurchase)

	// Calculate the subsidy for votes at this height.
	ssv := blockchain.SSVOriginal
	switch {
	case dcp0012Active:
		ssv = blockchain.SSVDCP0012
	case dcp0010Active:
		ssv = blockchain.SSVDCP0010
	}
	subsidy := subsidyCache.CalcStakeVoteSubsidyV3(int64(blockHeight), ssv)

	// Calculate the output values from this vote using the subsidy.
	voteRewardValues := stake.CalculateRewards(ticketValues,
		ticketPurchase.TxOut[0].Value, subsidy)

	// Begin constructing the vote transaction.
	vote := wire.NewMsgTx()

	// Add stakebase input to the vote.
	stakebaseOutPoint := wire.NewOutPoint(&chainhash.Hash{}, ^uint32(0),
		wire.TxTreeRegular)
	stakebaseInput := wire.NewTxIn(stakebaseOutPoint, subsidy,
		params.StakeBaseSigScript)
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
	vote.AddTxOut(&wire.TxOut{
		Value:    0,
		Version:  wire.DefaultPkScriptVersion, // XXX
		PkScript: blockRefScript,
	})

	// The second output contains the votebits encode as a null data script.
	voteScript, err := newVoteScript(voteBits)
	if err != nil {
		return nil, err
	}
	vote.AddTxOut(&wire.TxOut{
		Value:    0,
		Version:  wire.DefaultPkScriptVersion, // XXX
		PkScript: voteScript,
	})

	// All remaining outputs pay to the output destinations and amounts tagged
	// by the ticket purchase.
	for i, hash160 := range ticketHash160s {
		var addr stdaddr.StakeAddress
		var err error
		if ticketPayKinds[i] { // P2SH
			addr, err = stdaddr.NewAddressScriptHashV0FromHash(hash160, params)
		} else { // P2PKH
			addr, err = stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(hash160, params)
		}
		if err != nil {
			return nil, err
		}
		vers, script := addr.PayVoteCommitmentScript()
		vote.AddTxOut(&wire.TxOut{
			Value:    voteRewardValues[i],
			Version:  vers,
			PkScript: script,
		})
	}

	return vote, nil
}
