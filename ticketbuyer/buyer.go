// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wallet/udb"
)

var (
	// zeroUint32 is the zero value for a uint32.
	zeroUint32 = uint32(0)

	// stakeInfoReqTries is the maximum number of times to try
	// GetStakeInfo before failing.
	stakeInfoReqTries = 20

	// stakeInfoReqTryDelay is the time in seconds to wait before
	// doing another GetStakeInfo request.
	stakeInfoReqTryDelay = time.Second * 1
)

const (
	// TicketFeeMean is the string indicating that the mean ticket fee
	// should be used when determining ticket fee.
	TicketFeeMean = "mean"

	// TicketFeeMedian is the string indicating that the median ticket fee
	// should be used when determining ticket fee.
	TicketFeeMedian = "median"

	// PriceTargetVWAP is the string indicating that the volume
	// weighted average price should be used as the price target.
	PriceTargetVWAP = "vwap"

	// PriceTargetPool is the string indicating that the ticket pool
	// price should be used as the price target.
	PriceTargetPool = "pool"

	// PriceTargetDual is the string indicating that a combination of the
	// ticket pool price and the ticket VWAP should be used as the
	// price target.
	PriceTargetDual = "dual"
)

// Config stores the configuration options for ticket buyer.
type Config struct {
	AccountName               string
	AvgPriceMode              string
	AvgPriceVWAPDelta         int
	BalanceToMaintainAbsolute float64
	BalanceToMaintainRelative float64
	BlocksToAvg               int
	DontWaitForTickets        bool
	ExpiryDelta               int
	FeeSource                 string
	FeeTargetScaling          float64
	MinFee                    float64
	MaxFee                    float64
	MaxPerBlock               int
	MaxPriceAbsolute          float64
	MaxPriceRelative          float64
	MaxInMempool              int
	PoolAddress               string
	PoolFees                  float64
	SpreadTicketPurchases     bool
	TicketAddress             string
	TxFee                     float64
}

// TicketPurchaser is the main handler for purchasing tickets. It decides
// whether or not to do so based on information obtained from daemon and
// wallet chain servers.
type TicketPurchaser struct {
	cfg              *Config
	activeNet        *chaincfg.Params
	dcrdChainSvr     *dcrrpcclient.Client
	wallet           *wallet.Wallet
	ticketAddress    dcrutil.Address
	poolAddress      dcrutil.Address
	firstStart       bool
	windowPeriod     int          // The current window period
	idxDiffPeriod    int          // Relative block index within the difficulty period
	useMedian        bool         // Flag for using median for ticket fees
	priceMode        avgPriceMode // Price mode to use to calc average price
	heightCheck      map[int64]struct{}
	balEstimated     dcrutil.Amount
	ticketPrice      dcrutil.Amount
	stakePoolSize    uint32
	stakeLive        uint32
	stakeImmature    uint32
	stakeVoteSubsidy dcrutil.Amount

	// purchaserMtx protects the following runtime configurable options.
	purchaserMtx      sync.Mutex
	account           uint32
	balanceToMaintain float64
	maxPriceAbsolute  float64
	maxPriceRelative  float64
	maxFee            float64
	minFee            float64
	poolFees          float64
	maxPerBlock       int
	maxInMempool      int
	expiryDelta       int
}

// Config returns the current ticket buyer configuration.
func (t *TicketPurchaser) Config() (*Config, error) {
	t.purchaserMtx.Lock()
	var poolAddress string
	if t.poolAddress != nil {
		poolAddress = t.poolAddress.String()
	}
	accountName, err := t.AccountName()
	if err != nil {
		return nil, err
	}

	config := &Config{
		AccountName:               accountName,
		AvgPriceMode:              t.cfg.AvgPriceMode,
		AvgPriceVWAPDelta:         t.cfg.AvgPriceVWAPDelta,
		BalanceToMaintainAbsolute: t.balanceToMaintain,
		BlocksToAvg:               t.cfg.BlocksToAvg,
		DontWaitForTickets:        t.cfg.DontWaitForTickets,
		ExpiryDelta:               t.cfg.ExpiryDelta,
		FeeSource:                 t.cfg.FeeSource,
		FeeTargetScaling:          t.cfg.FeeTargetScaling,
		MinFee:                    t.cfg.MinFee,
		MaxFee:                    t.maxFee,
		MaxPerBlock:               t.maxPerBlock,
		MaxPriceAbsolute:          t.maxPriceAbsolute,
		MaxPriceRelative:          t.maxPriceRelative,
		MaxInMempool:              t.cfg.MaxInMempool,
		PoolAddress:               poolAddress,
		PoolFees:                  t.poolFees,
		SpreadTicketPurchases:     t.cfg.SpreadTicketPurchases,
		TicketAddress:             t.cfg.TicketAddress,
		TxFee:                     t.cfg.TxFee,
	}
	t.purchaserMtx.Unlock()
	return config, nil
}

