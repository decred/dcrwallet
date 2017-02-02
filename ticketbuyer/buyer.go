// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wallet"
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
	HighPricePenalty          float64
	MinFee                    float64
	MaxFee                    float64
	MaxPerBlock               int
	MaxPriceAbsolute          float64
	MaxPriceRelative          float64
	MaxPriceScale             float64
	MaxInMempool              int
	PoolAddress               string
	PoolFees                  float64
	PriceTarget               float64
	SpreadTicketPurchases     bool
	TicketAddress             string
	TxFee                     float64
	TicketFeeInfo             bool
	PrevToBuyDiffPeriod       int
	PrevToBuyHeight           int
}

// TicketPurchaser is the main handler for purchasing tickets. It decides
// whether or not to do so based on information obtained from daemon and
// wallet chain servers.
type TicketPurchaser struct {
	cfg            *Config
	activeNet      *chaincfg.Params
	dcrdChainSvr   *dcrrpcclient.Client
	wallet         *wallet.Wallet
	ticketAddress  dcrutil.Address
	poolAddress    dcrutil.Address
	firstStart     bool
	windowPeriod   int          // The current window period
	idxDiffPeriod  int          // Relative block index within the difficulty period
	useMedian      bool         // Flag for using median for ticket fees
	priceMode      avgPriceMode // Price mode to use to calc average price
	heightCheck    map[int64]struct{}
	balEstimated   dcrutil.Amount
	ticketPrice    dcrutil.Amount
	ProportionLive float64
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
	}, nil
}

