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

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/wtxmgr"
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
	AccountName           string
	AvgPriceMode          string
	AvgPriceVWAPDelta     int
	BalanceToMaintain     float64
	BlocksToAvg           int
	DontWaitForTickets    bool
	ExpiryDelta           int
	FeeSource             string
	FeeTargetScaling      float64
	MinFee                float64
	MinPriceScale         float64
	MaxFee                float64
	MaxPerBlock           int
	MaxPriceAbsolute      float64
	MaxPriceRelative      float64
	MaxPriceScale         float64
	MaxInMempool          int
	PoolAddress           string
	PoolFees              float64
	SpreadTicketPurchases bool
	TicketAddress         string
	TxFee                 float64
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
	maintainMaxPrice bool         // Flag for maximum price manipulation
	maintainMinPrice bool         // Flag for minimum price manipulation
	useMedian        bool         // Flag for using median for ticket fees
	priceMode        avgPriceMode // Price mode to use to calc average price
	heightCheck      map[int64]struct{}
	balEstimated     dcrutil.Amount
	ticketPrice      dcrutil.Amount
	ProportionLive   float64

	purchaserMtx          sync.Mutex
	accountName           string
	avgPriceMode          string
	avgPriceVWAPDelta     int
	balanceToMaintain     float64
	blocksToAvg           int
	dontWaitForTickets    bool
	expiryDelta           int
	feeSource             string
	feeTargetScaling      float64
	highPricePenalty      float64
	minFee                float64
	minPriceScale         float64
	maxFee                float64
	maxPerBlock           int
	maxPriceAbsolute      float64
	maxPriceRelative      float64
	maxPriceScale         float64
	maxInMempool          int
	poolFees              float64
	priceTarget           float64
	spreadTicketPurchases bool
	txFee                 float64
}

// AccountName returns the accountName of ticket purchaser configuration.
func (t *TicketPurchaser) AccountName() string {
	t.purchaserMtx.Lock()
	accountName := t.accountName
	t.purchaserMtx.Unlock()
	return accountName
}

// SetAccountName sets the accountName of ticket purchaser configuration.
func (t *TicketPurchaser) SetAccountName(accountName string) {
	t.purchaserMtx.Lock()
	t.accountName = accountName
	t.purchaserMtx.Unlock()
}

// AvgPriceMode returns the avgPriceMode of ticket purchaser configuration.
func (t *TicketPurchaser) AvgPriceMode() string {
	t.purchaserMtx.Lock()
	avgPriceMode := t.avgPriceMode
	t.purchaserMtx.Unlock()
	return avgPriceMode
}

// SetAvgPriceMode sets the avgPriceMode of ticket purchaser configuration.
func (t *TicketPurchaser) SetAvgPriceMode(avgPriceMode string) {
	t.purchaserMtx.Lock()
	t.avgPriceMode = avgPriceMode
	t.purchaserMtx.Unlock()
}

// AvgPriceVWAPDelta returns the avgPriceVWAPDelta of ticket purchaser configuration.
func (t *TicketPurchaser) AvgPriceVWAPDelta() int {
	t.purchaserMtx.Lock()
	avgPriceVWAPDelta := t.avgPriceVWAPDelta
	t.purchaserMtx.Unlock()
	return avgPriceVWAPDelta
}

// SetAvgPriceVWAPDelta sets the avgPriceVWAPDelta of ticket purchaser configuration.
func (t *TicketPurchaser) SetAvgPriceVWAPDelta(avgPriceVWAPDelta int) {
	t.purchaserMtx.Lock()
	t.avgPriceVWAPDelta = avgPriceVWAPDelta
	t.purchaserMtx.Unlock()
}

// BalanceToMaintain returns the balanceToMaintain of ticket purchaser configuration.
func (t *TicketPurchaser) BalanceToMaintain() float64 {
	t.purchaserMtx.Lock()
	balanceToMaintain := t.balanceToMaintain
	t.purchaserMtx.Unlock()
	return balanceToMaintain
}

// SetBalanceToMaintain sets the balanceToMaintain of ticket purchaser configuration.
func (t *TicketPurchaser) SetBalanceToMaintain(balanceToMaintain float64) {
	t.purchaserMtx.Lock()
	t.balanceToMaintain = balanceToMaintain
	t.purchaserMtx.Unlock()
}

