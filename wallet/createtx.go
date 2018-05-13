// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"reflect"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/mempool"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/wallet/internal/txsizes"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
	"golang.org/x/crypto/ripemd160"
)

// --------------------------------------------------------------------------------
// Constants and simple functions

const (
	// All transactions have 4 bytes for version, 4 bytes of locktime,
	// 4 bytes of expiry, and 2 varints for the number of inputs and
	// outputs, and 1 varint for the witnesses.
	txOverheadEstimate = 4 + 4 + 4 + 1 + 1 + 1

	// A worst case signature script to redeem a P2PKH output for a
	// compressed pubkey has 73 bytes of the possible DER signature
	// (with no leading 0 bytes for R and S), 33 bytes of compressed serialized pubkey,
	// and data push opcodes for both, plus one byte for the hash type flag
	// appended to the end of the signature.
	sigScriptEstimate = 1 + 73 + 1 + 33 + 1

	// A best case tx input serialization cost is chainhash.HashSize, 4 bytes
	// of output index, 1 byte for tree, 4 bytes of sequence, 16 bytes for
	// fraud proof, 1 varint for the sigscript size, and the estimated
	// signature script size.
	txInEstimate = chainhash.HashSize + 4 + 1 + 4 + 16 + 1 + sigScriptEstimate

	// singleInputTicketSize is the typical size of a normal P2PKH ticket
	// in bytes when the ticket has one input, rounded up.
	singleInputTicketSize = 300

	// doubleInputTicketSize is the typical size of a normal P2PKH ticket
	// in bytes when the ticket has two inputs, rounded up.
	doubleInputTicketSize = 550

	// defaultTicketFeeLimits is the default byte string for the default
	// fee limits imposed on a ticket.
	defaultTicketFeeLimits = 0x5800

	// maxStandardTxSize is the maximum size allowed for transactions that
	// are considered standard and will therefore be relayed and considered
	// for mining.
	// TODO: import from dcrd.
	maxStandardTxSize = 100000

	// sstxTicketCommitmentEstimate =
	// - version + amount +
	// OP_SSTX OP_DUP OP_HASH160 OP_DATA_20 OP_EQUALVERIFY OP_CHECKSIG
	sstxTicketCommitmentEstimate = 2 + 8 + 1 + 1 + 1 + 1 + 20 + 1 + 1

	// sstxSubsidyCommitmentEstimate =
	// version + amount + OP_RETURN OP_DATA_30
	sstxSubsidyCommitmentEstimate = 2 + 8 + 2 + 30

	// sstxChangeOutputEstimate =
	// version + amount + OP_SSTXCHANGE OP_DUP OP_HASH160 OP_DATA_20
	//	OP_EQUALVERIFY OP_CHECKSIG
	sstxChangeOutputEstimate = 2 + 8 + 1 + 1 + 1 + 1 + 20 + 1 + 1
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
// Error Handling

// ErrUnsupportedTransactionType represents an error where a transaction
// cannot be signed as the API only supports spending P2PKH outputs.
var ErrUnsupportedTransactionType = errors.New("Only P2PKH transactions " +
	"are supported")

// ErrNonPositiveAmount represents an error where an amount is
// not positive (either negative, or zero).
var ErrNonPositiveAmount = errors.New("amount is not positive")

// ErrNegativeFee represents an error where a fee is erroneously
// negative.
var ErrNegativeFee = errors.New("fee is negative")

// ErrSStxNotEnoughFunds indicates that not enough funds were available in the
// wallet to purchase a ticket.
var ErrSStxNotEnoughFunds = errors.New("not enough to purchase sstx")

// ErrSStxPriceExceedsSpendLimit indicates that the current ticket price exceeds
// the specified spend maximum spend limit.
var ErrSStxPriceExceedsSpendLimit = errors.New("ticket price exceeds spend limit")

// ErrNoOutsToConsolidate indicates that there were no outputs available
// to compress.
var ErrNoOutsToConsolidate = errors.New("no outputs to consolidate")

// ErrBlockchainReorganizing indicates that the blockchain is currently
// reorganizing.
var ErrBlockchainReorganizing = errors.New("blockchain is currently " +
	"reorganizing")

// ErrTicketPriceNotSet indicates that the wallet was recently connected
// and that the ticket price has not yet been set.
var ErrTicketPriceNotSet = errors.New("ticket price not yet established")

// ErrSStxInputOverflow indicates that too many inputs were used to generate
// a ticket.
var ErrSStxInputOverflow = errors.New("too many inputs to purchase ticket with")

// ErrClientPurchaseTicket is the error returned when the daemon has
// disconnected from the
var ErrClientPurchaseTicket = errors.New("sendrawtransaction failed: the " + "client has been shutdown")

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

	// EstMaxTicketFeeAmount is the estimated max ticket fee to be used for size
	// calculation for eligible utxos for ticket purchasing.
	EstMaxTicketFeeAmount = 0.1 * 1e8
)

// NewUnsignedTransaction constructs an unsigned transaction using unspent
// account outputs.
//
// The changeSource parameter is optional and can be nil.  When nil, and if a
// change output should be added, an internal change address is created for the
// account.
func (w *Wallet) NewUnsignedTransaction(outputs []*wire.TxOut, relayFeePerKb dcrutil.Amount, account uint32, minConf int32,
	algo OutputSelectionAlgorithm, changeSource txauthor.ChangeSource) (*txauthor.AuthoredTx, error) {

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
				return apperrors.E{
					ErrorCode:   apperrors.ErrAccountNotFound,
					Description: "account not found",
				}
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
			// available and ignores any returned InputSourceErrors.
			inputSource = func(dcrutil.Amount) (dcrutil.Amount, []*wire.TxIn, [][]byte, error) {
				total, inputs, prevScripts, err := sourceImpl.SelectInputs(dcrutil.MaxAmount)
				switch err.(type) {
				case txauthor.InputSourceError:
					err = nil
				}
				return total, inputs, prevScripts, err
			}
		default:
			return fmt.Errorf("unrecognized output selection algorithm %d", algo)
		}

		if changeSource == nil {
			persist := w.deferPersistReturnedChild(&changeSourceUpdates)
			changeSource = w.changeSource(persist, account)
		}

		var err error
		authoredTx, err = txauthor.NewUnsignedTransaction(outputs, relayFeePerKb,
			inputSource, changeSource)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(changeSourceUpdates) != 0 {
		err = walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
			for _, up := range changeSourceUpdates {
				err := up(tx)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}
	return authoredTx, err
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
		return nil, dcrjson.ErrInternal
	}

	return rec, w.TxStore.InsertMemPoolTx(ns, rec)
}

func (w *Wallet) insertCreditsIntoTxMgr(tx walletdb.ReadWriteTx,
	msgTx *wire.MsgTx, rec *udb.TxRecord) error {

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
					return err
				}
				err = w.markUsedAddress(tx, ma)
				if err != nil {
					return err
				}
				log.Debugf("Marked address %v used", addr)
				continue
			}

			// Missing addresses are skipped.  Other errors should
			// be propagated.
			code := err.(apperrors.E).ErrorCode
			if code != apperrors.ErrAddressNotFound {
				return err
			}
		}
	}

	return nil
}

// insertMultisigOutIntoTxMgr inserts a multisignature output into the
// transaction store database.
func (w *Wallet) insertMultisigOutIntoTxMgr(ns walletdb.ReadWriteBucket, msgTx *wire.MsgTx,
	index uint32) error {
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
	if !txrules.PaysHighFees(totalInput, tx) {
		return nil
	}
	return apperrors.New(apperrors.ErrHighFees, "transaction pays exceedingly high fees")
}

