// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

// This code was copied from dcrd/blockchain/difficulty.go and modified for
// dcrwallet's header storage.

import (
	"math/big"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/deployments/v2"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

var (
	// bigZero is 0 represented as a big.Int.  It is defined here to avoid
	// the overhead of creating it multiple times.
	bigZero = big.NewInt(0)
)

// findPrevTestNetDifficulty returns the difficulty of the previous block which
// did not have the special testnet minimum difficulty rule applied.
func (w *Wallet) findPrevTestNetDifficulty(dbtx walletdb.ReadTx, h *wire.BlockHeader, chain []*BlockNode) (uint32, error) {
	// Search backwards through the chain for the last block without
	// the special rule applied.
	blocksPerRetarget := w.chainParams.WorkDiffWindowSize * w.chainParams.WorkDiffWindows
	for int64(h.Height)%blocksPerRetarget != 0 && h.Bits == w.chainParams.PowLimitBits {
		if h.PrevBlock == (chainhash.Hash{}) {
			h = nil
			break
		}

		if len(chain) > 0 && int32(h.Height)-int32(chain[0].Header.Height) > 0 {
			h = chain[h.Height-chain[0].Header.Height-1].Header
		} else {
			var err error
			h, err = w.TxStore.GetBlockHeader(dbtx, &h.PrevBlock)
			if err != nil {
				return 0, err
			}
		}
	}

	// Return the found difficulty or the minimum difficulty if no
	// appropriate block was found.
	lastBits := w.chainParams.PowLimitBits
	if h != nil {
		lastBits = h.Bits
	}
	return lastBits, nil
}

// nextRequiredPoWDifficulty calculates the required proof-of-work difficulty
// for the block that references header as a parent.
func (w *Wallet) nextRequiredPoWDifficulty(dbtx walletdb.ReadTx, header *wire.BlockHeader, chain []*BlockNode, newBlockTime time.Time) (uint32, error) {
	// Get the old difficulty; if we aren't at a block height where it changes,
	// just return this.
	oldDiff := header.Bits
	oldDiffBig := blockchain.CompactToBig(header.Bits)

	// We're not at a retarget point, return the oldDiff.
	if (int64(header.Height)+1)%w.chainParams.WorkDiffWindowSize != 0 {
		// For networks that support it, allow special reduction of the
		// required difficulty once too much time has elapsed without
		// mining a block.
		if w.chainParams.ReduceMinDifficulty {
			// Return minimum difficulty when more than the desired
			// amount of time has elapsed without mining a block.
			reductionTime := int64(w.chainParams.MinDiffReductionTime /
				time.Second)
			allowMinTime := header.Timestamp.Unix() + reductionTime

			if newBlockTime.Unix() > allowMinTime {
				return w.chainParams.PowLimitBits, nil
			}

			// The block was mined within the desired timeframe, so
			// return the difficulty for the last block which did
			// not have the special minimum difficulty rule applied.
			return w.findPrevTestNetDifficulty(dbtx, header, chain)
		}

		return oldDiff, nil
	}

	// Declare some useful variables.
	RAFBig := big.NewInt(w.chainParams.RetargetAdjustmentFactor)
	nextDiffBigMin := blockchain.CompactToBig(header.Bits)
	nextDiffBigMin.Div(nextDiffBigMin, RAFBig)
	nextDiffBigMax := blockchain.CompactToBig(header.Bits)
	nextDiffBigMax.Mul(nextDiffBigMax, RAFBig)

	alpha := w.chainParams.WorkDiffAlpha

	// Number of nodes to traverse while calculating difficulty.
	nodesToTraverse := (w.chainParams.WorkDiffWindowSize *
		w.chainParams.WorkDiffWindows)

	// Initialize bigInt slice for the percentage changes for each window period
	// above or below the target.
	windowChanges := make([]*big.Int, w.chainParams.WorkDiffWindows)

	// Regress through all of the previous blocks and store the percent changes
	// per window period; use bigInts to emulate 64.32 bit fixed point.
	var olderTime, windowPeriod int64
	var weights uint64
	oldHeader := header
	recentTime := header.Timestamp.Unix()

	for i := int64(0); ; i++ {
		// Store and reset after reaching the end of every window period.
		if i%w.chainParams.WorkDiffWindowSize == 0 && i != 0 {
			olderTime = oldHeader.Timestamp.Unix()
			timeDifference := recentTime - olderTime

			// Just assume we're at the target (no change) if we've
			// gone all the way back to the genesis block.
			if oldHeader.Height == 0 {
				timeDifference = int64(w.chainParams.TargetTimespan /
					time.Second)
			}

			timeDifBig := big.NewInt(timeDifference)
			timeDifBig.Lsh(timeDifBig, 32) // Add padding
			targetTemp := big.NewInt(int64(w.chainParams.TargetTimespan /
				time.Second))

			windowAdjusted := targetTemp.Div(timeDifBig, targetTemp)

			// Weight it exponentially. Be aware that this could at some point
			// overflow if alpha or the number of blocks used is really large.
			windowAdjusted = windowAdjusted.Lsh(windowAdjusted,
				uint((w.chainParams.WorkDiffWindows-windowPeriod)*alpha))

			// Sum up all the different weights incrementally.
			weights += 1 << uint64((w.chainParams.WorkDiffWindows-windowPeriod)*
				alpha)

			// Store it in the slice.
			windowChanges[windowPeriod] = windowAdjusted

			windowPeriod++

			recentTime = olderTime
		}

		if i == nodesToTraverse {
			break // Exit for loop when we hit the end.
		}

		// Get the previous node while staying at the genesis block as needed.
		// Query the header from the provided chain instead of database if
		// present.  The parent of chain[0] is guaranteed to be in stored in the
		// database.
		if oldHeader.Height != 0 {
			if len(chain) > 0 && int32(oldHeader.Height)-int32(chain[0].Header.Height) > 0 {
				oldHeader = chain[oldHeader.Height-chain[0].Header.Height-1].Header
			} else {
				var err error
				oldHeader, err = w.TxStore.GetBlockHeader(dbtx, &oldHeader.PrevBlock)
				if err != nil {
					return 0, err
				}
			}
		}
	}

	// Sum up the weighted window periods.
	weightedSum := big.NewInt(0)
	for i := int64(0); i < w.chainParams.WorkDiffWindows; i++ {
		weightedSum.Add(weightedSum, windowChanges[i])
	}

	// Divide by the sum of all weights.
	weightsBig := big.NewInt(int64(weights))
	weightedSumDiv := weightedSum.Div(weightedSum, weightsBig)

	// Multiply by the old diff.
	nextDiffBig := weightedSumDiv.Mul(weightedSumDiv, oldDiffBig)

	// Right shift to restore the original padding (restore non-fixed point).
	nextDiffBig = nextDiffBig.Rsh(nextDiffBig, 32)

	// Check to see if we're over the limits for the maximum allowable retarget;
	// if we are, return the maximum or minimum except in the case that oldDiff
	// is zero.
	if oldDiffBig.Cmp(bigZero) == 0 { // This should never really happen,
		nextDiffBig.Set(nextDiffBig) // but in case it does...
	} else if nextDiffBig.Cmp(bigZero) == 0 {
		nextDiffBig.Set(w.chainParams.PowLimit)
	} else if nextDiffBig.Cmp(nextDiffBigMax) == 1 {
		nextDiffBig.Set(nextDiffBigMax)
	} else if nextDiffBig.Cmp(nextDiffBigMin) == -1 {
		nextDiffBig.Set(nextDiffBigMin)
	}

	// Limit new value to the proof of work limit.
	if nextDiffBig.Cmp(w.chainParams.PowLimit) > 0 {
		nextDiffBig.Set(w.chainParams.PowLimit)
	}

	// Log new target difficulty and return it.  The new target logging is
	// intentionally converting the bits back to a number instead of using
	// newTarget since conversion to the compact representation loses
	// precision.
	nextDiffBits := blockchain.BigToCompact(nextDiffBig)
	log.Debugf("Difficulty retarget at block height %d", header.Height+1)
	log.Debugf("Old target %08x (%064x)", header.Bits, oldDiffBig)
	log.Debugf("New target %08x (%064x)", nextDiffBits, blockchain.CompactToBig(nextDiffBits))

	return nextDiffBits, nil
}

// estimateSupply returns an estimate of the coin supply for the provided block
// height.  This is primarily used in the stake difficulty algorithm and relies
// on an estimate to simplify the necessary calculations.  The actual total
// coin supply as of a given block height depends on many factors such as the
// number of votes included in every prior block (not including all votes
// reduces the subsidy) and whether or not any of the prior blocks have been
// invalidated by stakeholders thereby removing the PoW subsidy for them.
func estimateSupply(params *chaincfg.Params, height int64) int64 {
	if height <= 0 {
		return 0
	}

	// Estimate the supply by calculating the full block subsidy for each
	// reduction interval and multiplying it the number of blocks in the
	// interval then adding the subsidy produced by number of blocks in the
	// current interval.
	supply := params.BlockOneSubsidy()
	reductions := height / params.SubsidyReductionInterval
	subsidy := params.BaseSubsidy
	for i := int64(0); i < reductions; i++ {
		supply += params.SubsidyReductionInterval * subsidy

		subsidy *= params.MulSubsidy
		subsidy /= params.DivSubsidy
	}
	supply += (1 + height%params.SubsidyReductionInterval) * subsidy

	// Blocks 0 and 1 have special subsidy amounts that have already been
	// added above, so remove what their subsidies would have normally been
	// which were also added above.
	supply -= params.BaseSubsidy * 2

	return supply
}

// sumPurchasedTickets returns the sum of the number of tickets purchased in the
// most recent specified number of blocks from the point of view of the passed
// header.
func (w *Wallet) sumPurchasedTickets(dbtx walletdb.ReadTx, startHeader *wire.BlockHeader, chain []*BlockNode, numToSum int64) (int64, error) {
	var numPurchased int64
	for h, numTraversed := startHeader, int64(0); h != nil && numTraversed < numToSum; numTraversed++ {
		numPurchased += int64(h.FreshStake)
		if h.PrevBlock == (chainhash.Hash{}) {
			break
		}
		if len(chain) > 0 && int32(h.Height)-int32(chain[0].Header.Height) > 0 {
			h = chain[h.Height-chain[0].Header.Height-1].Header
			continue
		}
		var err error
		h, err = w.TxStore.GetBlockHeader(dbtx, &h.PrevBlock)
		if err != nil {
			return 0, err
		}
	}

	return numPurchased, nil
}

// calcNextStakeDiffV2 calculates the next stake difficulty for the given set
// of parameters using the algorithm defined in DCP0001.
//
// This function contains the heart of the algorithm and thus is separated for
// use in both the actual stake difficulty calculation as well as estimation.
//
// The caller must perform all of the necessary chain traversal in order to
// get the current difficulty, previous retarget interval's pool size plus
// its immature tickets, as well as the current pool size plus immature tickets.
func calcNextStakeDiffV2(params *chaincfg.Params, nextHeight, curDiff, prevPoolSizeAll, curPoolSizeAll int64) int64 {
	// Shorter version of various parameter for convenience.
	votesPerBlock := int64(params.TicketsPerBlock)
	ticketPoolSize := int64(params.TicketPoolSize)
	ticketMaturity := int64(params.TicketMaturity)

	// Calculate the difficulty by multiplying the old stake difficulty
	// with two ratios that represent a force to counteract the relative
	// change in the pool size (Fc) and a restorative force to push the pool
	// size  towards the target value (Fr).
	//
	// Per DCP0001, the generalized equation is:
	//
	//   nextDiff = min(max(curDiff * Fc * Fr, Slb), Sub)
	//
	// The detailed form expands to:
	//
	//                        curPoolSizeAll      curPoolSizeAll
	//   nextDiff = curDiff * ---------------  * -----------------
	//                        prevPoolSizeAll    targetPoolSizeAll
	//
	//   Slb = w.chainParams.MinimumStakeDiff
	//
	//               estimatedTotalSupply
	//   Sub = -------------------------------
	//          targetPoolSize / votesPerBlock
	//
	// In order to avoid the need to perform floating point math which could
	// be problematic across languages due to uncertainty in floating point
	// math libs, this is further simplified to integer math as follows:
	//
	//                   curDiff * curPoolSizeAll^2
	//   nextDiff = -----------------------------------
	//              prevPoolSizeAll * targetPoolSizeAll
	//
	// Further, the Sub parameter must calculate the denomitor first using
	// integer math.
	targetPoolSizeAll := votesPerBlock * (ticketPoolSize + ticketMaturity)
	curPoolSizeAllBig := big.NewInt(curPoolSizeAll)
	nextDiffBig := big.NewInt(curDiff)
	nextDiffBig.Mul(nextDiffBig, curPoolSizeAllBig)
	nextDiffBig.Mul(nextDiffBig, curPoolSizeAllBig)
	nextDiffBig.Div(nextDiffBig, big.NewInt(prevPoolSizeAll))
	nextDiffBig.Div(nextDiffBig, big.NewInt(targetPoolSizeAll))

	// Limit the new stake difficulty between the minimum allowed stake
	// difficulty and a maximum value that is relative to the total supply.
	//
	// NOTE: This is intentionally using integer math to prevent any
	// potential issues due to uncertainty in floating point math libs.  The
	// ticketPoolSize parameter already contains the result of
	// (targetPoolSize / votesPerBlock).
	nextDiff := nextDiffBig.Int64()
	estimatedSupply := estimateSupply(params, nextHeight)
	maximumStakeDiff := estimatedSupply / ticketPoolSize
	if nextDiff > maximumStakeDiff {
		nextDiff = maximumStakeDiff
	}
	if nextDiff < params.MinimumStakeDiff {
		nextDiff = params.MinimumStakeDiff
	}
	return nextDiff
}

func (w *Wallet) ancestorHeaderAtHeight(dbtx walletdb.ReadTx, h *wire.BlockHeader, chain []*BlockNode, height int32) (*wire.BlockHeader, error) {
	switch {
	case height == int32(h.Height):
		return h, nil
	case height > int32(h.Height), height < 0:
		return nil, nil // dcrd's blockNode.Ancestor returns nil for child heights
	}

	if len(chain) > 0 && height-int32(chain[0].Header.Height) >= 0 {
		return chain[height-int32(chain[0].Header.Height)].Header, nil
	}

	// Because the parent of chain[0] must be in the main chain, the header can
	// be queried by its main chain height.
	ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
	hash, err := w.TxStore.GetMainChainBlockHashForHeight(ns, height)
	if err != nil {
		return nil, err
	}
	return w.TxStore.GetBlockHeader(dbtx, &hash)
}

// nextRequiredDCP0001PoSDifficulty calculates the required stake difficulty for
// the block after the passed previous block node based on the algorithm defined
// in DCP0001.
func (w *Wallet) nextRequiredDCP0001PoSDifficulty(dbtx walletdb.ReadTx, curHeader *wire.BlockHeader, chain []*BlockNode) (dcrutil.Amount, error) {
	// Stake difficulty before any tickets could possibly be purchased is
	// the minimum value.
	nextHeight := int64(0)
	if curHeader != nil {
		nextHeight = int64(curHeader.Height) + 1
	}
	stakeDiffStartHeight := int64(w.chainParams.CoinbaseMaturity) + 1
	if nextHeight < stakeDiffStartHeight {
		return dcrutil.Amount(w.chainParams.MinimumStakeDiff), nil
	}

	// Return the previous block's difficulty requirements if the next block
	// is not at a difficulty retarget interval.
	intervalSize := w.chainParams.StakeDiffWindowSize
	curDiff := curHeader.SBits
	if nextHeight%intervalSize != 0 {
		return dcrutil.Amount(curDiff), nil
	}

	// Get the pool size and number of tickets that were immature at the
	// previous retarget interval.
	//
	// NOTE: Since the stake difficulty must be calculated based on existing
	// blocks, it is always calculated for the block after a given block, so
	// the information for the previous retarget interval must be retrieved
	// relative to the block just before it to coincide with how it was
	// originally calculated.
	var prevPoolSize int64
	prevRetargetHeight := nextHeight - intervalSize - 1
	prevRetargetHeader, err := w.ancestorHeaderAtHeight(dbtx, curHeader, chain, int32(prevRetargetHeight))
	if err != nil {
		return 0, err
	}
	if prevRetargetHeader != nil {
		prevPoolSize = int64(prevRetargetHeader.PoolSize)
	}
	ticketMaturity := int64(w.chainParams.TicketMaturity)
	prevImmatureTickets, err := w.sumPurchasedTickets(dbtx, prevRetargetHeader, chain, ticketMaturity)
	if err != nil {
		return 0, err
	}

	// Return the existing ticket price for the first few intervals to avoid
	// division by zero and encourage initial pool population.
	prevPoolSizeAll := prevPoolSize + prevImmatureTickets
	if prevPoolSizeAll == 0 {
		return dcrutil.Amount(curDiff), nil
	}

	// Count the number of currently immature tickets.
	immatureTickets, err := w.sumPurchasedTickets(dbtx, curHeader, chain, ticketMaturity)
	if err != nil {
		return 0, err
	}

	// Calculate and return the final next required difficulty.
	curPoolSizeAll := int64(curHeader.PoolSize) + immatureTickets
	sdiff := calcNextStakeDiffV2(w.chainParams, nextHeight, curDiff, prevPoolSizeAll, curPoolSizeAll)
	return dcrutil.Amount(sdiff), nil
}

// NextStakeDifficulty returns the ticket price for the next block after the
// current main chain tip block.  This function only succeeds when DCP0001 is
// known to be active.  As a fallback, the StakeDifficulty method of
// wallet.NetworkBackend may be used to query the next ticket price from a
// trusted full node.
func (w *Wallet) NextStakeDifficulty() (dcrutil.Amount, error) {
	const op errors.Op = "wallet.NextStakeDifficulty"
	var sdiff dcrutil.Amount
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		ns := dbtx.ReadBucket(wtxmgrNamespaceKey)
		tipHash, tipHeight := w.TxStore.MainChainTip(ns)
		if !deployments.DCP0001.Active(tipHeight, w.chainParams.Net) {
			return errors.E(errors.Deployment, "DCP0001 is not known to be active")
		}
		tipHeader, err := w.TxStore.GetBlockHeader(dbtx, &tipHash)
		if err != nil {
			return err
		}
		sdiff, err = w.nextRequiredDCP0001PoSDifficulty(dbtx, tipHeader, nil)
		return err
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return sdiff, nil
}

// NextStakeDifficultyAfterHeader returns the ticket price for the child of h.
// All headers of ancestor blocks of h must be recorded by the wallet.  This
// function only succeeds when DCP0001 is known to be active.
func (w *Wallet) NextStakeDifficultyAfterHeader(h *wire.BlockHeader) (dcrutil.Amount, error) {
	const op errors.Op = "wallet.NextStakeDifficultyAfterHeader"
	if !deployments.DCP0001.Active(int32(h.Height), w.chainParams.Net) {
		return 0, errors.E(op, errors.Deployment, "DCP0001 is not known to be active")
	}
	var sdiff dcrutil.Amount
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		sdiff, err = w.nextRequiredDCP0001PoSDifficulty(dbtx, h, nil)
		return err
	})
	if err != nil {
		return 0, errors.E(op, err)
	}
	return sdiff, nil
}

