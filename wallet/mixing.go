// Copyright (c) 2019-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	cryptorand "crypto/rand"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/txrules"
	"decred.org/dcrwallet/v4/wallet/txsizes"
	"decred.org/dcrwallet/v4/wallet/udb"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/mixing"
	"github.com/decred/dcrd/mixing/mixclient"
	"github.com/decred/dcrd/mixing/mixpool"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/go-socks/socks"
	"golang.org/x/sync/errgroup"
)

// MixMessage queries the mixpool for a message.  Only messages that have been
// recently inv'd should be queried.
func (w *Wallet) MixMessage(query *chainhash.Hash) (mixing.Message, error) {
	return w.mixpool.Message(query)
}

type mixpoolBlockchain Wallet

func (b *mixpoolBlockchain) CurrentTip() (chainhash.Hash, int64) {
	w := (*Wallet)(b)
	ctx := context.Background()
	hash, height := w.MainChainTip(ctx)
	return hash, int64(height)
}

func (b *mixpoolBlockchain) ChainParams() *chaincfg.Params {
	return (*Wallet)(b).chainParams
}

func (w *Wallet) makeGen(ctx context.Context, account, branch uint32) mixclient.GenFunc {
	const op errors.Op = "gen"

	gen := func(mcount uint32) (wire.MixVect, error) {
		gen := make(wire.MixVect, mcount)
		var updates []func(walletdb.ReadWriteTx) error

		for i := uint32(0); i < mcount; i++ {
			persist := w.deferPersistReturnedChild(ctx, &updates)
			const accountName = "" // not used, so can be faked.
			mixAddr, err := w.nextAddress(ctx, op, persist,
				accountName, account, branch, WithGapPolicyIgnore())
			if err != nil {
				return nil, err
			}
			hash160er, ok := mixAddr.(stdaddr.Hash160er)
			if !ok {
				return nil, errors.E("address does not have Hash160 method")
			}
			gen[i] = *hash160er.Hash160()
		}

		err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
			for _, f := range updates {
				if err := f(dbtx); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, errors.E(op, err)
		}
		return gen, nil
	}
	return mixclient.GenFunc(gen)
}

// mixingWallet implements the mixclient.Wallet interface.
type mixingWallet Wallet

// BestBlock returns the wallet's current best tip block height and hash.
func (w *mixingWallet) BestBlock() (uint32, chainhash.Hash) {
	wallet := (*Wallet)(w)
	hash, height := wallet.MainChainTip(context.Background())
	return uint32(height), hash
}

// Mixpool returns access to the wallet's mixing message pool.
//
// The mixpool should only be used for message access and deletion,
// but never publishing; SubmitMixMessage must be used instead for
// message publishing.
func (w *mixingWallet) Mixpool() *mixpool.Pool {
	wallet := (*Wallet)(w)
	return wallet.mixpool
}

// SubmitMixMessage submits a mixing message to the wallet's mixpool
// and broadcasts it to the network.
func (w *mixingWallet) SubmitMixMessage(ctx context.Context, msg mixing.Message) (err error) {
	defer func() {
		if err != nil {
			w.mixpool.RemoveMessage(msg)
		}
	}()

	_, err = w.mixpool.AcceptMessage(msg)
	if err != nil {
		return err
	}

	w.networkBackendMu.Lock()
	n := w.networkBackend
	w.networkBackendMu.Unlock()
	if n == nil {
		return errors.NoPeers
	}
	return n.PublishMixMessages(ctx, msg)
}

// SignInput adds a signature script to a transaction input.
func (w *mixingWallet) SignInput(tx *wire.MsgTx, index int, prevScript []byte) error {
	wallet := (*Wallet)(w)
	ctx := context.Background()
	in := tx.TxIn[index]

	return walletdb.View(ctx, wallet.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

		const scriptVersion = 0
		_, addrs := stdscript.ExtractAddrs(scriptVersion, prevScript,
			wallet.chainParams)
		if len(addrs) != 1 {
			return errors.E(errors.Invalid, "previous output is not P2PKH")
		}
		apkh, ok := addrs[0].(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0)
		if !ok {
			return errors.E(errors.Invalid, "previous output is not P2PKH")
		}
		privKey, done, err := wallet.manager.PrivateKey(addrmgrNs, apkh)
		if err != nil {
			return err
		}
		defer done()
		sigscript, err := sign.SignatureScript(tx, index, prevScript,
			txscript.SigHashAll, privKey.Serialize(),
			dcrec.STEcdsaSecp256k1, true)
		if err != nil {
			return errors.E(errors.Op("sign.SignatureScript"), err)
		}
		in.SignatureScript = sigscript
		return nil
	})
}