// txToOutputs creates a transaction, selecting previous outputs from an account
// with no less than minconf confirmations, and creates a signed transaction
// that pays to each of the outputs.
func (w *Wallet) txToOutputs(outputs []*wire.TxOut, account uint32, minconf int32,
	randomizeChangeIdx bool) (*txauthor.AuthoredTx, error) {

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	return w.txToOutputsInternal(outputs, account, minconf, n,
		randomizeChangeIdx, w.RelayFee())
}

// txToOutputsSplitTx creates a input,output on each participants
// then each participant send to dcrtxmatcher server for create transaction
// dcrtxmatcher will send back the transaction for sign
func (w *Wallet) txToOutputsSplitTx(outputs []*wire.TxOut, account uint32, minconf int32,
	n NetworkBackend, randomizeChangeIdx bool, txFee dcrutil.Amount) (*txauthor.AuthoredTx, *[]func(walletdb.ReadWriteTx) error, error) {

	var atx *txauthor.AuthoredTx
	var changeSource txauthor.ChangeSource
	var changeSourceUpdates []func(walletdb.ReadWriteTx) error
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Create the unsigned transaction.
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
		inputSource := w.TxStore.MakeInputSource(txmgrNs, addrmgrNs, account,
			minconf, tipHeight)
		persist := w.deferPersistReturnedChild(&changeSourceUpdates)
		changeSource = w.changeSource(persist, account)

		fmt.Println("len changeSourceUpdates ", len(changeSourceUpdates))
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

		// dot not sign the transaction here
		//secrets := &secretSource{Manager: w.Manager, addrmgrNs: addrmgrNs}
		//err = atx.AddAllInputScripts(secrets)
		//for _, done := range secrets.doneFuncs {
		//	done()
		//}
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	// Ensure valid signatures were created.
	//err = validateMsgTx(atx.Tx, atx.PrevScripts)
	//if err != nil {
	//	fmt.Println("ERROR validateMsgTx ", err)
	//	//return nil, nil, err
	//}

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
			fmt.Printf("[changeSourceUpdates]- called \r\n")
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
		for _, txin := range tx.TxIn {
			fmt.Printf("[publishTx-after] - input prev outpoint splitTx hash :%s - index :%d - signature : %x\r\n",
				txin.PreviousOutPoint.Hash, txin.PreviousOutPoint.Index, txin.SignatureScript)

		}
		for _, txout := range tx.TxOut {
			fmt.Printf("[publishTx-after] - output splitTx amount :%d - version :%d - pkscript : %x\r\n",
				txout.Value, txout.Version, txout.PkScript)
		}

		return n.PublishTransaction(context.TODO(), tx)
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
func (w *Wallet) txToOutputsInternal(outputs []*wire.TxOut, account uint32, minconf int32,
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
		persist := w.deferPersistReturnedChild(&changeSourceUpdates)
		changeSource := w.changeSource(persist, account)
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
		return nil, err
	}

	// Ensure valid signatures were created.
	err = validateMsgTx(atx.Tx, atx.PrevScripts)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	rec, err := udb.NewTxRecordFromMsgTx(atx.Tx, time.Now())
	if err != nil {
		return nil, err
	}

	// Use a single DB update to store and publish the transaction.  If the
	// transaction is rejected, the update is rolled back.
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, up := range changeSourceUpdates {
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

		return n.PublishTransaction(context.TODO(), atx.Tx)
	})
	if err != nil {
		return nil, err
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
func (w *Wallet) txToMultisig(account uint32, amount dcrutil.Amount,
	pubkeys []*dcrutil.AddressSecpPubKey, nRequired int8,
	minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {

	var (
		ctx      *CreatedTx
		addr     dcrutil.Address
		msScript []byte
	)
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		ctx, addr, msScript, err = w.txToMultisigInternal(dbtx,
			account, amount, pubkeys, nRequired, minconf)
		return err
	})
	return ctx, addr, msScript, err
}

func (w *Wallet) txToMultisigInternal(dbtx walletdb.ReadWriteTx, account uint32,
	amount dcrutil.Amount, pubkeys []*dcrutil.AddressSecpPubKey, nRequired int8,
	minconf int32) (*CreatedTx, dcrutil.Address, []byte, error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	txToMultisigError :=
		func(err error) (*CreatedTx, dcrutil.Address, []byte, error) {
			return nil, nil, nil, err
		}

	n, err := w.NetworkBackend()
	if err != nil {
		return txToMultisigError(err)
	}

	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return txToMultisigError(ErrBlockchainReorganizing)
	}

	// Get current block's height and hash.
	_, topHeight := w.TxStore.MainChainTip(txmgrNs)

	// Add in some extra for fees. TODO In the future, make a better
	// fee estimator.
	var feeEstForTx dcrutil.Amount
	switch {
	case w.chainParams == &chaincfg.MainNetParams:
		feeEstForTx = 5e7
	case w.chainParams == &chaincfg.TestNet2Params:
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
		return txToMultisigError(err)
	}
	if eligible == nil {
		return txToMultisigError(
			fmt.Errorf("Not enough funds to send to multisig address"))
	}

	msgtx := wire.NewMsgTx()
	scriptSizers := []txsizes.ScriptSizer{}
	// Fill out inputs.
	var forSigning []udb.Credit
	totalInput := dcrutil.Amount(0)
	for _, e := range eligible {
		msgtx.AddTxIn(wire.NewTxIn(&e.OutPoint, nil))
		totalInput += e.Amount
		forSigning = append(forSigning, e)
		scriptSizers = append(scriptSizers, txsizes.P2SHScriptSize)
	}

	// Insert a multi-signature output, then insert this P2SH
	// hash160 into the address manager and the transaction
	// manager.
	msScript, err := txscript.MultiSigScript(pubkeys, int(nRequired))
	if err != nil {
		return txToMultisigError(err)
	}
	_, err = w.Manager.ImportScript(addrmgrNs, msScript)
	if err != nil {
		// We don't care if we've already used this address.
		if err.(apperrors.E).ErrorCode != apperrors.ErrDuplicateAddress {
			return txToMultisigError(err)
		}
	}
	err = w.TxStore.InsertTxScript(txmgrNs, msScript)
	if err != nil {
		return txToMultisigError(err)
	}
	scAddr, err := dcrutil.NewAddressScriptHash(msScript, w.chainParams)
	if err != nil {
		return txToMultisigError(err)
	}
	p2shScript, err := txscript.PayToAddrScript(scAddr)
	if err != nil {
		return txToMultisigError(err)
	}
	txout := wire.NewTxOut(int64(amount), p2shScript)
	msgtx.AddTxOut(txout)

	// Add change if we need it. The case in which
	// totalInput == amount+feeEst is skipped because
	// we don't need to add a change output in this
	// case.
	feeSize := txsizes.EstimateSerializeSize(scriptSizers, msgtx.TxOut, false)
	feeEst := txrules.FeeForSerializeSize(w.RelayFee(), feeSize)

	if totalInput < amount+feeEst {
		return txToMultisigError(fmt.Errorf("Not enough funds to send to " +
			"multisig address after accounting for fees"))
	}
	if totalInput > amount+feeEst {
		pkScript, _, err := w.changeSource(w.persistReturnedChild(dbtx), account)()
		if err != nil {
			return txToMultisigError(err)
		}
		change := totalInput - (amount + feeEst)
		msgtx.AddTxOut(wire.NewTxOut(int64(change), pkScript))
	}

	if err = signMsgTx(msgtx, forSigning, w.Manager, addrmgrNs,
		w.chainParams); err != nil {
		return txToMultisigError(err)
	}

	err = w.checkHighFees(totalInput, msgtx)
	if err != nil {
		return txToMultisigError(err)
	}

	err = n.PublishTransaction(context.TODO(), msgtx)
	if err != nil {
		return txToMultisigError(err)
	}

	// Request updates from dcrd for new transactions sent to this
	// script hash address.
	utilAddrs := make([]dcrutil.Address, 1)
	utilAddrs[0] = scAddr
	err = n.LoadTxFilter(context.TODO(), false, []dcrutil.Address{scAddr}, nil)
	if err != nil {
		return txToMultisigError(err)
	}

	err = w.insertMultisigOutIntoTxMgr(txmgrNs, msgtx, 0)
	if err != nil {
		return txToMultisigError(err)
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
func validateMsgTx(tx *wire.MsgTx, prevScripts [][]byte) error {
	for i, prevScript := range prevScripts {
		vm, err := txscript.NewEngine(prevScript, tx, i,
			sanityVerifyFlags, txscript.DefaultScriptVersion, nil)
		if err != nil {
			return fmt.Errorf("cannot create script engine: %s", err)
		}
		err = vm.Execute()
		if err != nil {
			prevOut := &tx.TxIn[i].PreviousOutPoint
			sigScript := tx.TxIn[i].SignatureScript
			return fmt.Errorf("script execution errored: %s "+
				"(spending outpoint %v pkscript %x with sigscript %x)",
				err, prevOut, prevScript, sigScript)
		}
	}
	return nil
}

func validateMsgTxCredits(tx *wire.MsgTx, prevCredits []udb.Credit) error {
	prevScripts := make([][]byte, 0, len(prevCredits))
	for _, c := range prevCredits {
		prevScripts = append(prevScripts, c.PkScript)
	}
	return validateMsgTx(tx, prevScripts)
}

// compressWallet compresses all the utxos in a wallet into a single change
// address. For use when it becomes dusty.
func (w *Wallet) compressWallet(maxNumIns int, account uint32, changeAddr dcrutil.Address) (*chainhash.Hash, error) {
	var hash *chainhash.Hash
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		hash, err = w.compressWalletInternal(dbtx, maxNumIns, account, changeAddr)
		return err
	})
	return hash, err
}