// BlocksToAverage returns the blocksToAvg of ticket purchaser configuration.
func (t *TicketPurchaser) BlocksToAverage() int {
	t.purchaserMtx.Lock()
	blocksToAvg := t.blocksToAvg
	t.purchaserMtx.Unlock()
	return blocksToAvg
}

// SetBlocksToAverage sets the blocksToAvg of ticket purchaser configuration.
func (t *TicketPurchaser) SetBlocksToAverage(blocksToAvg int) {
	t.purchaserMtx.Lock()
	t.blocksToAvg = blocksToAvg
	t.purchaserMtx.Unlock()
}

// DontWaitForTickets returns the dontWaitForTickets of ticket purchaser configuration.
func (t *TicketPurchaser) DontWaitForTickets() bool {
	t.purchaserMtx.Lock()
	dontWaitForTickets := t.dontWaitForTickets
	t.purchaserMtx.Unlock()
	return dontWaitForTickets
}

// SetDontWaitForTickets sets the dontWaitForTickets of ticket purchaser configuration.
func (t *TicketPurchaser) SetDontWaitForTickets(dontWaitForTickets bool) {
	t.purchaserMtx.Lock()
	t.dontWaitForTickets = dontWaitForTickets
	t.purchaserMtx.Unlock()
}

// ExpiryDelta returns the expiryDelta of ticket purchaser configuration.
func (t *TicketPurchaser) ExpiryDelta() int {
	t.purchaserMtx.Lock()
	expiryDelta := t.expiryDelta
	t.purchaserMtx.Unlock()
	return expiryDelta
}

// SetExpiryDelta sets the expiryDelta of ticket purchaser configuration.
func (t *TicketPurchaser) SetExpiryDelta(expiryDelta int) {
	t.purchaserMtx.Lock()
	t.expiryDelta = expiryDelta
	t.purchaserMtx.Unlock()
}

// FeeSource returns the feeSource of ticket purchaser configuration.
func (t *TicketPurchaser) FeeSource() string {
	t.purchaserMtx.Lock()
	feeSource := t.feeSource
	t.purchaserMtx.Unlock()
	return feeSource
}

// SetFeeSource sets the feeSource of ticket purchaser configuration.
func (t *TicketPurchaser) SetFeeSource(feeSource string) {
	t.purchaserMtx.Lock()
	t.feeSource = feeSource
	t.purchaserMtx.Unlock()
}

// FeeTargetScaling returns the feeTargetScaling of ticket purchaser configuration.
func (t *TicketPurchaser) FeeTargetScaling() float64 {
	t.purchaserMtx.Lock()
	feeTargetScaling := t.feeTargetScaling
	t.purchaserMtx.Unlock()
	return feeTargetScaling
}

// SetFeeTargetScaling sets the feeTargetScaling of ticket purchaser configuration.
func (t *TicketPurchaser) SetFeeTargetScaling(feeTargetScaling float64) {
	t.purchaserMtx.Lock()
	t.feeTargetScaling = feeTargetScaling
	t.purchaserMtx.Unlock()
}

// MinFee returns the minFee of ticket purchaser configuration.
func (t *TicketPurchaser) MinFee() float64 {
	t.purchaserMtx.Lock()
	minFee := t.minFee
	t.purchaserMtx.Unlock()
	return minFee
}

// SetMinFee sets the minFee of ticket purchaser configuration.
func (t *TicketPurchaser) SetMinFee(minFee float64) {
	t.purchaserMtx.Lock()
	t.minFee = minFee
	t.purchaserMtx.Unlock()
}

// MinPriceScale returns the minPriceScale of ticket purchaser configuration.
func (t *TicketPurchaser) MinPriceScale() float64 {
	t.purchaserMtx.Lock()
	minPriceScale := t.minPriceScale
	t.purchaserMtx.Unlock()
	return minPriceScale
}

// SetMinPriceScale sets the minPriceScale of ticket purchaser configuration.
func (t *TicketPurchaser) SetMinPriceScale(minPriceScale float64) {
	t.purchaserMtx.Lock()
	t.minPriceScale = minPriceScale
	t.purchaserMtx.Unlock()
}

