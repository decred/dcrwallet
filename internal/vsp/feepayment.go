package vsp

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/internal/uniformprng"
	"decred.org/dcrwallet/v3/rpc/client/dcrd"
	"decred.org/dcrwallet/v3/wallet"
	"decred.org/dcrwallet/v3/wallet/txrules"
	"decred.org/dcrwallet/v3/wallet/txsizes"
	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/txscript/v4/stdscript"
	"github.com/decred/dcrd/wire"
)

var prng lockedRand

type lockedRand struct {
	mu   sync.Mutex
	rand *uniformprng.Source
}

func (r *lockedRand) int63n(n int64) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Int63n(n)
}

// duration returns a random time.Duration in [0,d) with uniform distribution.
func (r *lockedRand) duration(d time.Duration) time.Duration {
	return time.Duration(r.int63n(int64(d)))
}

func (r *lockedRand) coinflip() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rand.Uint32n(2) == 0
}

func init() {
	source, err := uniformprng.RandSource(cryptorand.Reader)
	if err != nil {
		panic(err)
	}
	prng = lockedRand{
		rand: source,
	}
}

var (
	errStopped = errors.New("fee processing stopped")
	errNotSolo = errors.New("not a solo ticket")
)

// A random amount of delay (between zero and these jitter constants) is added
// before performing some background action with the VSP.  The delay is reduced
// when a ticket is currently live, as it may be called to vote any time.
const (
	immatureJitter = time.Hour
	liveJitter     = 5 * time.Minute
	unminedJitter  = 2 * time.Minute
)

type feePayment struct {
	client *Client
	ctx    context.Context

	// Set at feepayment creation and never changes
	ticketHash     chainhash.Hash
	commitmentAddr stdaddr.StakeAddress
	votingAddr     stdaddr.StakeAddress
	policy         Policy

	// Requires locking for all access outside of Client.feePayment
	mu            sync.Mutex
	votingKey     string
	ticketLive    int32
	ticketExpires int32
	fee           dcrutil.Amount
	feeAddr       stdaddr.Address
	feeHash       chainhash.Hash
	feeTx         *wire.MsgTx
	state         state
	err           error

	timerMu sync.Mutex
	timer   *time.Timer
}

type state uint32

const (
	_ state = iota
	unprocessed
	feePublished
	_ // ...
	ticketSpent
)

func parseTicket(ticket *wire.MsgTx, params *chaincfg.Params) (
	votingAddr, commitmentAddr stdaddr.StakeAddress, err error) {
	fail := func(err error) (_, _ stdaddr.StakeAddress, _ error) {
		return nil, nil, err
	}
	if !stake.IsSStx(ticket) {
		return fail(fmt.Errorf("%v is not a ticket", ticket))
	}
	_, addrs := stdscript.ExtractAddrs(ticket.TxOut[0].Version, ticket.TxOut[0].PkScript, params)
	if len(addrs) != 1 {
		return fail(fmt.Errorf("cannot parse voting addr"))
	}
	switch addr := addrs[0].(type) {
	case stdaddr.StakeAddress:
		votingAddr = addr
	default:
		return fail(fmt.Errorf("address cannot be used for voting rights: %v", err))
	}
	commitmentAddr, err = stake.AddrFromSStxPkScrCommitment(ticket.TxOut[1].PkScript, params)
	if err != nil {
		return fail(fmt.Errorf("cannot parse commitment address: %w", err))
	}
	return
}

func (fp *feePayment) ticketSpent() bool {
	ctx := fp.ctx
	ticketOut := wire.OutPoint{Hash: fp.ticketHash, Index: 0, Tree: 1}
	_, _, err := fp.client.Wallet.Spender(ctx, &ticketOut)
	return err == nil
}

