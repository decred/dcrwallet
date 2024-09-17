// Copyright (c) 2023-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/crypto/rand"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/vspd/types/v3"
)

var (
	errStopped = errors.New("fee processing stopped")
)

// A random amount of delay (between zero and these jitter constants) is added
// before performing some background action with the VSP.  The delay is reduced
// when a ticket is currently live, as it may be called to vote any time.
const (
	immatureJitter = time.Hour
	liveJitter     = 5 * time.Minute
	unminedJitter  = 2 * time.Minute
)

type vspFeePayment struct {
	client *VSPClient
	ctx    context.Context

	// Set at feePayment creation and never changes
	ticket *VSPTicket
	policy *VSPPolicy

	// Requires locking for all access outside of Client.feePayment
	mu      sync.Mutex
	fee     dcrutil.Amount
	feeAddr stdaddr.Address
	feeHash chainhash.Hash
	feeTx   *wire.MsgTx
	state   State
	err     error

	timerMu sync.Mutex
	timer   *time.Timer

	params *chaincfg.Params
}

type State uint32

const (
	_ State = iota
	Unprocessed
	FeePublished
	_ // ...
	TicketSpent
)

func (fp *vspFeePayment) removedExpiredOrSpent() bool {
	var reason string
	switch {
	case fp.ticket.Expired(fp.ctx):
		reason = "expired"
	case fp.ticket.Spent(fp.ctx):
		reason = "spent"
	}
	if reason != "" {
		fp.remove(reason)
		// nothing scheduled
		return true
	}
	return false
}

func (fp *vspFeePayment) remove(reason string) {
	fp.stop()
	fp.client.log.Infof("Ticket %v is %s; removing from VSP client", fp.ticket, reason)
	fp.client.mu.Lock()
	delete(fp.client.jobs, *fp.ticket.Hash())
	fp.client.mu.Unlock()
}

// feePayment returns an existing managed fee payment, or creates and begins
// processing a fee payment for a ticket.
func (c *VSPClient) feePayment(ctx context.Context, ticket *VSPTicket, paidConfirmed bool) (fp *vspFeePayment) {
	ticketHash := ticket.Hash()
	c.mu.Lock()
	fp = c.jobs[*ticketHash]
	c.mu.Unlock()
	if fp != nil {
		return fp
	}

	defer func() {
		if fp == nil {
			return
		}
		var schedule bool
		c.mu.Lock()
		fp2 := c.jobs[*ticketHash]
		if fp2 != nil {
			fp.stop()
			fp = fp2
		} else {
			c.jobs[*ticketHash] = fp
			schedule = true
		}
		c.mu.Unlock()
		if schedule {
			fp.schedule("reconcile payment", fp.reconcilePayment)
		}
	}()

	fp = &vspFeePayment{
		client: c,
		ctx:    context.Background(),
		ticket: ticket,
		policy: c.policy,
		params: c.wallet.chainParams,
	}

	// No VSP interaction is required for spent tickets.
	if fp.ticket.Spent(ctx) {
		fp.state = TicketSpent
		return fp
	}

	feeHash, err := ticket.FeeHash(ctx)
	if err != nil {
		// caller must schedule next method, as paying the fee may
		// require using provided transaction inputs.
		return fp
	}

	fee, err := ticket.FeeTx(ctx)
	if err != nil {
		// A fee hash is recorded for this ticket, but was not found in
		// the wallet.  This should not happen and may require manual
		// intervention.
		//
		// XXX should check ticketinfo and see if fee is not paid. if
		// possible, update it with a new fee.
		fp.err = fmt.Errorf("fee transaction not found in wallet: %w", err)
		return fp
	}

	fp.feeTx = fee
	fp.feeHash = feeHash

	// If database has been updated to paid or confirmed status, we can forgo
	// this step.
	if !paidConfirmed {
		err = fp.ticket.UpdateFeeStarted(ctx, feeHash, c.Client.URL, c.Client.PubKey)
		if err != nil {
			return fp
		}

		fp.state = Unprocessed // XXX fee created, but perhaps not submitted with vsp.
		fp.fee = -1            // XXX fee amount (not needed anymore?)
	}
	return fp
}

// Schedule a method to be executed.
// Any currently-scheduled method is replaced.
func (fp *vspFeePayment) schedule(name string, method func() error) {
	var delay time.Duration
	if method != nil {
		delay = fp.next()
	}

	fp.timerMu.Lock()
	defer fp.timerMu.Unlock()
	if fp.timer != nil {
		fp.timer.Stop()
		fp.timer = nil
	}
	if method != nil {
		fp.client.log.Debugf("Scheduling %q for ticket %s in %v", name, fp.ticket, delay)
		fp.timer = time.AfterFunc(delay, fp.task(name, method))
	}
}