// MaxFee returns the maxFee of ticket purchaser configuration.
func (t *TicketPurchaser) MaxFee() float64 {
	t.purchaserMtx.Lock()
	maxFee := t.maxFee
	t.purchaserMtx.Unlock()
	return maxFee
}

// SetMaxFee sets the maxFee of ticket purchaser configuration.
func (t *TicketPurchaser) SetMaxFee(maxFee float64) {
	t.purchaserMtx.Lock()
	t.maxFee = maxFee
	t.purchaserMtx.Unlock()
}

// MaxPerBlock returns the maxPerBlock of ticket purchaser configuration.
func (t *TicketPurchaser) MaxPerBlock() int {
	t.purchaserMtx.Lock()
	maxPerBlock := t.maxPerBlock
	t.purchaserMtx.Unlock()
	return maxPerBlock
}

// SetMaxPerBlock sets the maxPerBlock of ticket purchaser configuration.
func (t *TicketPurchaser) SetMaxPerBlock(maxPerBlock int) {
	t.purchaserMtx.Lock()
	t.maxPerBlock = maxPerBlock
	t.purchaserMtx.Unlock()
}

// MaxPriceAbsolute returns the maxPriceAbsolute of ticket purchaser configuration.
func (t *TicketPurchaser) MaxPriceAbsolute() float64 {
	t.purchaserMtx.Lock()
	maxPriceAbsolute := t.maxPriceAbsolute
	t.purchaserMtx.Unlock()
	return maxPriceAbsolute
}

// SetMaxPriceAbsolute sets the maxPriceAbsolute of ticket purchaser configuration.
func (t *TicketPurchaser) SetMaxPriceAbsolute(maxPriceAbsolute float64) {
	t.purchaserMtx.Lock()
	t.maxPriceAbsolute = maxPriceAbsolute
	t.purchaserMtx.Unlock()
}

// MaxPriceRelative returns the maxPriceRelative of ticket purchaser configuration.
func (t *TicketPurchaser) MaxPriceRelative() float64 {
	t.purchaserMtx.Lock()
	maxPriceRelative := t.maxPriceRelative
	t.purchaserMtx.Unlock()
	return maxPriceRelative
}

// SetMaxPriceRelative sets the maxPriceRelative of ticket purchaser configuration.
func (t *TicketPurchaser) SetMaxPriceRelative(maxPriceRelative float64) {
	t.purchaserMtx.Lock()
	t.maxPriceRelative = maxPriceRelative
	t.purchaserMtx.Unlock()
}

// MaxPriceScale returns the maxPriceScale of ticket purchaser configuration.
func (t *TicketPurchaser) MaxPriceScale() float64 {
	t.purchaserMtx.Lock()
	maxPriceScale := t.maxPriceScale
	t.purchaserMtx.Unlock()
	return maxPriceScale
}

// SetMaxPriceScale sets the maxPriceScale of ticket purchaser configuration.
func (t *TicketPurchaser) SetMaxPriceScale(maxPriceScale float64) {
	t.purchaserMtx.Lock()
	t.maxPriceScale = maxPriceScale
	t.purchaserMtx.Unlock()
}

// MaxInMempool returns the maxInMempool of ticket purchaser configuration.
func (t *TicketPurchaser) MaxInMempool() int {
	t.purchaserMtx.Lock()
	maxInMempool := t.maxInMempool
	t.purchaserMtx.Unlock()
	return maxInMempool
}

// SetMaxInMempool sets the maxInMempool of ticket purchaser configuration.
func (t *TicketPurchaser) SetMaxInMempool(maxInMempool int) {
	t.purchaserMtx.Lock()
	t.maxInMempool = maxInMempool
	t.purchaserMtx.Unlock()
}

// PoolAddress returns the poolAddress of ticket purchaser configuration.
func (t *TicketPurchaser) PoolAddress() string {
	t.purchaserMtx.Lock()
	poolAddress := t.poolAddress
	t.purchaserMtx.Unlock()
	return poolAddress.String()
}