func (w *Wallet) compressWalletInternal(dbtx walletdb.ReadWriteTx, maxNumIns int, account uint32,
	changeAddr dcrutil.Address) (*chainhash.Hash, error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	n, err := w.NetworkBackend()
	if err != nil {
		return nil, err
	}

	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
	}

	// Get current block's height
	_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

	minconf := int32(1)
	eligible, err := w.findEligibleOutputs(dbtx, account, minconf, tipHeight)
	if err != nil {
		return nil, err
	}

	if len(eligible) == 0 {
		return nil, ErrNoOutsToConsolidate
	}

	// Check if output address is default, and generate a new adress if needed
	if changeAddr == nil {
		changeAddr, err = w.newChangeAddress(w.persistReturnedChild(dbtx), account)
		if err != nil {
			return nil, err
		}
	}
	pkScript, err := txscript.PayToAddrScript(changeAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot create txout script: %s", err)
	}
	msgtx := wire.NewMsgTx()
	msgtx.AddTxOut(wire.NewTxOut(0, pkScript))
	maximumTxSize := maxTxSize
	if w.chainParams.Net == wire.MainNet {
		maximumTxSize = maxStandardTxSize
	}

	// Add the txins using all the eligible outputs.
	totalAdded := dcrutil.Amount(0)
	scriptSizers := []txsizes.ScriptSizer{}
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
		msgtx.AddTxIn(wire.NewTxIn(&e.OutPoint, nil))
		totalAdded += e.Amount
		forSigning = append(forSigning, e)
		scriptSizers = append(scriptSizers, txsizes.P2PKHScriptSize)
		count++
	}

	// Get an initial fee estimate based on the number of selected inputs
	// and added outputs, with no change.
	szEst := txsizes.EstimateSerializeSize(scriptSizers, msgtx.TxOut, false)
	feeEst := txrules.FeeForSerializeSize(w.RelayFee(), szEst)

	msgtx.TxOut[0].Value = int64(totalAdded - feeEst)

	if err = signMsgTx(msgtx, forSigning, w.Manager, addrmgrNs,
		w.chainParams); err != nil {
		return nil, err
	}
	if err := validateMsgTxCredits(msgtx, forSigning); err != nil {
		return nil, err
	}

	err = w.checkHighFees(totalAdded, msgtx)
	if err != nil {
		return nil, err
	}

	err = n.PublishTransaction(context.TODO(), msgtx)
	if err != nil {
		return nil, err
	}

	// Insert the transaction and credits into the transaction manager.
	rec, err := w.insertIntoTxMgr(txmgrNs, msgtx)
	if err != nil {
		return nil, err
	}
	err = w.insertCreditsIntoTxMgr(dbtx, msgtx, rec)
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
func makeTicket(params *chaincfg.Params, inputPool *extendedOutPoint,
	input *extendedOutPoint, addrVote dcrutil.Address, addrSubsidy dcrutil.Address,
	ticketCost int64, addrPool dcrutil.Address) (*wire.MsgTx, error) {
	mtx := wire.NewMsgTx()

	if addrPool != nil && inputPool != nil {
		txIn := wire.NewTxIn(inputPool.op, []byte{})
		mtx.AddTxIn(txIn)
	}

	txIn := wire.NewTxIn(input.op, []byte{})
	mtx.AddTxIn(txIn)

	// Create a new script which pays to the provided address with an
	// SStx tagged output.
	pkScript, err := txscript.PayToSStx(addrVote)
	if err != nil {
		return nil, err
	}

	txOut := wire.NewTxOut(ticketCost, pkScript)
	txOut.Version = txscript.DefaultScriptVersion
	mtx.AddTxOut(txOut)

	// Obtain the commitment amounts.
	var amountsCommitted []int64
	userSubsidyNullIdx := 0
	if inputPool == nil {
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
	if inputPool != nil {
		pkScript, err = txscript.GenerateSStxAddrPush(addrPool,
			dcrutil.Amount(amountsCommitted[0]), limits)
		if err != nil {
			return nil, fmt.Errorf("cannot create pool txout script: %s", err)
		}
		txout := wire.NewTxOut(int64(0), pkScript)
		mtx.AddTxOut(txout)

		// Create a new script which pays to the provided address with an
		// SStx change tagged output.
		pkScript, err = txscript.PayToSStxChange(addrZeroed)
		if err != nil {
			return nil, err
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
		return nil, fmt.Errorf("cannot create user txout script: %s", err)
	}
	txout := wire.NewTxOut(int64(0), pkScript)
	mtx.AddTxOut(txout)

	// Create a new script which pays to the provided address with an
	// SStx change tagged output.
	pkScript, err = txscript.PayToSStxChange(addrZeroed)
	if err != nil {
		return nil, err
	}

	txOut = wire.NewTxOut(0, pkScript)
	txOut.Version = txscript.DefaultScriptVersion
	mtx.AddTxOut(txOut)

	// Make sure we generated a valid SStx.
	if err := stake.CheckSStx(mtx); err != nil {
		return nil, err
	}

	return mtx, nil
}

func (w *Wallet) purchaseTicketsPrereq(req purchaseTicketRequest) (NetworkBackend, dcrutil.Amount, error) {
	n, err := w.NetworkBackend()
	if err != nil {
		return nil, dcrutil.Amount(0), err
	}

	// Ensure that the minuimum number of confirmations is greater than -1
	if req.minConf < 0 {
		return n, dcrutil.Amount(0), fmt.Errorf("Required number of confirmations should be greater than -1")
	}

	ticketPrice, err := n.StakeDifficulty(context.TODO())
	if err != nil {
		return n, dcrutil.Amount(0), err
	}

	return n, ticketPrice, nil
}

func (w *Wallet) purchaseTicketsSimple(req purchaseTicketRequest) ([]*chainhash.Hash, error) {
	n, ticketPrice, err := w.purchaseTicketsPrereq(req)
	if err != nil {
		return nil, err
	}

	tipHeight, err := w.sanityCheckExpiry(req.expiry)
	if err != nil {
		return nil, err
	}

	addrFunc := w.fetchAddressFunc()
	votingAddress, subsidyAddress, err := w.fetchAddresses(req.ticketAddr, req.account, addrFunc)

	neededPerTicket := req.minBalance + ticketPrice + EstMaxTicketFeeAmount

	ticket := wire.NewMsgTx()
	ticketHashes := make([]*chainhash.Hash, 0, req.numTickets)
	for i := 0; i < req.numTickets; i++ {
		eligible, err := w.findEligibleOutputCredits(req.account, req.minConf, neededPerTicket, tipHeight)
		if err != nil {
			return ticketHashes, err
		}

		// If eligible utxos is equal to or greater than req.splitTx
		// force a split tx
		numEligible := len(eligible)
		if numEligible >= int(req.splitTx) {
			log.Debug("Found %v eligible inputs for sstx, too many. Will use a split transaction instead", numEligible)
			tHash, err := w.forceSplit(req, 1)
			if err != nil {
				return ticketHashes, err
			}

			ticketHashes = append(ticketHashes, tHash)
			continue
		}

		log.Debugf("Found %v eligible inputs for sstx, no split transaction needed", numEligible)
		feeEst, err := w.estimateFee(eligible, votingAddress, ticketPrice)
		if err != nil {
			return ticketHashes, nil
		}

		forSigning, err := w.buildTicketTx(ticket, ticketPrice, votingAddress, subsidyAddress, eligible, req.minConf, feeEst)
		if err != nil {
			return ticketHashes, nil
		}

		if !stake.IsSStx(ticket) {
			return ticketHashes, fmt.Errorf("Invalid sstx")
		}

		if req.expiry > 0 {
			ticket.Expiry = uint32(req.expiry)
		}

		err = w.signAndValidateTicket(ticket, forSigning, nil)
		if err != nil {
			return ticketHashes, err
		}

		//		if req.dcrTxClient.Config().Enable {
		//			tx, err := req.dcrTxClient.JoinTransaction(ticket, votingAddress, ticketPrice)
		//			if err != nil {
		//				return ticketHashes, err
		//			}

		//			err = w.processTxRecordAndPublish(tx, n)
		//			if err != nil {
		//				return ticketHashes, err
		//			}

		//			ticketHash := tx.TxHash()
		//			ticketHashes = append(ticketHashes, &ticketHash)
		//			log.Infof("Successfully purchased and sent shared ticket %v", ticketHash)
		//			continue
		//		}

		err = w.processTxRecordAndPublish(ticket, n)
		if err != nil {
			return ticketHashes, err
		}

		ticketHash := ticket.TxHash()
		ticketHashes = append(ticketHashes, &ticketHash)
		log.Infof("Successfully sent SStx purchase transaction %v", ticketHash)
		// Re-initialize the wire message for the next tx inputs/outputs
		ticket = wire.NewMsgTx()

	}

	return ticketHashes, nil
}

func (w *Wallet) purchaseTicketsSplit(req purchaseTicketRequest, numTickets int) ([]*chainhash.Hash, error) {
	n, ticketPrice, err := w.purchaseTicketsPrereq(req)
	if err != nil {
		return nil, err
	}

	tipHeight, err := w.sanityCheckExpiry(req.expiry)
	if err != nil {
		return nil, err
	}

	addrFunc := w.fetchAddressFunc()

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
		return nil, fmt.Errorf("pool address given, but pool fees not set")
	}

	// Make sure that we have enough funds. Calculate different
	// ticket required amounts depending on whether or not a
	// pool output is needed. If the ticket fee increment is
	// unset in the request, use the global ticket fee increment.
	var neededPerTicket, ticketFee dcrutil.Amount
	ticketFeeIncrement := req.ticketFee
	if ticketFeeIncrement == 0 {
		ticketFeeIncrement = w.TicketFeeIncrement()
	}
	if poolAddress == nil {
		ticketFee = (ticketFeeIncrement * singleInputTicketSize) /
			1000
		neededPerTicket = ticketFee + ticketPrice
	} else {
		ticketFee = (ticketFeeIncrement * doubleInputTicketSize) /
			1000
		neededPerTicket = ticketFee + ticketPrice
	}

	// If we need to calculate the amount for a pool fee percentage,
	// do so now.
	var poolFeeAmt dcrutil.Amount
	if poolAddress != nil {
		poolFeeAmt = txrules.StakePoolTicketFee(ticketPrice, ticketFee,
			tipHeight, poolFees, w.ChainParams())
		if poolFeeAmt >= ticketPrice {
			return nil, fmt.Errorf("pool fee amt of %v >= than current "+
				"ticket price of %v", poolFeeAmt, ticketPrice)
		}
	}

	// Make sure this doesn't over spend based on the balance to
	// maintain. This component of the API is inaccessible to the
	// end user through the legacy RPC, so it should only ever be
	// set by internal calls e.g. automatic ticket purchase.
	if req.minBalance > 0 {
		bal, err := w.CalculateAccountBalance(req.account, req.minConf)
		if err != nil {
			return nil, err
		}

		estimatedFundsUsed := neededPerTicket * dcrutil.Amount(req.numTickets)
		if req.minBalance+estimatedFundsUsed > bal.Spendable {
			notEnoughFundsStr := fmt.Sprintf("not enough funds; balance to "+
				"maintain is %v and estimated cost is %v (resulting in %v "+
				"funds needed) but wallet account %v only has %v",
				req.minBalance.ToCoin(), estimatedFundsUsed.ToCoin(),
				req.minBalance.ToCoin()+estimatedFundsUsed.ToCoin(),
				req.account, bal.Spendable.ToCoin())
			log.Debugf("%s", notEnoughFundsStr)
			return nil, txauthor.InsufficientFundsError{}
		}
	}

	// Fetch the single use split address to break tickets into, to
	// immediately be consumed as tickets.
	//
	// This opens a write transaction.
	splitTxAddr, err := w.NewInternalAddress(req.account, WithGapPolicyWrap())
	if err != nil {
		return nil, err
	}

	//check dcrtxclient option to ensure current code also works with dcrtxclient enable option is disable in config file
	if req.dcrTxClient.Config().Enable {
		var splitOuts []*wire.TxOut
		pkScript, err := txscript.PayToAddrScript(splitTxAddr)
		if err != nil {
			return nil, fmt.Errorf("cannot create txout script: %s", err)
		}
		for i := 0; i < numTickets; i++ {
			splitOuts = append(splitOuts, wire.NewTxOut(int64(neededPerTicket), pkScript))
		}

		txFeeIncrement := req.txFee
		if txFeeIncrement == 0 {
			txFeeIncrement = w.RelayFee()
		}
		//need to do joint split transaction from this with txmatcher server
		splitTx, changeSourceFuncs, err := w.txToOutputsSplitTx(splitOuts, req.account, req.minConf,
			n, false, txFeeIncrement)
		if err != nil {
			return nil, fmt.Errorf("failed to send split transaction: %v", err)
		}

		for _, txin := range splitTx.Tx.TxIn {
			fmt.Printf("[Input] - input outpoint splitTx hash :%s - index :%d - signature : %x\r\n",
				txin.PreviousOutPoint.Hash, txin.PreviousOutPoint.Index, txin.SignatureScript)
		}
		for _, txout := range splitTx.Tx.TxOut {
			fmt.Printf("[Output] - output splitTx amount :%d - version :%d - pkscript : %x\r\n",
				txout.Value, txout.Version, txout.PkScript)

			if reflect.DeepEqual(txout.PkScript, pkScript) {
				fmt.Printf("[compare] - pkscript equals txoutpkscript : %x\r\n",
					pkScript)
			}
		}

		// Generate the tickets individually.
		ticketHashes := make([]*chainhash.Hash, 0, numTickets)

		tx, sesID, inputIds, outputIds, err := req.dcrTxClient.JoinSplitTx(splitTx.Tx, nil, ticketPrice)
		if err != nil {
			fmt.Println("req.dcrTxClient.JoinSplitTx error", err)
			return ticketHashes, err
		}

		for _, txin := range tx.TxIn {
			fmt.Printf("[JoinSplitTx-after] - input prev outpoint splitTx hash :%s - index :%d - signature : %x\r\n",
				txin.PreviousOutPoint.Hash, txin.PreviousOutPoint.Index, txin.SignatureScript)

		}
		for _, txout := range tx.TxOut {
			fmt.Printf("[JoinSplitTx-after] - output splitTx amount :%d - version :%d - pkscript : %x\r\n",
				txout.Value, txout.Version, txout.PkScript)
		}
		fmt.Println("resign data-tx inputIds", inputIds, outputIds)

		//re-signed the tx
		fmt.Println("Re-sign the transaction")
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
			fmt.Println("re-sign tx error", err)
			return ticketHashes, err
		}

		//submit signed input to server
		signedTx, publisher, err := req.dcrTxClient.SubmitSignedTx(tx, sesID)
		if err != nil {
			fmt.Println("re-sign tx error", err)
			return ticketHashes, err
		}
		for _, txin := range signedTx.TxIn {
			fmt.Printf("[SubmitSignedTx-after] - input prev outpoint splitTx hash :%s - index :%d - signature : %x\r\n",
				txin.PreviousOutPoint.Hash, txin.PreviousOutPoint.Index, txin.SignatureScript)
		}
		for _, txout := range signedTx.TxOut {
			fmt.Printf("[SubmitSignedTx-after] - output splitTx amount :%d - version :%d - pkscript : %x\r\n",
				txout.Value, txout.Version, txout.PkScript)
		}
		var publishedTx *wire.MsgTx
		if publisher {
			fmt.Println("SignedHash - publisher enter")
			err = w.publishTx(signedTx, changeSourceFuncs, w.networkBackend)
			if err != nil {
				fmt.Println("publishTx error", err)
				return ticketHashes, err
			}

			_, err := req.dcrTxClient.PublishResult(signedTx, sesID)
			if err != nil {
				fmt.Println("PublishResult error", err)
				return ticketHashes, err
			}
			publishedTx = signedTx
		} else {
			publishedTx, err = req.dcrTxClient.PublishResult(nil, sesID)
			if err != nil {
				fmt.Println("PublishResult error", err)
				return ticketHashes, err
			}
		}

		for i := 0; i < numTickets; i++ {
			index := 0
			for k, txout := range publishedTx.TxOut {
				fmt.Println("Index check txout %x, pk : %x ", txout.PkScript, splitOuts[i].PkScript)
				if reflect.DeepEqual(txout.PkScript, pkScript) {
					index = k
					fmt.Println("Index get ", index)
				}
			}
			outputIndex := outputIds[i]
			fmt.Println("outputIndex ", outputIndex)

			var eop *extendedOutPoint
			txOut := publishedTx.TxOut[outputIndex]
			eop = &extendedOutPoint{
				op: &wire.OutPoint{
					Hash:  publishedTx.TxHash(),
					Index: uint32(outputIndex),
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
				//signMsgTx set signaturescript to txin
				return signMsgTx(ticket, forSigning, w.Manager, ns, w.chainParams)
			})
			if err != nil {
				return ticketHashes, err
			}

			err = w.processTxRecordAndPublish(ticket, n)
			if err != nil {
				return ticketHashes, err
			}

			ticketHash := ticket.TxHash()
			ticketHashes = append(ticketHashes, &ticketHash)
			log.Infof("Successfully purchased and sent shared ticket transaction")
		}
		return ticketHashes, nil
	} else {
		var splitOuts []*wire.TxOut
		pkScript, err := txscript.PayToAddrScript(splitTxAddr)
		if err != nil {
			return nil, fmt.Errorf("cannot create txout script: %s", err)
		}
		for i := 0; i < numTickets; i++ {

			splitOuts = append(splitOuts, wire.NewTxOut(int64(neededPerTicket), pkScript))

			// No pool used.
			//			if poolAddress == nil {
			//				pkScript, err := txscript.PayToAddrScript(splitTxAddr)
			//				if err != nil {
			//					return nil, fmt.Errorf("cannot create txout script: %s", err)
			//				}

			//				splitOuts = append(splitOuts,
			//					wire.NewTxOut(int64(neededPerTicket), pkScript))
			//			} else {
			//				// Stake pool used.
			//				userAmt := neededPerTicket - poolFeeAmt
			//				poolAmt := poolFeeAmt

			//				// Pool amount.
			//				pkScript, err := txscript.PayToAddrScript(splitTxAddr)
			//				if err != nil {
			//					return nil, fmt.Errorf("cannot create txout script: %s", err)
			//				}

			//				splitOuts = append(splitOuts, wire.NewTxOut(int64(poolAmt), pkScript))

			//				// User amount.
			//				pkScript, err = txscript.PayToAddrScript(splitTxAddr)
			//				if err != nil {
			//					return nil, fmt.Errorf("cannot create txout script: %s", err)
			//				}

			//				splitOuts = append(splitOuts, wire.NewTxOut(int64(userAmt), pkScript))
			//			}

		}

		txFeeIncrement := req.txFee
		if txFeeIncrement == 0 {
			txFeeIncrement = w.RelayFee()
		}
		splitTx, err := w.txToOutputsInternal(splitOuts, req.account, req.minConf,
			n, false, txFeeIncrement)
		if err != nil {
			return nil, fmt.Errorf("failed to send split transaction: %v", err)
		}

		// After tickets are created and published, watch for future addresses used
		// by the split tx and any published tickets.
		defer func() {
			err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
				return w.watchFutureAddresses(tx)
			})
			if err != nil {
				log.Errorf("Failed to watch for future addresses after ticket "+
					"purchases: %v", err)
			}
		}()

		// Generate the tickets individually.
		ticketHashes := make([]*chainhash.Hash, 0, numTickets)
		for i := 0; i < numTickets; i++ {
			// Generate the extended outpoints that we need to use for ticket
			// inputs. There are two inputs for pool tickets corresponding to the
			// fees and the user subsidy, while user-handled tickets have only one
			// input.
			var eopPool, eop *extendedOutPoint
			txOut := splitTx.Tx.TxOut[i]

			eop = &extendedOutPoint{
				op: &wire.OutPoint{
					Hash:  splitTx.Tx.TxHash(),
					Index: uint32(i),
					Tree:  wire.TxTreeRegular,
				},
				amt:      txOut.Value,
				pkScript: txOut.PkScript,
			}

			//			if poolAddress == nil {
			//				txOut := splitTx.Tx.TxOut[i]

			//				eop = &extendedOutPoint{
			//					op: &wire.OutPoint{
			//						Hash:  splitTx.Tx.TxHash(),
			//						Index: uint32(i),
			//						Tree:  wire.TxTreeRegular,
			//					},
			//					amt:      txOut.Value,
			//					pkScript: txOut.PkScript,
			//				}
			//			} else {
			//				poolIdx := i * 2
			//				poolTxOut := splitTx.Tx.TxOut[poolIdx]
			//				userIdx := i*2 + 1
			//				txOut := splitTx.Tx.TxOut[userIdx]

			//				eopPool = &extendedOutPoint{
			//					op: &wire.OutPoint{
			//						Hash:  splitTx.Tx.TxHash(),
			//						Index: uint32(poolIdx),
			//						Tree:  wire.TxTreeRegular,
			//					},
			//					amt:      poolTxOut.Value,
			//					pkScript: poolTxOut.PkScript,
			//				}
			//				eop = &extendedOutPoint{
			//					op: &wire.OutPoint{
			//						Hash:  splitTx.Tx.TxHash(),
			//						Index: uint32(userIdx),
			//						Tree:  wire.TxTreeRegular,
			//					},
			//					amt:      txOut.Value,
			//					pkScript: txOut.PkScript,
			//				}
			//			}

			votingAddress, subsidyAddress, err := w.fetchAddresses(req.ticketAddr, req.account, addrFunc)

			// Generate the ticket msgTx and sign it.
			ticket, err := makeTicket(w.chainParams, eopPool, eop, votingAddress,
				subsidyAddress, int64(ticketPrice), poolAddress)
			if err != nil {
				return ticketHashes, err
			}

			var forSigning []udb.Credit
			//			if eopPool != nil {
			//				eopPoolCredit := udb.Credit{
			//					OutPoint:     *eopPool.op,
			//					BlockMeta:    udb.BlockMeta{},
			//					Amount:       dcrutil.Amount(eopPool.amt),
			//					PkScript:     eopPool.pkScript,
			//					Received:     time.Now(),
			//					FromCoinBase: false,
			//				}
			//				forSigning = append(forSigning, eopPoolCredit)
			//			}
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
				//signMsgTx set signaturescript to txin
				return signMsgTx(ticket, forSigning, w.Manager, ns, w.chainParams)
			})
			if err != nil {
				return ticketHashes, err
			}
			err = w.processTxRecordAndPublish(ticket, n)
			if err != nil {
				return ticketHashes, err
			}

			ticketHash := ticket.TxHash()
			ticketHashes = append(ticketHashes, &ticketHash)
			log.Infof("Successfully sent SStx purchase transaction %v", ticketHash)
		}
		return ticketHashes, nil
	}
}