func (fp *feePayment) ticketExpired() bool {
	ctx := fp.ctx
	w := fp.client.Wallet
	_, tipHeight := w.MainChainTip(ctx)

	fp.mu.Lock()
	expires := fp.ticketExpires
	fp.mu.Unlock()

	return expires > 0 && tipHeight >= expires
}

func (fp *feePayment) removedExpiredOrSpent() bool {
	var reason string
	switch {
	case fp.ticketExpired():
		reason = "expired"
	case fp.ticketSpent():
		reason = "spent"
	}
	if reason != "" {
		fp.remove(reason)
		// nothing scheduled
		return true
	}
	return false
}

func (fp *feePayment) remove(reason string) {
	fp.stop()
	log.Infof("ticket %v is %s; removing from VSP client", &fp.ticketHash, reason)
	fp.client.mu.Lock()
	delete(fp.client.jobs, fp.ticketHash)
	fp.client.mu.Unlock()
}

// feePayment returns an existing managed fee payment, or creates and begins
// processing a fee payment for a ticket.
func (c *Client) feePayment(ctx context.Context, ticketHash *chainhash.Hash, policy Policy, paidConfirmed bool) (fp *feePayment) {
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

	w := c.Wallet
	params := w.ChainParams()

	fp = &feePayment{
		client:     c,
		ctx:        ctx,
		ticketHash: *ticketHash,
		policy:     policy,
	}

	// No VSP interaction is required for spent tickets.
	if fp.ticketSpent() {
		fp.state = ticketSpent
		return fp
	}

	ticket, err := c.tx(ctx, ticketHash)
	if err != nil {
		log.Warnf("no ticket found for %v", ticketHash)
		return nil
	}

	_, ticketHeight, err := w.TxBlock(ctx, ticketHash)
	if err != nil {
		// This is not expected to ever error, as the ticket was fetched
		// from the wallet in the above call.
		log.Errorf("failed to query block which mines ticket: %v", err)
		return nil
	}
	if ticketHeight >= 2 {
		// Note the off-by-one; this is correct.  Tickets become live
		// one block after the params would indicate.
		fp.ticketLive = ticketHeight + int32(params.TicketMaturity) + 1
		fp.ticketExpires = fp.ticketLive + int32(params.TicketExpiry)
	}

	fp.votingAddr, fp.commitmentAddr, err = parseTicket(ticket, params)
	if err != nil {
		log.Errorf("%v is not a ticket: %v", ticketHash, err)
		return nil
	}
	// Try to access the voting key, ignore error unless the wallet is
	// locked.
	fp.votingKey, err = w.DumpWIFPrivateKey(ctx, fp.votingAddr)
	if err != nil && !errors.Is(err, errors.Locked) {
		log.Errorf("no voting key for ticket %v: %v", ticketHash, err)
		return nil
	}
	feeHash, err := w.VSPFeeHashForTicket(ctx, ticketHash)
	if err != nil {
		// caller must schedule next method, as paying the fee may
		// require using provided transaction inputs.
		return fp
	}

	fee, err := c.tx(ctx, &feeHash)
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
		err = w.UpdateVspTicketFeeToStarted(ctx, ticketHash, &feeHash, c.client.url, c.client.pub)
		if err != nil {
			return fp
		}

		fp.state = unprocessed // XXX fee created, but perhaps not submitted with vsp.
		fp.fee = -1            // XXX fee amount (not needed anymore?)
	}
	return fp
}

func (c *Client) tx(ctx context.Context, hash *chainhash.Hash) (*wire.MsgTx, error) {
	txs, _, err := c.Wallet.GetTransactionsByHashes(ctx, []*chainhash.Hash{hash})
	if err != nil {
		return nil, err
	}
	return txs[0], nil
}

// Schedule a method to be executed.
// Any currently-scheduled method is replaced.
func (fp *feePayment) schedule(name string, method func() error) {
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
		log.Debugf("scheduling %q for ticket %s in %v", name, &fp.ticketHash, delay)
		fp.timer = time.AfterFunc(delay, fp.task(name, method))
	}
}

