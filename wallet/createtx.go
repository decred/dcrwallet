// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/golangcrypto/ripemd160"

	"bytes"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/walletdb"
	"github.com/decred/dcrwallet/wtxmgr"
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
	// (with no leading 0 bytes for R and S), 65 bytes of serialized pubkey,
	// and data push opcodes for both, plus one byte for the hash type flag
	// appended to the end of the signature.
	sigScriptEstimate = 1 + 73 + 1 + 65 + 1

	// A best case tx input serialization cost is chainhash.HashSize, 4 bytes
	// of output index, 1 byte for tree, 4 bytes of sequence, 16 bytes for
	// fraud proof, and the estimated signature script size.
	txInEstimate = chainhash.HashSize + 4 + 1 + 8 + 4 + 4 + sigScriptEstimate

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

	// A P2PKH pkScript contains the following bytes:
	//  - OP_DUP
	//  - OP_HASH160
	//  - OP_DATA_20 + 20 bytes of pubkey hash
	//  - OP_EQUALVERIFY
	//  - OP_CHECKSIG
	pkScriptEstimate = 1 + 1 + 1 + 20 + 1 + 1

	// pkScriptEstimateSS is the estimated size of a ticket P2PKH output script.
	pkScriptEstimateSS = 1 + 1 + 1 + 1 + 20 + 1 + 1

	// txOutEstimate is a best case tx output serialization cost is 8 bytes
	// of value, two bytes of version, one byte of varint, and the pkScript
	// size.
	txOutEstimate = 8 + 2 + 1 + pkScriptEstimate

	// ssTxOutEsimate is the estimated size of a P2PKH ticket output.
	ssTxOutEsimate = 8 + 2 + 1 + pkScriptEstimateSS

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
)

var (
	// maxTxSize is the maximum size of a transaction we can
	// build with the wallet.
	maxTxSize = chaincfg.MainNetParams.MaxTxSize
)

func estimateTxSize(numInputs, numOutputs int) int {
	return txOverheadEstimate + txInEstimate*numInputs + txOutEstimate*numOutputs
}

// EstimateTxSize is the exported version of estimateTxSize which provides
// an estimate of the tx size based on the number of inputs, outputs, and some
// assumed overhead.
func EstimateTxSize(numInputs, numOutputs int) int {
	return estimateTxSize(numInputs, numOutputs)
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

// DefaultTicketFeeIncrement is the default minimum stake transaction fees per KB (0.01
// coin, measured in atoms).
const DefaultTicketFeeIncrement dcrutil.Amount = 1e6

// EstMaxTicketFeeAmount is the estimated max ticket fee to be used for size
// calculation for eligible utxos for ticket purchasing.
const EstMaxTicketFeeAmount = 0.1 * 1e8

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

	var authoredTx *txauthor.AuthoredTx
	err := walletdb.Update(w.db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrNamespaceKey)
		txmgrNs := tx.ReadBucket(wtxmgrNamespaceKey)
		_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

		if account != waddrmgr.ImportedAddrAccount {
			lastAcct, err := w.Manager.LastAccount(addrmgrNs)
			if err != nil {
				return err
			}
			if account > lastAcct {
				return waddrmgr.ManagerError{
					ErrorCode:   waddrmgr.ErrAccountNotFound,
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
			var internalPool *addressPool
			if account == waddrmgr.ImportedAddrAccount {
				internalPool = w.getAddressPools(waddrmgr.DefaultAccountNum).internal
			} else {
				internalPool = w.getAddressPools(account).internal
			}
			defer internalPool.mutex.Unlock()
			internalPool.mutex.Lock()

			changeSource = func() ([]byte, uint16, error) {
				// Derive the change output script.
				changeAddress, err := internalPool.getNewAddress(addrmgrNs)
				if err != nil {
					return nil, 0, err
				}
				script, err := txscript.PayToAddrScript(changeAddress)
				return script, txscript.DefaultScriptVersion, err
			}
		}

		var err error
		authoredTx, err = txauthor.NewUnsignedTransaction(outputs, relayFeePerKb,
			inputSource, changeSource)
		return err
	})
	return authoredTx, err
}

// secretSource is an implementation of txauthor.SecretSource for the wallet's
// address manager.
type secretSource struct {
	*waddrmgr.Manager
	addrmgrNs walletdb.ReadBucket
}

