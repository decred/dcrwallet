// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"context"
	"sync"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3"
)

const minconf = 1

// Config modifies the behavior of TB.
type Config struct {
	// Account to buy tickets from
	Account uint32

	// Account to derive voting addresses from; overridden by VotingAddr
	VotingAccount uint32

	// Minimum amount to maintain in purchasing account
	Maintain dcrutil.Amount

	// Address to assign voting rights; overrides VotingAccount
	VotingAddr dcrutil.Address

	// Commitment address for stakepool fees
	PoolFeeAddr dcrutil.Address

	// Stakepool fee percentage (between 0-100)
	PoolFees float64

	// Limit maximum number of purchased tickets per block
	Limit int
}

// TB is an automated ticket buyer, buying as many tickets as possible given an
// account's available balance.  TB may be configured to buy tickets for any
// arbitrary voting address or (optional) stakepool.
type TB struct {
	wallet *wallet.Wallet

	cfg Config
	mu  sync.Mutex
}

// New returns a new TB to buy tickets from a wallet using the default config.
func New(w *wallet.Wallet) *TB {
	return &TB{wallet: w}
}

// Run executes the ticket buyer.  If the private passphrase is incorrect, or
// ever becomes incorrect due to a wallet passphrase change, Run exits with an
// errors.Passphrase error.
func (tb *TB) Run(ctx context.Context, passphrase []byte) error {
	err := tb.wallet.Unlock(passphrase, nil)
	if err != nil {
		return err
	}

	c := tb.wallet.NtfnServer.MainTipChangedNotifications()
	defer c.Done()

	var mu sync.Mutex
	var done bool
	var errc = make(chan error, 1)
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			done = true
			mu.Unlock()
			return ctx.Err()
		case n := <-c.C:
			if len(n.AttachedBlocks) == 0 {
				continue
			}
			go func() {
				defer mu.Unlock()
				mu.Lock()
				if done {
					return
				}
				b := n.AttachedBlocks[len(n.AttachedBlocks)-1]
				err := tb.buy(ctx, passphrase, b)
				if err != nil {
					log.Errorf("Ticket purchasing failed: %v", err)
					if errors.Is(errors.Passphrase, err) {
						errc <- err
						done = true
					}
				}
			}()
		case err := <-errc:
			return err
		}
	}
}

func (tb *TB) buy(ctx context.Context, passphrase []byte, tip *chainhash.Hash) error {
	w := tb.wallet

	// Don't buy tickets for this attached block when transactions are not
	// synced through the tip block.
	rp, err := w.RescanPoint()
	if err != nil {
		return err
	}
	if rp != nil {
		log.Debugf("Skipping purchase: transactions are not synced")
		return nil
	}

	// Unable to publish any transactions if the network backend is unset.
	_, err = w.NetworkBackend()
	if err != nil {
		return err
	}

	// Ensure wallet is unlocked with the current passphrase.  If the passphase
	// is changed, the Run exits and TB must be restarted with the new
	// passphrase.
	err = w.Unlock(passphrase, nil)
	if err != nil {
		return err
	}

	header, err := w.BlockHeader(tip)
	if err != nil {
		return err
	}
	height := int32(header.Height)

	intervalSize := int32(w.ChainParams().StakeDiffWindowSize)
	currentInterval := height / intervalSize
	nextIntervalStart := (currentInterval + 1) * intervalSize
	// Skip purchase when no more tickets may be purchased in this interval and
	// the next sdiff is unknown.  The earliest any ticket may be mined is two
	// blocks from now, with the next block containing the split transaction
	// that the ticket purchase spends.
	if height+2 == nextIntervalStart {
		log.Debugf("Skipping purchase: next sdiff interval starts soon")
		return nil
	}
	// Set expiry to prevent tickets from being mined in the next
	// sdiff interval.  When the next block begins the new interval,
	// the ticket is being purchased for the next interval; therefore
	// increment expiry by a full sdiff window size to prevent it
	// being mined in the interval after the next.
	expiry := nextIntervalStart
	if height+1 == nextIntervalStart {
		expiry += intervalSize
	}

	// Read config
	tb.mu.Lock()
	account := tb.cfg.Account
	votingAccount := tb.cfg.VotingAccount
	maintain := tb.cfg.Maintain
	votingAddr := tb.cfg.VotingAddr
	poolFeeAddr := tb.cfg.PoolFeeAddr
	poolFees := tb.cfg.PoolFees
	limit := tb.cfg.Limit
	tb.mu.Unlock()

	// Determine how many tickets to buy
	bal, err := w.CalculateAccountBalance(account, minconf)
	if err != nil {
		return err
	}
	spendable := bal.Spendable
	if spendable < maintain {
		log.Debugf("Skipping purchase: low available balance")
		return nil
	}
	spendable -= maintain
	sdiff, err := w.NextStakeDifficultyAfterHeader(header)
	if err != nil {
		return err
	}
	buy := int(spendable / sdiff)
	if buy == 0 {
		log.Debugf("Skipping purchase: low available balance")
		return nil
	}
	if max := int(w.ChainParams().MaxFreshStakePerBlock); buy > max {
		buy = max
	}
	if limit > 0 && buy > limit {
		buy = limit
	}

	// Derive a voting address from voting account when address is unset.
	if votingAddr == nil {
		votingAddr, err = w.NewInternalAddress(votingAccount, wallet.WithGapPolicyWrap())
		if err != nil {
			return err
		}
	}

	feeRate := w.RelayFee()
	tix, err := w.PurchaseTickets(maintain, -1, minconf, votingAddr, account,
		buy, poolFeeAddr, poolFees, expiry, feeRate, feeRate)
	for _, hash := range tix {
		log.Infof("Purchased ticket %v at stake difficulty %v", hash, sdiff)
	}
	if err != nil {
		// Invalid passphrase errors must be returned so Run exits.
		if errors.Is(errors.Passphrase, err) {
			return err
		}
		log.Errorf("One or more tickets could not be purchased: %v", err)
	}
	return nil
}

// AccessConfig runs f with the current config passed as a parameter.  The
// config is protected by a mutex and this function is safe for concurrent
// access to read or modify the config.  It is unsafe to leak a pointer to the
// config, but a copy of *cfg is legal.
func (tb *TB) AccessConfig(f func(cfg *Config)) {
	tb.mu.Lock()
	f(&tb.cfg)
	tb.mu.Unlock()
}
