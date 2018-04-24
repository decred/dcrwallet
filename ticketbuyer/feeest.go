// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ticketbuyer

import (
	"sort"

	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrwallet/errors"
)

const (
	// windowsToConsider is the number of windows to consider
	// when there is not enough block information to determine
	// what the best fee should be.
	windowsToConsider = 20
)

// diffPeriodFee defines some statistics about a difficulty fee period
// compared to the current difficulty period.
type diffPeriodFee struct {
	difficulty dcrutil.Amount
	difference dcrutil.Amount // Difference from current difficulty
	fee        dcrutil.Amount
}

// diffPeriodFees is slice type definition used to satisfy the sorting
// interface.
type diffPeriodFees []*diffPeriodFee

func (p diffPeriodFees) Len() int { return len(p) }
func (p diffPeriodFees) Less(i, j int) bool {
	return p[i].difference < p[j].difference
}
func (p diffPeriodFees) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

// findClosestFeeWindows is used when there is not enough block information
// from recent blocks to figure out what to set the user's ticket fees to.
// Instead, it uses data from the last windowsToConsider many windows and
// takes an average fee from the closest one.
func (t *TicketPurchaser) findClosestFeeWindows(difficulty dcrutil.Amount,
	useMedian bool) (dcrutil.Amount, error) {
	wtcUint32 := uint32(windowsToConsider)
	info, err := t.dcrdChainSvr.TicketFeeInfo(&zeroUint32, &wtcUint32)
	if err != nil {
		return 0.0, err
	}

	if len(info.FeeInfoWindows) == 0 {
		return 0.0, errors.Errorf("not enough windows to find mean fee " +
			"available")
	}

	// Fetch all the mean fees and window difficulties. Calculate
	// the difference from the current window and sort, then use
	// the mean fee from the period that has the closest difficulty.
	var sortable diffPeriodFees
	for i := range info.FeeInfoWindows {
		// Skip the first window if it's not full.
		span := info.FeeInfoWindows[i].EndHeight -
			info.FeeInfoWindows[i].StartHeight
		if i == 0 && int64(span) < t.activeNet.StakeDiffWindowSize {
			continue
		}

		startHeight := int64(info.FeeInfoWindows[i].StartHeight)
		blH, err := t.dcrdChainSvr.GetBlockHash(startHeight)
		if err != nil {
			return 0, err
		}
		blkHeader, err := t.dcrdChainSvr.GetBlockHeader(blH)
		if err != nil {
			return 0, err
		}

		windowDiffAmt := dcrutil.Amount(blkHeader.SBits)

		var fee dcrutil.Amount
		if !useMedian {
			fee, err = dcrutil.NewAmount(info.FeeInfoWindows[i].Mean)
		} else {
			fee, err = dcrutil.NewAmount(info.FeeInfoWindows[i].Median)
		}
		if err != nil {
			return 0, err
		}

		// Skip all windows for which fee information does not exist
		// because tickets were not purchased.
		if fee == 0 {
			continue
		}

		// Absolute value
		diff := windowDiffAmt - difficulty
		if diff < 0 {
			diff *= -1
		}

		dpf := &diffPeriodFee{
			difficulty: windowDiffAmt,
			difference: diff,
			fee:        fee,
		}
		sortable = append(sortable, dpf)
	}

	sort.Sort(sortable)

	// No data available, prevent a panic.
	if len(sortable) == 0 {
		return 0, nil
	}

	return sortable[0].fee, nil
}

// findMeanTicketFeeBlocks finds the mean of the mean of fees from BlocksToAvg
// many blocks using the ticketfeeinfo RPC API.
func (t *TicketPurchaser) findTicketFeeBlocks(useMedian bool) (dcrutil.Amount, error) {
	btaUint32 := uint32(t.cfg.BlocksToAvg)
	info, err := t.dcrdChainSvr.TicketFeeInfo(&btaUint32, nil)
	if err != nil {
		return 0.0, err
	}

	var sum, tmp dcrutil.Amount
	for i := range info.FeeInfoBlocks {
		if !useMedian {
			tmp, err = dcrutil.NewAmount(info.FeeInfoBlocks[i].Mean)
		} else {
			tmp, err = dcrutil.NewAmount(info.FeeInfoBlocks[i].Median)
		}
		if err != nil {
			return 0, err
		}
		sum += tmp
	}

	return sum / dcrutil.Amount(t.cfg.BlocksToAvg), nil
}
