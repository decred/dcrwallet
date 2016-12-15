// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"strings"
	"time"

	"github.com/decred/dcrutil"
)

var (
	// vwapReqTries is the maximum number of times to try
	// TicketVWAP before failing.
	vwapReqTries = 20

	// vwapReqTryDelay is the time in seconds to wait before
	// doing another TicketVWAP request.
	vwapReqTryDelay = time.Millisecond * 500
)

// priceMode is the mode to use for the average ticket price.
type avgPriceMode int

const (
	// AvgPriceVWAPMode indicates to use only the VWAP.
	AvgPriceVWAPMode = iota

	// AvgPricePoolMode indicates to use only the average
	// price in the ticket pool.
	AvgPricePoolMode

	// AvgPriceDualMode indicates to use bothe the VWAP and
	// the average pool price.
	AvgPriceDualMode
)

// vwapHeightOffsetErrStr is a definitive portion of the RPC error returned
// when the chain has not yet sufficiently synced to the chain tip.
var vwapHeightOffsetErrStr = "beyond blockchain tip height"

// containsVWAPHeightOffsetError specifies whether or not the error
// matches the error returned when the chain has not yet sufficiently
// synced to the chain tip.
func containsVWAPHeightOffsetError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), vwapHeightOffsetErrStr)
}

// calcAverageTicketPrice calculates the average price of a ticket based on
// the parameters set by the user.
func (t *TicketPurchaser) calcAverageTicketPrice(height int64) (dcrutil.Amount, error) {
	// Pull and store relevant data about the blockchain. Calculate a
	// "reasonable" ticket price by using the VWAP for the last delta many
	// blocks or the average price of all tickets in the ticket pool.
	// If the user has selected dual mode, it will use an average of
	// these two prices.
	var avgPricePoolAmt dcrutil.Amount
	if t.priceMode == AvgPricePoolMode || t.priceMode == AvgPriceDualMode {
		poolValue, err := t.dcrdChainSvr.GetTicketPoolValue()
		if err != nil {
			return 0, err
		}
		bestBlockH, err := t.dcrdChainSvr.GetBestBlockHash()
		if err != nil {
			return 0, err
		}
		bestBlock, err := t.dcrdChainSvr.GetBlock(bestBlockH)
		if err != nil {
			return 0, err
		}
		poolSize := bestBlock.MsgBlock().Header.PoolSize

		// Do not allow zero pool sizes to prevent a possible
		// panic below.
		if poolSize == 0 {
			poolSize++
		}

		avgPricePoolAmt = poolValue / dcrutil.Amount(poolSize)

		if t.priceMode == AvgPricePoolMode {
			return avgPricePoolAmt, err
		}
	}

	var ticketVWAP dcrutil.Amount
	if t.priceMode == AvgPriceVWAPMode || t.priceMode == AvgPriceDualMode {
		// Don't let the starting height be <0, which in the case of
		// uint32 ends up a really big number.
		startVWAPHeight := uint32(0)
		if height-int64(t.cfg.AvgPriceVWAPDelta) > 0 {
			startVWAPHeight = uint32(height - int64(t.cfg.AvgPriceVWAPDelta))
		}
		endVWAPHeight := uint32(height)

		// Sometimes the chain is a little slow to update, retry
		// if the chain gives us an issue.
		var err error
		for i := 0; i < vwapReqTries; i++ {
			ticketVWAP, err = t.dcrdChainSvr.TicketVWAP(&startVWAPHeight,
				&endVWAPHeight)
			if err != nil && containsVWAPHeightOffsetError(err) {
				log.Tracef("Failed to fetch ticket VWAP "+
					"on attempt %v: %v", i, err.Error())
				err = nil
				time.Sleep(vwapReqTryDelay)
				continue
			}
			if err == nil {
				break
			}
		}
		if err != nil {
			return 0, err
		}

		if t.priceMode == AvgPriceVWAPMode {
			return ticketVWAP, err
		}
	}

	return (ticketVWAP + avgPricePoolAmt) / 2, nil
}