// AccountName returns the account name to use for purchasing tickets.
func (t *TicketPurchaser) AccountName() (string, error) {
	t.purchaserMtx.Lock()
	accountName, err := t.wallet.AccountName(t.account)
	t.purchaserMtx.Unlock()
	return accountName, err
}

// Account returns the account to use for purchasing tickets.
func (t *TicketPurchaser) Account() uint32 {
	t.purchaserMtx.Lock()
	account := t.account
	t.purchaserMtx.Unlock()
	return account
}

// SetAccount sets the account to use for purchasing tickets.
func (t *TicketPurchaser) SetAccount(account uint32) {
	t.purchaserMtx.Lock()
	t.account = account
	t.purchaserMtx.Unlock()
}

// BalanceToMaintain returns the balance to be maintained in the wallet.
func (t *TicketPurchaser) BalanceToMaintain() float64 {
	t.purchaserMtx.Lock()
	balanceToMaintain := t.balanceToMaintain
	t.purchaserMtx.Unlock()
	return balanceToMaintain
}

// SetBalanceToMaintain sets the balance to be maintained in the wallet.
func (t *TicketPurchaser) SetBalanceToMaintain(balanceToMaintain float64) {
	t.purchaserMtx.Lock()
	t.balanceToMaintain = balanceToMaintain
	t.purchaserMtx.Unlock()
}

// MaxPriceAbsolute returns the max absolute price to purchase a ticket.
func (t *TicketPurchaser) MaxPriceAbsolute() float64 {
	t.purchaserMtx.Lock()
	maxPriceAbsolute := t.maxPriceAbsolute
	t.purchaserMtx.Unlock()
	return maxPriceAbsolute
}

// SetMaxPriceAbsolute sets the max absolute price to purchase a ticket.
func (t *TicketPurchaser) SetMaxPriceAbsolute(maxPriceAbsolute float64) {
	t.purchaserMtx.Lock()
	t.maxPriceAbsolute = maxPriceAbsolute
	t.purchaserMtx.Unlock()
}

// MaxPriceRelative returns the scaling factor which is multipled by average
// price to calculate a relative max for the ticket price.
func (t *TicketPurchaser) MaxPriceRelative() float64 {
	t.purchaserMtx.Lock()
	maxPriceRelative := t.maxPriceRelative
	t.purchaserMtx.Unlock()
	return maxPriceRelative
}

// SetMaxPriceRelative sets scaling factor.
func (t *TicketPurchaser) SetMaxPriceRelative(maxPriceRelative float64) {
	t.purchaserMtx.Lock()
	t.maxPriceRelative = maxPriceRelative
	t.purchaserMtx.Unlock()
}

// MaxFee returns the max ticket fee per KB to use when purchasing tickets.
func (t *TicketPurchaser) MaxFee() float64 {
	t.purchaserMtx.Lock()
	maxFee := t.maxFee
	t.purchaserMtx.Unlock()
	return maxFee
}

// SetMaxFee sets the max ticket fee per KB to use when purchasing tickets.
func (t *TicketPurchaser) SetMaxFee(maxFee float64) {
	t.purchaserMtx.Lock()
	t.maxFee = maxFee
	t.purchaserMtx.Unlock()
}

// MinFee returns the min ticket fee per KB to use when purchasing tickets.
func (t *TicketPurchaser) MinFee() float64 {
	t.purchaserMtx.Lock()
	minFee := t.minFee
	t.purchaserMtx.Unlock()
	return minFee
}

// SetMinFee sets the min ticket fee per KB to use when purchasing tickets.
func (t *TicketPurchaser) SetMinFee(minFee float64) {
	t.purchaserMtx.Lock()
	t.minFee = minFee
	t.purchaserMtx.Unlock()
}