func (s secretSource) GetKey(addr dcrutil.Address) (chainec.PrivateKey, bool, error) {
	ma, err := s.Address(s.addrmgrNs, addr)
	if err != nil {
		return nil, false, err
	}
	mpka, ok := ma.(waddrmgr.ManagedPubKeyAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedPubKeyAddress", addr, ma)
		return nil, false, e
	}
	privKey, err := mpka.PrivKey()
	if err != nil {
		return nil, false, err
	}
	return privKey, ma.Compressed(), nil
}

func (s secretSource) GetScript(addr dcrutil.Address) ([]byte, error) {
	ma, err := s.Address(s.addrmgrNs, addr)
	if err != nil {
		return nil, err
	}
	msa, ok := ma.(waddrmgr.ManagedScriptAddress)
	if !ok {
		e := fmt.Errorf("managed address type for %v is `%T` but "+
			"want waddrmgr.ManagedScriptAddress", addr, ma)
		return nil, e
	}
	return msa.Script()
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
func (w *Wallet) insertIntoTxMgr(ns walletdb.ReadWriteBucket, msgTx *wire.MsgTx) (*wtxmgr.TxRecord, error) {
	// Create transaction record and insert into the db.
	rec, err := wtxmgr.NewTxRecordFromMsgTx(msgTx, time.Now())
	if err != nil {
		return nil, dcrjson.ErrInternal
	}

	return rec, w.TxStore.InsertMemPoolTx(ns, rec)
}

func (w *Wallet) insertCreditsIntoTxMgr(tx walletdb.ReadWriteTx,
	msgTx *wire.MsgTx, rec *wtxmgr.TxRecord) error {

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
				err = w.Manager.MarkUsed(addrmgrNs, addr)
				if err != nil {
					return err
				}
				log.Debugf("Marked address %v used", addr)
				continue
			}

			// Missing addresses are skipped.  Other errors should
			// be propagated.
			code := err.(waddrmgr.ManagerError).ErrorCode
			if code != waddrmgr.ErrAddressNotFound {
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
	rec, err := wtxmgr.NewTxRecordFromMsgTx(msgTx, time.Now())
	if err != nil {
		return err
	}

	return w.TxStore.AddMultisigOut(ns, rec, nil, index)
}

// txToOutputs creates a transaction, selecting previous outputs from an account
// with no less than minconf confirmations, and creates a signed transaction
// that pays to each of the outputs.
func (w *Wallet) txToOutputs(outputs []*wire.TxOut, account uint32, minconf int32,
	randomizeChangeIdx bool) (atx *txauthor.AuthoredTx, err error) {

	chainClient, err := w.requireChainClient()
	if err != nil {
		return nil, err
	}

	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
	}

	// Initialize the address pool for use. If we are using an imported
	// account, loopback to the default account to create change.
	var internalPool *addressPool
	if account == waddrmgr.ImportedAddrAccount {
		err := w.CheckAddressPoolsInitialized(waddrmgr.DefaultAccountNum)
		if err != nil {
			return nil, err
		}
		internalPool = w.getAddressPools(waddrmgr.DefaultAccountNum).internal
	} else {
		err := w.CheckAddressPoolsInitialized(account)
		if err != nil {
			return nil, err
		}
		internalPool = w.getAddressPools(account).internal
	}

	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		internalPool.mutex.Lock()
		var err error
		atx, err = w.txToOutputsInternal(dbtx, outputs, account,
			minconf, internalPool, chainClient, randomizeChangeIdx,
			w.RelayFee())
		if err == nil {
			addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
			internalPool.BatchFinish(addrmgrNs)
		} else {
			internalPool.BatchRollback()
		}
		internalPool.mutex.Unlock()
		return err
	})
	return atx, err
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
func (w *Wallet) txToOutputsInternal(dbtx walletdb.ReadWriteTx, outputs []*wire.TxOut,
	account uint32, minconf int32, internalPool *addressPool, chainClient *chain.RPCClient,
	randomizeChangeIdx bool, txFee dcrutil.Amount) (atx *txauthor.AuthoredTx, err error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	// Get current block's height and hash.
	_, topHeight := w.TxStore.MainChainTip(txmgrNs)

	inputSource := w.TxStore.MakeInputSource(txmgrNs, addrmgrNs, account,
		minconf, topHeight)
	changeSource := func() ([]byte, uint16, error) {
		// Derive the change output script.
		changeAddress, err := internalPool.getNewAddress(addrmgrNs)
		if err != nil {
			return nil, 0, err
		}
		script, err := txscript.PayToAddrScript(changeAddress)
		return script, txscript.DefaultScriptVersion, err
	}
	tx, err := txauthor.NewUnsignedTransaction(outputs, txFee,
		inputSource.SelectInputs, changeSource)
	if err != nil {
		return nil, err
	}

	// Randomize change position, if change exists, before signing.  This
	// doesn't affect the serialize size, so the change amount will still be
	// valid.
	if tx.ChangeIndex >= 0 && randomizeChangeIdx {
		tx.RandomizeChangePosition()
	}

	err = tx.AddAllInputScripts(secretSource{w.Manager, addrmgrNs})
	if err != nil {
		return nil, err
	}

	err = validateMsgTx(tx.Tx, tx.PrevScripts)
	if err != nil {
		return nil, err
	}

	if tx.ChangeIndex >= 0 && account == waddrmgr.ImportedAddrAccount {
		changeAmount := dcrutil.Amount(tx.Tx.TxOut[tx.ChangeIndex].Value)
		log.Warnf("Spend from imported account produced change: moving"+
			" %v from imported account into default account.", changeAmount)
	}

	_, err = chainClient.SendRawTransaction(tx.Tx, w.AllowHighFees)
	if err != nil {
		return nil, err
	}

	// Ensure the transaction is saved.
	//
	// TODO: this can be improved by not using the same codepath as notified
	// relevant transactions, since this does a lot of extra work.
	var buf bytes.Buffer
	err = tx.Tx.Serialize(&buf)
	if err != nil {
		return nil, err
	}
	err = w.processTransaction(dbtx, buf.Bytes(), nil, nil)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// constructMultiSigScript create a multisignature output script from a
// given list of public keys.
func constructMultiSigScript(keys []dcrutil.AddressSecpPubKey,
	nRequired int) ([]byte, error) {
	keysesPrecious := make([]*dcrutil.AddressSecpPubKey, len(keys))

	return txscript.MultiSigScript(keysesPrecious, nRequired)
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

	// Initialize the address pool for use. If we are using an imported
	// account, loopback to the default account to create change.
	var internalPool *addressPool
	if account == waddrmgr.ImportedAddrAccount {
		err := w.CheckAddressPoolsInitialized(waddrmgr.DefaultAccountNum)
		if err != nil {
			return txToMultisigError(err)
		}
		internalPool = w.getAddressPools(waddrmgr.DefaultAccountNum).internal
	} else {
		err := w.CheckAddressPoolsInitialized(account)
		if err != nil {
			return txToMultisigError(err)
		}
		internalPool = w.getAddressPools(account).internal
	}

	// TODO: Yes this looks suspicious but it's a simplification of what the
	// code before it was doing.  Stop copy pasting code carelessly.
	internalPool.mutex.Lock()
	defer internalPool.mutex.Unlock()
	defer internalPool.BatchRollback()
	addrFunc := internalPool.getNewAddress

	chainClient, err := w.requireChainClient()
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
	case w.chainParams == &chaincfg.TestNetParams:
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

	// Fill out inputs.
	var forSigning []wtxmgr.Credit
	totalInput := dcrutil.Amount(0)
	numInputs := 0
	for _, e := range eligible {
		msgtx.AddTxIn(wire.NewTxIn(&e.OutPoint, nil))
		totalInput += e.Amount
		forSigning = append(forSigning, e)

		numInputs++
	}

	// Insert a multi-signature output, then insert this P2SH
	// hash160 into the address manager and the transaction
	// manager.
	totalOutput := dcrutil.Amount(0)
	msScript, err := txscript.MultiSigScript(pubkeys, int(nRequired))
	if err != nil {
		return txToMultisigError(err)
	}
	_, err = w.Manager.ImportScript(addrmgrNs, msScript)
	if err != nil {
		// We don't care if we've already used this address.
		if err.(waddrmgr.ManagerError).ErrorCode !=
			waddrmgr.ErrDuplicateAddress {
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
	totalOutput += amount

	// Add change if we need it. The case in which
	// totalInput == amount+feeEst is skipped because
	// we don't need to add a change output in this
	// case.
	feeSize := estimateTxSize(numInputs, 2)
	feeIncrement := w.RelayFee()

	feeEst := feeForSize(feeIncrement, feeSize)

	if totalInput < amount+feeEst {
		return txToMultisigError(fmt.Errorf("Not enough funds to send to " +
			"multisig address after accounting for fees"))
	}
	if totalInput > amount+feeEst {
		changeAddr, err := addrFunc(addrmgrNs)
		if err != nil {
			return txToMultisigError(err)
		}
		change := totalInput - (amount + feeEst)
		pkScript, err := txscript.PayToAddrScript(changeAddr)
		if err != nil {
			return txToMultisigError(
				fmt.Errorf("cannot create txout script: %s", err))
		}
		msgtx.AddTxOut(wire.NewTxOut(int64(change), pkScript))
	}

	if err = signMsgTx(msgtx, forSigning, w.Manager, addrmgrNs,
		w.chainParams); err != nil {
		return txToMultisigError(err)
	}

	_, err = chainClient.SendRawTransaction(msgtx, w.AllowHighFees)
	if err != nil {
		return txToMultisigError(err)
	}

	// Request updates from dcrd for new transactions sent to this
	// script hash address.
	utilAddrs := make([]dcrutil.Address, 1)
	utilAddrs[0] = scAddr
	err = chainClient.LoadTxFilter(false, []dcrutil.Address{scAddr}, nil)
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
			txscript.StandardVerifyFlags, txscript.DefaultScriptVersion, nil)
		if err != nil {
			return fmt.Errorf("cannot create script engine: %s", err)
		}
		err = vm.Execute()
		if err != nil {
			return fmt.Errorf("cannot validate transaction: %s", err)
		}
	}
	return nil
}

func validateMsgTxCredits(tx *wire.MsgTx, prevCredits []wtxmgr.Credit) error {
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

	chainClient, err := w.requireChainClient()
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

	// Initialize the address pool for use. If we are using an imported
	// account, loopback to the default account to create change.
	var internalPool *addressPool
	if account == waddrmgr.ImportedAddrAccount {
		err := w.CheckAddressPoolsInitialized(waddrmgr.DefaultAccountNum)
		if err != nil {
			return nil, err
		}
		internalPool = w.getAddressPools(waddrmgr.DefaultAccountNum).internal
	} else {
		err := w.CheckAddressPoolsInitialized(account)
		if err != nil {
			return nil, err
		}
		internalPool = w.getAddressPools(account).internal
	}
	txSucceeded := false
	internalPool.mutex.Lock()
	defer internalPool.mutex.Unlock()
	defer func() {
		if txSucceeded {
			internalPool.BatchFinish(addrmgrNs)
		} else {
			internalPool.BatchRollback()
		}
	}()
	addrFunc := internalPool.getNewAddress

	minconf := int32(1)
	eligible, err := w.findEligibleOutputs(dbtx, account, minconf, tipHeight)
	if err != nil {
		return nil, err
	}

	if len(eligible) == 0 {
		return nil, ErrNoOutsToConsolidate
	}

	txInCount := len(eligible)
	if maxNumIns < txInCount {
		txInCount = maxNumIns
	}

	// Get an initial fee estimate based on the number of selected inputs
	// and added outputs, with no change.
	szEst := estimateTxSize(txInCount, 1)
	var feeIncrement dcrutil.Amount
	feeIncrement = w.RelayFee()

	feeEst := feeForSize(feeIncrement, szEst)

	// Check if output address is default, and generate a new adress if needed
	if changeAddr == nil {
		changeAddr, err = addrFunc(addrmgrNs)
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
	msgTxSize := msgtx.SerializeSize()
	maximumTxSize := maxTxSize
	if w.chainParams.Net == wire.MainNet {
		maximumTxSize = maxStandardTxSize
	}

	// Add the txins using all the eligible outputs.
	totalAdded := dcrutil.Amount(0)
	count := 0
	var forSigning []wtxmgr.Credit
	for _, e := range eligible {
		if count >= maxNumIns {
			break
		}
		// Add the size of a wire.OutPoint
		msgTxSize += txInEstimate
		if msgTxSize > maximumTxSize {
			break
		}
		msgtx.AddTxIn(wire.NewTxIn(&e.OutPoint, nil))
		totalAdded += e.Amount
		forSigning = append(forSigning, e)

		count++
	}

	msgtx.TxOut[0].Value = int64(totalAdded - feeEst)

	if err = signMsgTx(msgtx, forSigning, w.Manager, addrmgrNs,
		w.chainParams); err != nil {
		return nil, err
	}
	if err := validateMsgTxCredits(msgtx, forSigning); err != nil {
		return nil, err
	}

	txSha, err := chainClient.SendRawTransaction(msgtx, w.AllowHighFees)
	if err != nil {
		return nil, err
	}
	txSucceeded = true

	// Insert the transaction and credits into the transaction manager.
	rec, err := w.insertIntoTxMgr(txmgrNs, msgtx)
	if err != nil {
		return nil, err
	}
	err = w.insertCreditsIntoTxMgr(dbtx, msgtx, rec)
	if err != nil {
		return nil, err
	}

	log.Infof("Successfully consolidated funds in transaction %v", txSha)

	return txSha, nil
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
	if _, err := stake.IsSStx(mtx); err != nil {
		return nil, err
	}

	return mtx, nil
}

// purchaseTickets indicates to the wallet that a ticket should be purchased
// using all currently available funds.  The ticket address parameter in the
// request can be nil in which case the ticket address associated with the
// wallet instance will be used.  Also, when the spend limit in the request is
// greater than or equal to 0, tickets that cost more than that limit will
// return an error that not enough funds are available.
func (w *Wallet) purchaseTickets(req purchaseTicketRequest) ([]*chainhash.Hash, error) {
	var ticketHashes []*chainhash.Hash
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		ticketHashes, err = w.purchaseTicketsInternal(dbtx, req)
		return err
	})
	return ticketHashes, err
}

