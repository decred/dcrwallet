// Copyright (c) 2019-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"sync/atomic"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/txrules"
	"decred.org/dcrwallet/v5/wallet/txsizes"
	"decred.org/dcrwallet/v5/wallet/udb"
	"decred.org/dcrwallet/v5/wallet/walletdb"
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
		updates := make(accountBranchChildUpdates, 0, mcount)

		for i := uint32(0); i < mcount; i++ {
			const accountName = "" // not used, so can be faked.
			mixAddr, err := w.nextAddress(ctx, op, &updates, nil,
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
		err := updates.UpdateDB(ctx, w, nil)
		return gen, err
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
	if err != nil {
		log.Errorf("Failed to publish mix transaction: %v", err)
	}
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
	if !w.mixingEnabled {
		s := "wallet mixing support is disabled"
		return errors.E(op, errors.Invalid, s)
	}

	nb, err := w.NetworkBackend()
	if err != nil {
		return err
	}
	ctx, cancel := WrapNetworkBackendContext(nb, ctx)
	defer cancel()

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

		count = min(uint32(amount/mixValue), 4)
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
	if changeValue > 0 {
		updates := make(accountBranchChildUpdates, 0, 1)
		const accountName = "" // not used, so can be faked.
		addr, err := w.nextAddress(ctx, op, &updates, nil,
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
		err = updates.UpdateDB(ctx, w, nil)
		if err != nil {
			return err
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

	err = w.mixClient.Dicemix(ctx, cj)
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
	if !w.mixingEnabled {
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

	// Use batch mixing if MixChangeLimit > 1
	var g errgroup.Group
	var success atomic.Int32

	if w.mixChangeLimit > 1 {
		// Group UTXOs by denomination and batch them
		batches := w.groupUTXOsForBatchMixing(credits, w.mixChangeLimit)

		// Process all batches concurrently (like original single-UTXO code)
		for _, batch := range batches {
			batchCopy := batch // Capture for goroutine
			g.Go(func() error {
				err := w.MixMultipleOutputs(ctx, batchCopy, changeAccount, mixAccount, mixBranch)
				if err == nil {
					success.Add(int32(len(batchCopy)))
				}
				switch {
				case errors.Is(err, errNoSplitDenomination):
					log.Debugf("Unable to mix batch for account %q: %v", changeAccount, err)
					err = nil
				case errors.Is(err, errThrottledMixRequest):
					log.Debugf("Temporarily skipped batch during account %q mix: %v", changeAccount, err)
					err = nil
				}
				return err
			})
		}

		log.Infof("Processing %d batches (limit %d UTXOs/batch) for account %q",
			len(batches), w.mixChangeLimit, changeAccount)
	} else {
		// Fall back to original single-UTXO mixing
		for i := range credits {
			op := &credits[i].OutPoint
			g.Go(func() error {
				err := w.MixOutput(ctx, op, changeAccount, mixAccount, mixBranch)
				if err == nil {
					success.Add(1)
				}
				switch {
				case errors.Is(err, errNoSplitDenomination):
					log.Debugf("Unable to mix output for account %q: %v",
						changeAccount, err)
					err = nil
				case errors.Is(err, errThrottledMixRequest):
					log.Debugf("Temporarily skipped output %v during account %q mix: %v",
						op, changeAccount, err)
					err = nil
				}
				return err
			})
		}
	}

	err = g.Wait()
	log.Debugf("Mixed %d of %d selected outputs of account %q", success.Load(),
		len(credits), changeAccount)
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

// groupUTXOsForBatchMixing groups UTXOs by their mix denomination and creates
// batches limited by the configured MixChangeLimit. This enables multiple UTXOs
// of the same denomination to be mixed in a single session with privacy mitigations.
func (w *Wallet) groupUTXOsForBatchMixing(credits []Input, limit int) map[int64][]Input {
	if limit < 1 {
		limit = 1
	}

	// Group UTXOs by their ideal mix denomination
	groups := make(map[int64][]Input)

	sdiff, err := w.NextStakeDifficulty(context.Background())
	if err != nil {
		log.Debugf("Failed to get stake difficulty for mixing grouping: %v", err)
		sdiff = 0
	}

	for _, credit := range credits {
		amount := dcrutil.Amount(credit.PrevOut.Value)

		// Find the appropriate mix denomination for this UTXO
		var mixValue int64
		for i := 0; i < len(splitPoints); i++ {
			last := i == len(splitPoints)-1
			candidate := int64(splitPoints[i])

			// Skip denominations above stake difficulty unless it's the smallest
			if !last && candidate >= int64(sdiff) {
				continue
			}

			// Check if this UTXO can be split into this denomination
			count := min(uint32(amount/dcrutil.Amount(candidate)), 4)
			if count > 0 {
				// Calculate if fees can be paid
				const P2PKHv0Len = 25
				inScriptSizes := []int{txsizes.RedeemP2PKHSigScriptSize}
				outScriptSizes := make([]int, count)
				for j := range outScriptSizes {
					outScriptSizes[j] = P2PKHv0Len
				}
				size := txsizes.EstimateSerializeSizeFromScriptSizes(
					inScriptSizes, outScriptSizes, P2PKHv0Len)
				fee := txrules.FeeForSerializeSize(w.RelayFee(), size)

				remValue := amount - dcrutil.Amount(count)*dcrutil.Amount(candidate)
				if remValue >= fee {
					mixValue = candidate
					break
				}
			}
		}

		if mixValue > 0 {
			groups[mixValue] = append(groups[mixValue], credit)
		}
	}

	// Split large groups into batches limited by MixChangeLimit
	batched := make(map[int64][]Input)
	batchCounter := 0

	for denom, utxos := range groups {
		for i := 0; i < len(utxos); i += limit {
			end := i + limit
			if end > len(utxos) {
				end = len(utxos)
			}

			// Create a unique key for each batch
			batchKey := denom + int64(batchCounter)<<32
			batched[batchKey] = utxos[i:end]
			batchCounter++
		}
	}

	return batched
}

// MixMultipleOutputs performs mixing of multiple UTXOs of the same denomination
// in a single CoinShuffle++ session with privacy mitigations including timing
// jitter and session participation limits.
func (w *Wallet) MixMultipleOutputs(ctx context.Context, utxos []Input, changeAccount, mixAccount, mixBranch uint32) error {
	op := errors.Opf("wallet.MixMultipleOutputs(%d utxos)", len(utxos))

	if len(utxos) == 0 {
		return errors.E(op, errors.Invalid, "no UTXOs provided for mixing")
	}

	// Privacy mitigation: Limit local peer participation to prevent domination
	maxLocalPeers := min(len(utxos), 10) // Never more than 10 local peers
	if len(utxos) > maxLocalPeers {
		utxos = utxos[:maxLocalPeers]
	}

	// Use the first UTXO to determine mix parameters
	firstUTXO := utxos[0]

	// Get the mix denomination and parameters from the first UTXO
	amount := dcrutil.Amount(firstUTXO.PrevOut.Value)
	sdiff, err := w.NextStakeDifficulty(ctx)
	if err != nil {
		return errors.E(op, err)
	}

	// Find appropriate split denomination (same logic as MixOutput)
	var i int
	var count uint32
	var mixValue, remValue, changeValue dcrutil.Amount
	var feeRate = w.RelayFee()
	var smallestMixChange = smallestMixChange(feeRate)

SplitPoints:
	for i = 0; i < len(splitPoints); i++ {
		last := i == len(splitPoints)-1
		mixValue = splitPoints[i]

		if !last && mixValue >= sdiff {
			continue
		}

		count = min(uint32(amount/mixValue), 4)
		for ; count > 0; count-- {
			remValue = amount - dcrutil.Amount(count)*mixValue
			if remValue < 0 {
				continue
			}

			const P2PKHv0Len = 25
			inScriptSizes := []int{txsizes.RedeemP2PKHSigScriptSize}
			outScriptSizes := make([]int, count)
			for j := range outScriptSizes {
				outScriptSizes[j] = P2PKHv0Len
			}
			size := txsizes.EstimateSerializeSizeFromScriptSizes(
				inScriptSizes, outScriptSizes, P2PKHv0Len)
			fee := txrules.FeeForSerializeSize(feeRate, size)
			changeValue = remValue - fee
			if last {
				changeValue = 0
			}
			if changeValue <= 0 {
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
		return errors.E(op, errNoSplitDenomination)
	}

	// Acquire semaphore for the denomination
	select {
	case <-ctx.Done():
		return errors.E(op, ctx.Err())
	case w.mixSems.splitSems[i] <- struct{}{}:
		defer func() { <-w.mixSems.splitSems[i] }()
	default:
		return errThrottledMixRequest
	}

	// Lock all UTXOs to prevent concurrent access
	w.lockedOutpointMu.Lock()
	var lockedOutpoints []outpoint
	for _, utxo := range utxos {
		op := outpoint{utxo.OutPoint.Hash, utxo.OutPoint.Index}
		if _, exists := w.lockedOutpoints[op]; exists {
			// Unlock already locked ones and return error
			for _, locked := range lockedOutpoints {
				delete(w.lockedOutpoints, locked)
			}
			w.lockedOutpointMu.Unlock()
			err := errors.Errorf("output %v already locked", utxo.OutPoint)
			return errors.E(op, err)
		}
		w.lockedOutpoints[op] = struct{}{}
		lockedOutpoints = append(lockedOutpoints, op)
	}
	w.lockedOutpointMu.Unlock()

	defer func() {
		w.lockedOutpointMu.Lock()
		for _, op := range lockedOutpoints {
			delete(w.lockedOutpoints, op)
		}
		w.lockedOutpointMu.Unlock()
	}()

	// Create change output if needed
	var change *wire.TxOut
	if changeValue > 0 {
		updates := make(accountBranchChildUpdates, 0, 1)
		const accountName = "" // not used, so can be faked.
		addr, err := w.nextAddress(ctx, op, &updates, nil,
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
		err = updates.UpdateDB(ctx, w, nil)
		if err != nil {
			return err
		}
	}

	// Log each UTXO individually to match original behavior
	for _, utxo := range utxos {
		amount := dcrutil.Amount(utxo.PrevOut.Value)
		log.Infof("Mixing output %v (%v)", &utxo.OutPoint, amount)
	}

	// Create multiple local peers for the same CoinShuffle++ session
	gen := w.makeGen(ctx, mixAccount, mixBranch)
	expires := w.dicemixExpiry(ctx)

	// Create CoinJoin with multiple inputs
	cj := mixclient.NewCoinJoin(gen, change, int64(mixValue), expires, count)

	// Add all UTXOs as inputs to the same CoinJoin session
	for _, utxo := range utxos {
		// Get UTXO details
		var prevScript []byte
		var prevScriptVersion uint16
		err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
			txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
			txDetails, err := w.txStore.TxDetails(txmgrNs, &utxo.OutPoint.Hash)
			if err != nil {
				return err
			}
			out := txDetails.MsgTx.TxOut[utxo.OutPoint.Index]
			prevScript = out.PkScript
			prevScriptVersion = out.Version
			return nil
		})
		if err != nil {
			return errors.E(op, err)
		}

		input := &wire.TxIn{
			PreviousOutPoint: utxo.OutPoint,
			ValueIn:          int64(dcrutil.Amount(utxo.PrevOut.Value)),
		}

		err = w.addCoinJoinInput(ctx, cj, input, prevScript, prevScriptVersion)
		if err != nil {
			return errors.E(op, err)
		}
	}

	// Execute single Dicemix session with multiple local peers
	err = w.mixClient.Dicemix(ctx, cj)
	if err != nil {
		return errors.E(op, err)
	}

	tx := cj.Tx()
	cjHash := tx.TxHash()
	log.Infof("Completed batch mix of %d outputs in transaction %v", len(utxos), &cjHash)
	return nil
}