// SetPoolAddress sets the poolAddress of ticket purchaser configuration.
func (t *TicketPurchaser) SetPoolAddress(poolAddress string) error {
	t.purchaserMtx.Lock()
	poolAddressDecoded, err := dcrutil.DecodeNetworkAddress(poolAddress)
	if err != nil {
		return err
	}
	t.poolAddress = poolAddressDecoded
	t.purchaserMtx.Unlock()
	return nil
}

// PoolFees returns the poolFees of ticket purchaser configuration.
func (t *TicketPurchaser) PoolFees() float64 {
	t.purchaserMtx.Lock()
	poolFees := t.poolFees
	t.purchaserMtx.Unlock()
	return poolFees
}

// SetPoolFees sets the poolFees of ticket purchaser configuration.
func (t *TicketPurchaser) SetPoolFees(poolFees float64) {
	t.purchaserMtx.Lock()
	t.poolFees = poolFees
	t.purchaserMtx.Unlock()
}

// SpreadTicketPurchases returns the spreadTicketPurchases of ticket purchaser configuration.
func (t *TicketPurchaser) SpreadTicketPurchases() bool {
	t.purchaserMtx.Lock()
	spreadTicketPurchases := t.spreadTicketPurchases
	t.purchaserMtx.Unlock()
	return spreadTicketPurchases
}

// SetSpreadTicketPurchases sets the spreadTicketPurchases of ticket purchaser configuration.
func (t *TicketPurchaser) SetSpreadTicketPurchases(spreadTicketPurchases bool) {
	t.purchaserMtx.Lock()
	t.spreadTicketPurchases = spreadTicketPurchases
	t.purchaserMtx.Unlock()
}

// TicketAddress returns the ticketAddress of ticket purchaser configuration.
func (t *TicketPurchaser) TicketAddress() string {
	t.purchaserMtx.Lock()
	ticketAddress := t.ticketAddress
	t.purchaserMtx.Unlock()
	return ticketAddress.String()
}

// SetTicketAddress sets the ticketAddress of ticket purchaser configuration.
func (t *TicketPurchaser) SetTicketAddress(ticketAddress string) error {
	t.purchaserMtx.Lock()
	ticketAddressDecoded, err := dcrutil.DecodeNetworkAddress(ticketAddress)
	if err != nil {
		return err
	}
	t.ticketAddress = ticketAddressDecoded
	t.purchaserMtx.Unlock()
	return nil
}

// TxFee returns the txFee of ticket purchaser configuration.
func (t *TicketPurchaser) TxFee() float64 {
	t.purchaserMtx.Lock()
	txFee := t.txFee
	t.purchaserMtx.Unlock()
	return txFee
}

// SetTxFee sets the txFee of ticket purchaser configuration.
func (t *TicketPurchaser) SetTxFee(txFee float64) {
	t.purchaserMtx.Lock()
	t.txFee = txFee
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

	maintainMaxPrice := false
	if cfg.MaxPriceScale > 0.0 {
		maintainMaxPrice = true
	}

	maintainMinPrice := false
	if cfg.MinPriceScale > 0.0 {
		maintainMinPrice = true
	}

	priceMode := avgPriceMode(AvgPriceVWAPMode)
	switch cfg.AvgPriceMode {
	case PriceTargetPool:
		priceMode = AvgPricePoolMode
	case PriceTargetDual:
		priceMode = AvgPriceDualMode
	}

	return &TicketPurchaser{
		cfg:              cfg,
		activeNet:        activeNet,
		dcrdChainSvr:     dcrdChainSvr,
		wallet:           w,
		firstStart:       true,
		ticketAddress:    ticketAddress,
		poolAddress:      poolAddress,
		maintainMaxPrice: maintainMaxPrice,
		maintainMinPrice: maintainMinPrice,
		useMedian:        cfg.FeeSource == TicketFeeMedian,
		priceMode:        priceMode,
		heightCheck:      make(map[int64]struct{}),

		accountName:           cfg.AccountName,
		avgPriceMode:          cfg.AvgPriceMode,
		avgPriceVWAPDelta:     cfg.AvgPriceVWAPDelta,
		balanceToMaintain:     cfg.BalanceToMaintain,
		blocksToAvg:           cfg.BlocksToAvg,
		dontWaitForTickets:    cfg.DontWaitForTickets,
		expiryDelta:           cfg.ExpiryDelta,
		feeSource:             cfg.FeeSource,
		feeTargetScaling:      cfg.FeeTargetScaling,
		minFee:                cfg.MinFee,
		minPriceScale:         cfg.MinPriceScale,
		maxFee:                cfg.MaxFee,
		maxPerBlock:           cfg.MaxPerBlock,
		maxPriceAbsolute:      cfg.MaxPriceAbsolute,
		maxPriceRelative:      cfg.MaxPriceRelative,
		maxPriceScale:         cfg.MaxPriceScale,
		maxInMempool:          cfg.MaxInMempool,
		poolFees:              cfg.PoolFees,
		spreadTicketPurchases: cfg.SpreadTicketPurchases,
		txFee: cfg.TxFee,
	}, nil
}