func (w *Wallet) purchaseTicketsInternal(dbtx walletdb.ReadWriteTx, req purchaseTicketRequest) ([]*chainhash.Hash, error) {
	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)
	stakemgrNs := dbtx.ReadWriteBucket(wstakemgrNamespaceKey)

	// Ensure the minimum number of required confirmations is positive.
	if req.minConf < 0 {
		return nil, fmt.Errorf("need positive minconf")
	}

	// Need a positive or zero expiry that is higher than the next block to
	// generate.
	if req.expiry < 0 {
		return nil, fmt.Errorf("need positive expiry")
	}
	_, tipHeight := w.TxStore.MainChainTip(txmgrNs)
	if req.expiry <= tipHeight+1 && req.expiry > 0 {
		return nil, fmt.Errorf("need expiry that is beyond next height ("+
			"given: %v, next height %v)", req.expiry, tipHeight+1)
	}

	// Initialize the address pool for use.
	var internalPool *addressPool
	err := w.CheckAddressPoolsInitialized(req.account)
	if err != nil {
		return nil, err
	}
	internalPool = w.getAddressPools(req.account).internal

	// Fire up the address pool for usage in generating tickets.
	internalPool.mutex.Lock()
	defer internalPool.mutex.Unlock()
	txSucceeded := false
	defer func() {
		if txSucceeded {
			internalPool.BatchFinish(addrmgrNs)
		} else {
			internalPool.BatchRollback()
		}
	}()
	addrFunc := internalPool.getNewAddress

	if w.addressReuse {
		addrFunc = func(ns walletdb.ReadWriteBucket) (dcrutil.Address, error) {
			return w.reusedAddress(ns)
		}
	}

	chainClient, err := w.requireChainClient()
	if err != nil {
		return nil, err
	}

	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
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

	// Get the current ticket price from the daemon.
	ticketPricesF64, err := w.ChainClient().GetStakeDifficulty()
	if err != nil {
		return nil, err
	}
	ticketPrice, err := dcrutil.NewAmount(ticketPricesF64.NextStakeDifficulty)
	if err != nil {
		return nil, err
	}

	// Ensure the ticket price does not exceed the spend limit if set.
	if req.spendLimit >= 0 && ticketPrice > req.spendLimit {
		return nil, ErrSStxPriceExceedsSpendLimit
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
		return nil, fmt.Errorf("pool address given, but pool fees not set")
	}

	// Make sure that we have enough funds. Calculate different
	// ticket required amounts depending on whether or not a
	// pool output is needed. If the ticket fee increment is
	// unset in the request, use the global ticket fee increment.
	neededPerTicket := dcrutil.Amount(0)
	ticketFee := dcrutil.Amount(0)
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
		bal, err := w.CalculateAccountBalance(account, req.minConf)
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
				account, bal.Spendable.ToCoin())
			log.Debugf("%s", notEnoughFundsStr)
			return nil, txauthor.InsufficientFundsError{}
		}
	}

	// Fetch the single use split address to break tickets into, to
	// immediately be consumed as tickets.
	splitTxAddr, err := internalPool.getNewAddress(addrmgrNs)
	if err != nil {
		return nil, err
	}

	// Create the split transaction by using txToOutputs. This varies
	// based upon whether or not the user is using a stake pool or not.
	// For the default stake pool implementation, the user pays out the
	// first ticket commitment of a smaller amount to the pool, while
	// paying themselves with the larger ticket commitment.
	var splitOuts []*wire.TxOut
	for i := 0; i < req.numTickets; i++ {
		// No pool used.
		if poolAddress == nil {
			pkScript, err := txscript.PayToAddrScript(splitTxAddr)
			if err != nil {
				return nil, fmt.Errorf("cannot create txout script: %s", err)
			}

			splitOuts = append(splitOuts,
				wire.NewTxOut(int64(neededPerTicket), pkScript))
		} else {
			// Stake pool used.
			userAmt := neededPerTicket - poolFeeAmt
			poolAmt := poolFeeAmt

			// Pool amount.
			pkScript, err := txscript.PayToAddrScript(splitTxAddr)
			if err != nil {
				return nil, fmt.Errorf("cannot create txout script: %s", err)
			}

			splitOuts = append(splitOuts, wire.NewTxOut(int64(poolAmt), pkScript))

			// User amount.
			pkScript, err = txscript.PayToAddrScript(splitTxAddr)
			if err != nil {
				return nil, fmt.Errorf("cannot create txout script: %s", err)
			}

			splitOuts = append(splitOuts, wire.NewTxOut(int64(userAmt), pkScript))
		}

	}

	txFeeIncrement := req.txFee
	if txFeeIncrement == 0 {
		txFeeIncrement = w.RelayFee()
	}
	splitTx, err := w.txToOutputsInternal(dbtx, splitOuts, account, req.minConf, internalPool,
		chainClient, false, txFeeIncrement)
	if err != nil {
		return nil, err
	}

	// At this point, addresses have been used in tx in the
	// mempool, so we need to close the batch after. It might
	// be the case that tickets fail after this, but it should
	// be very unlikely since all tickets will use known,
	// accepted outputs.
	txSucceeded = true

	// Generate the tickets individually.
	ticketHashes := make([]*chainhash.Hash, req.numTickets)
	for i := 0; i < req.numTickets; i++ {
		// Generate the extended outpoints that we
		// need to use for ticket inputs. There are
		// two inputs for pool tickets corresponding
		// to the fees and the user subsidy, while
		// user-handled tickets have only one input.
		var eopPool, eop *extendedOutPoint
		if poolAddress == nil {
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
		} else {
			poolIdx := i * 2
			poolTxOut := splitTx.Tx.TxOut[poolIdx]
			userIdx := i*2 + 1
			txOut := splitTx.Tx.TxOut[userIdx]

			eopPool = &extendedOutPoint{
				op: &wire.OutPoint{
					Hash:  splitTx.Tx.TxHash(),
					Index: uint32(poolIdx),
					Tree:  wire.TxTreeRegular,
				},
				amt:      poolTxOut.Value,
				pkScript: poolTxOut.PkScript,
			}
			eop = &extendedOutPoint{
				op: &wire.OutPoint{
					Hash:  splitTx.Tx.TxHash(),
					Index: uint32(userIdx),
					Tree:  wire.TxTreeRegular,
				},
				amt:      txOut.Value,
				pkScript: txOut.PkScript,
			}
		}

		// If the user hasn't specified a voting address
		// to delegate voting to, just use an address from
		// this wallet. Check the passed address from the
		// request first, then check the ticket address
		// stored from the configuation. Finally, generate
		// an address.
		addrVote := req.ticketAddr
		if addrVote == nil {
			addrVote = w.ticketAddress
			if addrVote == nil {
				addrVote, err = addrFunc(addrmgrNs)
				if err != nil {
					return nil, err
				}
			}
		}

		addrSubsidy, err := addrFunc(addrmgrNs)
		if err != nil {
			return nil, err
		}

		// Generate the ticket msgTx and sign it.
		ticket, err := makeTicket(w.ChainParams(), eopPool, eop, addrVote,
			addrSubsidy, int64(ticketPrice), poolAddress)
		if err != nil {
			return nil, err
		}
		var forSigning []wtxmgr.Credit
		if eopPool != nil {
			eopPoolCredit := wtxmgr.Credit{
				OutPoint:     *eopPool.op,
				BlockMeta:    wtxmgr.BlockMeta{},
				Amount:       dcrutil.Amount(eopPool.amt),
				PkScript:     eopPool.pkScript,
				Received:     time.Now(),
				FromCoinBase: false,
			}
			forSigning = append(forSigning, eopPoolCredit)
		}
		eopCredit := wtxmgr.Credit{
			OutPoint:     *eop.op,
			BlockMeta:    wtxmgr.BlockMeta{},
			Amount:       dcrutil.Amount(eop.amt),
			PkScript:     eop.pkScript,
			Received:     time.Now(),
			FromCoinBase: false,
		}
		forSigning = append(forSigning, eopCredit)

		// Set the expiry.
		ticket.Expiry = uint32(req.expiry)

		if err = signMsgTx(ticket, forSigning, w.Manager, addrmgrNs,
			w.chainParams); err != nil {
			return nil, err
		}
		if err := validateMsgTxCredits(ticket, forSigning); err != nil {
			return nil, err
		}

		// Send the ticket over the network.
		txHash, err := chainClient.SendRawTransaction(ticket, w.AllowHighFees)
		if err != nil {
			return nil, err
		}

		// Insert the transaction and credits into the transaction manager.
		rec, err := w.insertIntoTxMgr(txmgrNs, ticket)
		if err != nil {
			return nil, err
		}
		err = w.insertCreditsIntoTxMgr(dbtx, ticket, rec)
		if err != nil {
			return nil, err
		}
		txTemp := dcrutil.NewTx(ticket)

		// The ticket address may be for another wallet. Don't insert the
		// ticket into the stake manager unless we actually own output zero
		// of it. If this is the case, the chainntfns.go handlers will
		// automatically insert it.
		if _, err := w.Manager.Address(addrmgrNs, addrVote); err == nil {
			if w.ticketAddress == nil {
				err = w.StakeMgr.InsertSStx(stakemgrNs, txTemp, w.VoteBits)
				if err != nil {
					return nil, fmt.Errorf("Failed to insert SStx %v"+
						"into the stake store", txTemp.Hash())
				}
			}
		}

		log.Infof("Successfully sent SStx purchase transaction %v", txHash)
		ticketHashes[i] = txHash
	}

	return ticketHashes, nil
}