func (fp *vspFeePayment) next() time.Duration {
	w := fp.client.wallet
	_, tipHeight := w.MainChainTip(fp.ctx)

	ticketLive := fp.ticket.LiveHeight(fp.ctx)
	ticketExpires := fp.ticket.ExpiryHeight(fp.ctx)

	var jitter time.Duration
	switch {
	case tipHeight < ticketLive: // immature, mined ticket
		blocksUntilLive := ticketLive - tipHeight
		jitter = fp.params.TargetTimePerBlock * time.Duration(blocksUntilLive)
		if jitter > immatureJitter {
			jitter = immatureJitter
		}
	case tipHeight < ticketExpires: // live ticket
		jitter = liveJitter
	default: // unmined ticket
		jitter = unminedJitter
	}

	return rand.Duration(jitter)
}

// task returns a function running a feePayment method.
// If the method errors, the error is logged, and the payment is put
// in an errored state and may require manual processing.
func (fp *vspFeePayment) task(name string, method func() error) func() {
	return func() {
		err := method()
		fp.mu.Lock()
		fp.err = err
		fp.mu.Unlock()
		if err != nil {
			fp.client.log.Errorf("Ticket %v: %v: %v", fp.ticket, name, err)
		}
	}
}

func (fp *vspFeePayment) stop() {
	fp.schedule("", nil)
}

func (fp *vspFeePayment) receiveFeeAddress() error {
	ctx := fp.ctx

	// stop processing if ticket is expired or spent
	if fp.removedExpiredOrSpent() {
		// nothing scheduled
		return errStopped
	}

	// Fetch ticket and its parent transaction (typically, a split
	// transaction).
	ticketHex, err := marshalTx(fp.ticket.RawTx())
	if err != nil {
		return err
	}
	parentHex, err := marshalTx(fp.ticket.ParentTx())
	if err != nil {
		return err
	}

	req := types.FeeAddressRequest{
		Timestamp:  time.Now().Unix(),
		TicketHash: fp.ticket.Hash().String(),
		TicketHex:  ticketHex,
		ParentHex:  parentHex,
	}

	resp, err := fp.client.FeeAddress(ctx, req, fp.ticket.CommitmentAddr())
	if err != nil {
		return err
	}

	feeAmount := dcrutil.Amount(resp.FeeAmount)
	feeAddr, err := stdaddr.DecodeAddress(resp.FeeAddress, fp.params)
	if err != nil {
		return fmt.Errorf("server fee address invalid: %w", err)
	}

	fp.client.log.Infof("VSP requires fee %v", feeAmount)
	if feeAmount > fp.policy.MaxFee {
		return fmt.Errorf("server fee amount too high: %v > %v",
			feeAmount, fp.policy.MaxFee)
	}

	// XXX validate server timestamp?

	fp.mu.Lock()
	fp.fee = feeAmount
	fp.feeAddr = feeAddr
	fp.mu.Unlock()

	return nil
}

// makeFeeTx adds outputs to tx to pay a VSP fee, optionally adding inputs as
// well to fund the transaction if no input value is already provided in the
// transaction.
//
// If tx is nil, fp.feeTx may be assigned or modified, but the pointer will not
// be dereferenced.
func (fp *vspFeePayment) makeFeeTx(tx *wire.MsgTx) error {
	ctx := fp.ctx
	w := fp.client.wallet

	fp.mu.Lock()
	fee := fp.fee
	fpFeeTx := fp.feeTx
	feeAddr := fp.feeAddr
	fp.mu.Unlock()

	// The rest of this function will operate on the tx pointer, with fp.feeTx
	// assigned to the result on success.
	// Update tx to use the partially created fpFeeTx if any has been started.
	// The transaction pointed to by the caller will be dereferenced and modified
	// when non-nil.
	if fpFeeTx != nil {
		if tx != nil {
			*tx = *fpFeeTx
		} else {
			tx = fpFeeTx
		}
	}
	// Fee transaction with outputs is already finished.
	if fpFeeTx != nil && len(fpFeeTx.TxOut) != 0 {
		return nil
	}
	// When both transactions are nil, create a new empty transaction.
	if tx == nil {
		tx = wire.NewMsgTx()
	}

	// XXX fp.fee == -1?
	if fee == 0 {
		err := fp.receiveFeeAddress()
		if err != nil {
			return err
		}
		fp.mu.Lock()
		fee = fp.fee
		feeAddr = fp.feeAddr
		fp.mu.Unlock()
	}

	err := w.CreateVspPayment(ctx, tx, fee, feeAddr, fp.policy.FeeAcct, fp.policy.ChangeAcct)
	if err != nil {
		return fmt.Errorf("unable to create VSP fee tx for ticket %v: %w", fp.ticket, err)
	}

	feeHash := tx.TxHash()
	err = fp.ticket.UpdateFeePaid(ctx, feeHash, fp.client.URL, fp.client.PubKey)
	if err != nil {
		return err
	}

	fp.mu.Lock()
	fp.feeTx = tx
	fp.feeHash = feeHash
	fp.mu.Unlock()

	// nothing scheduled
	return nil
}