func (w *Wallet) purchaseTickets(req purchaseTicketRequest) ([]*chainhash.Hash, error) {
	if req.splitTx == 1 {
		return w.purchaseTicketsSplit(req, req.numTickets)
	}
	return w.purchaseTicketsSimple(req)
}

// processTxRecordAndPublish creates a transaction record, inserts the record
// in the walletdb and publishes the transaction over the decred network
func (w *Wallet) processTxRecordAndPublish(tx *wire.MsgTx, n NetworkBackend) error {
	// Create transaction record
	rec, err := udb.NewTxRecordFromMsgTx(tx, time.Now())
	if err != nil {
		fmt.Println("[processTxRecordAndPublish.NewTxRecordFromMsgTx] - error ", err)
		return err
	}

	// Open db to insert and publish the transaction
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		err := w.processTransactionRecord(dbtx, rec, nil, nil)
		if err != nil {
			fmt.Println("[processTxRecordAndPublish.processTxRecordAndPublish] - error ", err)
			return err
		}
		return n.PublishTransaction(context.TODO(), tx)
	})

	return err
}

// findEligibleOutputCredits abstracts the call to the findEligibleOutputsAmount func
// it checks for possible errors and handles accordingly
func (w *Wallet) findEligibleOutputCredits(account uint32, minConf int32, amountNeeded dcrutil.Amount, tipheight int32) ([]udb.Credit, error) {
	var eligible []udb.Credit
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		eligible, err = w.findEligibleOutputsAmount(dbtx, account, minConf, amountNeeded, tipheight)
		return err
	})
	if err != nil {
		return nil, err
	}

	if len(eligible) == 0 {
		return nil, ErrSStxNotEnoughFunds
	}
	if len(eligible) > stake.MaxInputsPerSStx {
		return nil, ErrSStxInputOverflow
	}

	return eligible, nil
}