// PublishTransaction adds the transaction to the wallet and publishes
// it to the network.
func (w *mixingWallet) PublishTransaction(ctx context.Context, tx *wire.MsgTx) error {
	wallet := (*Wallet)(w)

	wallet.networkBackendMu.Lock()
	n := wallet.networkBackend
	wallet.networkBackendMu.Unlock()

	_, err := wallet.PublishTransaction(ctx, tx, n)
	return err
}

func (w *Wallet) dicemixExpiry(ctx context.Context) uint32 {
	_, height := w.MainChainTip(ctx)
	return mixing.MaxExpiry(uint32(height), w.chainParams) - 2
}

// addCoinJoinInput adds a wallet's controlled UTXO to the coinjoin
// transaction.  This method looks up the private key of the previous output
// to create the UTXO signature proof and requires the wallet or account to be
// unlocked.
func (w *Wallet) addCoinJoinInput(ctx context.Context, cj *mixclient.CoinJoin,
	input *wire.TxIn, prevScript []byte, prevScriptVersion uint16) error {

	const scriptVersion = 0
	_, addrs := stdscript.ExtractAddrs(scriptVersion, prevScript,
		w.chainParams)
	if len(addrs) != 1 {
		return errors.E(errors.Invalid, "previous output is not P2PKH")
	}
	prevP2PKH, ok := addrs[0].(*stdaddr.AddressPubKeyHashEcdsaSecp256k1V0)
	if !ok {
		return errors.E(errors.Invalid, "previous output is not P2PKH")
	}
	var privKey *secp256k1.PrivateKey
	var privKeyDone func()
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		var err error
		privKey, privKeyDone, err = w.manager.PrivateKey(addrmgrNs, prevP2PKH)
		return err
	})
	if err != nil {
		if privKeyDone != nil {
			privKeyDone()
		}
		return err
	}

	err = cj.AddInput(input, prevScript, prevScriptVersion, privKey)
	privKeyDone()
	return err
}

// must be sorted large to small
var splitPoints = [...]dcrutil.Amount{
	1 << 36, // 687.19476736
	1 << 34, // 171.79869184
	1 << 32, // 042.94967296
	1 << 30, // 010.73741824
	1 << 28, // 002.68435456
	1 << 26, // 000.67108864
	1 << 24, // 000.16777216
	1 << 22, // 000.04194304
	1 << 20, // 000.01048576
	1 << 18, // 000.00262144
}

func smallestMixChange(feeRate dcrutil.Amount) dcrutil.Amount {
	inScriptSizes := []int{txsizes.RedeemP2PKHSigScriptSize}
	outScriptSizes := []int{txsizes.P2PKHPkScriptSize}
	size := txsizes.EstimateSerializeSizeFromScriptSizes(
		inScriptSizes, outScriptSizes, 0)
	fee := txrules.FeeForSerializeSize(feeRate, size)
	return fee + splitPoints[len(splitPoints)-1]
}

type mixSemaphores struct {
	splitSems [len(splitPoints)]chan struct{}
}

func newMixSemaphores(n int) mixSemaphores {
	var m mixSemaphores
	for i := range m.splitSems {
		m.splitSems[i] = make(chan struct{}, n)
	}
	return m
}

var (
	errNoSplitDenomination = errors.New("no suitable split denomination")
	errThrottledMixRequest = errors.New("throttled mix request for split denomination")
)

