// Copyright (c) 2019-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"net"
	"time"

	"decred.org/cspp/v2"
	"decred.org/cspp/v2/coinjoin"
	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/wallet/txrules"
	"decred.org/dcrwallet/v3/wallet/txsizes"
	"decred.org/dcrwallet/v3/wallet/udb"
	"decred.org/dcrwallet/v3/wallet/walletdb"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
	"github.com/decred/go-socks/socks"
	"golang.org/x/sync/errgroup"
)

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

// DialFunc provides a method to dial a network connection.
// If the dialed network connection is secured by TLS, TLS
// configuration is provided by the method, not the caller.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

func (w *Wallet) MixOutput(ctx context.Context, dialTLS DialFunc, csppserver string, output *wire.OutPoint, changeAccount, mixAccount, mixBranch uint32) error {
	op := errors.Opf("wallet.MixOutput(%v)", output)

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
	var amount dcrutil.Amount
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)
		txDetails, err := w.txStore.TxDetails(txmgrNs, &output.Hash)
		if err != nil {
			return err
		}
		prevScript = txDetails.MsgTx.TxOut[output.Index].PkScript
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

	var i, count int
	var mixValue, remValue, changeValue dcrutil.Amount
	var feeRate = w.RelayFee()
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

		count = int(amount / mixValue)
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
			if txrules.IsDustAmount(changeValue, P2PKHv0Len, feeRate) {
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

	const (
		txVersion = 1
		locktime  = 0
		expiry    = 0
	)
	pairing := coinjoin.EncodeDesc(coinjoin.P2PKHv0, int64(mixValue), txVersion, locktime, expiry)
	ses, err := cspp.NewSession(rand.Reader, debugLog, pairing, count)
	if err != nil {
		return errors.E(op, err)
	}
	var conn net.Conn
	if dialTLS != nil {
		conn, err = dialTLS(ctx, "tcp", csppserver)
	} else {
		conn, err = tls.Dial("tcp", csppserver, nil)
	}
	if err != nil {
		return errors.E(op, err)
	}
	defer conn.Close()
	log.Infof("Dialed CSPPServer %v -> %v", conn.LocalAddr(), conn.RemoteAddr())

	log.Infof("Mixing output %v (%v)", output, amount)
	cj := w.newCsppJoin(ctx, change, mixValue, mixAccount, mixBranch, count)
	cj.addTxIn(prevScript, &wire.TxIn{
		PreviousOutPoint: *output,
		ValueIn:          int64(amount),
	})
	err = ses.DiceMix(ctx, conn, cj)
	if err != nil {
		return errors.E(op, err)
	}
	cjHash := cj.tx.TxHash()
	log.Infof("Completed CoinShuffle++ mix of output %v in transaction %v", output, &cjHash)

	var watch []wire.OutPoint
	w.lockedOutpointMu.Lock()
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, f := range updates {
			if err := f(dbtx); err != nil {
				return err
			}
		}
		rec, err := udb.NewTxRecordFromMsgTx(cj.tx, time.Now())
		if err != nil {
			return errors.E(op, err)
		}
		watch, err = w.processTransactionRecord(ctx, dbtx, rec, nil, nil)
		if err != nil {
			return err
		}
		return nil
	})
	w.lockedOutpointMu.Unlock()
	if err != nil {
		return errors.E(op, err)
	}
	n, _ := w.NetworkBackend()
	if n != nil {
		err = w.publishAndWatch(ctx, op, n, cj.tx, watch)
	}
	return err
}

// MixAccount individually mixes outputs of an account into standard
// denominations, creating newly mixed outputs for a mixed account.
//
// Due to performance concerns of timing out in a CoinShuffle++ run, this
// function may throttle how many of the outputs are mixed each call.
func (w *Wallet) MixAccount(ctx context.Context, dialTLS DialFunc, csppserver string, changeAccount, mixAccount, mixBranch uint32) error {
	const op errors.Op = "wallet.MixAccount"

	_, tipHeight := w.MainChainTip(ctx)
	w.lockedOutpointMu.Lock()
	var credits []Input
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		const minconf = 1
		const targetAmount = 0
		var minAmount = splitPoints[len(splitPoints)-1]
		const maxResults = 32
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
			err := w.MixOutput(ctx, dialTLS, csppserver, op, changeAccount, mixAccount, mixBranch)
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
