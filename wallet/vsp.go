// Copyright (c) 2023-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/udb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
	vspd "github.com/decred/vspd/client/v4"
)

type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type VSPPolicy struct {
	MaxFee     dcrutil.Amount
	ChangeAcct uint32 // to derive fee addresses
	FeeAcct    uint32 // to pay fees from, if inputs are not provided to Process
}

type VSPClient struct {
	wallet *Wallet
	policy *VSPPolicy
	*vspd.Client

	mu   sync.Mutex
	jobs map[chainhash.Hash]*vspFeePayment

	log slog.Logger
}

type VSPClientConfig struct {
	// URL specifies the base URL of the VSP
	URL string

	// PubKey specifies the VSP's base64 encoded public key
	PubKey string

	// Default policy for fee payments unless another is provided by the
	// caller.
	Policy *VSPPolicy
}

func (w *Wallet) NewVSPClient(cfg VSPClientConfig, log slog.Logger, dialer DialFunc) (*VSPClient, error) {
	if cfg.URL == "" {
		return nil, errors.New("vsp url can not be null")
	}

	if cfg.PubKey == "" {
		return nil, errors.New("vsp pubkey can not be null")
	}

	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	pubKey, err := base64.StdEncoding.DecodeString(cfg.PubKey)
	if err != nil {
		return nil, err
	}

	client := &vspd.Client{
		URL:    u.String(),
		PubKey: pubKey,
		Sign:   w.SignMessage,
		Log:    log,
	}
	client.Transport = &http.Transport{
		DialContext: dialer,
	}

	v := &VSPClient{
		wallet: w,
		policy: cfg.Policy,
		Client: client,
		jobs:   make(map[chainhash.Hash]*vspFeePayment),
		log:    log,
	}
	return v, nil
}

func (c *VSPClient) FeePercentage(ctx context.Context) (float64, error) {
	resp, err := c.Client.VspInfo(ctx)
	if err != nil {
		return -1, err
	}
	return resp.FeePercentage, nil
}

// ProcessUnprocessedTickets adds the provided tickets to the client. Noop if
// a given ticket is already added.
func (c *VSPClient) ProcessUnprocessedTickets(ctx context.Context, tickets []*VSPTicket) {
	var wg sync.WaitGroup

	for _, ticket := range tickets {
		c.mu.Lock()
		fp := c.jobs[*ticket.Hash()]
		c.mu.Unlock()
		if fp != nil {
			// Already processing this ticket with the VSP.
			continue
		}

		// Start processing in the background.
		wg.Add(1)
		go func(t *VSPTicket) {
			defer wg.Done()
			err := c.Process(ctx, t, nil)
			if err != nil {
				c.log.Error(err)
			}
		}(ticket)
	}

	wg.Wait()
}

// ProcessManagedTickets adds the provided tickets to the client and resumes
// their fee payment process. Noop if a given ticket is already added, or if the
// ticket is not registered with the VSP. This is used to recover VSP tracking
// after seed restores.
func (c *VSPClient) ProcessManagedTickets(ctx context.Context, tickets []*VSPTicket) error {
	for _, ticket := range tickets {
		hash := ticket.Hash()
		c.mu.Lock()
		_, ok := c.jobs[*hash]
		c.mu.Unlock()
		if ok {
			// Already processing this ticket with the VSP.
			continue
		}

		// Make ticketstatus api call and only continue if ticket is
		// found managed by this vsp.  The rest is the same codepath as
		// for processing a new ticket.
		status, err := c.status(ctx, ticket)
		if err != nil {
			if errors.Is(err, errors.Locked) {
				return err
			}
			continue
		}

		if status.FeeTxStatus == "confirmed" {
			feeHash, err := chainhash.NewHashFromStr(status.FeeTxHash)
			if err != nil {
				return err
			}
			err = ticket.UpdateFeeConfirmed(ctx, *feeHash, c.Client.URL, c.Client.PubKey)
			if err != nil {
				return err
			}
			return nil
		} else if status.FeeTxHash != "" {
			feeHash, err := chainhash.NewHashFromStr(status.FeeTxHash)
			if err != nil {
				return err
			}
			err = ticket.UpdateFeePaid(ctx, *feeHash, c.Client.URL, c.Client.PubKey)
			if err != nil {
				return err
			}
			_ = c.feePayment(ctx, ticket, true)
		} else {
			// Fee hasn't been paid at the provided VSP, so this should do that if needed.
			_ = c.feePayment(ctx, ticket, false)
		}

	}

	return nil
}