// ValidateHeaderChainDifficulties validates the PoW and PoS difficulties of all
// blocks in chain[idx:].  The parent of chain[0] must be recorded as wallet
// main chain block.  If a consensus violation is caught, a subslice of chain
// beginning with the invalid block is returned.
func (w *Wallet) ValidateHeaderChainDifficulties(chain []*BlockNode, idx int) ([]*BlockNode, error) {
	var invalid []*BlockNode
	err := walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		invalid, err = w.validateHeaderChainDifficulties(dbtx, chain, idx)
		return err
	})
	return invalid, err
}

func (w *Wallet) validateHeaderChainDifficulties(dbtx walletdb.ReadTx, chain []*BlockNode, idx int) ([]*BlockNode, error) {
	const op errors.Op = "wallet.validateHeaderChainDifficulties"

	inMainChain, _ := w.TxStore.BlockInMainChain(dbtx, &chain[0].Header.PrevBlock)
	if !inMainChain {
		return nil, errors.E(op, errors.Bug, "parent of chain[0] is not in main chain")
	}

	var parent *wire.BlockHeader

	for ; idx < len(chain); idx++ {
		n := chain[idx]
		h := n.Header
		hash := h.BlockHash()
		if parent == nil && h.Height != 0 {
			if idx == 0 {
				var err error
				parent, err = w.TxStore.GetBlockHeader(dbtx, &h.PrevBlock)
				if err != nil {
					return nil, err
				}
			} else {
				parent = chain[idx-1].Header
			}
		}

		// Validate advertised and performed work
		bits, err := w.nextRequiredPoWDifficulty(dbtx, parent, chain, h.Timestamp)
		if err != nil {
			return nil, errors.E(op, err)
		}
		if h.Bits != bits {
			err := errors.Errorf("%v has invalid PoW difficulty, got %x, want %x",
				&hash, h.Bits, bits)
			return chain[idx:], errors.E(op, errors.Consensus, err)
		}
		err = blockchain.CheckProofOfWork(h, w.chainParams.PowLimit)
		if err != nil {
			return chain[idx:], errors.E(op, errors.Consensus, err)
		}

		// Validate ticket price
		if deployments.DCP0001.Active(int32(h.Height), w.chainParams.Net) {
			sdiff, err := w.nextRequiredDCP0001PoSDifficulty(dbtx, parent, chain)
			if err != nil {
				return nil, errors.E(op, err)
			}
			if dcrutil.Amount(h.SBits) != sdiff {
				err := errors.Errorf("%v has invalid PoS difficulty, got %v, want %v",
					&hash, dcrutil.Amount(h.SBits), sdiff)
				return chain[idx:], errors.E(op, errors.Consensus, err)
			}
		}

		parent = h
	}

	return nil, nil
}