func (fp *feePayment) next() time.Duration {
	w := fp.client.Wallet
	params := w.ChainParams()
	_, tipHeight := w.MainChainTip(fp.ctx)

	fp.mu.Lock()
	ticketLive := fp.ticketLive
	ticketExpires := fp.ticketExpires
	fp.mu.Unlock()

	var jitter time.Duration
	switch {
	case tipHeight < ticketLive: // immature, mined ticket
		blocksUntilLive := ticketExpires - tipHeight
		jitter = params.TargetTimePerBlock * time.Duration(blocksUntilLive)
		if jitter > immatureJitter {
			jitter = immatureJitter
		}
	case tipHeight < ticketExpires: // live ticket
		jitter = liveJitter
	default: // unmined ticket
		jitter = unminedJitter
	}

	return prng.duration(jitter)
}

// task returns a function running a feePayment method.
// If the method errors, the error is logged, and the payment is put
// in an errored state and may require manual processing.
func (fp *feePayment) task(name string, method func() error) func() {
	return func() {
		err := method()
		fp.mu.Lock()
		fp.err = err
		fp.mu.Unlock()
		if err != nil {
			log.Errorf("ticket %v: %v: %v", &fp.ticketHash, name, err)
		}
	}
}

func (fp *feePayment) stop() {
	fp.schedule("", nil)
}