// TicketAddress returns the address to send ticket outputs to.
func (t *TicketPurchaser) TicketAddress() dcrutil.Address {
	t.purchaserMtx.Lock()
	ticketAddress := t.ticketAddress
	t.purchaserMtx.Unlock()
	return ticketAddress
}

// SetTicketAddress sets the address to send ticket outputs to.
func (t *TicketPurchaser) SetTicketAddress(ticketAddress dcrutil.Address) {
	t.purchaserMtx.Lock()
	t.ticketAddress = ticketAddress
	t.purchaserMtx.Unlock()
}

// PoolAddress returns the pool address where ticket fees are sent.
func (t *TicketPurchaser) PoolAddress() dcrutil.Address {
	t.purchaserMtx.Lock()
	poolAddress := t.poolAddress
	t.purchaserMtx.Unlock()
	return poolAddress
}

// SetPoolAddress sets the pool address where ticket fees are sent.
func (t *TicketPurchaser) SetPoolAddress(poolAddress dcrutil.Address) {
	t.purchaserMtx.Lock()
	t.poolAddress = poolAddress
	t.purchaserMtx.Unlock()
}

// PoolFees returns the percent of ticket per ticket fee mandated by the pool.
func (t *TicketPurchaser) PoolFees() float64 {
	t.purchaserMtx.Lock()
	poolFees := t.poolFees
	t.purchaserMtx.Unlock()
	return poolFees
}

// SetPoolFees sets the percent of ticket per ticket fee mandated by the pool.
func (t *TicketPurchaser) SetPoolFees(poolFees float64) {
	t.purchaserMtx.Lock()
	t.poolFees = poolFees
	t.purchaserMtx.Unlock()
}

// MaxPerBlock returns the max tickets to purchase for a block.
func (t *TicketPurchaser) MaxPerBlock() int {
	t.purchaserMtx.Lock()
	maxPerBlock := t.maxPerBlock
	t.purchaserMtx.Unlock()
	return maxPerBlock
}

// SetMaxPerBlock sets the max tickets to purchase for a block.
func (t *TicketPurchaser) SetMaxPerBlock(maxPerBlock int) {
	t.purchaserMtx.Lock()
	t.maxPerBlock = maxPerBlock
	t.purchaserMtx.Unlock()
}

// MaxInMempool returns the max tickets to allow in the mempool.
func (t *TicketPurchaser) MaxInMempool() int {
	t.purchaserMtx.Lock()
	maxInMempool := t.maxInMempool
	t.purchaserMtx.Unlock()
	return maxInMempool
}

// SetMaxInMempool sets the max tickets to allow in the mempool.
func (t *TicketPurchaser) SetMaxInMempool(maxInMempool int) {
	t.purchaserMtx.Lock()
	t.maxInMempool = maxInMempool
	t.purchaserMtx.Unlock()
}

// ExpiryDelta returns the expiry delta.
func (t *TicketPurchaser) ExpiryDelta() int {
	t.purchaserMtx.Lock()
	expiryDelta := t.expiryDelta
	t.purchaserMtx.Unlock()
	return expiryDelta
}

// SetExpiryDelta sets the expiry delta.
func (t *TicketPurchaser) SetExpiryDelta(expiryDelta int) {
	t.purchaserMtx.Lock()
	t.expiryDelta = expiryDelta
	t.purchaserMtx.Unlock()
}

// NewTicketPurchaser creates a new TicketPurchaser.
func NewTicketPurchaser(cfg *Config,
	dcrdChainSvr *dcrrpcclient.Client,
	w *wallet.Wallet,
	activeNet *chaincfg.Params) (*TicketPurchaser, error) {
	var ticketAddress dcrutil.Address
	var err error
	if cfg.TicketAddress != "" {
		ticketAddress, err = dcrutil.DecodeAddress(cfg.TicketAddress,
			activeNet)
		if err != nil {
			return nil, err
		}
	}
	var poolAddress dcrutil.Address
	if cfg.PoolAddress != "" {
		poolAddress, err = dcrutil.DecodeNetworkAddress(cfg.PoolAddress)
		if err != nil {
			return nil, err
		}
	}

	priceMode := avgPriceMode(AvgPriceVWAPMode)
	switch cfg.AvgPriceMode {
	case PriceTargetPool:
		priceMode = AvgPricePoolMode
	case PriceTargetDual:
		priceMode = AvgPriceDualMode
	}

	account, err := w.AccountNumber(cfg.AccountName)
	if err != nil {
		return nil, err
	}

	return &TicketPurchaser{
		cfg:           cfg,
		activeNet:     activeNet,
		dcrdChainSvr:  dcrdChainSvr,
		wallet:        w,
		firstStart:    true,
		ticketAddress: ticketAddress,
		poolAddress:   poolAddress,
		useMedian:     cfg.FeeSource == TicketFeeMedian,
		priceMode:     priceMode,
		heightCheck:   make(map[int64]struct{}),

		account:           account,
		balanceToMaintain: cfg.BalanceToMaintainAbsolute,
		maxFee:            cfg.MaxFee,
		maxPerBlock:       cfg.MaxPerBlock,
		maxPriceAbsolute:  cfg.MaxPriceAbsolute,
		maxPriceRelative:  cfg.MaxPriceRelative,
		poolFees:          cfg.PoolFees,
		maxInMempool:      cfg.MaxInMempool,
		expiryDelta:       cfg.ExpiryDelta,
	}, nil
}