// MixOutput performs a mix of a single output into standard sized outputs
// under the current ticket price.
func (w *Wallet) MixOutput(ctx context.Context, output *wire.OutPoint, changeAccount, mixAccount,
	mixBranch uint32) error {

	op := errors.Opf("wallet.MixOutput(%v)", output)

	// Mixing requests require wallet mixing support.
	if !w.mixing {
		s := "wallet mixing support is disabled"
		return errors.E(op, errors.Invalid, s)
	}

	sdiff, err := w.NextStakeDifficulty(ctx)
	if err != nil {
		return errors.E(op, err)
	}

	w.lockedOutpointMu.Lock()
	if _, exists := w.lockedOutpoints[outpoint{output.Hash, output.Index}]; exists {
		w.lockedOutpointMu.Unlock()
		err = errors.Errorf("output %v already locked", output)
		return errors.E(op, err)
	}

	var prevScript []byte
	var prevScriptVersion uint16
	var amount dcrutil.Amount
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		txDetails, err := w.txStore.TxDetails(txmgrNs, &output.Hash)
		if err != nil {
			return err
		}
		out := txDetails.MsgTx.TxOut[output.Index]
		prevScript = out.PkScript
		prevScriptVersion = out.Version
		amount = dcrutil.Amount(txDetails.MsgTx.TxOut[output.Index].Value)
		return nil
	})
	if err != nil {
		w.lockedOutpointMu.Unlock()
		return errors.E(op, err)
	}
	w.lockedOutpoints[outpoint{output.Hash, output.Index}] = struct{}{}
	w.lockedOutpointMu.Unlock()

	defer func() {
		w.lockedOutpointMu.Lock()
		delete(w.lockedOutpoints, outpoint{output.Hash, output.Index})
		w.lockedOutpointMu.Unlock()
	}()

	var i int
	var count uint32
	var mixValue, remValue, changeValue dcrutil.Amount
	var feeRate = w.RelayFee()
	var smallestMixChange = smallestMixChange(feeRate)
SplitPoints:
	for i = 0; i < len(splitPoints); i++ {
		last := i == len(splitPoints)-1
		mixValue = splitPoints[i]

		// When the sdiff is more than this mixed output amount, there
		// is a smaller common mixed amount with more pairing activity
		// (due to CoinShuffle++ participation from ticket buyers).
		// Skipping this amount and moving to the next smallest common
		// mixed amount will result in quicker pairings, or pairings
		// occurring at all.  The number of mixed outputs is capped to
		// prevent a single mix being overwhelmingly funded by a single
		// output, and to conserve memory resources.
		if !last && mixValue >= sdiff {
			continue
		}

		count = uint32(amount / mixValue)
		if count > 4 {
			count = 4
		}
		for ; count > 0; count-- {
			remValue = amount - dcrutil.Amount(count)*mixValue
			if remValue < 0 {
				continue
			}

			// Determine required fee and change value, if possible.
			// No change is ever included when mixing at the
			// smallest amount.
			const P2PKHv0Len = 25
			inScriptSizes := []int{txsizes.RedeemP2PKHSigScriptSize}
			outScriptSizes := make([]int, count)
			for i := range outScriptSizes {
				outScriptSizes[i] = P2PKHv0Len
			}
			size := txsizes.EstimateSerializeSizeFromScriptSizes(
				inScriptSizes, outScriptSizes, P2PKHv0Len)
			fee := txrules.FeeForSerializeSize(feeRate, size)
			changeValue = remValue - fee
			if last {
				changeValue = 0
			}
			if changeValue <= 0 {
				// Determine required fee without a change
				// output.  A lower mix count or amount is
				// required if the fee is still not payable.
				size = txsizes.EstimateSerializeSizeFromScriptSizes(
					inScriptSizes, outScriptSizes, 0)
				fee = txrules.FeeForSerializeSize(feeRate, size)
				if remValue < fee {
					continue
				}
				changeValue = 0
			}
			if changeValue < smallestMixChange {
				changeValue = 0
			}

			break SplitPoints
		}
	}
	if i == len(splitPoints) {
		err := errors.Errorf("output %v (%v): %w", output, amount, errNoSplitDenomination)
		return errors.E(op, err)
	}
	select {
	case <-ctx.Done():
		return errors.E(op, ctx.Err())
	case w.mixSems.splitSems[i] <- struct{}{}:
		defer func() { <-w.mixSems.splitSems[i] }()
	default:
		return errThrottledMixRequest
	}

	var change *wire.TxOut
	var updates []func(walletdb.ReadWriteTx) error
	if changeValue > 0 {
		persist := w.deferPersistReturnedChild(ctx, &updates)
		const accountName = "" // not used, so can be faked.
		addr, err := w.nextAddress(ctx, op, persist,
			accountName, changeAccount, udb.InternalBranch, WithGapPolicyIgnore())
		if err != nil {
			return errors.E(op, err)
		}
		version, changeScript := addr.PaymentScript()
		change = &wire.TxOut{
			Value:    int64(changeValue),
			PkScript: changeScript,
			Version:  version,
		}
	}

	log.Infof("Mixing output %v (%v)", output, amount)

	gen := w.makeGen(ctx, mixAccount, mixBranch)
	expires := w.dicemixExpiry(ctx)
	cj := mixclient.NewCoinJoin(gen, change, int64(mixValue), expires, count)
	input := &wire.TxIn{
		PreviousOutPoint: *output,
		ValueIn:          int64(amount),
	}
	err = w.addCoinJoinInput(ctx, cj, input, prevScript, prevScriptVersion)
	if err != nil {
		return errors.E(op, err)
	}

	err = w.mixClient.Dicemix(ctx, cryptorand.Reader, cj)
	if err != nil {
		return errors.E(op, err)
	}

	tx := cj.Tx()
	cjHash := tx.TxHash()
	log.Infof("Completed CoinShuffle++ mix of output %v in transaction %v", output, &cjHash)
	return nil
}