// signAndValidateTicket signs the passed msgtx and performs validation checks
func (w *Wallet) signAndValidateTicket(ticket *wire.MsgTx, forSigning []udb.Credit, eop *extendedOutPoint) error {
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(waddrmgrNamespaceKey)
		return signMsgTx(ticket, forSigning, w.Manager, ns, w.chainParams)
	})
	if err != nil {
		return err
	}

	err = validateMsgTxCredits(ticket, forSigning)
	if err != nil {
		return err
	}

	if eop != nil {
		err = w.checkHighFees(dcrutil.Amount(eop.amt), ticket)
	}

	return err
}

func (w *Wallet) buildTicketTx(msgtx *wire.MsgTx, ticketPrice dcrutil.Amount, votingAddress dcrutil.Address, subsidyAddress dcrutil.Address,
	eligible []udb.Credit, minConf int32, feeEst dcrutil.Amount) ([]udb.Credit, error) {

	var forSigning []udb.Credit
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		forSigning, err = w.buildTicketTxInternal(dbtx, msgtx, ticketPrice, votingAddress, subsidyAddress,
			eligible, minConf, feeEst)

		return err
	})
	return forSigning, err
}

func (w *Wallet) buildTicketTxInternal(dbtx walletdb.ReadWriteTx, msgtx *wire.MsgTx, ticketPrice dcrutil.Amount, votingAddress dcrutil.Address, subsidyAddress dcrutil.Address,
	eligible []udb.Credit, minConf int32, feeEst dcrutil.Amount) ([]udb.Credit, error) {

	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
	}

	maximumTxSize := maxTxSize
	if w.chainParams.Net == wire.MainNet {
		maximumTxSize = maxStandardTxSize
	}

	pkScript, err := txscript.PayToSStx(votingAddress)
	if err != nil {
		return nil, fmt.Errorf("cannot create txout script: %s", err)
	}
	txout := wire.NewTxOut(int64(ticketPrice), pkScript)
	msgtx.AddTxOut(txout)

	totalAdded := dcrutil.Amount(0)
	var forSigning []udb.Credit
	for _, credit := range eligible {
		if msgtx.SerializeSize() > maximumTxSize {
			break
		}

		msgtx.AddTxIn(wire.NewTxIn(&credit.OutPoint, nil))
		totalAdded += credit.Amount
		forSigning = append(forSigning, credit)
	}

	if totalAdded < ticketPrice {
		return nil, ErrSStxNotEnoughFunds
	}

	limits := uint16(defaultTicketFeeLimits)

	// This is the total change that should be paid
	totalChange := totalAdded - ticketPrice - feeEst

	// total commitment
	totalCommitment := ticketPrice + feeEst

	for _, credit := range eligible {
		prop := float64(credit.Amount) / float64(totalAdded)

		// for each eligible credit
		// change and commitment should be in proportion to credit size

		changeWithThisCredit := totalChange.MulF64(prop)
		commitmentWithTisCredit := totalCommitment.MulF64(prop)

		pkScript, err := txscript.GenerateSStxAddrPush(subsidyAddress, commitmentWithTisCredit, limits)
		if err != nil {
			return nil, fmt.Errorf("cannot create txout script: %s", err)
		}
		txout := wire.NewTxOut(int64(0), pkScript)
		msgtx.AddTxOut(txout)

		// fetch change address
		changeAddr, err := w.newChangeAddress(w.persistReturnedChild(dbtx), udb.DefaultAccountNum)
		if err != nil {
			return nil, err
		}

		// Add ne changeSStx Output. This randomizes the index of output
		pkScript, err = txscript.PayToSStxChange(changeAddr)
		if err != nil {
			return nil, fmt.Errorf("cannot create txout script: %s", err)
		}

		msgtx.AddTxOut(wire.NewTxOut(int64(changeWithThisCredit), pkScript))
	}
	return forSigning, nil
}

