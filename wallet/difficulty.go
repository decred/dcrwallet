// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

// This code was copied from dcrd/blockchain/difficulty.go and modified for
// dcrwallet's header storage.

import (
	"context"
	"math/big"
	"time"

	"decred.org/dcrwallet/v5/deployments"
	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/walletdb"
	blockchain "github.com/decred/dcrd/blockchain/standalone/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

func (w *Wallet) isTestNet3() bool {
	return w.chainParams.Net == wire.TestNet3
}

const testNet3MaxDiffActivationHeight = 962928

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
			h, err = w.txStore.GetBlockHeader(dbtx, &h.PrevBlock)
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

// calcNextBlake256Diff calculates the required difficulty for the block AFTER
// the passed header based on the difficulty retarget rules for the blake256
// hash algorithm used at Decred launch.
//
// The ancestor chain of the header being tested MUST be in the wallet's main
// chain or in the passed chain slice.
func (w *Wallet) calcNextBlake256Diff(dbtx walletdb.ReadTx, header *wire.BlockHeader,
	chain []*BlockNode, newBlockTime time.Time) (uint32, error) {

	// Get the old difficulty; if we aren't at a block height where it changes,
	// just return this.
	oldDiff := header.Bits
	oldDiffBig := blockchain.CompactToBig(header.Bits)

	// We're not at a retarget point, return the oldDiff.
	params := w.chainParams
	nextHeight := int64(header.Height) + 1
	if nextHeight%params.WorkDiffWindowSize != 0 {
		// For networks that support it, allow special reduction of the
		// required difficulty once too much time has elapsed without
		// mining a block.
		//
		// Note that this behavior is deprecated and thus is only supported on
		// testnet v3 prior to the max diff activation height.  It will be
		// removed in future version of testnet.
		if params.ReduceMinDifficulty && (!w.isTestNet3() || nextHeight <
			testNet3MaxDiffActivationHeight) {

			// Return minimum difficulty when more than the desired
			// amount of time has elapsed without mining a block.
			reductionTime := int64(params.MinDiffReductionTime /
				time.Second)
			allowMinTime := header.Timestamp.Unix() + reductionTime
			if newBlockTime.Unix() > allowMinTime {
				return params.PowLimitBits, nil
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

	alpha := params.WorkDiffAlpha

	// Number of windows to traverse while calculating difficulty.
	windowsToTraverse := params.WorkDiffWindows

	// Initialize bigInt slice for the percentage changes for each window period
	// above or below the target.
	windowChanges := make([]*big.Int, params.WorkDiffWindows)

	// Regress through all of the previous blocks and store the percent changes
	// per window period; use bigInts to emulate 64.32 bit fixed point.
	//
	// The regression is made by skipping to the block where each window change
	// takes place (by height), therefore we assume that the header that is
	// tested is a child of the wallet's main chain.
	var olderTime, windowPeriod int64
	var weights uint64
	oldHeight := header.Height
	recentTime := header.Timestamp.Unix()

	ns := dbtx.ReadBucket(wtxmgrNamespaceKey)

	for i := int64(0); ; i++ {
		// Store and reset after reaching the end of every window period.
		if i != 0 {
			timeDifference := recentTime - olderTime

			// Just assume we're at the target (no change) if we've
			// gone all the way back to the genesis block.
			if oldHeight == 0 {
				timeDifference = int64(params.TargetTimespan /
					time.Second)
			}

			timeDifBig := big.NewInt(timeDifference)
			timeDifBig.Lsh(timeDifBig, 32) // Add padding
			targetTemp := big.NewInt(int64(params.TargetTimespan /
				time.Second))

			windowAdjusted := targetTemp.Div(timeDifBig, targetTemp)

			// Weight it exponentially. Be aware that this could at some point
			// overflow if alpha or the number of blocks used is really large.
			windowAdjusted = windowAdjusted.Lsh(windowAdjusted,
				uint((params.WorkDiffWindows-windowPeriod)*alpha))

			// Sum up all the different weights incrementally.
			weights += 1 << uint64((params.WorkDiffWindows-windowPeriod)*
				alpha)

			// Store it in the slice.
			windowChanges[windowPeriod] = windowAdjusted

			windowPeriod++

			recentTime = olderTime
		}

		if i == windowsToTraverse {
			break // Exit for loop when we hit the end.
		}

		// Get the previous node while staying at the genesis block as needed.
		// Query the header from the provided chain instead of database if
		// present.  The parent of chain[0] is guaranteed to be in stored in the
		// database.
		if int64(oldHeight) > params.WorkDiffWindowSize {
			oldHeight -= uint32(params.WorkDiffWindowSize)
		} else {
			oldHeight = 0
		}
		if oldHeight != 0 {
			if len(chain) > 0 && int32(oldHeight)-int32(chain[0].Header.Height) >= 0 {
				idx := oldHeight - chain[0].Header.Height
				olderTime = chain[idx].Header.Timestamp.Unix()
			} else {
				oldHeaderHash, err := w.txStore.GetMainChainBlockHashForHeight(ns, int32(oldHeight))
				if err != nil {
					return 0, err
				}
				olderTime, err = w.txStore.GetBlockHeaderTime(dbtx, &oldHeaderHash)
				if err != nil {
					return 0, err
				}
			}
		}
	}

	// Sum up the weighted window periods.
	weightedSum := big.NewInt(0)
	for i := int64(0); i < params.WorkDiffWindows; i++ {
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
		nextDiffBig.Set(params.PowLimit)
	} else if nextDiffBig.Cmp(nextDiffBigMax) == 1 {
		nextDiffBig.Set(nextDiffBigMax)
	} else if nextDiffBig.Cmp(nextDiffBigMin) == -1 {
		nextDiffBig.Set(nextDiffBigMin)
	}

	// Prevent the difficulty from going lower than the minimum allowed
	// difficulty.
	//
	// Larger numbers result in a lower difficulty, so imposing a minimum
	// difficulty equates to limiting the maximum target value.
	if nextDiffBig.Cmp(params.PowLimit) > 0 {
		nextDiffBig.Set(params.PowLimit)
	}

	// Prevent the difficulty from going higher than a maximum allowed
	// difficulty on the test network.  This is to prevent runaway difficulty on
	// testnet by ASICs and GPUs since it's not reasonable to require
	// high-powered hardware to keep the test network running smoothly.
	//
	// Smaller numbers result in a higher difficulty, so imposing a maximum
	// difficulty equates to limiting the minimum target value.
	//
	// This rule is only active on the version 3 test network once the max diff
	// activation height has been reached.
	if w.minTestNetTarget != nil && nextDiffBig.Cmp(w.minTestNetTarget) < 0 &&
		(!w.isTestNet3() || nextHeight >= testNet3MaxDiffActivationHeight) {

		nextDiffBig = w.minTestNetTarget
	}

	// Convert the difficulty to the compact representation and return it.
	nextDiffBits := blockchain.BigToCompact(nextDiffBig)
	return nextDiffBits, nil
}

func (w *Wallet) loadCachedBlake3WorkDiffCandidateAnchor() *wire.BlockHeader {
	w.cachedBlake3WorkDiffCandidateAnchorMu.Lock()
	defer w.cachedBlake3WorkDiffCandidateAnchorMu.Unlock()

	return w.cachedBlake3WorkDiffCandidateAnchor
}

func (w *Wallet) storeCachedBlake3WorkDiffCandidateAnchor(candidate *wire.BlockHeader) {
	w.cachedBlake3WorkDiffCandidateAnchorMu.Lock()
	defer w.cachedBlake3WorkDiffCandidateAnchorMu.Unlock()

	w.cachedBlake3WorkDiffCandidateAnchor = candidate
}

// isBlake3PowAgendaForcedActive returns whether or not the agenda to change the
// proof of work hash function to blake3, as defined in DCP0011, is forced
// active by the chain parameters.
func (w *Wallet) isBlake3PowAgendaForcedActive() bool {
	const deploymentID = chaincfg.VoteIDBlake3Pow
	deployment, ok := w.deploymentsByID[deploymentID]
	if !ok {
		return false
	}

	return deployment.ForcedChoiceID == "yes"
}

// calcNextBlake3DiffFromAnchor calculates the required difficulty for the block
// AFTER the passed previous block node relative to the given anchor block based
// on the difficulty retarget rules defined in DCP0011.
//
// This function is safe for concurrent access.
func (w *Wallet) calcNextBlake3DiffFromAnchor(prevNode, blake3Anchor *wire.BlockHeader) uint32 {
	// Calculate the time and height deltas as the difference between the
	// provided block and the blake3 anchor block.
	//
	// Notice that if the difficulty prior to the activation point were being
	// maintained, this would need to be the timestamp and height of the parent
	// of the blake3 anchor block (except when the anchor is the genesis block)
	// in order for the absolute calculations to exactly match the behavior of
	// relative calculations.
	//
	// However, since the initial difficulty is reset with the agenda, no
	// additional offsets are needed.
	timeDelta := prevNode.Timestamp.Unix() - blake3Anchor.Timestamp.Unix()
	heightDelta := int64(prevNode.Height) - int64(blake3Anchor.Height)

	// Calculate the next target difficulty using the ASERT algorithm.
	//
	// Note that the difficulty of the anchor block is NOT used for the initial
	// difficulty because the difficulty must be reset due to the change to
	// blake3 for proof of work.  The initial difficulty comes from the chain
	// parameters instead.
	params := w.chainParams
	nextDiff := blockchain.CalcASERTDiff(params.WorkDiffV2Blake3StartBits,
		params.PowLimit, int64(params.TargetTimePerBlock.Seconds()), timeDelta,
		heightDelta, params.WorkDiffV2HalfLifeSecs)

	// Prevent the difficulty from going higher than a maximum allowed
	// difficulty on the test network.  This is to prevent runaway difficulty on
	// testnet by ASICs and GPUs since it's not reasonable to require
	// high-powered hardware to keep the test network running smoothly.
	//
	// Smaller numbers result in a higher difficulty, so imposing a maximum
	// difficulty equates to limiting the minimum target value.
	if w.minTestNetTarget != nil && nextDiff < w.minTestNetDiffBits {
		nextDiff = w.minTestNetDiffBits
	}

	return nextDiff
}

// calcWantHeight calculates the height of the final block of the previous
// interval given a stake validation height, stake validation interval, and
// block height.
func calcWantHeight(stakeValidationHeight, interval, height int64) int64 {
	// The adjusted height accounts for the fact the starting validation
	// height does not necessarily start on an interval and thus the
	// intervals might not be zero-based.
	intervalOffset := stakeValidationHeight % interval
	adjustedHeight := height - intervalOffset - 1
	return (adjustedHeight - ((adjustedHeight + 1) % interval)) +
		intervalOffset
}

// checkDifficultyPositional ensures the difficulty specified in the block
// header matches the calculated difficulty based on the difficulty retarget
// rules.  These checks do not, and must not, rely on having the full block data
// of all ancestors available.
//
// This function is safe for concurrent access.
func (w *Wallet) checkDifficultyPositional(dbtx walletdb.ReadTx, header *wire.BlockHeader,
	prevNode *wire.BlockHeader, chain []*BlockNode) error {
	// -------------------------------------------------------------------------
	// The ability to determine whether or not the blake3 proof of work agenda
	// is active is not possible in the general case here because that relies on
	// additional context that is not available in the positional checks.
	// However, it is important to check for valid bits in the positional checks
	// to protect against various forms of malicious behavior.
	//
	// Thus, with the exception of the special cases where it is possible to
	// definitively determine the agenda is active, allow valid difficulty bits
	// under both difficulty algorithms while rejecting blocks that satisify
	// neither here in the positional checks and allow the contextual checks
	// that happen later to ensure the difficulty bits are valid specifically
	// for the correct difficulty algorithm as determined by the state of the
	// blake3 proof of work agenda.
	// -------------------------------------------------------------------------

	// Ensure the difficulty specified in the block header matches the
	// calculated difficulty using the algorithm defined in DCP0011 when the
	// blake3 proof of work agenda is always active.
	//
	// Apply special handling for networks where the agenda is always active to
	// always require the initial starting difficulty for the first block and to
	// treat the first block as the anchor once it has been mined.
	//
	// This is to done to help provide better difficulty target behavior for the
	// initial blocks on such networks since the genesis block will necessarily
	// have a hard-coded timestamp that will very likely be outdated by the time
	// mining starts.  As a result, merely using the genesis block as the anchor
	// for all blocks would likely result in a lot of the initial blocks having
	// a significantly lower difficulty than desired because they would all be
	// behind the ideal schedule relative to that outdated timestamp.
	if w.isBlake3PowAgendaForcedActive() {
		var blake3Diff uint32

		// Use the initial starting difficulty for the first block.
		if prevNode.Height == 0 {
			blake3Diff = w.chainParams.WorkDiffV2Blake3StartBits
		} else {
			// Treat the first block as the anchor for all descendants of it.
			anchor, err := w.ancestorHeaderAtHeight(dbtx, prevNode, chain, 1)
			if err != nil {
				return err
			}
			blake3Diff = w.calcNextBlake3DiffFromAnchor(prevNode, anchor)
		}

		if header.Bits != blake3Diff {
			err := errors.Errorf("%w: block difficulty of %d is not the expected "+
				"value of %d (difficulty algorithm: ASERT)",
				blockchain.ErrUnexpectedDifficulty, header.Bits, blake3Diff)
			return errors.E(errors.Consensus, err)
		}

		return nil
	}

	// Only the original difficulty algorithm needs to be checked when it is
	// impossible for the blake3 proof of work agenda to be active or the block
	// is not solved for blake3.
	//
	// Note that since the case where the blake3 proof of work agenda is always
	// active is already handled above, the only remaining way for the agenda to
	// be active is for it to have been voted in which requires voting to be
	// possible (stake validation height), at least one interval of voting, and
	// one interval of being locked in.
	isSolvedBlake3 := func(header *wire.BlockHeader) bool {
		powHash := header.PowHashV2()
		err := blockchain.CheckProofOfWorkHash(&powHash, header.Bits)
		return err == nil
	}
	rcai := int64(w.chainParams.RuleChangeActivationInterval)
	svh := w.chainParams.StakeValidationHeight
	firstPossibleActivationHeight := svh + rcai*2
	minBlake3BlockVersion := uint32(10)
	if w.chainParams.Net != wire.MainNet {
		minBlake3BlockVersion++
	}
	isBlake3PossiblyActive := uint32(header.Version) >= minBlake3BlockVersion &&
		int64(header.Height) >= firstPossibleActivationHeight
	if !isBlake3PossiblyActive || !isSolvedBlake3(header) {
		// Ensure the difficulty specified in the block header matches the
		// calculated difficulty based on the previous block and difficulty
		// retarget rules for the blake256 hash algorithm used at Decred launch.
		blake256Diff, err := w.calcNextBlake256Diff(dbtx, prevNode, chain, header.Timestamp)
		if err != nil {
			return err
		}
		if header.Bits != blake256Diff {
			err := errors.Errorf("%w: block difficulty of %d is not the expected "+
				"value of %d (difficulty algorithm: EMA)",
				blockchain.ErrUnexpectedDifficulty, header.Bits, blake256Diff)
			return errors.E(errors.Consensus, err)
		}

		return nil
	}

	// At this point, the blake3 proof of work agenda might possibly be active
	// and the block is solved using blake3, so the agenda is very likely
	// active.
	//
	// Calculating the required difficulty once the agenda activates for the
	// algorithm defined in DCP0011 requires the block prior to the activation
	// of the agenda as an anchor.  However, as previously discussed, the
	// additional context needed to definitively determine when the agenda
	// activated is not available here in the positional checks.
	//
	// In light of that, the following logic uses the fact that the agenda could
	// have only possibly activated at a rule change activation interval to
	// iterate backwards one interval at a time through all possible candidate
	// anchors until one of them results in a required difficulty that matches.
	//
	// In the case there is a match, the header is assumed to be valid enough to
	// make it through the positional checks.
	//
	// As an additional optimization to avoid a bunch of extra work during the
	// initial header sync, a candidate anchor that results in a matching
	// required difficulty is cached and tried first on subsequent descendant
	// headers since it is very likely to be the correct one.
	cachedCandidate := w.loadCachedBlake3WorkDiffCandidateAnchor()
	if cachedCandidate != nil {
		isAncestor, err := w.isAncestorOf(dbtx, cachedCandidate, prevNode, chain)
		if err != nil {
			return err
		}
		if isAncestor {
			blake3Diff := w.calcNextBlake3DiffFromAnchor(prevNode, cachedCandidate)
			if header.Bits == blake3Diff {
				return nil
			}
		}
	}

	// Iterate backwards through all possible anchor candidates which
	// consist of the final blocks of previous rule change activation
	// intervals so long as the block also has a version that is at least
	// the minimum version that is enforced before voting on the agenda
	// could have even started and the agenda could still possibly be
	// active.
	finalNodeHeight := calcWantHeight(svh, rcai, int64(header.Height))
	candidate, err := w.ancestorHeaderAtHeight(dbtx, prevNode, chain, int32(finalNodeHeight))
	if err != nil {
		return err
	}
	for candidate != nil &&
		uint32(candidate.Version) >= minBlake3BlockVersion &&
		int64(candidate.Height) >= firstPossibleActivationHeight-1 {

		blake3Diff := w.calcNextBlake3DiffFromAnchor(prevNode, candidate)
		if header.Bits == blake3Diff {
			w.storeCachedBlake3WorkDiffCandidateAnchor(candidate)
			return nil
		}
		candidate, err = w.relativeAncestor(dbtx, candidate, rcai, chain)
		if err != nil {
			return err
		}
	}

	// At this point, none of the possible difficulties for blake3 matched, so
	// the agenda is very likely not actually active and therefore the only
	// remaining valid option is the original difficulty algorithm.
	//
	// Ensure the difficulty specified in the block header matches the
	// calculated difficulty based on the previous block and difficulty retarget
	// rules for the blake256 hash algorithm used at Decred launch.
	blake256Diff, err := w.calcNextBlake256Diff(dbtx, prevNode, chain, header.Timestamp)
	if err != nil {
		return err
	}
	if header.Bits != blake256Diff {
		err := errors.Errorf("%w: block difficulty of %d is not the expected value "+
			"of %d (difficulty algorithm: EMA)",
			blockchain.ErrUnexpectedDifficulty, header.Bits, blake256Diff)
		return errors.E(errors.Consensus, err)
	}

	return nil
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
		h, err = w.txStore.GetBlockHeader(dbtx, &h.PrevBlock)
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
	hash, err := w.txStore.GetMainChainBlockHashForHeight(ns, height)
	if err != nil {
		return nil, err
	}
	return w.txStore.GetBlockHeader(dbtx, &hash)
}

// isAncestorOf returns whether or not node is an ancestor of the provided
// target node.
//
// Replaces dcrd's internal/blockchain func (node *blockNode).IsAncestorOf(target *blockNode).
func (w *Wallet) isAncestorOf(dbtx walletdb.ReadTx, node, target *wire.BlockHeader,
	chain []*BlockNode) (bool, error) {

	ancestorHeader, err := w.ancestorHeaderAtHeight(dbtx, target, chain, int32(node.Height))
	if err != nil {
		return false, err
	}
	return ancestorHeader.BlockHash() == node.BlockHash(), nil
}

// relativeAncestor returns the ancestor block node a relative 'distance' blocks
// before this node.  This is equivalent to calling Ancestor with the node's
// height minus provided distance.
//
// Replaces dcrd's internal/blockchain func (node *blockNode) RelativeAncestor(distance int64).
func (w *Wallet) relativeAncestor(dbtx walletdb.ReadTx, node *wire.BlockHeader,
	distance int64, chain []*BlockNode) (*wire.BlockHeader, error) {

	return w.ancestorHeaderAtHeight(dbtx, node, chain, int32(node.Height)-int32(distance))
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
func (w *Wallet) NextStakeDifficulty(ctx context.Context) (dcrutil.Amount, error) {
	const op errors.Op = "wallet.NextStakeDifficulty"
	var sdiff dcrutil.Amount
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		tipHash, tipHeight := w.txStore.MainChainTip(dbtx)
		if !deployments.DCP0001.Active(tipHeight, w.chainParams.Net) {
			return errors.E(errors.Deployment, "DCP0001 is not known to be active")
		}
		tipHeader, err := w.txStore.GetBlockHeader(dbtx, &tipHash)
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
func (w *Wallet) NextStakeDifficultyAfterHeader(ctx context.Context, h *wire.BlockHeader) (dcrutil.Amount, error) {
	const op errors.Op = "wallet.NextStakeDifficultyAfterHeader"
	if !deployments.DCP0001.Active(int32(h.Height), w.chainParams.Net) {
		return 0, errors.E(op, errors.Deployment, "DCP0001 is not known to be active")
	}
	var sdiff dcrutil.Amount
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
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
func (w *Wallet) ValidateHeaderChainDifficulties(ctx context.Context, chain []*BlockNode, idx int) ([]*BlockNode, error) {
	var invalid []*BlockNode
	err := walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		var err error
		invalid, err = w.validateHeaderChainDifficulties(dbtx, chain, idx)
		return err
	})
	return invalid, err
}

func (w *Wallet) validateHeaderChainDifficulties(dbtx walletdb.ReadTx, chain []*BlockNode, idx int) ([]*BlockNode, error) {
	const op errors.Op = "wallet.validateHeaderChainDifficulties"

	inMainChain, _ := w.txStore.BlockInMainChain(dbtx, &chain[0].Header.PrevBlock)
	if !inMainChain {
		return nil, errors.E(op, errors.Bug, "parent of chain[0] is not in main chain")
	}

	var parent *wire.BlockHeader

	for ; idx < len(chain); idx++ {
		n := chain[idx]
		h := n.Header
		hash := n.Hash
		if parent == nil && h.Height != 0 {
			if idx == 0 {
				var err error
				parent, err = w.txStore.GetBlockHeader(dbtx, &h.PrevBlock)
				if err != nil {
					return nil, err
				}
			} else {
				parent = chain[idx-1].Header
			}
		}

		// Validate advertised and performed work
		err := w.checkDifficultyPositional(dbtx, h, parent, chain)
		if err != nil {
			return chain[idx:], errors.E(op, err)
		}
		// Check V1 Proof of Work
		err = blockchain.CheckProofOfWork(hash, h.Bits, w.chainParams.PowLimit)
		if err != nil {
			// Check V2 Proof of Work
			blake3PowHash := n.Header.PowHashV2()
			err = blockchain.CheckProofOfWork(&blake3PowHash, h.Bits,
				w.chainParams.PowLimit)
		}
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
					hash, dcrutil.Amount(h.SBits), sdiff)
				return chain[idx:], errors.E(op, errors.Consensus, err)
			}
		}

		parent = h
	}

	return nil, nil
}