func (c *VSPClient) status(ctx context.Context, ticket *VSPTicket) (*types.TicketStatusResponse, error) {

	req := types.TicketStatusRequest{
		TicketHash: ticket.Hash().String(),
	}

	resp, err := c.Client.TicketStatus(ctx, req, ticket.CommitmentAddr())
	if err != nil {
		return nil, err
	}

	// XXX validate server timestamp?

	return resp, nil
}

func (c *VSPClient) setVoteChoices(ctx context.Context, ticket *VSPTicket,
	choices map[string]string, tspendPolicy map[string]string, treasuryPolicy map[string]string) error {

	req := types.SetVoteChoicesRequest{
		Timestamp:      time.Now().Unix(),
		TicketHash:     ticket.Hash().String(),
		VoteChoices:    choices,
		TSpendPolicy:   tspendPolicy,
		TreasuryPolicy: treasuryPolicy,
	}

	_, err := c.Client.SetVoteChoices(ctx, req, ticket.CommitmentAddr())
	if err != nil {
		return err
	}

	// XXX validate server timestamp?

	return nil
}

func (fp *vspFeePayment) reconcilePayment() error {
	ctx := fp.ctx
	w := fp.client.wallet

	// stop processing if ticket is expired or spent
	// XXX if ticket is no longer saved by wallet (because the tx expired,
	// or was double spent, etc) remove the fee payment.
	if fp.removedExpiredOrSpent() {
		// nothing scheduled
		return errStopped
	}

	// A fee amount and address must have been created by this point.
	// Ensure that the fee transaction can be created, otherwise reschedule
	// this method until it is.  There is no need to check the wallet for a
	// fee transaction matching a known hash; this is performed when
	// creating the feePayment.
	fp.mu.Lock()
	feeTx := fp.feeTx
	fp.mu.Unlock()
	if feeTx == nil || len(feeTx.TxOut) == 0 {
		err := fp.makeFeeTx(nil)
		if err != nil {
			var apiErr types.ErrorResponse
			if errors.As(err, &apiErr) && apiErr.Code == types.ErrTicketCannotVote {
				fp.remove("ticket cannot vote")
			}
			return err
		}
	}

	// A fee address has been obtained, and the fee transaction has been
	// created, but it is unknown if the VSP has received the fee and will
	// vote using the ticket.
	//
	// If the fee is mined, then check the status of the ticket and payment
	// with the VSP, to ensure that it has marked the fee payment as paid.
	//
	// If the fee is not mined, an API call with the VSP is used so it may
	// receive and publish the transaction.  A follow up on the ticket
	// status is scheduled for some time in the future.

	err := fp.submitPayment()
	fp.mu.Lock()
	feeHash := fp.feeHash
	fp.mu.Unlock()
	var apiErr types.ErrorResponse
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case types.ErrFeeAlreadyReceived:
			err = w.SetPublished(ctx, &feeHash, true)
			if err != nil {
				return err
			}
			err = fp.ticket.UpdateFeePaid(ctx, feeHash, fp.client.URL, fp.client.PubKey)
			if err != nil {
				return err
			}
			err = nil
		case types.ErrInvalidFeeTx, types.ErrCannotBroadcastFee:
			err := fp.ticket.UpdateFeeErrored(ctx, fp.client.URL, fp.client.PubKey)
			if err != nil {
				return err
			}
			// Attempt to create a new fee transaction
			fp.mu.Lock()
			fp.feeHash = chainhash.Hash{}
			fp.feeTx = nil
			fp.mu.Unlock()
			// err not nilled, so reconcile payment is rescheduled.
		}
	}
	if err != nil {
		// Nothing left to try except trying again.
		fp.schedule("reconcile payment", fp.reconcilePayment)
		return err
	}

	err = fp.ticket.UpdateFeePaid(ctx, feeHash, fp.client.URL, fp.client.PubKey)
	if err != nil {
		return err
	}

	return fp.confirmPayment()

	/*
		// XXX? for each input, c.Wallet.UnlockOutpoint(&outpoint.Hash, outpoint.Index)
		// xxx, or let the published tx replace the unpublished one, and unlock
		// outpoints as it is processed.

	*/
}