// PurchaseStats stats is a collection of statistics related to the ticket purchase.
type PurchaseStats struct {
	Height        int64
	PriceMaxScale float64
	PriceAverage  float64
	PriceNext     float64
	PriceCurrent  float64
	MempoolAll    int
	MempoolOwn    int
	Purchased     int
	LeftWindow    int
	FeeMin        float64
	FeeMax        float64
	FeeMedian     float64
	FeeMean       float64
	FeeOwn        float64
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

	if t.cfg.TicketFeeInfo {
		oneBlock := uint32(1)
		info, err := t.dcrdChainSvr.TicketFeeInfo(&oneBlock, &zeroUint32)
		if err != nil {
			return ps, err
		}
		ps.FeeMin = info.FeeInfoBlocks[0].Min
		ps.FeeMax = info.FeeInfoBlocks[0].Max
		ps.FeeMedian = info.FeeInfoBlocks[0].Median
		ps.FeeMean = info.FeeInfoBlocks[0].Mean

		// Expensive call to fetch all tickets in the mempool
		all, err := t.allTicketsInMempool()
		if err != nil {
			return ps, err
		}
		ps.MempoolAll = all
	}

	// Initialize based on where we are in the window
	winSize := t.activeNet.StakeDiffWindowSize
	thisIdxDiffPeriod := int(height % winSize)
	nextIdxDiffPeriod := int((height + 1) % winSize)
	thisWindowPeriod := int(height / winSize)

	refreshProportionLive := false
	if t.firstStart {
		t.firstStart = false
		log.Debugf("First run for ticket buyer")
		log.Debugf("Transaction relay fee: %v DCR", t.cfg.TxFee)
		refreshProportionLive = true
	} else {
		if nextIdxDiffPeriod == 0 {
			// Starting a new window
			log.Debugf("Resetting stake window variables")
			refreshProportionLive = true
		}
		if thisIdxDiffPeriod != 0 && thisWindowPeriod > t.windowPeriod {
			// Disconnected and reconnected in a different window
			log.Debugf("Reconnected in a different window, now at height %v", height)
			refreshProportionLive = true
		}
	}

	// Set these each round
	t.idxDiffPeriod = int(height % winSize)
	t.windowPeriod = int(height / winSize)

	if refreshProportionLive {
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
		t.ProportionLive = float64(curStakeInfo.Live) / float64(curStakeInfo.PoolSize)
		log.Debugf("Proportion live: %.4f%%", t.ProportionLive*100)
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

	// Make sure that our wallet is connected to the daemon and the
	// wallet is unlocked, otherwise abort.
	// TODO: Check daemon connected
	if t.wallet.Locked() {
		return ps, fmt.Errorf("Wallet not unlocked to allow ticket purchases")
	}

	avgPriceAmt, err := t.calcAverageTicketPrice(height)
	if err != nil {
		return ps, fmt.Errorf("Failed to calculate average ticket price amount: %s",
			err.Error())
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
	if t.cfg.MaxPriceAbsolute > 0 && t.cfg.MaxPriceAbsolute < avgPrice*t.cfg.MaxPriceRelative {
		maxPriceAmt, err = dcrutil.NewAmount(t.cfg.MaxPriceAbsolute)
		if err != nil {
			return ps, err
		}
		log.Debugf("Using absolute max price: %v", maxPriceAmt)
	} else {
		maxPriceAmt, err = dcrutil.NewAmount(avgPrice * t.cfg.MaxPriceRelative)
		if err != nil {
			return ps, err
		}
		log.Debugf("Using relative max price: %v", maxPriceAmt)
	}

	// Scale the average price according to the configuration parameters
	// to find the maximum price for users that are electing to
	// attempting to manipulate the stake difficulty
	maxPriceScaledAmt, err := dcrutil.NewAmount(t.cfg.MaxPriceScale * avgPrice)
	if err != nil {
		return ps, err
	}
	if t.cfg.MaxPriceScale > 0.0 {
		log.Debugf("Max price to maintain this window: %v", maxPriceScaledAmt)
	}
	ps.PriceMaxScale = maxPriceScaledAmt.ToCoin()

	account, err := t.wallet.AccountNumber(t.cfg.AccountName)
	if err != nil {
		return ps, err
	}
	bal, err := t.wallet.CalculateAccountBalance(account, 0)
	if err != nil {
		return ps, err
	}
	log.Debugf("Spendable balance for account '%s': %v", t.cfg.AccountName, bal.Spendable)
	ps.Balance = int64(bal.Spendable)

	// Disable purchasing if the ticket price is too high based on
	// the cutoff or if the estimated ticket price is above
	// our scaled cutoff based on the ideal ticket price.
	if nextStakeDiff > maxPriceAmt {
		log.Infof("Not buying because max price exceeded: "+
			"(max price: %v, ticket price: %v)", maxPriceAmt, nextStakeDiff)
		return ps, nil
	}
	if t.cfg.MaxPriceScale > 0.0 && (sDiffEsts.Expected > maxPriceScaledAmt.ToCoin()) &&
		maxPriceScaledAmt != 0 {
		log.Infof("Not buying because the "+
			"next window estimate %v DCR is higher than the scaled max "+
			"price %v", sDiffEsts.Expected, maxPriceScaledAmt)
		return ps, nil
	}

	// If we still have tickets in the memory pool, don't try
	// to buy even more tickets.
	inMP, err := t.ownTicketsInMempool()
	if err != nil {
		return ps, err
	}
	ps.MempoolOwn = inMP
	if !t.cfg.DontWaitForTickets {
		if inMP > t.cfg.MaxInMempool {
			log.Infof("Currently waiting for %v tickets to enter the "+
				"blockchain before buying more tickets (in mempool: %v,"+
				" max allowed in mempool %v)", inMP-t.cfg.MaxInMempool,
				inMP, t.cfg.MaxInMempool)
			return ps, nil
		}
	}

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
	if feeToUse > t.cfg.MaxFee {
		log.Infof("Not buying because max fee exceed: (max fee: %.8f DCR,  scaled fee: %.8f DCR)",
			t.cfg.MaxFee, feeToUse)
		return ps, nil
	}
	if feeToUse < t.cfg.MinFee {
		log.Debugf("Using min ticket fee: %.8f DCR (scaled fee: %.8f DCR)", t.cfg.MinFee, feeToUse)
		feeToUse = t.cfg.MinFee
	} else {
		log.Tracef("Using scaled ticket fee: %.8f DCR", feeToUse)
	}
	feeToUseAmt, err := dcrutil.NewAmount(feeToUse)
	if err != nil {
		return ps, err
	}
	t.wallet.SetTicketFeeIncrement(feeToUseAmt)

	ps.FeeOwn = feeToUse

	// Set the balancetomaintain to the configuration parameter that is higher
	// Absolute or relative balance to maintain
	var balanceToMaintainAmt dcrutil.Amount
	if t.cfg.BalanceToMaintainAbsolute > 0 && t.cfg.BalanceToMaintainAbsolute >
		bal.Total.ToCoin()*t.cfg.BalanceToMaintainRelative {

		balanceToMaintainAmt, err = dcrutil.NewAmount(t.cfg.BalanceToMaintainAbsolute)
		if err != nil {
			return ps, err
		}
		log.Debugf("Using absolute balancetomaintain: %v", balanceToMaintainAmt)
	} else {
		balanceToMaintainAmt, err = dcrutil.NewAmount(bal.Total.ToCoin() * t.cfg.BalanceToMaintainRelative)
		if err != nil {
			return ps, err
		}
		log.Debugf("Using relative balancetomaintain: %v", balanceToMaintainAmt)
	}

	// Calculate how many tickets to buy
	ticketsLeftInWindow := (int(winSize) - t.idxDiffPeriod) * int(t.activeNet.MaxFreshStakePerBlock)
	log.Tracef("Ticket allotment left in window is %v, blocks left is %v",
		ticketsLeftInWindow, (int(winSize) - t.idxDiffPeriod))

	toBuyForBlock := int(math.Floor((bal.Spendable.ToCoin() - balanceToMaintainAmt.ToCoin()) / nextStakeDiff.ToCoin()))
	if toBuyForBlock < 0 {
		toBuyForBlock = 0
	}
	if toBuyForBlock == 0 {
		log.Infof("Not enough funds to buy tickets: (spendable: %v, balancetomaintain: %v) ",
			bal.Spendable.ToCoin(), balanceToMaintainAmt.ToCoin())
	}

	// For spreading your ticket purchases evenly throughout window.
	// Use available funds to calculate how many tickets to buy, and also
	// approximate the income you're going to have from older tickets that
	// you've voted and are maturing during this window (tixWillRedeem)
	if t.cfg.SpreadTicketPurchases && toBuyForBlock > 0 {
		log.Debugf("Spreading purchases throughout window")

		// Number of blocks remaining to purchase tickets in this window
		blocksRemaining := int(winSize) - t.idxDiffPeriod
		// Estimated number of tickets you will vote on and redeem this window
		tixWillRedeem := float64(blocksRemaining) * float64(t.activeNet.TicketsPerBlock) * t.ProportionLive
		// Amount of tickets that can be bought with existing funds
		tixCanBuy := (bal.Spendable.ToCoin() - balanceToMaintainAmt.ToCoin()) / nextStakeDiff.ToCoin()
		if tixCanBuy < 0 {
			tixCanBuy = 0
		}
		// Estimated number of tickets you can buy with current funds and
		// funds from incoming redeemed tickets
		tixCanBuyAll := tixCanBuy + tixWillRedeem
		// Amount of tickets that can be bought per block with current funds
		buyPerBlock := tixCanBuy / float64(blocksRemaining)
		// Amount of tickets that can be bought per block with current and redeemed funds
		buyPerBlockAll := tixCanBuyAll / float64(blocksRemaining)

		log.Debugf("To spend existing funds within this window you need %.1f tickets per block"+
			", %.1f purchases total", buyPerBlock, tixCanBuy)
		log.Debugf("With %.1f%% proportion live, you are expected to redeem %.1f tickets "+
			"in the remaining %d usable blocks", t.ProportionLive*100, tixWillRedeem, blocksRemaining)
		log.Debugf("With expected funds from redeemed tickets, you can buy %.1f%% more tickets",
			tixWillRedeem/tixCanBuy*100)
		log.Infof("Will buy ~%.1f tickets per block, %.1f purchases total", buyPerBlockAll, tixCanBuyAll)

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
			log.Infof("Limiting to 1 purchase per block")
		} else {
			log.Infof("Limiting to %d purchases per block", maxPerBlock)
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
			balanceToMaintainAmt.ToCoin()
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

	// If an address wasn't passed, create an internal address in
	// the wallet for the ticket address.
	var ticketAddress dcrutil.Address
	if t.ticketAddress != nil {
		ticketAddress = t.ticketAddress
	} else {
		ticketAddress, err =
			t.wallet.NewAddress(account, waddrmgr.InternalBranch)
		if err != nil {
			return ps, err
		}
	}

	// Purchase tickets.
	poolFeesAmt, err := dcrutil.NewAmount(t.cfg.PoolFees)
	if err != nil {
		return ps, err
	}
	expiry := int32(int(height) + t.cfg.ExpiryDelta)
	hashes, err := t.wallet.PurchaseTickets(0,
		maxPriceAmt,
		0,
		ticketAddress,
		account,
		toBuyForBlock,
		t.poolAddress,
		poolFeesAmt.ToCoin(),
		expiry,
		t.wallet.RelayFee(),
		t.wallet.TicketFeeIncrement(),
	)
	if err != nil {
		return ps, err
	}
	tickets, ok := hashes.([]*chainhash.Hash)
	if !ok {
		return nil, fmt.Errorf("Unable to decode ticket hashes")
	}
	ps.Purchased = toBuyForBlock
	for i := range tickets {
		log.Infof("Purchased ticket %v at stake difficulty %v (%v "+
			"fees per KB used)", tickets[i], nextStakeDiff.ToCoin(),
			feeToUseAmt.ToCoin())
	}

	bal, err = t.wallet.CalculateAccountBalance(account, 0)
	if err != nil {
		return ps, err
	}
	log.Debugf("Usable balance for account '%s' after purchases: %v", t.cfg.AccountName, bal.Spendable)
	ps.Balance = int64(bal.Spendable)

	return ps, nil
}