// PurchaseStats stats is a collection of statistics related to the ticket purchase.
type PurchaseStats struct {
	Height        int64
	PriceMaxScale float64
	PriceAverage  float64
	PriceNext     float64
	PriceCurrent  float64
	Purchased     int
	LeftWindow    int
	Balance       int64
	TicketPrice   int64
}

// Purchase is the main handler for purchasing tickets for the user.
// TODO Not make this an inlined pile of crap.
func (t *TicketPurchaser) Purchase(height int64) (*PurchaseStats, error) {

	ps := &PurchaseStats{Height: height}
	// Check to make sure that the current height has not already been seen
	// for a reorg or a fork
	if _, exists := t.heightCheck[height]; exists {
		log.Debugf("We've already seen this height, "+
			"reorg/fork detected at height %v", height)
		return ps, nil
	}
	t.heightCheck[height] = struct{}{}

	// Initialize based on where we are in the window
	winSize := t.activeNet.StakeDiffWindowSize
	thisIdxDiffPeriod := int(height % winSize)
	nextIdxDiffPeriod := int((height + 1) % winSize)
	thisWindowPeriod := int(height / winSize)

	refreshStakeInfo := false
	if t.firstStart {
		t.firstStart = false
		log.Debugf("First run for ticket buyer")
		log.Debugf("Transaction relay fee: %v DCR", t.cfg.TxFee)
		refreshStakeInfo = true
	} else {
		if nextIdxDiffPeriod == 0 {
			// Starting a new window
			log.Debugf("Resetting stake window variables")
			refreshStakeInfo = true
		}
		if thisIdxDiffPeriod != 0 && thisWindowPeriod > t.windowPeriod {
			// Disconnected and reconnected in a different window
			log.Debugf("Reconnected in a different window, now at height %v", height)
			refreshStakeInfo = true
		}
	}

	// Set these each round
	t.idxDiffPeriod = int(height % winSize)
	t.windowPeriod = int(height / winSize)

	if refreshStakeInfo {
		log.Debugf("Getting StakeInfo")
		var curStakeInfo *wallet.StakeInfoData
		var err error
		for i := 1; i <= stakeInfoReqTries; i++ {
			curStakeInfo, err = t.wallet.StakeInfo()
			if err != nil {
				log.Debugf("Waiting for StakeInfo, attempt %v: (%v)", i, err.Error())
				time.Sleep(stakeInfoReqTryDelay)
				continue
			}
			if err == nil {
				log.Debugf("Got StakeInfo")
				break
			}
		}

		if err != nil {
			return ps, err
		}
		t.stakePoolSize = curStakeInfo.PoolSize
		t.stakeLive = curStakeInfo.Live
		t.stakeImmature = curStakeInfo.Immature

		subsidyCache := blockchain.NewSubsidyCache(height, t.wallet.ChainParams())
		subsidy := blockchain.CalcStakeVoteSubsidy(subsidyCache, height, t.wallet.ChainParams())
		t.stakeVoteSubsidy = dcrutil.Amount(subsidy)
		log.Tracef("Stake vote subsidy: %v", t.stakeVoteSubsidy)
	}

	// Parse the ticket purchase frequency. Positive numbers mean
	// that many tickets per block. Negative numbers mean to only
	// purchase one ticket once every abs(num) blocks.
	maxPerBlock := 0
	switch {
	case t.cfg.MaxPerBlock == 0:
		return ps, nil
	case t.cfg.MaxPerBlock > 0:
		maxPerBlock = t.cfg.MaxPerBlock
	case t.cfg.MaxPerBlock < 0:
		if int(height)%t.cfg.MaxPerBlock != 0 {
			return ps, nil
		}
		maxPerBlock = 1
	}

	avgPriceAmt, err := t.calcAverageTicketPrice(height)
	if err != nil {
		return ps, fmt.Errorf("Failed to calculate average ticket price amount: %s",
			err.Error())
	}
	if avgPriceAmt < dcrutil.Amount(t.activeNet.MinimumStakeDiff) {
		avgPriceAmt = dcrutil.Amount(t.activeNet.MinimumStakeDiff)
	}

	avgPrice := avgPriceAmt.ToCoin()
	log.Debugf("Calculated average ticket price: %v", avgPriceAmt)
	ps.PriceAverage = avgPrice

	nextStakeDiff, err := t.wallet.StakeDifficulty()
	log.Tracef("Next stake difficulty: %v", nextStakeDiff)
	if err != nil {
		return ps, err
	}
	ps.PriceCurrent = float64(nextStakeDiff)
	t.ticketPrice = nextStakeDiff
	ps.TicketPrice = int64(nextStakeDiff)

	sDiffEsts, err := t.dcrdChainSvr.EstimateStakeDiff(nil)
	if err != nil {
		return ps, err
	}
	ps.PriceNext = sDiffEsts.Expected
	log.Tracef("Estimated stake diff: (min: %v, expected: %v, max: %v)",
		sDiffEsts.Min, sDiffEsts.Expected, sDiffEsts.Max)

	// Set the max price to the configuration parameter that is lower
	// Absolute or relative max price
	var maxPriceAmt dcrutil.Amount
	maxPriceAbs := t.MaxPriceAbsolute()
	if maxPriceAbs > 0 && maxPriceAbs < avgPrice*t.cfg.MaxPriceRelative {
		maxPriceAmt, err = dcrutil.NewAmount(maxPriceAbs)
		if err != nil {
			return ps, err
		}
		log.Debugf("Using absolute max price: %v", maxPriceAmt)
	} else {
		maxPriceAmt, err = dcrutil.NewAmount(avgPrice * t.MaxPriceRelative())
		if err != nil {
			return ps, err
		}
		log.Debugf("Using relative max price: %v", maxPriceAmt)
	}

	accountName, err := t.AccountName()
	if err != nil {
		return ps, err
	}
	account := t.Account()
	bal, err := t.wallet.CalculateAccountBalance(account, 0)
	if err != nil {
		return ps, err
	}
	log.Debugf("Spendable balance for account '%s': %v", accountName, bal.Spendable)
	ps.Balance = int64(bal.Spendable)

	// Disable purchasing if the ticket price is too high based on
	// the cutoff or if the estimated ticket price is above
	// our scaled cutoff based on the ideal ticket price.
	if nextStakeDiff > maxPriceAmt {
		log.Infof("Not buying because max price exceeded: "+
			"(max price: %v, ticket price: %v)", maxPriceAmt, nextStakeDiff)
		return ps, nil
	}

	// If we still have tickets in the memory pool, don't try
	// to buy even more tickets.
	inMP, err := t.ownTicketsInMempool()
	if err != nil {
		return ps, err
	}
	log.Tracef("Own tickets in mempool: %v", inMP)
	if !t.cfg.DontWaitForTickets {
		if inMP >= t.MaxInMempool() {
			log.Infof("Currently waiting for %v tickets to enter the "+
				"blockchain before buying more tickets (in mempool: %v,"+
				" max allowed in mempool %v)", inMP-t.MaxInMempool(),
				inMP, t.cfg.MaxInMempool)
			return ps, nil
		}
	}

	// Lookup how many tickets purchase slots were filled in the last block
	oneBlock := uint32(1)
	info, err := t.dcrdChainSvr.TicketFeeInfo(&oneBlock, &zeroUint32)
	if err != nil {
		return ps, err
	}
	if len(info.FeeInfoBlocks) < 1 {
		return ps, fmt.Errorf("error FeeInfoBlocks bad length")
	}
	ticketPurchasesInLastBlock := int(info.FeeInfoBlocks[0].Number)
	log.Tracef("Ticket purchase slots filled in last block: %v", ticketPurchasesInLastBlock)

	// Expensive call to fetch all tickets in the mempool
	mempoolall, err := t.allTicketsInMempool()
	if err != nil {
		return ps, err
	}
	log.Tracef("All tickets in mempool: %v", mempoolall)

	var feeToUse float64
	maxStake := int(t.activeNet.MaxFreshStakePerBlock)
	if ticketPurchasesInLastBlock < maxStake && mempoolall < maxStake {
		log.Debugf("Using min ticket fee: %.8f DCR", t.cfg.MinFee)
		feeToUse = t.cfg.MinFee
		if ticketPurchasesInLastBlock < maxStake {
			log.Tracef("(ticket purchase slots available in last block)")
		}
		if mempoolall < maxStake {
			log.Tracef("(total tickets in mempool is less than max per block)")
		}
	} else {
		// If might be the case that there weren't enough recent
		// blocks to average fees from. Use data from the last
		// window with the closest difficulty.
		chainFee := 0.0
		if t.idxDiffPeriod < t.cfg.BlocksToAvg {
			chainFee, err = t.findClosestFeeWindows(nextStakeDiff.ToCoin(),
				t.useMedian)
			if err != nil {
				return ps, err
			}
		} else {
			chainFee, err = t.findTicketFeeBlocks(t.useMedian)
			if err != nil {
				return ps, err
			}
		}

		// Scale the mean fee upwards according to what was asked
		// for by the user.
		feeToUse := chainFee * t.cfg.FeeTargetScaling
		log.Tracef("Average ticket fee: %.8f DCR", chainFee)
		maxFee := t.MaxFee()
		minFee := t.MinFee()
		if feeToUse > maxFee {
			log.Infof("Not buying because max fee exceed: (max fee: %.8f DCR,  scaled fee: %.8f DCR)",
				maxFee, feeToUse)
			return ps, nil
		}
		if feeToUse < minFee {
			log.Debugf("Using min ticket fee: %.8f DCR (scaled fee: %.8f DCR)", minFee, feeToUse)
			feeToUse = minFee
			log.Tracef("Using scaled ticket fee: %.8f DCR", feeToUse)
		}
	}
	feeToUseAmt, err := dcrutil.NewAmount(feeToUse)
	if err != nil {
		return ps, err
	}
	t.wallet.SetTicketFeeIncrement(feeToUseAmt)

	// Set the balancetomaintain to the configuration parameter that is higher
	// Absolute or relative balance to maintain
	balanceToMaintainAmt := t.BalanceToMaintain()
	if balanceToMaintainAmt > bal.Total.ToCoin()*t.cfg.BalanceToMaintainRelative {
		log.Debugf("Using absolute balancetomaintain: %v", balanceToMaintainAmt)
	} else {
		balanceToMaintainAmt = bal.Total.ToCoin() * t.cfg.BalanceToMaintainRelative
		log.Debugf("Using relative balancetomaintain: %v", balanceToMaintainAmt)
	}

	// Calculate how many tickets to buy
	ticketsLeftInWindow := (int(winSize) - t.idxDiffPeriod) * int(t.activeNet.MaxFreshStakePerBlock)
	log.Tracef("Ticket allotment left in window is %v, blocks left is %v",
		ticketsLeftInWindow, (int(winSize) - t.idxDiffPeriod))

	toBuyForBlock := int(math.Floor((bal.Spendable.ToCoin() - balanceToMaintainAmt) / nextStakeDiff.ToCoin()))
	if toBuyForBlock < 0 {
		toBuyForBlock = 0
	}
	if toBuyForBlock == 0 {
		log.Infof("Not enough funds to buy tickets: (spendable: %v, balancetomaintain: %v) ",
			bal.Spendable.ToCoin(), balanceToMaintainAmt)
	}

	// For spreading your ticket purchases evenly throughout window.
	// Use available funds to calculate how many tickets to buy, and also
	// approximate the income you're going to have from older tickets that
	// you've voted and are maturing during this window
	if t.cfg.SpreadTicketPurchases && toBuyForBlock > 0 {
		log.Debugf("Spreading purchases throughout window")

		// same as proportionlive that getstakeinfo rpc shows
		proportionLive := float64(t.stakeLive) / float64(t.stakePoolSize)
		// Number of blocks remaining to purchase tickets in this window
		blocksRemaining := int(winSize) - t.idxDiffPeriod
		// Estimated number of tickets you will vote on and redeem this window
		tixWillRedeem := float64(blocksRemaining) * float64(t.activeNet.TicketsPerBlock) * proportionLive
		// Average price of your tickets in the pool
		yourAvgTixPrice := 0.0
		if t.stakeLive+t.stakeImmature != 0 {
			yourAvgTixPrice = bal.LockedByTickets.ToCoin() / float64(t.stakeLive+t.stakeImmature)
		}
		// Estimated amount of funds to redeem in the remaining window
		redeemedFunds := tixWillRedeem * yourAvgTixPrice
		// Estimated amount of funds becoming available from stake vote reward
		stakeRewardFunds := tixWillRedeem * t.stakeVoteSubsidy.ToCoin()
		// Estimated number of tickets to be bought with redeemed funds
		tixToBuyWithRedeemedFunds := redeemedFunds / nextStakeDiff.ToCoin()
		// Estimated number of tickets to be bought with stake reward
		tixToBuyWithStakeRewardFunds := stakeRewardFunds / nextStakeDiff.ToCoin()
		// Amount of tickets that can be bought with existing funds
		tixCanBuy := (bal.Spendable.ToCoin() - balanceToMaintainAmt) / nextStakeDiff.ToCoin()
		if tixCanBuy < 0 {
			tixCanBuy = 0
		}
		// Estimated number of tickets you can buy with current funds and
		// funds from incoming redeemed tickets
		tixCanBuyAll := tixCanBuy + tixToBuyWithRedeemedFunds + tixToBuyWithStakeRewardFunds
		// Amount of tickets that can be bought per block with current funds
		buyPerBlock := tixCanBuy / float64(blocksRemaining)
		// Amount of tickets that can be bought per block with current and redeemed funds
		buyPerBlockAll := tixCanBuyAll / float64(blocksRemaining)

		log.Debugf("Your average purchase price for tickets in the pool is %.2f DCR", yourAvgTixPrice)
		log.Debugf("Available funds of %.2f DCR can buy %.2f tickets, %.2f tickets per block",
			bal.Spendable.ToCoin()-balanceToMaintainAmt, tixCanBuy, buyPerBlock)
		log.Debugf("With %.2f%% proportion live, you will redeem ~%.2f tickets in the remaining %d blocks",
			proportionLive*100, tixWillRedeem, blocksRemaining)
		log.Debugf("Redeemed ticket value expected is %.2f DCR, buys %.2f tickets, %.2f%% more",
			redeemedFunds, tixToBuyWithRedeemedFunds, tixToBuyWithRedeemedFunds/tixCanBuy*100)
		log.Debugf("Stake reward expected is %.2f DCR, buys %.2f tickets, %.2f%% more",
			stakeRewardFunds, tixToBuyWithStakeRewardFunds, tixToBuyWithStakeRewardFunds/tixCanBuy*100)
		log.Infof("Will buy ~%.2f tickets per block, %.2f ticket purchases remain this window", buyPerBlockAll, tixCanBuyAll)

		if blocksRemaining > 0 && tixCanBuy > 0 {
			// rand is for the remainder
			// if 1.25 buys per block are needed, then buy 2 tickets 1/4th of the time
			rand.Seed(time.Now().UTC().UnixNano())

			if tixCanBuyAll >= float64(blocksRemaining) {
				// When buying one or more tickets per block
				toBuyForBlock = int(math.Floor(buyPerBlockAll))
				ticketRemainder := int(math.Floor(tixCanBuyAll)) % blocksRemaining
				if rand.Float64() <= float64(ticketRemainder) {
					toBuyForBlock++
				}
			} else {
				// When buying less than one ticket per block
				if rand.Float64() <= buyPerBlockAll {
					log.Debugf("Buying one this round")
					toBuyForBlock = 1
				} else {
					toBuyForBlock = 0
					log.Debugf("Skipping this round")
				}
			}
			// This is for a possible rare case where there is not enough available funds to
			// purchase a ticket because funds expected from redeemed tickets are overdue
			if toBuyForBlock > int(math.Floor(tixCanBuy)) {
				log.Debugf("Waiting for current windows tickets to redeem")
				log.Debugf("Cant yet buy %d, will buy %d",
					toBuyForBlock, int(math.Floor(tixCanBuy)))
				toBuyForBlock = int(math.Floor(tixCanBuy))
			}
		} else {
			// Do not buy any tickets
			toBuyForBlock = 0
			if tixCanBuy < 0 {
				log.Debugf("tixCanBuy < 0, this should not occur")
			} else if tixCanBuy < 1 {
				log.Debugf("Not enough funds to purchase a ticket")
			}
			if blocksRemaining < 0 {
				log.Debugf("blocksRemaining < 0, this should not occur")
			} else if blocksRemaining == 0 {
				log.Debugf("There are no more opportunities to purchase tickets in this window")
			}
		}
	}

	// Only the maximum number of tickets at each block
	// should be purchased, as specified by the user.
	if toBuyForBlock > maxPerBlock {
		toBuyForBlock = maxPerBlock
		if maxPerBlock == 1 {
			log.Infof("Limiting to 1 purchase so that maxperblock is not exceeded")
		} else {
			log.Infof("Limiting to %d purchases so that maxperblock is not exceeded", maxPerBlock)
		}
	}

	// We've already purchased all the tickets we need to.
	if toBuyForBlock <= 0 {
		log.Infof("Not buying any tickets this round")
		return ps, nil
	}

	// Check our balance versus the amount of tickets we need to buy.
	// If there is not enough money, decrement and recheck the balance
	// to see if fewer tickets may be purchased. Abort if we don't
	// have enough moneys.
	notEnough := func(bal dcrutil.Amount, toBuy int, sd dcrutil.Amount) bool {
		return (bal.ToCoin() - float64(toBuy)*sd.ToCoin()) <
			balanceToMaintainAmt
	}
	if notEnough(bal.Spendable, toBuyForBlock, nextStakeDiff) {
		for notEnough(bal.Spendable, toBuyForBlock, nextStakeDiff) {
			if toBuyForBlock == 0 {
				break
			}

			toBuyForBlock--
			log.Debugf("Not enough, decremented amount of tickets to buy")
		}

		if toBuyForBlock == 0 {
			log.Infof("Not buying because spendable balance would be %v "+
				"but balance to maintain is %v",
				(bal.Spendable.ToCoin() - float64(toBuyForBlock)*
					nextStakeDiff.ToCoin()),
				balanceToMaintainAmt)
			return ps, nil
		}
	}

	// When buying, do not exceed maxinmempool
	if !t.cfg.DontWaitForTickets {
		if toBuyForBlock+inMP > t.cfg.MaxInMempool {
			toBuyForBlock = t.cfg.MaxInMempool - inMP
			log.Debugf("Limiting to %d purchases so that maxinmempool is not exceeded", toBuyForBlock)
		}
	}

	// If an address wasn't passed, create an internal address in
	// the wallet for the ticket address.
	ticketAddress := t.TicketAddress()
	if ticketAddress == nil {
		ticketAddress, err =
			t.wallet.NewAddress(account, udb.InternalBranch)
		if err != nil {
			return ps, err
		}
	}

	// Purchase tickets.
	poolFeesAmt, err := dcrutil.NewAmount(t.cfg.PoolFees)
	if err != nil {
		return ps, err
	}

	// Ticket purchase requires 2 blocks to confirm
	expiry := int32(int(height) + t.ExpiryDelta() + 2)
	hashes, purchaseErr := t.wallet.PurchaseTickets(0,
		maxPriceAmt,
		0, // 0 minconf is used so tickets can be bought from split outputs
		ticketAddress,
		account,
		toBuyForBlock,
		t.PoolAddress(),
		poolFeesAmt.ToCoin(),
		expiry,
		t.wallet.RelayFee(),
		t.wallet.TicketFeeIncrement(),
	)
	// Not sure why but this is an empty interface.
	tickets, ok := hashes.([]*chainhash.Hash)
	if ok {
		for i := range tickets {
			log.Infof("Purchased ticket %v at stake difficulty %v (%v "+
				"fees per KB used)", tickets[i], nextStakeDiff.ToCoin(),
				feeToUseAmt.ToCoin())
		}
		ps.Purchased = len(tickets)
	}
	if purchaseErr != nil {
		log.Errorf("One or more tickets could not be purchased: %v", purchaseErr)
	}

	bal, err = t.wallet.CalculateAccountBalance(account, 0)
	if err != nil {
		return ps, err
	}
	log.Debugf("Usable balance for account '%s' after purchases: %v", accountName, bal.Spendable)
	ps.Balance = int64(bal.Spendable)

	if len(tickets) == 0 && purchaseErr != nil {
		return ps, purchaseErr
	}

	return ps, nil
}