// MixAccount individually mixes outputs of an account into standard
// denominations, creating newly mixed outputs for a mixed account.
//
// Due to performance concerns of timing out in a CoinShuffle++ run, this
// function may throttle how many of the outputs are mixed each call.
func (w *Wallet) MixAccount(ctx context.Context, changeAccount, mixAccount,
	mixBranch uint32) error {
	const op errors.Op = "wallet.MixAccount"

	// Mixing requests require wallet mixing support.
	if !w.mixing {
		s := "wallet mixing support is disabled"
		return errors.E(op, errors.Invalid, s)
	}

	_, tipHeight := w.MainChainTip(ctx)
	w.lockedOutpointMu.Lock()
	var credits []Input
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		const minconf = 2
		const targetAmount = 0
		var minAmount = splitPoints[len(splitPoints)-1]
		var maxResults = cap(w.mixSems.splitSems[0]) * len(splitPoints)
		credits, err = w.findEligibleOutputsAmount(dbtx, changeAccount, minconf,
			targetAmount, tipHeight, minAmount, maxResults)
		return err
	})
	if err != nil {
		w.lockedOutpointMu.Unlock()
		return errors.E(op, err)
	}
	w.lockedOutpointMu.Unlock()

	var g errgroup.Group
	for i := range credits {
		op := &credits[i].OutPoint
		g.Go(func() error {
			err := w.MixOutput(ctx, op, changeAccount, mixAccount, mixBranch)
			if errors.Is(err, errThrottledMixRequest) {
				return nil
			}
			if errors.Is(err, errNoSplitDenomination) {
				return nil
			}
			if errors.Is(err, socks.ErrPoolMaxConnections) {
				return nil
			}
			return err
		})
	}
	err = g.Wait()
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

// PossibleCoinJoin tests if a transaction may be a CSPP-mixed transaction.
// It can return false positives, as one can create a tx which looks like a
// coinjoin tx, although it isn't.
func PossibleCoinJoin(tx *wire.MsgTx) (isMix bool, mixDenom int64, mixCount uint32) {
	if len(tx.TxOut) < 3 || len(tx.TxIn) < 3 {
		return false, 0, 0
	}

	numberOfOutputs := len(tx.TxOut)
	numberOfInputs := len(tx.TxIn)

	mixedOuts := make(map[int64]uint32)
	scripts := make(map[string]int)
	for _, o := range tx.TxOut {
		scripts[string(o.PkScript)]++
		if scripts[string(o.PkScript)] > 1 {
			return false, 0, 0
		}
		val := o.Value
		// Multiple zero valued outputs do not count as a coinjoin mix.
		if val == 0 {
			continue
		}
		mixedOuts[val]++
	}

	for val, count := range mixedOuts {
		if count < 3 {
			continue
		}
		if val > mixDenom {
			mixDenom = val
			mixCount = count
		}

		outputsWithNotSameAmount := uint32(numberOfOutputs) - count
		if outputsWithNotSameAmount > uint32(numberOfInputs) {
			return false, 0, 0
		}
	}

	isMix = mixCount >= uint32(len(tx.TxOut)/2)
	return
}

// AcceptMixMessage adds a mixing message received from the network backend to
// the wallet's mixpool.
func (w *Wallet) AcceptMixMessage(msg mixing.Message) error {
	_, err := w.mixpool.AcceptMessage(msg)
	if err != nil {
		return err
	}

	return nil
}