// Process begins processing a VSP fee payment for a ticket.  If feeTx contains
// inputs, is used to pay the VSP fee.  Otherwise, new inputs are selected and
// locked to prevent double spending the fee.
//
// feeTx may be nil or may point to an empty transaction. It is modified with
// the inputs and the fee and change outputs before returning without an error.
// The fee transaction is also recorded as unpublised in the wallet, and the fee
// hash is associated with the ticket.
func (c *VSPClient) Process(ctx context.Context, ticket *VSPTicket, feeTx *wire.MsgTx) error {
	vspTicket, err := ticket.VSPTicketInfo(ctx)
	if err != nil && !errors.Is(err, errors.NotExist) {
		return err
	}
	feeStatus := udb.VSPFeeProcessStarted // Will be used if the ticket isn't registered to the vsp yet.
	if vspTicket != nil {
		feeStatus = udb.FeeStatus(vspTicket.FeeTxStatus)
	}

	switch feeStatus {
	case udb.VSPFeeProcessStarted, udb.VSPFeeProcessErrored:
		// If VSPTicket has been started or errored then attempt to create a new fee
		// transaction, submit it then confirm.
		fp := c.feePayment(ctx, ticket, false)
		if fp == nil {
			err := ticket.UpdateFeeErrored(ctx, c.Client.URL, c.Client.PubKey)
			if err != nil {
				return err
			}
			return fmt.Errorf("fee payment cannot be processed")
		}
		fp.mu.Lock()
		if fp.feeTx == nil {
			fp.feeTx = feeTx
		}
		fp.mu.Unlock()
		err := fp.receiveFeeAddress()
		if err != nil {
			err := ticket.UpdateFeeErrored(ctx, c.Client.URL, c.Client.PubKey)
			if err != nil {
				return err
			}
			// XXX, retry? (old Process retried)
			// but this may not be necessary any longer as the parent of
			// the ticket is always relayed to the vsp as well.
			return err
		}
		err = fp.makeFeeTx(feeTx)
		if err != nil {
			err := ticket.UpdateFeeErrored(ctx, c.Client.URL, c.Client.PubKey)
			if err != nil {
				return err
			}
			return err
		}
		return fp.submitPayment()
	case udb.VSPFeeProcessPaid:
		// If a VSP ticket has been paid, but confirm payment.
		if len(vspTicket.Host) > 0 && vspTicket.Host != c.Client.URL {
			// Cannot confirm a paid ticket that is already with another VSP.
			return fmt.Errorf("ticket already paid or confirmed with another vsp")
		}
		fp := c.feePayment(ctx, ticket, true)
		if fp == nil {
			// Don't update VSPStatus to Errored if it was already paid or
			// confirmed.
			return fmt.Errorf("fee payment cannot be processed")
		}

		return fp.confirmPayment()
	case udb.VSPFeeProcessConfirmed:
		// VSPTicket has already been confirmed, there is nothing to process.
		return nil
	}
	return nil
}

// SetVoteChoice takes the provided consensus, tspend and treasury key voting
// preferences, and checks if they match the status of the specified ticket from
// the connected VSP. The status provides the current voting preferences so we
// can just update from there if need be.
func (c *VSPClient) SetVoteChoice(ctx context.Context, ticket *VSPTicket,
	choices map[string]string, tspendPolicy map[string]string, treasuryPolicy map[string]string) error {

	// Retrieve current voting preferences from VSP.
	status, err := c.status(ctx, ticket)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return err
		}
		c.log.Errorf("Could not check status of VSP ticket %s: %v", ticket, err)
		return nil
	}

	// Check for any mismatch between the provided voting preferences and the
	// VSP preferences to determine if VSP needs to be updated.
	update := false

	// Check consensus vote choices.
	for newAgenda, newChoice := range choices {
		vspChoice, ok := status.VoteChoices[newAgenda]
		if !ok {
			update = true
			break
		}
		if vspChoice != newChoice {
			update = true
			break
		}
	}

	// Check tspend policies.
	for newTSpend, newChoice := range tspendPolicy {
		vspChoice, ok := status.TSpendPolicy[newTSpend]
		if !ok {
			update = true
			break
		}
		if vspChoice != newChoice {
			update = true
			break
		}
	}

	// Check treasury policies.
	for newKey, newChoice := range treasuryPolicy {
		vspChoice, ok := status.TSpendPolicy[newKey]
		if !ok {
			update = true
			break
		}
		if vspChoice != newChoice {
			update = true
			break
		}
	}

	if !update {
		c.log.Debugf("VSP already has correct vote choices for ticket %s", ticket)
		return nil
	}

	c.log.Debugf("Updating vote choices on VSP for ticket %s", ticket)
	err = c.setVoteChoices(ctx, ticket, choices, tspendPolicy, treasuryPolicy)
	if err != nil {
		return err
	}
	return nil
}

// VSPTicketInfo stores per-ticket info tracked by a VSP Client instance.
type VSPTicketInfo struct {
	TicketHash     chainhash.Hash
	CommitmentAddr stdaddr.StakeAddress
	VotingAddr     stdaddr.StakeAddress
	State          State
	Fee            dcrutil.Amount
	FeeHash        chainhash.Hash

	// TODO: include stuff returned by the status() call?
}

// TrackedTickets returns information about all outstanding tickets tracked by
// a vsp.Client instance.
//
// Currently this returns only info about tickets which fee hasn't been paid or
// confirmed at enough depth to be considered committed to.
func (c *VSPClient) TrackedTickets() []*VSPTicketInfo {
	// Collect all jobs first, to avoid working under two different locks.
	c.mu.Lock()
	jobs := make([]*vspFeePayment, 0, len(c.jobs))
	for _, job := range c.jobs {
		jobs = append(jobs, job)
	}
	c.mu.Unlock()

	tickets := make([]*VSPTicketInfo, 0, len(jobs))
	for _, job := range jobs {
		job.mu.Lock()
		tickets = append(tickets, &VSPTicketInfo{
			TicketHash:     *job.ticket.Hash(),
			CommitmentAddr: job.ticket.CommitmentAddr(),
			VotingAddr:     job.ticket.VotingAddr(),
			State:          job.state,
			Fee:            job.fee,
			FeeHash:        job.feeHash,
		})
		job.mu.Unlock()
	}

	return tickets
}