// PurchaseStats stats is a collection of statistics related to the ticket purchase.
type PurchaseStats struct {
	Height        int64
	PriceMinScale float64
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

	// Initialize based on where we are in the window
	winSize := t.activeNet.StakeDiffWindowSize
	thisIdxDiffPeriod := int(height % winSize)
	nextIdxDiffPeriod := int((height + 1) % winSize)
	thisWindowPeriod := int(height / winSize)

	refreshProportionLive := false
	if t.firstStart {
		t.firstStart = false
		log.Debugf("First run for ticket buyer")
		log.Debugf("Transaction relay fee: %v DCR", t.TxFee())
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
	case t.MaxPerBlock() == 0:
		return ps, nil
	case t.MaxPerBlock() > 0:
		maxPerBlock = int(t.MaxPerBlock())
	case t.MaxPerBlock() < 0:
		if int(height)%t.MaxPerBlock() != 0 {
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
	if t.MaxPriceAbsolute() > 0 && t.MaxPriceAbsolute() < avgPrice*t.MaxPriceRelative() {
		maxPriceAmt, err = dcrutil.NewAmount(t.MaxPriceAbsolute())
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

	// Scale the average price according to the configuration parameters
	// to find minimum and maximum prices for users that are electing to
	// attempting to manipulate the stake difficulty.
	minPriceScaledAmt, err := dcrutil.NewAmount(t.MinPriceScale() * avgPrice)
	if err != nil {
		return ps, err
	}
	if t.maintainMinPrice {
		log.Debugf("Min price to maintain this window: %v", minPriceScaledAmt)
	}
	ps.PriceMinScale = minPriceScaledAmt.ToCoin()
	maxPriceScaledAmt, err := dcrutil.NewAmount(t.MaxPriceScale() * avgPrice)
	if err != nil {
		return ps, err
	}
	if t.maintainMaxPrice {
		log.Debugf("Max price to maintain this window: %v", maxPriceScaledAmt)
	}
	ps.PriceMaxScale = maxPriceScaledAmt.ToCoin()

	account, err := t.wallet.AccountNumber(t.AccountName())
	if err != nil {
		return ps, err
	}
	balSpendable, err := t.wallet.CalculateAccountBalance(account, 0, wtxmgr.BFBalanceSpendable)
	if err != nil {
		return ps, err
	}
	log.Debugf("Spendable balance for account '%s': %v", t.AccountName(), balSpendable)
	ps.Balance = int64(balSpendable)

	// Disable purchasing if the ticket price is too high based on
	// the cutoff or if the estimated ticket price is above
	// our scaled cutoff based on the ideal ticket price.
	if nextStakeDiff > maxPriceAmt {
		log.Infof("Not buying because max price exceeded: "+
			"(max price: %v, ticket price: %v)", maxPriceAmt, nextStakeDiff)
		return ps, nil
	}
	if t.maintainMaxPrice && (sDiffEsts.Expected > maxPriceScaledAmt.ToCoin()) &&
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
	if !t.DontWaitForTickets() {
		if inMP > int(t.MaxInMempool()) {
			log.Infof("Currently waiting for %v tickets to enter the "+
				"blockchain before buying more tickets (in mempool: %v,"+
				" max allowed in mempool %v)", inMP-int(t.MaxInMempool()),
				inMP, t.MaxInMempool())
			return ps, nil
		}
	}

	// If might be the case that there weren't enough recent
	// blocks to average fees from. Use data from the last
	// window with the closest difficulty.
	chainFee := 0.0
	if t.idxDiffPeriod < int(t.BlocksToAverage()) {
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
	feeToUse := chainFee * t.FeeTargetScaling()
	log.Tracef("Average ticket fee: %.8f DCR", chainFee)
	maxFee := t.MaxFee()
	if feeToUse > maxFee {
		log.Infof("Not buying because max fee exceed: (max fee: %.8f DCR,  scaled fee: %.8f DCR)",
			maxFee, feeToUse)
		return ps, nil
	}
	if feeToUse < t.MinFee() {
		log.Debugf("Using min ticket fee: %.8f DCR (scaled fee: %.8f DCR)", t.MinFee(), feeToUse)
		feeToUse = t.MinFee()
	} else {
		log.Tracef("Using scaled ticket fee: %.8f DCR", feeToUse)
	}
	feeToUseAmt, err := dcrutil.NewAmount(feeToUse)
	if err != nil {
		return ps, err
	}
	t.wallet.SetTicketFeeIncrement(feeToUseAmt)

	ps.FeeOwn = feeToUse

	// Calculate how many tickets to buy
	ticketsLeftInWindow := (int(winSize) - t.idxDiffPeriod) * int(t.activeNet.MaxFreshStakePerBlock)
	log.Tracef("Ticket allotment left in window is %v, blocks left is %v",
		ticketsLeftInWindow, (int(winSize) - t.idxDiffPeriod))

	toBuyForBlock := int(math.Floor(balSpendable.ToCoin() / nextStakeDiff.ToCoin()))

	// For spreading your ticket purchases evenly throughout window.
	// Use available funds to calculate how many tickets to buy, and also
	// approximate the income you're going to have from older tickets that
	// you've voted and are maturing during this window (tixWillRedeem)
	if t.SpreadTicketPurchases() {
		log.Debugf("Spreading purchases throughout window")

		// Number of blocks remaining to purchase tickets in this window
		blocksRemaining := int(winSize) - t.idxDiffPeriod
		// Estimated number of tickets you will vote on and redeem this window
		tixWillRedeem := float64(blocksRemaining) * float64(t.activeNet.TicketsPerBlock) * t.ProportionLive
		// Amount of tickets that can be bought with existing funds
		tixCanBuy := balSpendable.ToCoin() / nextStakeDiff.ToCoin()
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

	// Hijack the number to purchase for this block if we have minimum
	// ticket price manipulation enabled.
	if t.maintainMinPrice && toBuyForBlock < maxPerBlock {
		if sDiffEsts.Expected < minPriceScaledAmt.ToCoin() {
			toBuyForBlock = maxPerBlock
			log.Debugf("Attempting to manipulate the stake difficulty "+
				"so that the price does not fall below the set minimum "+
				"%v (current estimate for next stake difficulty: %v) by "+
				"purchasing an additional round of tickets",
				minPriceScaledAmt, sDiffEsts.Expected)
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
			t.BalanceToMaintain()
	}
	if notEnough(balSpendable, toBuyForBlock, nextStakeDiff) {
		for notEnough(balSpendable, toBuyForBlock, nextStakeDiff) {
			if toBuyForBlock == 0 {
				break
			}

			toBuyForBlock--
		}

		if toBuyForBlock == 0 {
			log.Infof("Not buying because our balance "+
				"after buying tickets is estimated to be %v but balance "+
				"to maintain is set to %v",
				(balSpendable.ToCoin() - float64(toBuyForBlock)*
					nextStakeDiff.ToCoin()),
				t.BalanceToMaintain())
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
	poolFeesAmt, err := dcrutil.NewAmount(t.PoolFees())
	if err != nil {
		return ps, err
	}
	expiry := int32(int(height) + t.ExpiryDelta())
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

	balSpendable, err = t.wallet.CalculateAccountBalance(account, 0,
		wtxmgr.BFBalanceSpendable)
	if err != nil {
		return ps, err
	}
	log.Debugf("Usable balance for account '%s' after purchases: %v", t.AccountName(), balSpendable)
	ps.Balance = int64(balSpendable)

	return ps, nil
}