// txToSStx creates a raw SStx transaction sending the amounts for each
// address/amount pair and fee to each address and the miner.  minconf
// specifies the minimum number of confirmations required before an
// unspent output is eligible for spending. Leftover input funds not sent
// to addr or as a fee for the miner are sent to a newly generated
// address. If change is needed to return funds back to an owned
// address, changeUtxo will point to a unconfirmed (height = -1, zeroed
// block hash) Utxo.  ErrInsufficientFunds is returned if there are not
// enough eligible unspent outputs to create the transaction.
func (w *Wallet) txToSStx(pair map[string]dcrutil.Amount,
	inputCredits []wtxmgr.Credit, inputs []dcrjson.SStxInput,
	payouts []dcrjson.SStxCommitOut, account uint32, minconf int32) (*CreatedTx, error) {

	var tx *CreatedTx
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		var err error
		tx, err = w.txToSStxInternal(dbtx, pair, inputCredits, inputs,
			payouts, account, minconf)
		return err
	})
	return tx, err
}

func (w *Wallet) txToSStxInternal(dbtx walletdb.ReadWriteTx, pair map[string]dcrutil.Amount,
	inputCredits []wtxmgr.Credit, inputs []dcrjson.SStxInput,
	payouts []dcrjson.SStxCommitOut, account uint32, minconf int32) (tx *CreatedTx, err error) {

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)

	internalPool := w.getAddressPools(waddrmgr.DefaultAccountNum).internal
	internalPool.mutex.Lock()
	addrFunc := internalPool.getNewAddress
	defer func() {
		if err == nil {
			internalPool.BatchFinish(addrmgrNs)
		} else {
			internalPool.BatchRollback()
		}
		internalPool.mutex.Unlock()
	}()

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
		addr, err := dcrutil.DecodeAddress(addrStr, w.chainParams)
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

		if input.Vout < 0 {
			return nil, dcrjson.Error{
				Code:    dcrjson.ErrInvalidParameter.Code,
				Message: "Invalid parameter, vout must be positive",
			}
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
	rewards := []string{}
	for _, value := range payouts {
		rewards = append(rewards, value.Addr)
	}

	var changeAddr dcrutil.Address

	for i := range inputs {
		// Add the OP_RETURN commitment amounts and payout to
		// addresses.
		var addr dcrutil.Address

		if payouts[i].Addr == "" {
			var err error
			addr, err = addrFunc(addrmgrNs)
			if err != nil {
				return nil, err
			}
		} else {
			addr, err = dcrutil.DecodeAddress(payouts[i].Addr,
				w.chainParams)
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
			changeAddr, err = addrFunc(addrmgrNs)
			if err != nil {
				return nil, err
			}
		} else {
			a, err := dcrutil.DecodeAddress(payouts[i].ChangeAddr, w.chainParams)
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
	if _, err := stake.IsSStx(msgtx); err != nil {
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

// addOutputsSStx is used to add outputs for a stake SStx.
// DECRED TODO
func addOutputsSStx(msgtx *wire.MsgTx,
	pair map[string]dcrutil.Amount,
	amountsIn []int64,
	payouts map[string]string) error {

	return nil
}

// txToSSGen ...
// DECRED TODO
func (w *Wallet) txToSSGen(ticketHash chainhash.Hash, blockHash chainhash.Hash,
	height int64, votebits uint16) (*CreatedTx, error) {
	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
	}

	return nil, nil
}

// txToSSRtx ...
// DECRED TODO
func (w *Wallet) txToSSRtx(ticketHash chainhash.Hash) (*CreatedTx, error) {
	w.reorganizingLock.Lock()
	reorg := w.reorganizing
	w.reorganizingLock.Unlock()
	if reorg {
		return nil, ErrBlockchainReorganizing
	}

	return nil, nil
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
	currentHeight int32) ([]wtxmgr.Credit, error) {

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
	eligible := make([]wtxmgr.Credit, 0, len(unspent))
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
func (w *Wallet) FindEligibleOutputs(account uint32, minconf int32, currentHeight int32) ([]wtxmgr.Credit, error) {
	var unspentOutputs []wtxmgr.Credit
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
	amount dcrutil.Amount, currentHeight int32) ([]wtxmgr.Credit, error) {

	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

	unspent, err := w.TxStore.UnspentOutputsForAmount(txmgrNs, addrmgrNs,
		amount, currentHeight, minconf, false, account)
	if err != nil {
		return nil, err
	}

	eligible := make([]wtxmgr.Credit, 0, len(unspent))
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
func signMsgTx(msgtx *wire.MsgTx, prevOutputs []wtxmgr.Credit,
	mgr *waddrmgr.Manager, addrmgrNs walletdb.ReadBucket, chainParams *chaincfg.Params) error {
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

		ai, err := mgr.Address(addrmgrNs, apkh)
		if err != nil {
			return fmt.Errorf("cannot get address info: %v", err)
		}

		pka := ai.(waddrmgr.ManagedPubKeyAddress)
		privkey, err := pka.PrivKey()
		if err != nil {
			return fmt.Errorf("cannot get private key: %v", err)
		}

		sigscript, err := txscript.SignatureScript(msgtx, i,
			output.PkScript, txscript.SigHashAll, privkey,
			ai.Compressed())
		if err != nil {
			return fmt.Errorf("cannot create sigscript: %s", err)
		}
		msgtx.TxIn[i].SignatureScript = sigscript
	}

	return nil
}

// minimumFee estimates the minimum fee required for a transaction.
// If cfg.DisallowFree is false, a fee may be zero so long as txLen
// s less than 1 kilobyte and none of the outputs contain a value
// less than 1 bitcent. Otherwise, the fee will be calculated using
// incr, incrementing the fee for each kilobyte of transaction.
func minimumFee(incr dcrutil.Amount, txLen int, outputs []*wire.TxOut,
	prevOutputs []wtxmgr.Credit, height int32, disallowFree bool) dcrutil.Amount {
	allowFree := false
	if !disallowFree {
		allowFree = allowNoFeeTx(height, prevOutputs, txLen)
	}
	fee := feeForSize(incr, txLen)

	if allowFree && txLen < 1000 {
		fee = 0
	}

	if fee < incr {
		for _, txOut := range outputs {
			if txOut.Value < dcrutil.AtomsPerCent {
				return incr
			}
		}
	}

	// How can fee be smaller than 0 here?
	if fee < 0 || fee > dcrutil.MaxAmount {
		fee = dcrutil.MaxAmount
	}

	return fee
}

// allowNoFeeTx calculates the transaction priority and checks that the
// priority reaches a certain threshold.  If the threshhold is
// reached, a free transaction fee is allowed.
func allowNoFeeTx(curHeight int32, txouts []wtxmgr.Credit, txSize int) bool {
	const blocksPerDayEstimate = 144.0
	const txSizeEstimate = 250.0
	const threshold = dcrutil.AtomsPerCoin * blocksPerDayEstimate / txSizeEstimate

	var weightedSum int64
	for _, txout := range txouts {
		depth := chainDepth(txout.Height, curHeight)
		weightedSum += int64(txout.Amount) * int64(depth)
	}
	priority := float64(weightedSum) / float64(txSize)
	return priority > threshold
}

// chainDepth returns the chaindepth of a target given the current
// blockchain height.
func chainDepth(target, current int32) int32 {
	if target == -1 {
		// target is not yet in a block.
		return 0
	}

	// target is in a block.
	return current - target + 1
}

// randomAddress returns a random address. Mainly used for 0-value (unspendable)
// OP_SSTXCHANGE tagged outputs.
func randomAddress(params *chaincfg.Params) (dcrutil.Address, error) {
	b := make([]byte, ripemd160.Size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return dcrutil.NewAddressPubKeyHash(b, params,
		chainec.ECTypeSecp256k1)
}