func (fp *feePayment) receiveFeeAddress() error {
	ctx := fp.ctx
	w := fp.client.Wallet
	params := w.ChainParams()

	// stop processing if ticket is expired or spent
	if fp.removedExpiredOrSpent() {
		// nothing scheduled
		return errStopped
	}

	// Fetch ticket and its parent transaction (typically, a split
	// transaction).
	ticket, err := fp.client.tx(ctx, &fp.ticketHash)
	if err != nil {
		return fmt.Errorf("failed to retrieve ticket: %w", err)
	}
	parentHash := &ticket.TxIn[0].PreviousOutPoint.Hash
	parent, err := fp.client.tx(ctx, parentHash)
	if err != nil {
		return fmt.Errorf("failed to retrieve parent %v of ticket: %w",
			parentHash, err)
	}

	var response struct {
		Timestamp  int64  `json:"timestamp"`
		FeeAddress string `json:"feeaddress"`
		FeeAmount  int64  `json:"feeamount"`
		Request    []byte `json:"request"`
	}
	requestBody, err := json.Marshal(&struct {
		Timestamp  int64          `json:"timestamp"`
		TicketHash string         `json:"tickethash"`
		TicketHex  json.Marshaler `json:"tickethex"`
		ParentHex  json.Marshaler `json:"parenthex"`
	}{
		Timestamp:  time.Now().Unix(),
		TicketHash: fp.ticketHash.String(),
		TicketHex:  txMarshaler(ticket),
		ParentHex:  txMarshaler(parent),
	})
	if err != nil {
		return err
	}
	err = fp.client.post(ctx, "/api/v3/feeaddress", fp.commitmentAddr, &response,
		json.RawMessage(requestBody))
	if err != nil {
		return err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, response.Request) {
		return fmt.Errorf("server response has differing request: %#v != %#v",
			requestBody, response.Request)
	}

	feeAmount := dcrutil.Amount(response.FeeAmount)
	feeAddr, err := stdaddr.DecodeAddress(response.FeeAddress, params)
	if err != nil {
		return fmt.Errorf("server fee address invalid: %w", err)
	}

	log.Infof("VSP requires fee %v", feeAmount)
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
func (fp *feePayment) makeFeeTx(tx *wire.MsgTx) error {
	ctx := fp.ctx
	w := fp.client.Wallet

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

	// Reserve new outputs to pay the fee if outputs have not already been
	// reserved.  This will the the case for fee payments that were begun on
	// already purchased tickets, where the caller did not ensure that fee
	// outputs would already be reserved.
	if len(tx.TxIn) == 0 {
		const minconf = 1
		inputs, err := w.ReserveOutputsForAmount(ctx, fp.policy.FeeAcct, fee, minconf)
		if err != nil {
			return fmt.Errorf("unable to reserve enough output value to "+
				"pay VSP fee for ticket %v: %w", fp.ticketHash, err)
		}
		for _, in := range inputs {
			tx.AddTxIn(wire.NewTxIn(&in.OutPoint, in.PrevOut.Value, nil))
		}
		// The transaction will be added to the wallet in an unpublished
		// state, so there is no need to leave the outputs locked.
		defer func() {
			for _, in := range inputs {
				w.UnlockOutpoint(&in.OutPoint.Hash, in.OutPoint.Index)
			}
		}()
	}

	var input int64
	for _, in := range tx.TxIn {
		input += in.ValueIn
	}
	if input < int64(fee) {
		err := fmt.Errorf("not enough input value to pay fee: %v < %v",
			dcrutil.Amount(input), fee)
		return err
	}

	vers, feeScript := feeAddr.PaymentScript()

	addr, err := w.NewChangeAddress(ctx, fp.policy.ChangeAcct)
	if err != nil {
		log.Warnf("failed to get new change address: %v", err)
		return err
	}
	var changeOut *wire.TxOut
	switch addr := addr.(type) {
	case wallet.Address:
		vers, script := addr.PaymentScript()
		changeOut = &wire.TxOut{PkScript: script, Version: vers}
	default:
		return fmt.Errorf("failed to convert '%T' to wallet.Address", addr)
	}

	tx.TxOut = append(tx.TxOut[:0], &wire.TxOut{
		Value:    int64(fee),
		Version:  vers,
		PkScript: feeScript,
	})
	feeRate := w.RelayFee()
	scriptSizes := make([]int, len(tx.TxIn))
	for i := range scriptSizes {
		scriptSizes[i] = txsizes.RedeemP2PKHSigScriptSize
	}
	est := txsizes.EstimateSerializeSize(scriptSizes, tx.TxOut, txsizes.P2PKHPkScriptSize)
	change := input
	change -= tx.TxOut[0].Value
	change -= int64(txrules.FeeForSerializeSize(feeRate, est))
	if !txrules.IsDustAmount(dcrutil.Amount(change), txsizes.P2PKHPkScriptSize, feeRate) {
		changeOut.Value = change
		tx.TxOut = append(tx.TxOut, changeOut)
		// randomize position
		if prng.coinflip() {
			tx.TxOut[0], tx.TxOut[1] = tx.TxOut[1], tx.TxOut[0]
		}
	}

	feeHash := tx.TxHash()

	// sign
	sigErrs, err := w.SignTransaction(ctx, tx, txscript.SigHashAll, nil, nil, nil)
	if err != nil || len(sigErrs) > 0 {
		log.Errorf("failed to sign transaction: %v", err)
		sigErrStr := ""
		for _, sigErr := range sigErrs {
			log.Errorf("\t%v", sigErr)
			sigErrStr = fmt.Sprintf("\t%v", sigErr) + " "
		}
		if err != nil {
			return err
		}
		return fmt.Errorf(sigErrStr)
	}

	err = w.SetPublished(ctx, &feeHash, false)
	if err != nil {
		return err
	}
	err = w.AddTransaction(ctx, tx, nil)
	if err != nil {
		return err
	}
	err = w.UpdateVspTicketFeeToPaid(ctx, &fp.ticketHash, &feeHash, fp.client.url, fp.client.pub)
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

type ticketStatus struct {
	Timestamp       int64             `json:"timestamp"`
	TicketConfirmed bool              `json:"ticketconfirmed"`
	FeeTxStatus     string            `json:"feetxstatus"`
	FeeTxHash       string            `json:"feetxhash"`
	VoteChoices     map[string]string `json:"votechoices"`
	TSpendPolicy    map[string]string `json:"tspendpolicy"`
	TreasuryPolicy  map[string]string `json:"treasurypolicy"`
	Request         []byte            `json:"request"`
}

func (c *Client) status(ctx context.Context, ticketHash *chainhash.Hash) (*ticketStatus, error) {
	w := c.Wallet
	params := w.ChainParams()

	ticketTx, err := c.tx(ctx, ticketHash)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve ticket %v: %w", ticketHash, err)
	}
	if len(ticketTx.TxOut) != 3 {
		return nil, fmt.Errorf("ticket %v has multiple commitments: %w", ticketHash, errNotSolo)
	}

	if !stake.IsSStx(ticketTx) {
		return nil, fmt.Errorf("%v is not a ticket", ticketHash)
	}
	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, params)
	if err != nil {
		return nil, fmt.Errorf("failed to extract commitment address from %v: %w",
			ticketHash, err)
	}

	var resp ticketStatus
	requestBody, err := json.Marshal(&struct {
		TicketHash string `json:"tickethash"`
	}{
		TicketHash: ticketHash.String(),
	})
	if err != nil {
		return nil, err
	}
	err = c.post(ctx, "/api/v3/ticketstatus", commitmentAddr, &resp,
		json.RawMessage(requestBody))
	if err != nil {
		return nil, err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, resp.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, resp.Request)
		return nil, fmt.Errorf("server response contains differing request")
	}

	// XXX validate server timestamp?

	return &resp, nil
}