func (w *Wallet) estimateFee(eligible []udb.Credit, votingAddress dcrutil.Address,
	ticketPrice dcrutil.Amount) (dcrutil.Amount, error) {

	msgtx := wire.NewMsgTx()

	scriptSizers := []txsizes.ScriptSizer{}

	for range eligible {
		scriptSizers = append(scriptSizers, txsizes.P2SHScriptSize)
	}

	pkScript, err := txscript.PayToSStx(votingAddress)
	if err != nil {
		return dcrutil.Amount(0), fmt.Errorf("cannot create txout script: %s", err)
	}
	txout := wire.NewTxOut(int64(ticketPrice), pkScript)
	msgtx.AddTxOut(txout)

	limits := uint16(defaultTicketFeeLimits)
	for _, credit := range eligible {
		pkScript, err := txscript.GenerateSStxAddrPush(votingAddress, credit.Amount, limits)
		if err != nil {
			return dcrutil.Amount(0), fmt.Errorf("cannot create txout script: %s", err)
		}
		txout := wire.NewTxOut(int64(0), pkScript)
		msgtx.AddTxOut(txout)

		// Add ne changeSStx Output. This randomizes the index of output
		pkScript, err = txscript.PayToSStxChange(votingAddress)
		if err != nil {
			return dcrutil.Amount(0), fmt.Errorf("cannot create txout script: %s", err)
		}
		msgtx.AddTxOut(wire.NewTxOut(int64(0), pkScript))
	}

	szEst := txsizes.EstimateSerializeSize(scriptSizers, msgtx.TxOut, true)
	feeEst := txrules.FeeForSerializeSize(w.RelayFee(), szEst)

	return feeEst, nil

}

