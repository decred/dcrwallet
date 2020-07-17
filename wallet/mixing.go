// Copyright (c) 2019-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"net"

	"decred.org/cspp"
	"decred.org/cspp/coinjoin"
	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/wallet/txauthor"
	"decred.org/dcrwallet/wallet/txrules"
	"decred.org/dcrwallet/wallet/txsizes"
	"decred.org/dcrwallet/wallet/udb"
	"decred.org/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrd/dcrutil/v3"
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

var splitSems = [len(splitPoints)]chan struct{}{}

func init() {
	for i := range splitSems {
		splitSems[i] = make(chan struct{}, 10)
	}
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

	var updates []func(walletdb.ReadWriteTx) error

	hold, err := w.holdUnlock()
	if err != nil {
		return errors.E(op, err)
	}
	defer hold.release()

	w.lockedOutpointMu.Lock()
	if _, exists := w.lockedOutpoints[*output]; exists {
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
	w.lockedOutpoints[*output] = struct{}{}
	w.lockedOutpointMu.Unlock()

	defer func() {
		w.lockedOutpointMu.Lock()
		delete(w.lockedOutpoints, *output)
		w.lockedOutpointMu.Unlock()
	}()

	var count int
	var mixValue, remValue dcrutil.Amount
	for i, v := range splitPoints {
		// When the sdiff is more than four times this mixed output
		// amount, there is a smaller common mixed amount with more
		// pairing activity (due to CoinShuffle++ participation from
		// ticket buyers).  Skipping this amount and moving to the next
		// smallest common mixed amount will result in quicker pairings,
		// or pairings occurring at all.  The number of mixed outputs is
		// capped to prevent a single mix being overwhelmingly funded by
		// a single output, and to conserve memory resources.
		if i != len(splitPoints)-1 && 4*v >= sdiff {
			continue
		}
		count = int(amount / v)
		if count > 4 {
			count = 4
		}
		if count > 0 {
			remValue = amount - dcrutil.Amount(count)*v
			mixValue = v
			select {
			case <-ctx.Done():
				return errors.E(op, ctx.Err())
			case splitSems[i] <- struct{}{}:
				defer func() { <-splitSems[i] }()
			default:
				return errThrottledMixRequest
			}
			break
		}
	}
	if mixValue == splitPoints[len(splitPoints)-1] {
		remValue = 0
	}
	if mixValue == 0 {
		err := errors.Errorf("output %v (%v): %w", output, amount, errNoSplitDenomination)
		return errors.E(op, err)
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

	// Create change output from remaining value and contributed fee
	const P2PKHv0Len = 25
	feeRate := w.RelayFee()
	inScriptSizes := []int{txsizes.RedeemP2PKHSigScriptSize}
	outScriptSizes := make([]int, count)
	for i := range outScriptSizes {
		outScriptSizes[i] = P2PKHv0Len
	}
	size := txsizes.EstimateSerializeSizeFromScriptSizes(inScriptSizes, outScriptSizes, P2PKHv0Len)
	changeValue := remValue - txrules.FeeForSerializeSize(feeRate, size)
	var change *wire.TxOut
	if !txrules.IsDustAmount(changeValue, P2PKHv0Len, feeRate) {
		persist := w.deferPersistReturnedChild(ctx, &updates)
		const accountName = "" // not used, so can be faked.
		addr, err := w.nextAddress(ctx, op, persist,
			accountName, changeAccount, udb.InternalBranch, WithGapPolicyIgnore())
		if err != nil {
			return errors.E(op, err)
		}
		changeScript, version, err := addressScript(addr)
		if err != nil {
			return errors.E(op, err)
		}
		change = &wire.TxOut{
			Value:    int64(changeValue),
			PkScript: changeScript,
			Version:  version,
		}
	}

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

	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, f := range updates {
			if err := f(dbtx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}

	return nil
}

// MixAccount individually mixes outputs of an account into standard
// denominations, creating newly mixed outputs for a mixed account.
//
// Due to performance concerns of timing out in a CoinShuffle++ run, this
// function may throttle how many of the outputs are mixed each call.
func (w *Wallet) MixAccount(ctx context.Context, dialTLS DialFunc, csppserver string, changeAccount, mixAccount, mixBranch uint32) error {
	const op errors.Op = "wallet.MixAccount"

	hold, err := w.holdUnlock()
	if err != nil {
		return errors.E(op, err)
	}
	defer hold.release()

	_, tipHeight := w.MainChainTip(ctx)
	w.lockedOutpointMu.Lock()
	var credits []Input
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		credits, err = w.findEligibleOutputs(dbtx, changeAccount, 1, tipHeight)
		return err
	})
	w.lockedOutpointMu.Unlock()
	if err != nil {
		return errors.E(op, err)
	}
	unlockedCredits := credits[:0]
	for i := range credits {
		amount := dcrutil.Amount(credits[i].PrevOut.Value)
		if amount <= splitPoints[len(splitPoints)-1] {
			continue
		}
		unlockedCredits = append(unlockedCredits, credits[i])
	}
	credits = unlockedCredits
	shuffle(len(credits), func(i, j int) {
		credits[i], credits[j] = credits[j], credits[i]
	})
	if len(credits) > 32 { // simple throttle
		credits = credits[:32]
	}
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

// randomInputSource wraps an InputSource to randomly pick UTXOs.
// This involves reading all UTXOs from the underlying source into memory.
func randomInputSource(source txauthor.InputSource) txauthor.InputSource {
	all, err := source(dcrutil.MaxAmount)
	if err == nil {
		shuffleUTXOs(all)
	}
	var n int
	var tot dcrutil.Amount
	return func(target dcrutil.Amount) (*txauthor.InputDetail, error) {
		if err != nil {
			return nil, err
		}
		if all.Amount <= target {
			return all, nil
		}
		for n < len(all.Inputs) {
			tot += dcrutil.Amount(all.Inputs[n].ValueIn)
			n++
			if tot >= target {
				break
			}
		}
		selected := &txauthor.InputDetail{
			Amount:            tot,
			Inputs:            all.Inputs[:n],
			Scripts:           all.Scripts[:n],
			RedeemScriptSizes: all.RedeemScriptSizes[:n],
		}
		return selected, nil
	}
}
