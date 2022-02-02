package vsp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"

	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/wallet/udb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type Policy struct {
	MaxFee     dcrutil.Amount
	ChangeAcct uint32 // to derive fee addresses
	FeeAcct    uint32 // to pay fees from, if inputs are not provided to Process
}

type Client struct {
	Wallet *wallet.Wallet
	Policy Policy
	*client

	mu   sync.Mutex
	jobs map[chainhash.Hash]*feePayment
}

type Config struct {
	// URL specifies the base URL of the VSP
	URL string

	// PubKey specifies the VSP's base64 encoded public key
	PubKey string

	// Dialer specifies an optional dialer when connecting to the VSP.
	Dialer DialFunc

	// Wallet specifies a loaded wallet.
	Wallet *wallet.Wallet

	// Default policy for fee payments unless another is provided by the
	// caller.
	Policy Policy
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, err
	}
	pubKey, err := base64.StdEncoding.DecodeString(cfg.PubKey)
	if err != nil {
		return nil, err
	}
	if cfg.Wallet == nil {
		return nil, fmt.Errorf("wallet option not set")
	}

	client := newClient(u.String(), pubKey, cfg.Wallet)
	client.Transport = &http.Transport{
		DialContext: cfg.Dialer,
	}

	v := &Client{
		Wallet: cfg.Wallet,
		Policy: cfg.Policy,
		client: client,
		jobs:   make(map[chainhash.Hash]*feePayment),
	}
	return v, nil
}

func (c *Client) FeePercentage(ctx context.Context) (float64, error) {
	var resp struct {
		FeePercentage float64 `json:"feepercentage"`
	}
	err := c.get(ctx, "/api/v3/vspinfo", &resp)
	if err != nil {
		return -1, err
	}
	return resp.FeePercentage, nil
}

// ProcessUnprocessedTickets processes all tickets that don't currently have
// any association with a VSP.
func (c *Client) ProcessUnprocessedTickets(ctx context.Context, policy Policy) {
	var wg sync.WaitGroup
	c.Wallet.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Skip tickets which have a fee tx already associated with
		// them; they are already processed by some vsp.
		_, err := c.Wallet.VSPFeeHashForTicket(ctx, hash)
		if err == nil {
			return nil
		}
		confirmed, err := c.Wallet.IsVSPTicketConfirmed(ctx, hash)
		if err != nil && !errors.Is(err, errors.NotExist) {
			log.Error(err)
			return nil
		}

		if confirmed {
			return nil
		}

		c.mu.Lock()
		fp := c.jobs[*hash]
		c.mu.Unlock()
		if fp != nil {
			// Already processing this ticket with the VSP.
			return nil
		}

		// Start processing in the background.
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := c.Process(ctx, hash, nil)
			if err != nil {
				log.Error(err)
			}
		}()

		return nil
	})
	wg.Wait()
}

// ProcessTicket attempts to process a given ticket based on the hash provided.
func (c *Client) ProcessTicket(ctx context.Context, hash *chainhash.Hash) error {
	err := c.Process(ctx, hash, nil)
	if err != nil {
		return err
	}
	return nil
}

// ProcessManagedTickets discovers tickets which were previously registered with
// a VSP and begins syncing them in the background.  This is used to recover VSP
// tracking after seed restores, and is only performed on unspent and unexpired
// tickets.
func (c *Client) ProcessManagedTickets(ctx context.Context, policy Policy) error {
	err := c.Wallet.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// We only want to process tickets that haven't been confirmed yet.
		confirmed, err := c.Wallet.IsVSPTicketConfirmed(ctx, hash)
		if err != nil && !errors.Is(err, errors.NotExist) {
			log.Error(err)
			return nil
		}
		if confirmed {
			return nil
		}
		c.mu.Lock()
		_, ok := c.jobs[*hash]
		c.mu.Unlock()
		if ok {
			// Already processing this ticket with the VSP.
			return nil
		}

		// Make ticketstatus api call and only continue if ticket is
		// found managed by this vsp.  The rest is the same codepath as
		// for processing a new ticket.
		status, err := c.status(ctx, hash)
		if err != nil {
			if errors.Is(err, errors.Locked) {
				return err
			}
			return nil
		}

		if status.FeeTxStatus == "confirmed" {
			feeHash, err := chainhash.NewHashFromStr(status.FeeTxHash)
			if err != nil {
				return err
			}
			err = c.Wallet.UpdateVspTicketFeeToConfirmed(ctx, hash, feeHash, c.client.url, c.client.pub)
			if err != nil {
				return err
			}
			return nil
		} else if status.FeeTxHash != "" {
			feeHash, err := chainhash.NewHashFromStr(status.FeeTxHash)
			if err != nil {
				return err
			}
			err = c.Wallet.UpdateVspTicketFeeToPaid(ctx, hash, feeHash, c.client.url, c.client.pub)
			if err != nil {
				return err
			}
			_ = c.feePayment(hash, policy, true)
		} else {
			// Fee hasn't been paid at the provided VSP, so this should do that if needed.
			_ = c.feePayment(hash, policy, false)
		}

		return nil
	})
	return err
}