func (w *Wallet) forceSplit(req purchaseTicketRequest, numTickets int) (*chainhash.Hash, error) {
	ticketHash, err := w.purchaseTicketsSplit(req, numTickets)
	if err == nil && len(ticketHash) == 1 {
		return ticketHash[0], nil
	}
	return nil, err
}

// txToSStx creates a raw SStx transaction sending the amounts for each
// address/amount pair and fee to each address and the miner.  minconf
// specifies the minimum number of confirmations required before an
// unspent output is eligible for spending. Leftover input funds not sent
// to addr or as a fee for the miner are sent to a newly generated
// address. If change is needed to return funds back to an owned
// address, changeUtxo will point to a unconfirmed (height = -1, zeroed
// enough eligible unspent outputs to create the transaction.
// block hash) Utxo.  ErrInsufficientFunds is returned if there are not
func (w *Wallet) txToSStx(pair map[string]dcrutil.Amount,
	inputCredits []udb.Credit, inputs []dcrjson.SStxInput,
	payouts []dcrjson.SStxCommitOut, account uint32, minconf int32) (*CreatedTx, error) {

	var tx *CreatedTx
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		tx, err = w.txToSStxInternal(dbtx, pair, inputCredits, inputs,
			payouts)
		return err
	})
	return tx, err
}

func (w *Wallet) txToSStxInternal(dbtx walletdb.ReadWriteTx, pair map[string]dcrutil.Amount,
	inputCredits []udb.Credit, inputs []dcrjson.SStxInput,
	payouts []dcrjson.SStxCommitOut) (tx *CreatedTx, err error) {

	// Quit if the blockchain is reorganizing.
	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
	}

	if len(inputs) != len(payouts) {
		return nil, fmt.Errorf("input and payout must have the same length")
	}

	// create new empty msgTx
	msgtx := wire.NewMsgTx()
	var minAmount dcrutil.Amount
	// create tx output from pair addr given
	for addrStr, amt := range pair {
		if amt <= 0 {
			return nil, ErrNonPositiveAmount
		}
		minAmount += amt
		addr, err := dcrutil.DecodeAddress(addrStr)
		if err != nil {
			return nil, fmt.Errorf("cannot decode address: %s", err)
		}

		// Add output to spend amt to addr.
		pkScript, err := txscript.PayToSStx(addr)
		if err != nil {
			return nil, fmt.Errorf("cannot create txout script: %s", err)
		}
		txout := wire.NewTxOut(int64(amt), pkScript)

		msgtx.AddTxOut(txout)
	}
	// totalAdded is the total amount from utxos
	totalAdded := dcrutil.Amount(0)

	// Range over all eligible utxos to add all to sstx inputs
	for _, input := range inputs {
		txHash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return nil, dcrjson.ErrDecodeHexString
		}

		if !(input.Tree == wire.TxTreeRegular ||
			input.Tree == wire.TxTreeStake) {
			return nil, dcrjson.Error{
				Code:    dcrjson.ErrInvalidParameter.Code,
				Message: "Invalid parameter, tx tree must be regular or stake",
			}
		}

		prevOut := wire.NewOutPoint(txHash, input.Vout, input.Tree)
		msgtx.AddTxIn(wire.NewTxIn(prevOut, nil))
		totalAdded += dcrutil.Amount(input.Amt)
	}

	if totalAdded < minAmount {
		return nil, ErrSStxNotEnoughFunds
	}

	var changeAddr dcrutil.Address

	for i := range inputs {
		// Add the OP_RETURN commitment amounts and payout to
		// addresses.
		var addr dcrutil.Address

		if payouts[i].Addr == "" {
			var err error
			addr, err = w.newChangeAddress(w.persistReturnedChild(dbtx),
				udb.DefaultAccountNum)
			if err != nil {
				return nil, err
			}
		} else {
			addr, err = dcrutil.DecodeAddress(payouts[i].Addr)
			if err != nil {
				return nil, fmt.Errorf("cannot decode address: %s", err)
			}

			// Ensure the address is one of the supported types and that
			// the network encoded with the address matches the network the
			// server is currently on.
			switch addr.(type) {
			case *dcrutil.AddressPubKeyHash:
			default:
				return nil, dcrjson.ErrInvalidAddressOrKey
			}
		}

		// Create an OP_RETURN push containing the pubkeyhash to send rewards to.
		// Apply limits to revocations for fees while not allowing
		// fees for votes.
		// Revocations (foremost byte)
		// 0x58 = 01 (Enabled)  010100 = 0x18 or 24
		//                              (2^24 or 16777216 atoms fee allowance)
		//                                 --> 0.16777216 coins
		// Votes (last byte)
		// 0x00 = 00 (Disabled) 000000
		limits := uint16(0x5800)
		pkScript, err := txscript.GenerateSStxAddrPush(addr,
			dcrutil.Amount(payouts[i].CommitAmt), limits)
		if err != nil {
			return nil, fmt.Errorf("cannot create txout script: %s", err)
		}
		txout := wire.NewTxOut(int64(0), pkScript)
		msgtx.AddTxOut(txout)

		// Add change to txouts.
		if payouts[i].ChangeAddr == "" {
			var err error
			changeAddr, err = w.newChangeAddress(w.persistReturnedChild(dbtx),
				udb.DefaultAccountNum)
			if err != nil {
				return nil, err
			}
		} else {
			a, err := dcrutil.DecodeAddress(payouts[i].ChangeAddr)
			if err != nil {
				return nil, err
			}
			// Ensure the address is one of the supported types and that
			// the network encoded with the address matches the network the
			// server is currently on.
			switch a.(type) {
			case *dcrutil.AddressPubKeyHash:
			case *dcrutil.AddressScriptHash:
			default:
				return nil, dcrjson.ErrInvalidAddressOrKey
			}
			changeAddr = a
		}

		err = addSStxChange(msgtx,
			dcrutil.Amount(payouts[i].ChangeAmt),
			changeAddr)
		if err != nil {
			return nil, err
		}

	}

	if stake.IsSStx(msgtx) {
		return nil, err
	}
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		return signMsgTx(msgtx, inputCredits, w.Manager, addrmgrNs, w.chainParams)
	})
	if err != nil {
		return nil, err
	}
	if err := validateMsgTxCredits(msgtx, inputCredits); err != nil {
		return nil, err
	}
	info := &CreatedTx{
		MsgTx:       msgtx,
		ChangeAddr:  nil,
		ChangeIndex: -1,
	}

	// TODO: Add to the stake manager

	return info, nil
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
			target := int32(w.chainParams.CoinbaseMaturity)
			if !confirmed(target, output.Height, currentHeight) {
				continue
			}
		case class == txscript.StakeRevocationTy:
			target := int32(w.chainParams.CoinbaseMaturity)
			if !confirmed(target, output.Height, currentHeight) {
				continue
			}
		case class == txscript.StakeSubChangeTy:
			target := int32(w.chainParams.SStxChangeMaturity)
			if !confirmed(target, output.Height, currentHeight) {
				continue
			}
		case class == txscript.PubKeyHashTy:
			if output.FromCoinBase {
				target := int32(w.chainParams.CoinbaseMaturity)
				if !confirmed(target, output.Height, currentHeight) {
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
	var unspentOutputs []udb.Credit
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		unspentOutputs, err = w.findEligibleOutputs(dbtx, account, minconf, currentHeight)
		return err
	})
	return unspentOutputs, err
}