func (c *Client) setVoteChoices(ctx context.Context, ticketHash *chainhash.Hash,
	choices []wallet.AgendaChoice, tspendPolicy map[string]string, treasuryPolicy map[string]string) error {
	w := c.Wallet
	params := w.ChainParams()

	ticketTx, err := c.tx(ctx, ticketHash)
	if err != nil {
		return fmt.Errorf("failed to retrieve ticket %v: %w", ticketHash, err)
	}

	if !stake.IsSStx(ticketTx) {
		return fmt.Errorf("%v is not a ticket", ticketHash)
	}
	if len(ticketTx.TxOut) != 3 {
		return fmt.Errorf("ticket %v has multiple commitments: %w", ticketHash, errNotSolo)
	}

	commitmentAddr, err := stake.AddrFromSStxPkScrCommitment(ticketTx.TxOut[1].PkScript, params)
	if err != nil {
		return fmt.Errorf("failed to extract commitment address from %v: %w",
			ticketHash, err)
	}

	agendaChoices := make(map[string]string, len(choices))

	// Prepare agenda choice
	for _, c := range choices {
		agendaChoices[c.AgendaID] = c.ChoiceID
	}

	var resp ticketStatus
	requestBody, err := json.Marshal(&struct {
		Timestamp      int64             `json:"timestamp"`
		TicketHash     string            `json:"tickethash"`
		VoteChoices    map[string]string `json:"votechoices"`
		TSpendPolicy   map[string]string `json:"tspendpolicy"`
		TreasuryPolicy map[string]string `json:"treasurypolicy"`
	}{
		Timestamp:      time.Now().Unix(),
		TicketHash:     ticketHash.String(),
		VoteChoices:    agendaChoices,
		TSpendPolicy:   tspendPolicy,
		TreasuryPolicy: treasuryPolicy,
	})
	if err != nil {
		return err
	}

	err = c.post(ctx, "/api/v3/setvotechoices", commitmentAddr, &resp,
		json.RawMessage(requestBody))
	if err != nil {
		return err
	}

	// verify initial request matches server
	if !bytes.Equal(requestBody, resp.Request) {
		log.Warnf("server response has differing request: %#v != %#v",
			requestBody, resp.Request)
		return fmt.Errorf("server response contains differing request")
	}

	// XXX validate server timestamp?

	return nil
}