// Process begins processing a VSP fee payment for a ticket.  If feeTx contains
// inputs, is used to pay the VSP fee.  Otherwise, new inputs are selected and
// locked to prevent double spending the fee.
//
// feeTx must not be nil, but may point to an empty transaction, and is modified
// with the inputs and the fee and change outputs before returning without an
// error.  The fee transaction is also recorded as unpublised in the wallet, and
// the fee hash is associated with the ticket.
func (c *Client) Process(ctx context.Context, ticketHash *chainhash.Hash, feeTx *wire.MsgTx) error {
	return c.ProcessWithPolicy(ctx, ticketHash, feeTx, c.Policy)
}

// ProcessWithPolicy is the same as Process but allows a fee payment policy to
// be specified, instead of using the client's default policy.
func (c *Client) ProcessWithPolicy(ctx context.Context, ticketHash *chainhash.Hash, feeTx *wire.MsgTx,
	policy Policy) error {

	vspTicket, err := c.Wallet.VSPTicketInfo(ctx, ticketHash)
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
		fp := c.feePayment(ticketHash, policy, false)
		if fp == nil {
			err := c.Wallet.UpdateVspTicketFeeToErrored(ctx, ticketHash, c.client.url, c.client.pub)
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
			err := c.Wallet.UpdateVspTicketFeeToErrored(ctx, ticketHash, c.client.url, c.client.pub)
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
			err := c.Wallet.UpdateVspTicketFeeToErrored(ctx, ticketHash, c.client.url, c.client.pub)
			if err != nil {
				return err
			}
			return err
		}
		return fp.submitPayment()
	case udb.VSPFeeProcessPaid:
		// If a VSP ticket has been paid, but confirm payment.
		if len(vspTicket.Host) > 0 && vspTicket.Host != c.client.url {
			// Cannot confirm a paid ticket that is already with another VSP.
			return fmt.Errorf("ticket already paid or confirmed with another vsp")
		}
		fp := c.feePayment(ticketHash, policy, true)
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
func (c *Client) SetVoteChoice(ctx context.Context, hash *chainhash.Hash,
	choices []wallet.AgendaChoice, tspendPolicy map[string]string, treasuryPolicy map[string]string) error {

	// Retrieve current voting preferences from VSP.
	status, err := c.status(ctx, hash)
	if err != nil {
		if errors.Is(err, errors.Locked) {
			return err
		}
		log.Errorf("Could not check status of VSP ticket %s: %v", hash, err)
		return nil
	}

	// Check for any mismatch between the provided voting preferences and the
	// VSP preferences to determine if VSP needs to be updated.
	update := false

	// Check consensus vote choices.
	for _, newChoice := range choices {
		vspChoice, ok := status.VoteChoices[newChoice.AgendaID]
		if !ok {
			update = true
			break
		}
		if vspChoice != newChoice.ChoiceID {
			update = true
			break
		}
	}

	// Apply the above changes to the two checks below.

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
		log.Debugf("VSP already has correct vote choices for ticket %s", hash)
		return nil
	}

	log.Debugf("Updating vote choices on VSP for ticket %s", hash)
	err = c.setVoteChoices(ctx, hash, choices, tspendPolicy, treasuryPolicy)
	if err != nil {
		return err
	}
	return nil
}

// TicketInfo stores per-ticket info tracked by a VSP Client instance.
type TicketInfo struct {
	TicketHash     chainhash.Hash
	CommitmentAddr stdaddr.StakeAddress
	VotingAddr     stdaddr.StakeAddress
	State          uint32
	Fee            dcrutil.Amount
	FeeHash        chainhash.Hash

	// TODO: include stuff returned by the status() call?
}

// TrackedTickets returns information about all outstanding tickets tracked by
// a vsp.Client instance.
//
// Currently this returns only info about tickets which fee hasn't been paid or
// confirmed at enough depth to be considered committed to.
func (c *Client) TrackedTickets() []*TicketInfo {
	// Collect all jobs first, to avoid working under two different locks.
	c.mu.Lock()
	jobs := make([]*feePayment, 0, len(c.jobs))
	for _, job := range c.jobs {
		jobs = append(jobs, job)
	}
	c.mu.Unlock()

	tickets := make([]*TicketInfo, 0, len(c.jobs))
	for _, job := range jobs {
		job.mu.Lock()
		tickets = append(tickets, &TicketInfo{
			TicketHash:     job.ticketHash,
			CommitmentAddr: job.commitmentAddr,
			VotingAddr:     job.votingAddr,
			State:          uint32(job.state),
			Fee:            job.fee,
			FeeHash:        job.feeHash,
		})
		job.mu.Unlock()
	}

	return tickets
}