// findEligibleOutputsAmount uses wtxmgr to find a number of unspent
// outputs while doing maturity checks there.
func (w *Wallet) findEligibleOutputsAmount(dbtx walletdb.ReadTx, account uint32, minconf int32,
	amount dcrutil.Amount, currentHeight int32) ([]udb.Credit, error) {

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

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
	}

	return eligible, nil
}

// signMsgTx sets the SignatureScript for every item in msgtx.TxIn.
// It must be called every time a msgtx is changed.
// Only P2PKH outputs are supported at this point.
func signMsgTx(msgtx *wire.MsgTx, prevOutputs []udb.Credit,
	mgr *udb.Manager, addrmgrNs walletdb.ReadBucket, chainParams *chaincfg.Params) error {
	if len(prevOutputs) != len(msgtx.TxIn) {
		return fmt.Errorf(
			"Number of prevOutputs (%d) does not match number of tx inputs (%d)",
			len(prevOutputs), len(msgtx.TxIn))
	}
	for i, output := range prevOutputs {
		// Errors don't matter here, as we only consider the
		// case where len(addrs) == 1.
		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(
			txscript.DefaultScriptVersion, output.PkScript, chainParams)
		if len(addrs) != 1 {
			continue
		}
		apkh, ok := addrs[0].(*dcrutil.AddressPubKeyHash)
		if !ok {
			return ErrUnsupportedTransactionType
		}

		privKey, done, err := mgr.PrivateKey(addrmgrNs, apkh)
		if err != nil {
			return err
		}
		defer done()

		sigscript, err := txscript.SignatureScript(msgtx, i, output.PkScript,
			txscript.SigHashAll, privKey, true)
		if err != nil {
			return fmt.Errorf("cannot create sigscript: %s", err)
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
		address, err := w.Manager.Address(addrmgrNs, addr)
		if err != nil {
			return nil, false, err
		}

		pka, ok := address.(udb.ManagedPubKeyAddress)
		if !ok {
			return nil, false, errors.New("address is not a pubkey address")
		}

		key, done, err := w.Manager.PrivateKey(addrmgrNs, addr)
		if err != nil {
			return nil, false, err
		}
		doneFuncs = append(doneFuncs, done)

		return key, pka.Compressed(), nil
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
		tx.TxIn[inputToSign].SignatureScript, chainec.ECTypeSecp256k1)
	if err != nil {
		return err
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
	stakebaseInput := wire.NewTxIn(stakebaseOutPoint, nil)
	stakebaseInput.ValueIn = subsidy
	vote.AddTxIn(stakebaseInput)

	// Votes reference the ticket purchase with the second input.
	ticketOutPoint := wire.NewOutPoint(ticketHash, 0, wire.TxTreeStake)
	vote.AddTxIn(wire.NewTxIn(ticketOutPoint, nil))

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
	revocation.AddTxIn(wire.NewTxIn(ticketOutPoint, nil))
	scriptSizers := []txsizes.ScriptSizer{txsizes.P2SHScriptSize}

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
	sizeEstimate := txsizes.EstimateSerializeSize(scriptSizers, revocation.TxOut, false)
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
	return nil, errors.New("no suitable revocation outputs to pay relay fee")
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

// fetchAddressFunc returns a change address
func (w *Wallet) fetchAddressFunc() func(persistReturnedChildFunc, uint32) (dcrutil.Address, error) {
	addrFunc := w.newChangeAddress
	if w.addressReuse {
		xpub := w.addressBuffers[udb.DefaultAccountNum].albExternal.branchXpub
		addr, err := deriveChildAddress(xpub, 0, w.chainParams)
		addrFunc = func(persistReturnedChildFunc, uint32) (dcrutil.Address, error) {
			return addr, err
		}
	}
	return addrFunc
}

// fetchAddresses returns a voting address. It checks if an address
// was passed along with the request. if none was passed, it checks
// for an address stored in configuration. If it finds none, it generates
// an address from user wallet
func (w *Wallet) fetchAddresses(ticketAddress dcrutil.Address,
	account uint32, addrFunc func(persistReturnedChildFunc, uint32) (dcrutil.Address, error)) (dcrutil.Address, dcrutil.Address, error) {
	addr := ticketAddress
	var saddr dcrutil.Address
	if addr == nil {
		if w.ticketAddress != nil {
			addr = w.ticketAddress
		} else {
			err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
				var err error
				addr, err = addrFunc(w.persistReturnedChild(dbtx), account)
				return err
			})
			if err != nil {
				return nil, nil, err
			}
		}
	}

	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		saddr, err = addrFunc(w.persistReturnedChild(dbtx), account)
		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return addr, saddr, nil
}

// randomAddress returns a random address. Mainly used for 0-value (unspendable)
// OP_SSTXCHANGE tagged outputs.
func randomAddress(params *chaincfg.Params) (dcrutil.Address, error) {
	b := make([]byte, ripemd160.Size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return dcrutil.NewAddressPubKeyHash(b, params, chainec.ECTypeSecp256k1)
}

func estimateSSTxSize(numInputs int) int {
	return txOverheadEstimate + txInEstimate*numInputs +
		sstxTicketCommitmentEstimate +
		(sstxSubsidyCommitmentEstimate+
			sstxChangeOutputEstimate)*numInputs
}

func feeForSize(incr dcrutil.Amount, sz int) dcrutil.Amount {
	return dcrutil.Amount(1+sz/1000) * incr
}

// FeeForSize is the exported version of feeForSize which returns a fee
// based on the provided feeIncrement and provided size.
func FeeForSize(incr dcrutil.Amount, sz int) dcrutil.Amount {
	return feeForSize(incr, sz)
}
