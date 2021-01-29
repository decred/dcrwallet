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
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

const requiredConfs = 6 + 2

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

// ForUnspentUnexpiredTickets performs a function on every unexpired and unspent
// ticket from the wallet.
func (c *Client) ForUnspentUnexpiredTickets(ctx context.Context,
	f func(hash *chainhash.Hash) error) error {

	w := c.Wallet
	params := w.ChainParams()

	iter := func(ticketSummaries []*wallet.TicketSummary, _ *wire.BlockHeader) (bool, error) {
		for _, ticketSummary := range ticketSummaries {
			switch ticketSummary.Status {
			case wallet.TicketStatusLive:
			case wallet.TicketStatusImmature:
			case wallet.TicketStatusUnspent:
			default:
				continue
			}

			ticketHash := *ticketSummary.Ticket.Hash
			err := f(&ticketHash)
			if err != nil {
				return false, err
			}
		}

		return false, nil
	}

	_, blockHeight := w.MainChainTip(ctx)
	startBlockNum := blockHeight -
		int32(params.TicketExpiry+uint32(params.TicketMaturity)-requiredConfs)
	startBlock := wallet.NewBlockIdentifierFromHeight(startBlockNum)
	endBlock := wallet.NewBlockIdentifierFromHeight(blockHeight)
	return w.GetTickets(ctx, iter, startBlock, endBlock)
}

// ProcessUnprocessedTickets ...
func (c *Client) ProcessUnprocessedTickets(ctx context.Context, policy Policy) {
	var wg sync.WaitGroup
	c.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
		// Skip tickets which have a fee tx already associated with
		// them; they are already processed by some vsp.
		_, err := c.Wallet.VSPFeeHashForTicket(ctx, hash)
		if err == nil {
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

// ProcessManagedTickets discovers tickets which were previously registered with
// a VSP and begins syncing them in the background.  This is used to recover VSP
// tracking after seed restores, and is only performed on unspent and unexpired
// tickets.
func (c *Client) ProcessManagedTickets(ctx context.Context, policy Policy) error {
	err := c.ForUnspentUnexpiredTickets(ctx, func(hash *chainhash.Hash) error {
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

		if status.FeeTxHash != "" {
			feeHash, err := chainhash.NewHashFromStr(status.FeeTxHash)
			if err != nil {
				return err
			}
			err = c.Wallet.UpdateVspTicketFeeToPaid(ctx, hash, feeHash)
			if err != nil {
				return err
			}
		}

		_ = c.feePayment(hash, policy)
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

	fp := c.feePayment(ticketHash, policy)
	if fp == nil {
		return fmt.Errorf("fee payment cannot be processed")
	}

	err := fp.receiveFeeAddress()
	if err != nil {
		// XXX, retry? (old Process retried)
		// but this may not be necessary any longer as the parent of
		// the ticket is always relayed to the vsp as well.
		return err
	}
	err = fp.makeFeeTx(feeTx)
	if err != nil {
		return err
	}
	if feeTx != nil {
		*feeTx = *fp.feeTx
	}
	return fp.submitPayment()
}