func (fp *feePayment) reconcilePayment() error {
	ctx := fp.ctx
	w := fp.client.Wallet

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
			var apiErr *BadRequestError
			if errors.As(err, &apiErr) && apiErr.Code == codeTicketCannotVote {
				fp.remove("ticket cannot vote")
				// Attempt to Revoke Tickets, we're not returning any errors here
				// and just logging.
				n, err := w.NetworkBackend()
				if err != nil {
					log.Errorf("unable to get network backend for revoking tickets %v", err)
				} else {
					if rpc, ok := n.(*dcrd.RPC); ok {
						err := w.RevokeTickets(ctx, rpc)
						if err != nil {
							log.Errorf("cannot revoke vsp tickets %v", err)
						}
					}
				}
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
	var apiErr *BadRequestError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case codeFeeAlreadyReceived:
			err = w.SetPublished(ctx, &feeHash, true)
			if err != nil {
				return err
			}
			err = w.UpdateVspTicketFeeToPaid(ctx, &fp.ticketHash, &feeHash, fp.client.url, fp.client.pub)
			if err != nil {
				return err
			}
			err = nil
		case codeInvalidFeeTx, codeCannotBroadcastFee:
			err := w.UpdateVspTicketFeeToErrored(ctx, &fp.ticketHash, fp.client.url, fp.client.pub)
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

	err = w.UpdateVspTicketFeeToPaid(ctx, &fp.ticketHash, &feeHash, fp.client.url, fp.client.pub)
	if err != nil {
		return err
	}

	// confirmPayment will remove the fee payment processing when the fee
	// has reached sufficient confirmations, and reschedule itself if the
	// fee is not confirmed yet.  If the fee tx is ever removed from the
	// wallet, this will schedule another reconcile.
	return fp.confirmPayment()

	/*
		// XXX? for each input, c.Wallet.UnlockOutpoint(&outpoint.Hash, outpoint.Index)
		// xxx, or let the published tx replace the unpublished one, and unlock
		// outpoints as it is processed.

	*/
}

func (fp *feePayment) submitPayment() (err error) {
	ctx := fp.ctx
	w := fp.client.Wallet

	// stop processing if ticket is expired or spent
	if fp.removedExpiredOrSpent() {
		// nothing scheduled
		return errStopped
	}

	// submitting a payment requires the fee tx to already be created.
	fp.mu.Lock()
	feeTx := fp.feeTx
	votingKey := fp.votingKey
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
	if votingKey == "" {
		votingKey, err = w.DumpWIFPrivateKey(ctx, fp.votingAddr)
		if err != nil {
			return err
		}
		fp.mu.Lock()
		fp.votingKey = votingKey
		fp.mu.Unlock()
	}

	// Retrieve voting preferences
	voteChoices := make(map[string]string)
	agendaChoices, _, err := w.AgendaChoices(ctx, &fp.ticketHash)
	if err != nil {
		return err
	}
	for _, agendaChoice := range agendaChoices {
		voteChoices[agendaChoice.AgendaID] = agendaChoice.ChoiceID
	}

	var payfeeResponse struct {
		Timestamp int64  `json:"timestamp"`
		Request   []byte `json:"request"`
	}
	requestBody, err := json.Marshal(&struct {
		Timestamp      int64             `json:"timestamp"`
		TicketHash     string            `json:"tickethash"`
		FeeTx          json.Marshaler    `json:"feetx"`
		VotingKey      string            `json:"votingkey"`
		VoteChoices    map[string]string `json:"votechoices"`
		TSpendPolicy   map[string]string `json:"tspendpolicy"`
		TreasuryPolicy map[string]string `json:"treasurypolicy"`
	}{
		Timestamp:      time.Now().Unix(),
		TicketHash:     fp.ticketHash.String(),
		FeeTx:          txMarshaler(feeTx),
		VotingKey:      votingKey,
		VoteChoices:    voteChoices,
		TSpendPolicy:   w.TSpendPolicyForTicket(&fp.ticketHash),
		TreasuryPolicy: w.TreasuryKeyPolicyForTicket(&fp.ticketHash),
	})
	if err != nil {
		return err
	}
	err = fp.client.post(ctx, "/api/v3/payfee", fp.commitmentAddr,
		&payfeeResponse, json.RawMessage(requestBody))
	if err != nil {
		var apiErr *BadRequestError
		if errors.As(err, &apiErr) && apiErr.Code == codeFeeExpired {
			// Fee has been expired, so abandon current feetx, set fp.feeTx
			// to nil and retry submit payment to make a new fee tx.
			feeHash := feeTx.TxHash()
			err := w.AbandonTransaction(ctx, &feeHash)
			if err != nil {
				log.Errorf("error abandoning expired fee tx %v", err)
			}
			fp.feeTx = nil
		}
		return fmt.Errorf("payfee: %w", err)
	}

	// Check for matching original request.
	// This is signed by the VSP, and the signature
	// has already been checked above.
	if !bytes.Equal(requestBody, payfeeResponse.Request) {
		return fmt.Errorf("server response has differing request: %#v != %#v",
			requestBody, payfeeResponse.Request)
	}
	// TODO - validate server timestamp?

	log.Infof("successfully processed %v", fp.ticketHash)
	return nil
}

func (fp *feePayment) confirmPayment() (err error) {
	ctx := fp.ctx
	w := fp.client.Wallet

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

	status, err := fp.client.status(ctx, &fp.ticketHash)
	// Suppress log if the wallet is currently locked.
	if err != nil && !errors.Is(err, errors.Locked) {
		log.Warnf("Rescheduling status check for %v: %v", &fp.ticketHash, err)
	}
	if err != nil {
		// Stop processing if the status check cannot be performed, but
		// a significant amount of confirmations are observed on the fee
		// transaction.
		//
		// Otherwise, chedule another confirmation check, in case the
		// status API can be performed at a later time or more
		// confirmations are observed.
		fp.mu.Lock()
		feeHash := fp.feeHash
		fp.mu.Unlock()
		confs, err := w.TxConfirms(ctx, &feeHash)
		if err != nil {
			return err
		}
		if confs >= 6 {
			fp.remove("confirmed")
			err = w.UpdateVspTicketFeeToConfirmed(ctx, &fp.ticketHash, &feeHash, fp.client.url, fp.client.pub)
			if err != nil {
				return err
			}
			return nil
		}
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
		log.Infof("VSP has successfully sent the fee tx for %v", &fp.ticketHash)
		// Broadcasted, but not confirmed.
		fp.schedule("confirm payment", fp.confirmPayment)
		return nil
	case "confirmed":
		fp.remove("confirmed by VSP")
		// nothing scheduled
		err = w.UpdateVspTicketFeeToConfirmed(ctx, &fp.ticketHash, &fp.feeHash, fp.client.url, fp.client.pub)
		if err != nil {
			return err
		}
		return nil
	case "error":
		log.Warnf("VSP failed to broadcast feetx for %v -- restarting payment",
			&fp.ticketHash)
		fp.schedule("reconcile payment", fp.reconcilePayment)
		return nil
	default:
		// XXX put in unknown state
		log.Warnf("VSP responded with %v for %v", status.FeeTxStatus,
			&fp.ticketHash)
	}

	return nil
}

type marshaler struct {
	marshaled []byte
	err       error
}

func (m *marshaler) MarshalJSON() ([]byte, error) {
	return m.marshaled, m.err
}

func txMarshaler(tx *wire.MsgTx) json.Marshaler {
	var buf bytes.Buffer
	buf.Grow(2 + tx.SerializeSize()*2)
	buf.WriteByte('"')
	err := tx.Serialize(hex.NewEncoder(&buf))
	buf.WriteByte('"')
	return &marshaler{buf.Bytes(), err}
}