func (fp *vspFeePayment) submitPayment() (err error) {
	ctx := fp.ctx
	w := fp.client.wallet

	// stop processing if ticket is expired or spent
	if fp.removedExpiredOrSpent() {
		// nothing scheduled
		return errStopped
	}

	// submitting a payment requires the fee tx to already be created.
	fp.mu.Lock()
	feeTx := fp.feeTx
	fp.mu.Unlock()
	if feeTx == nil {
		feeTx = new(wire.MsgTx)
	}
	if len(feeTx.TxOut) == 0 {
		err := fp.makeFeeTx(feeTx)
		if err != nil {
			return err
		}
	}

	// Retrieve voting preferences
	voteChoices, err := fp.ticket.AgendaChoices(ctx)
	if err != nil {
		return err
	}

	feeTxHex, err := marshalTx(feeTx)
	if err != nil {
		return err
	}

	req := types.PayFeeRequest{
		Timestamp:      time.Now().Unix(),
		TicketHash:     fp.ticket.Hash().String(),
		FeeTx:          feeTxHex,
		VotingKey:      fp.ticket.VotingKey(),
		VoteChoices:    voteChoices,
		TSpendPolicy:   fp.ticket.TSpendPolicy(),
		TreasuryPolicy: fp.ticket.TreasuryKeyPolicy(),
	}

	_, err = fp.client.PayFee(ctx, req, fp.ticket.CommitmentAddr())
	if err != nil {
		var apiErr types.ErrorResponse
		if errors.As(err, &apiErr) && apiErr.Code == types.ErrFeeExpired {
			// Fee has been expired, so abandon current feetx, set fp.feeTx
			// to nil and retry submit payment to make a new fee tx.
			feeHash := feeTx.TxHash()
			err := w.AbandonTransaction(ctx, &feeHash)
			if err != nil {
				fp.client.log.Errorf("error abandoning expired fee tx %v", err)
			}
			fp.mu.Lock()
			fp.feeTx = nil
			fp.mu.Unlock()
		}
		return fmt.Errorf("payfee: %w", err)
	}

	// TODO - validate server timestamp?

	fp.client.log.Infof("successfully processed %v", fp.ticket)
	return nil
}

// confirmPayment will remove the fee payment processing when the fee has
// reached sufficient confirmations, and reschedule itself if the fee is not
// confirmed yet.  If the fee tx is ever removed from the wallet, this will
// schedule another reconcile.
func (fp *vspFeePayment) confirmPayment() (err error) {
	ctx := fp.ctx

	// stop processing if ticket is expired or spent
	if fp.removedExpiredOrSpent() {
		// nothing scheduled
		return errStopped
	}

	defer func() {
		if err != nil && !errors.Is(err, errStopped) {
			fp.schedule("reconcile payment", fp.reconcilePayment)
		}
	}()

	status, err := fp.client.status(ctx, fp.ticket)
	if err != nil {
		fp.client.log.Warnf("Rescheduling status check for %v: %v", fp.ticket, err)
		fp.schedule("confirm payment", fp.confirmPayment)
		return nil
	}

	switch status.FeeTxStatus {
	case "received":
		// VSP has received the fee tx but has not yet broadcast it.
		// VSP will only broadcast the tx when ticket has 6+ confirmations.
		fp.schedule("confirm payment", fp.confirmPayment)
		return nil
	case "broadcast":
		fp.client.log.Infof("VSP has successfully sent the fee tx for %v", fp.ticket)
		// Broadcasted, but not confirmed.
		fp.schedule("confirm payment", fp.confirmPayment)
		return nil
	case "confirmed":
		fp.remove("confirmed by VSP")
		// nothing scheduled
		fp.mu.Lock()
		feeHash := fp.feeHash
		fp.mu.Unlock()
		err = fp.ticket.UpdateFeeConfirmed(ctx, feeHash, fp.client.URL, fp.client.PubKey)
		if err != nil {
			return err
		}
		return nil
	case "error":
		fp.client.log.Warnf("VSP failed to broadcast feetx for %v -- restarting payment",
			fp.ticket)
		fp.schedule("reconcile payment", fp.reconcilePayment)
		return nil
	default:
		// XXX put in unknown state
		fp.client.log.Warnf("VSP responded with unknown FeeTxStatus %q for %v",
			status.FeeTxStatus, fp.ticket)
	}

	return nil
}

func marshalTx(tx *wire.MsgTx) (string, error) {
	var buf bytes.Buffer
	buf.Grow(tx.SerializeSize() * 2)
	err := tx.Serialize(hex.NewEncoder(&buf))
	return buf.String(), err
}
