// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"context"
	"math/big"
	"time"

	"decred.org/dcrwallet/errors"
	"decred.org/dcrwallet/wallet/txrules"
	"decred.org/dcrwallet/wallet/udb"
	"decred.org/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrd/blockchain/stake/v3"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	gcs2 "github.com/decred/dcrd/gcs/v2"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
)

func (w *Wallet) extendMainChain(ctx context.Context, op errors.Op, dbtx walletdb.ReadWriteTx,
	header *wire.BlockHeader, f *gcs2.FilterV2, transactions []*wire.MsgTx) ([]wire.OutPoint, error) {
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	blockHash := header.BlockHash()

	// Propagate the error unless this block is already included in the main
	// chain.
	err := w.txStore.ExtendMainChain(txmgrNs, header, f)
	if err != nil && !errors.Is(err, errors.Exist) {
		return nil, errors.E(op, err)
	}

	// Notify interested clients of the connected block.
	w.NtfnServer.notifyAttachedBlock(dbtx, header, &blockHash)

	blockMeta, err := w.txStore.GetBlockMetaForHash(txmgrNs, &blockHash)
	if err != nil {
		return nil, errors.E(op, err)
	}

	var watch []wire.OutPoint
	for _, tx := range transactions {
		rec, err := udb.NewTxRecordFromMsgTx(tx, time.Now())
		if err != nil {
			return nil, errors.E(op, err)
		}
		ops, err := w.processTransactionRecord(ctx, dbtx, rec, header, &blockMeta)
		if err != nil {
			return nil, errors.E(op, err)
		}
		watch = append(watch, ops...)
	}

	return watch, nil
}

// ChainSwitch updates the wallet's main chain, either by extending the chain
// with new blocks, or switching to a better sidechain.  A sidechain for removed
// blocks (if any) is returned.  If relevantTxs is non-nil, the block marker for
// the latest block with processed transactions is updated for the new tip
// block.
func (w *Wallet) ChainSwitch(ctx context.Context, forest *SidechainForest, chain []*BlockNode, relevantTxs map[chainhash.Hash][]*wire.MsgTx) ([]*BlockNode, error) {
	const op errors.Op = "wallet.ChainSwitch"

	if len(chain) == 0 {
		return nil, errors.E(op, errors.Invalid, "zero-length chain")
	}

	w.recentlyPublishedMu.Lock()
	for txHash := range relevantTxs {
		delete(w.recentlyPublished, txHash)
	}
	w.recentlyPublishedMu.Unlock()

	chainTipChanges := &MainTipChangedNotification{
		AttachedBlocks: make([]*chainhash.Hash, 0, len(chain)),
		DetachedBlocks: nil,
		NewHeight:      int32(chain[len(chain)-1].Header.Height),
	}

	sideChainForkHeight := int32(chain[0].Header.Height)
	var prevChain []*BlockNode

	newWork := chain[len(chain)-1].workSum
	oldWork := new(big.Int)

	w.lockedOutpointMu.Lock()

	var watchOutPoints []wire.OutPoint
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

		tipHash, tipHeight := w.txStore.MainChainTip(txmgrNs)

		if sideChainForkHeight <= tipHeight {
			chainTipChanges.DetachedBlocks = make([]*chainhash.Hash, tipHeight-sideChainForkHeight+1)
			prevChain = make([]*BlockNode, tipHeight-sideChainForkHeight+1)
			for i := tipHeight; i >= sideChainForkHeight; i-- {
				hash, err := w.txStore.GetMainChainBlockHashForHeight(txmgrNs, i)
				if err != nil {
					return err
				}
				header, err := w.txStore.GetBlockHeader(dbtx, &hash)
				if err != nil {
					return err
				}
				_, filter, err := w.txStore.CFilterV2(dbtx, &hash)
				if err != nil {
					return err
				}

				// DetachedBlocks and prevChain are sorted in order of increasing heights.
				chainTipChanges.DetachedBlocks[i-sideChainForkHeight] = &hash
				prevChain[i-sideChainForkHeight] = NewBlockNode(header, &hash, filter)

				// For transaction notifications, the blocks are notified in reverse
				// height order.
				w.NtfnServer.notifyDetachedBlock(header)

				oldWork.Add(oldWork, blockchain.CalcWork(header.Bits))
			}

			if newWork.Cmp(oldWork) != 1 {
				return errors.Errorf("failed reorganize: sidechain ending at block %v has less total work "+
					"than the main chain tip block %v", chain[len(chain)-1].Hash, &tipHash)
			}

			// Remove blocks on the current main chain that are at or above the
			// height of the block that begins the side chain.
			err := w.txStore.Rollback(txmgrNs, addrmgrNs, sideChainForkHeight)
			if err != nil {
				return err
			}
		}

		for _, n := range chain {
			if voteVersion(w.chainParams) < n.Header.StakeVersion {
				log.Warnf("Old vote version detected (v%v), please update your "+
					"wallet to the latest version.", voteVersion(w.chainParams))
			}

			watch, err := w.extendMainChain(ctx, op, dbtx, n.Header, n.FilterV2, relevantTxs[*n.Hash])
			if err != nil {
				return err
			}
			watchOutPoints = append(watchOutPoints, watch...)

			// Add the block hash to the notification.
			chainTipChanges.AttachedBlocks = append(chainTipChanges.AttachedBlocks, n.Hash)
		}

		if relevantTxs != nil {
			// To avoid skipped blocks, the marker is not advanced if there is a
			// gap between the existing rescan point (main chain fork point of
			// the current marker) and the first block attached in this chain
			// switch.
			r, err := w.rescanPoint(dbtx)
			if err != nil {
				return err
			}
			rHeader, err := w.txStore.GetBlockHeader(dbtx, r)
			if err != nil {
				return err
			}
			if !(rHeader.Height+1 < chain[0].Header.Height) {
				marker := chain[len(chain)-1].Hash
				log.Debugf("Updating processed txs block marker to %v", marker)
				err := w.txStore.UpdateProcessedTxsBlockMarker(dbtx, marker)
				if err != nil {
					return err
				}
			}
		}

		// Prune unmined transactions that don't belong on the extended chain.
		// An error here is not fatal and should just be logged.
		//
		// TODO: The stake difficulty passed here is not correct.  This must be
		// the difficulty of the next block, not the tip block.
		tip := chain[len(chain)-1]
		hashes, err := w.txStore.PruneUnmined(dbtx, tip.Header.SBits)
		if err != nil {
			log.Errorf("Failed to prune unmined transactions when "+
				"connecting block height %v: %v", tip.Header.Height, err)
		}

		for _, hash := range hashes {
			w.NtfnServer.notifyRemovedTransaction(*hash)
		}
		return nil
	})
	w.lockedOutpointMu.Unlock()
	if err != nil {
		return nil, errors.E(op, err)
	}

	if n, err := w.NetworkBackend(); err == nil {
		_, err = w.watchHDAddrs(ctx, false, n)
		if err != nil {
			return nil, errors.E(op, err)
		}

		if len(watchOutPoints) > 0 {
			err = n.LoadTxFilter(ctx, false, nil, watchOutPoints)
			if err != nil {
				log.Errorf("Failed to watch outpoints: %v", err)
			}
		}
	}

	forest.PruneTree(chain[0].Hash)
	forest.Prune(int32(chain[len(chain)-1].Header.Height), w.chainParams)

	w.NtfnServer.notifyMainChainTipChanged(chainTipChanges)
	w.NtfnServer.sendAttachedBlockNotification(ctx)

	return prevChain, nil
}

// evaluateStakePoolTicket evaluates a stake pool ticket to see if it's
// acceptable to the stake pool. The ticket must pay out to the stake
// pool cold wallet, and must have a sufficient fee.
func (w *Wallet) evaluateStakePoolTicket(rec *udb.TxRecord, blockHeight int32, poolUser dcrutil.Address) bool {
	tx := rec.MsgTx

	// Check the first commitment output (txOuts[1])
	// and ensure that the address found there exists
	// in the list of approved addresses. Also ensure
	// that the fee exists and is of the amount
	// requested by the pool.
	commitmentOut := tx.TxOut[1]
	commitAddr, err := stake.AddrFromSStxPkScrCommitment(
		commitmentOut.PkScript, w.chainParams)
	if err != nil {
		log.Warnf("Cannot parse commitment address from ticket %v: %v",
			&rec.Hash, err)
		return false
	}

	// Extract the fee from the ticket.
	in := dcrutil.Amount(0)
	for i := range tx.TxOut {
		if i%2 != 0 {
			commitAmt, err := stake.AmountFromSStxPkScrCommitment(
				tx.TxOut[i].PkScript)
			if err != nil {
				log.Warnf("Cannot parse commitment amount for output %i from ticket %v: %v",
					i, &rec.Hash, err)
				return false
			}
			in += commitAmt
		}
	}
	out := dcrutil.Amount(0)
	for i := range tx.TxOut {
		out += dcrutil.Amount(tx.TxOut[i].Value)
	}
	fees := in - out

	_, exists := w.stakePoolColdAddrs[commitAddr.Address()]
	if exists {
		commitAmt, err := stake.AmountFromSStxPkScrCommitment(
			commitmentOut.PkScript)
		if err != nil {
			log.Warnf("Cannot parse commitment amount from ticket %v: %v", &rec.Hash, err)
			return false
		}

		// Calculate the fee required based on the current
		// height and the required amount from the pool.
		feeNeeded := txrules.StakePoolTicketFee(dcrutil.Amount(
			tx.TxOut[0].Value), fees, blockHeight, w.poolFees,
			w.chainParams)
		if commitAmt < feeNeeded {
			log.Warnf("User %s submitted ticket %v which "+
				"has less fees than are required to use this "+
				"stake pool and is being skipped (required: %v"+
				", found %v)", commitAddr.Address(),
				tx.TxHash(), feeNeeded, commitAmt)

			// Reject the entire transaction if it didn't
			// pay the pool server fees.
			return false
		}
	} else {
		log.Warnf("Unknown pool commitment address %s for ticket %v",
			commitAddr.Address(), tx.TxHash())
		return false
	}

	log.Debugf("Accepted valid stake pool ticket %v committing %v in fees",
		tx.TxHash(), tx.TxOut[0].Value)

	return true
}

// AddTransaction stores tx, marking it as mined in the block described by
// blockHash, or recording it to the wallet's mempool when nil.
func (w *Wallet) AddTransaction(ctx context.Context, tx *wire.MsgTx, blockHash *chainhash.Hash) error {
	const op errors.Op = "wallet.AddTransaction"

	w.recentlyPublishedMu.Lock()
	_, recent := w.recentlyPublished[tx.TxHash()]
	w.recentlyPublishedMu.Unlock()
	if recent {
		return nil
	}

	w.lockedOutpointMu.Lock()
	var watchOutPoints []wire.OutPoint
	err := walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		rec, err := udb.NewTxRecordFromMsgTx(tx, time.Now())
		if err != nil {
			return err
		}

		// Prevent orphan votes from entering the wallet's unmined transaction
		// set.
		if isVote(&rec.MsgTx) && blockHash == nil {
			votedBlock, _ := stake.SSGenBlockVotedOn(&rec.MsgTx)
			tipBlock, _ := w.txStore.MainChainTip(txmgrNs)
			if votedBlock != tipBlock {
				log.Debugf("Rejected unmined orphan vote %v which votes on block %v",
					&rec.Hash, &votedBlock)
				return nil
			}
		}

		var header *wire.BlockHeader
		var meta *udb.BlockMeta
		switch {
		case blockHash != nil:
			inChain, _ := w.txStore.BlockInMainChain(dbtx, blockHash)
			if !inChain {
				break
			}
			header, err = w.txStore.GetBlockHeader(dbtx, blockHash)
			if err != nil {
				return err
			}
			meta = new(udb.BlockMeta)
			*meta, err = w.txStore.GetBlockMetaForHash(txmgrNs, blockHash)
			if err != nil {
				return err
			}
		}

		watchOutPoints, err = w.processTransactionRecord(ctx, dbtx, rec, header, meta)
		return err
	})
	w.lockedOutpointMu.Unlock()
	if err != nil {
		return errors.E(op, err)
	}
	if n, err := w.NetworkBackend(); err == nil && len(watchOutPoints) > 0 {
		_, err := w.watchHDAddrs(ctx, false, n)
		if err != nil {
			return errors.E(op, err)
		}
		if len(watchOutPoints) > 0 {
			err = n.LoadTxFilter(ctx, false, nil, watchOutPoints)
			if err != nil {
				log.Errorf("Failed to watch outpoints: %v", err)
			}
		}
	}
	return nil
}

func (w *Wallet) processTransactionRecord(ctx context.Context, dbtx walletdb.ReadWriteTx, rec *udb.TxRecord,
	header *wire.BlockHeader, blockMeta *udb.BlockMeta) (watchOutPoints []wire.OutPoint, err error) {

	const op errors.Op = "wallet.processTransactionRecord"

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	stakemgrNs := dbtx.ReadWriteBucket(wstakemgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	height := int32(-1)
	if header != nil {
		height = int32(header.Height)
	}

	// At the moment all notified transactions are assumed to actually be
	// relevant.  This assumption will not hold true when SPV support is
	// added, but until then, simply insert the transaction because there
	// should either be one or more relevant inputs or outputs.
	if header == nil {
		err = w.txStore.InsertMemPoolTx(txmgrNs, rec)
		if errors.Is(err, errors.Exist) {
			log.Warnf("Refusing to add unmined transaction %v since same "+
				"transaction already exists mined", &rec.Hash)
			return nil, nil
		}
	} else {
		err = w.txStore.InsertMinedTx(txmgrNs, addrmgrNs, rec, &blockMeta.Hash)
	}
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Handle incoming SStx; store them in the stake manager if we own
	// the OP_SSTX tagged out, except if we're operating as a stake pool
	// server. In that case, additionally consider the first commitment
	// output as well.
	if w.stakePoolEnabled && header != nil && rec.TxType == stake.TxTypeSStx {
		// Errors don't matter here.  If addrs is nil, the range below
		// does nothing.
		txOut := rec.MsgTx.TxOut[0]
		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(txOut.Version,
			txOut.PkScript, w.chainParams)
		insert := false
		for _, addr := range addrs {
			if !w.manager.ExistsHash160(addrmgrNs, addr.Hash160()[:]) {
				continue
			}

			// We are operating as a stake pool. The below
			// function will ONLY add the ticket into the
			// stake pool if it has been found within a
			// block.
			if header == nil {
				break
			}

			if w.evaluateStakePoolTicket(rec, height, addr) {
				// Be sure to insert this into the user's stake
				// pool entry into the stake manager.
				poolTicket := &udb.PoolTicket{
					Ticket:       rec.Hash,
					HeightTicket: uint32(height),
					Status:       udb.TSImmatureOrLive,
				}
				err := w.stakeMgr.UpdateStakePoolUserTickets(
					stakemgrNs, addr, poolTicket)
				if err != nil {
					log.Warnf("Failed to insert stake pool "+
						"user ticket: %v", err)
				}
				log.Debugf("Inserted stake pool ticket %v for user %v "+
					"into the stake store database", &rec.Hash, addr)

				insert = true
				break
			}

			// At this point the ticket must be invalid, so insert it into the
			// list of invalid user tickets.
			err := w.stakeMgr.UpdateStakePoolUserInvalTickets(
				stakemgrNs, addr, &rec.Hash)
			if err != nil {
				log.Warnf("Failed to update pool user %v with "+
					"invalid ticket %v", addr.Address(),
					rec.Hash)
			}
		}

		if insert {
			err := w.stakeMgr.InsertSStx(stakemgrNs, dcrutil.NewTx(&rec.MsgTx))
			if err != nil {
				log.Errorf("Failed to insert SStx %v"+
					"into the stake store.", &rec.Hash)
			}
		}
	}

	// Handle incoming mined votes (only in stakepool mode)
	if w.stakePoolEnabled && rec.TxType == stake.TxTypeSSGen && header != nil {
		ticketHash := &rec.MsgTx.TxIn[1].PreviousOutPoint.Hash
		txInHeight := rec.MsgTx.TxIn[1].BlockHeight
		poolTicket := &udb.PoolTicket{
			Ticket:       *ticketHash,
			HeightTicket: txInHeight,
			Status:       udb.TSVoted,
			SpentBy:      rec.Hash,
			HeightSpent:  uint32(height),
		}

		poolUser, err := w.stakeMgr.SStxAddress(stakemgrNs, ticketHash)
		if err != nil {
			log.Warnf("Failed to fetch stake pool user for "+
				"ticket %v (voted ticket): %v", ticketHash, err)
		} else {
			err = w.stakeMgr.UpdateStakePoolUserTickets(
				stakemgrNs, poolUser, poolTicket)
			if err != nil {
				log.Warnf("Failed to update stake pool ticket for "+
					"stake pool user %s after voting",
					poolUser.Address())
			} else {
				log.Debugf("Updated voted stake pool ticket %v "+
					"for user %v into the stake store database ("+
					"vote hash: %v)", ticketHash, poolUser, &rec.Hash)
			}
		}
	}

	// Handle incoming mined revocations (only in stakepool mode)
	if w.stakePoolEnabled && rec.TxType == stake.TxTypeSSRtx && header != nil {
		txInHash := &rec.MsgTx.TxIn[0].PreviousOutPoint.Hash
		txInHeight := rec.MsgTx.TxIn[0].BlockHeight
		poolTicket := &udb.PoolTicket{
			Ticket:       *txInHash,
			HeightTicket: txInHeight,
			Status:       udb.TSMissed,
			SpentBy:      rec.Hash,
			HeightSpent:  uint32(height),
		}

		poolUser, err := w.stakeMgr.SStxAddress(stakemgrNs, txInHash)
		if err != nil {
			log.Warnf("failed to fetch stake pool user for "+
				"ticket %v (missed ticket)", txInHash)
		} else {
			err = w.stakeMgr.UpdateStakePoolUserTickets(
				stakemgrNs, poolUser, poolTicket)
			if err != nil {
				log.Warnf("failed to update stake pool ticket for "+
					"stake pool user %s after revoking",
					poolUser.Address())
			} else {
				log.Debugf("Updated missed stake pool ticket %v "+
					"for user %v into the stake store database ("+
					"revocation hash: %v)", txInHash, poolUser, &rec.Hash)
			}
		}
	}

	// Skip unlocking outpoints if the transaction is a vote or revocation as the lock
	// is not held.
	skipOutpoints := rec.TxType == stake.TxTypeSSGen || rec.TxType == stake.TxTypeSSRtx

	// Handle input scripts that contain P2PKs that we care about.
	for i, input := range rec.MsgTx.TxIn {
		if !skipOutpoints {
			prev := input.PreviousOutPoint
			delete(w.lockedOutpoints, outpoint{prev.Hash, prev.Index})
		}

		if txscript.IsMultisigSigScript(input.SignatureScript) {
			rs := txscript.MultisigRedeemScriptFromScriptSig(input.SignatureScript)

			class, addrs, _, err := txscript.ExtractPkScriptAddrs(
				0, rs, w.chainParams)
			if err != nil {
				// Non-standard outputs are skipped.
				continue
			}
			if class != txscript.MultiSigTy {
				// This should never happen, but be paranoid.
				continue
			}

			isRelevant := false
			for _, addr := range addrs {
				ma, err := w.manager.Address(addrmgrNs, addr)
				if err != nil {
					// Missing addresses are skipped.  Other errors should be
					// propagated.
					if errors.Is(err, errors.NotExist) {
						continue
					}
					return nil, errors.E(op, err)
				}
				isRelevant = true
				err = w.markUsedAddress(op, dbtx, ma)
				if err != nil {
					return nil, err
				}
				log.Debugf("Marked address %v used", addr)
			}

			// Add the script to the script databases.
			// TODO Markused script address? cj
			if isRelevant {
				n, _ := w.NetworkBackend()
				addr, err := w.manager.ImportScript(addrmgrNs, rs)
				switch {
				case errors.Is(err, errors.Exist):
				case err != nil:
					return nil, errors.E(op, err)
				case n != nil:
					addrs := []dcrutil.Address{addr.Address()}
					err := n.LoadTxFilter(ctx, false, addrs, nil)
					if err != nil {
						return nil, errors.E(op, err)
					}
				}
			}

			// If we're spending a multisig outpoint we know about,
			// update the outpoint. Inefficient because you deserialize
			// the entire multisig output info. Consider a specific
			// exists function in udb. The error here is skipped
			// because the absence of an multisignature output for
			// some script can not always be considered an error. For
			// example, the wallet might be rescanning as called from
			// the above function and so does not have the output
			// included yet.
			mso, err := w.txStore.GetMultisigOutput(txmgrNs, &input.PreviousOutPoint)
			if mso != nil && err == nil {
				err = w.txStore.SpendMultisigOut(txmgrNs, &input.PreviousOutPoint,
					rec.Hash, uint32(i))
				if err != nil {
					return nil, errors.E(op, err)
				}
			}
		}
	}

	// Check every output to determine whether it is controlled by a
	// wallet key.  If so, mark the output as a credit and mark
	// outpoints to watch.
	for i, output := range rec.MsgTx.TxOut {
		class, addrs, _, err := txscript.ExtractPkScriptAddrs(output.Version,
			output.PkScript, w.chainParams)
		if err != nil {
			// Non-standard outputs are skipped.
			continue
		}
		isStakeType := class == txscript.StakeSubmissionTy ||
			class == txscript.StakeSubChangeTy ||
			class == txscript.StakeGenTy ||
			class == txscript.StakeRevocationTy
		if isStakeType {
			class, err = txscript.GetStakeOutSubclass(output.PkScript)
			if err != nil {
				err = errors.E(op, errors.E(errors.Op("txscript.GetStakeOutSubclass"), err))
				log.Error(err)
				continue
			}
		}

		isTicketCommit := rec.TxType == stake.TxTypeSStx && i%2 == 1
		watchOutPoint := true
		if isTicketCommit {
			// For ticket commitments, decode the address stored in the pkscript
			// and evaluate ownership of that.
			addr, err := stake.AddrFromSStxPkScrCommitment(output.PkScript,
				w.chainParams)
			if err != nil {
				log.Warnf("failed to decode ticket commitment script of %s:%d",
					rec.Hash, i)
				continue
			}
			addrs = []dcrutil.Address{addr}
			watchOutPoint = false
		} else if output.Value == 0 {
			// The only case of outputs with 0 value that we need to handle are
			// ticket commitments. All other outputs can be ignored.
			continue
		}

		var tree int8
		if isStakeType {
			tree = 1
		}
		outpoint := wire.OutPoint{Hash: rec.Hash, Tree: tree}
		for _, addr := range addrs {
			ma, err := w.manager.Address(addrmgrNs, addr)
			// Missing addresses are skipped.  Other errors should
			// be propagated.
			if errors.Is(err, errors.NotExist) {
				continue
			}
			if err != nil {
				return nil, errors.E(op, err)
			}
			if isTicketCommit {
				err = w.txStore.AddTicketCommitment(txmgrNs, rec, uint32(i),
					ma.Account())
			} else {
				err = w.txStore.AddCredit(txmgrNs, rec, blockMeta,
					uint32(i), ma.Internal(), ma.Account())
			}
			if err != nil {
				return nil, errors.E(op, err)
			}
			err = w.markUsedAddress(op, dbtx, ma)
			if err != nil {
				return nil, err
			}
			if watchOutPoint {
				outpoint.Index = uint32(i)
				watchOutPoints = append(watchOutPoints, outpoint)
			}
			log.Debugf("Marked address %v used", addr)
		}

		// Handle P2SH addresses that are multisignature scripts
		// with keys that we own.
		if class == txscript.ScriptHashTy {
			var expandedScript []byte
			for _, addr := range addrs {
				expandedScript, err = w.manager.RedeemScript(addrmgrNs, addr)
				if err != nil {
					log.Debugf("failed to find redeemscript for "+
						"address %v in address manager: %v",
						addr.Address(), err)
					continue
				}
			}

			// Otherwise, extract the actual addresses and
			// see if any belong to us.
			expClass, multisigAddrs, _, err := txscript.ExtractPkScriptAddrs(
				0,
				expandedScript,
				w.chainParams)
			if err != nil {
				return nil, errors.E(op, errors.E(errors.Op("txscript.ExtractPkScriptAddrs"), err))
			}

			// Skip non-multisig scripts.
			if expClass != txscript.MultiSigTy {
				continue
			}

			for _, maddr := range multisigAddrs {
				_, err := w.manager.Address(addrmgrNs, maddr)
				// An address we own; handle accordingly.
				if err == nil {
					err := w.txStore.AddMultisigOut(
						dbtx, rec, blockMeta, uint32(i))
					if err != nil {
						// This will throw if there are multiple private keys
						// for this multisignature output owned by the wallet,
						// so it's routed to debug.
						log.Debugf("unable to add multisignature output: %v", err)
					}
				}
			}
		}
	}

	if (rec.TxType == stake.TxTypeSSGen) || (rec.TxType == stake.TxTypeSSRtx) {
		err = w.txStore.RedeemTicketCommitments(txmgrNs, rec, blockMeta)
		if err != nil {
			log.Errorf("Error redeeming ticket commitments: %v", err)
		}
	}

	// Send notification of mined or unmined transaction to any interested
	// clients.
	//
	// TODO: Avoid the extra db hits.
	if header == nil {
		details, err := w.txStore.UniqueTxDetails(txmgrNs, &rec.Hash, nil)
		if err != nil {
			log.Errorf("Cannot query transaction details for notifiation: %v", err)
		} else {
			w.NtfnServer.notifyUnminedTransaction(dbtx, details)
		}
	} else {
		details, err := w.txStore.UniqueTxDetails(txmgrNs, &rec.Hash, &blockMeta.Block)
		if err != nil {
			log.Errorf("Cannot query transaction details for notifiation: %v", err)
		} else {
			w.NtfnServer.notifyMinedTransaction(dbtx, details, blockMeta)
		}
	}

	return watchOutPoints, nil
}

// selectOwnedTickets returns a slice of tickets hashes from the tickets
// argument that are owned by the wallet.
//
// Because votes must be created for tickets tracked by both the transaction
// manager and the stake manager, this function checks both.
func selectOwnedTickets(w *Wallet, dbtx walletdb.ReadTx, tickets []*chainhash.Hash) []*chainhash.Hash {
	var owned []*chainhash.Hash
	for _, ticketHash := range tickets {
		if w.txStore.OwnTicket(dbtx, ticketHash) || w.stakeMgr.OwnTicket(ticketHash) {
			owned = append(owned, ticketHash)
		}
	}
	return owned
}

// VoteOnOwnedTickets creates and publishes vote transactions for all owned
// tickets in the winningTicketHashes slice if wallet voting is enabled.  The
// vote is only valid when voting on the block described by the passed block
// hash and height.  When a network backend is associated with the wallet,
// relevant commitment outputs are loaded as watched data.
func (w *Wallet) VoteOnOwnedTickets(ctx context.Context, winningTicketHashes []*chainhash.Hash, blockHash *chainhash.Hash, blockHeight int32) error {
	const op errors.Op = "wallet.VoteOnOwnedTickets"

	if !w.votingEnabled || blockHeight < int32(w.chainParams.StakeValidationHeight)-1 {
		return nil
	}

	n, err := w.NetworkBackend()
	if err != nil {
		return errors.E(op, err)
	}

	// TODO The behavior of this is not quite right if tons of blocks
	// are coming in quickly, because the transaction store will end up
	// out of sync with the voting channel here. This should probably
	// be fixed somehow, but this should be stable for networks that
	// are voting at normal block speeds.

	var ticketHashes []*chainhash.Hash
	var votes []*wire.MsgTx
	var usedVoteBits []stake.VoteBits
	defaultVoteBits := w.VoteBits()
	var watchOutPoints []wire.OutPoint
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Only consider tickets owned by this wallet.
		ticketHashes = selectOwnedTickets(w, dbtx, winningTicketHashes)
		if len(ticketHashes) == 0 {
			return nil
		}

		votes = make([]*wire.MsgTx, len(ticketHashes))
		usedVoteBits = make([]stake.VoteBits, len(ticketHashes))

		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)

		for i, ticketHash := range ticketHashes {
			ticketPurchase, err := w.txStore.Tx(txmgrNs, ticketHash)
			if err != nil && errors.Is(err, errors.NotExist) {
				ticketPurchase, err = w.stakeMgr.TicketPurchase(dbtx, ticketHash)
			}
			if err != nil {
				log.Errorf("Failed to read ticket purchase transaction for "+
					"owned winning ticket %v: %v", ticketHash, err)
				continue
			}

			// Don't create votes when this wallet doesn't have voting
			// authority.
			owned, err := w.hasVotingAuthority(addrmgrNs, ticketPurchase)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}

			ticketVoteBits := defaultVoteBits
			// Check for and use per-ticket votebits if set for this ticket.
			if tvb, found := w.readDBTicketVoteBits(dbtx, ticketHash); found {
				ticketVoteBits = tvb
			}
			vote, err := createUnsignedVote(ticketHash, ticketPurchase,
				blockHeight, blockHash, ticketVoteBits, w.subsidyCache, w.chainParams)
			if err != nil {
				log.Errorf("Failed to create vote transaction for ticket "+
					"hash %v: %v", ticketHash, err)
				continue
			}
			err = w.signVote(addrmgrNs, ticketPurchase, vote)
			if err != nil {
				log.Errorf("Failed to sign vote for ticket hash %v: %v",
					ticketHash, err)
				continue
			}
			votes[i] = vote
			usedVoteBits[i] = ticketVoteBits

			watchOutPoints = w.appendRelevantOutpoints(watchOutPoints, dbtx, vote)
		}
		return nil
	})
	if err != nil {
		log.Errorf("View failed: %v", errors.E(op, err))
	}

	// Remove nil votes without preserving order.
	for i := 0; i < len(votes); {
		if votes[i] == nil {
			votes[i], votes[len(votes)-1] = votes[len(votes)-1], votes[i]
			votes = votes[:len(votes)-1]
			continue
		}
		i++
	}

	voteRecords := make([]*udb.TxRecord, 0, len(votes))
	for i := range votes {
		rec, err := udb.NewTxRecordFromMsgTx(votes[i], time.Now())
		if err != nil {
			log.Errorf("Failed to create transaction record: %v", err)
			continue
		}
		voteRecords = append(voteRecords, rec)
	}
	w.recentlyPublishedMu.Lock()
	for i := range voteRecords {
		w.recentlyPublished[voteRecords[i].Hash] = struct{}{}

		log.Infof("Voting on block %v (height %v) using ticket %v "+
			"(vote hash: %v bits: %v)", blockHash, blockHeight,
			ticketHashes[i], &voteRecords[i].Hash, usedVoteBits[i].Bits)
	}
	w.recentlyPublishedMu.Unlock()

	// Publish before recording votes in database to slightly reduce latency.
	err = n.PublishTransactions(ctx, votes...)
	if err != nil {
		log.Errorf("Failed to send one or more votes: %v", err)
	}

	if len(watchOutPoints) > 0 {
		err := n.LoadTxFilter(ctx, false, nil, watchOutPoints)
		if err != nil {
			log.Errorf("Failed to watch outpoints: %v", err)
		}
	}

	// w.lockedOutpointMu is intentionally not locked.
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for i := range voteRecords {
			_, err := w.processTransactionRecord(ctx, dbtx, voteRecords[i], nil, nil)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if n, err := w.NetworkBackend(); err == nil {
		_, err := w.watchHDAddrs(ctx, false, n)
		if err != nil {
			return err
		}
	}
	return nil
}

// RevokeOwnedTickets revokes any owned tickets specified in the
// missedTicketHashes slice.  When a network backend is associated
// with the wallet, relevant commitment outputs are loaded as watched
// data.
func (w *Wallet) RevokeOwnedTickets(ctx context.Context, missedTicketHashes []*chainhash.Hash) error {
	const op errors.Op = "wallet.RevokeOwnedTickets"

	n, err := w.NetworkBackend()
	if err != nil {
		return errors.E(op, err)
	}

	var ticketHashes []*chainhash.Hash
	var revocations []*wire.MsgTx
	relayFee := w.RelayFee()
	var watchOutPoints []wire.OutPoint
	err = walletdb.View(ctx, w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Only consider tickets owned by this wallet.
		ticketHashes = selectOwnedTickets(w, dbtx, missedTicketHashes)
		if len(ticketHashes) == 0 {
			return nil
		}

		revocations = make([]*wire.MsgTx, len(ticketHashes))

		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		for i, ticketHash := range ticketHashes {
			ticketPurchase, err := w.txStore.Tx(txmgrNs, ticketHash)
			if err != nil && errors.Is(err, errors.NotExist) {
				ticketPurchase, err = w.stakeMgr.TicketPurchase(dbtx, ticketHash)
			}
			if err != nil {
				log.Errorf("Failed to read ticket purchase transaction for "+
					"missed or expired ticket %v: %v", ticketHash, err)
				continue
			}

			// Don't create revocations when this wallet doesn't have voting
			// authority.
			owned, err := w.hasVotingAuthority(addrmgrNs, ticketPurchase)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}

			revocation, err := createUnsignedRevocation(ticketHash, ticketPurchase,
				relayFee)
			if err != nil {
				log.Errorf("Failed to create revocation transaction for ticket "+
					"hash %v: %v", ticketHash, err)
				continue
			}
			err = w.signRevocation(addrmgrNs, ticketPurchase, revocation)
			if err != nil {
				log.Errorf("Failed to sign revocation for ticket hash %v: %v",
					ticketHash, err)
				continue
			}
			err = w.checkHighFees(dcrutil.Amount(ticketPurchase.TxOut[0].Value), revocation)
			if err != nil {
				log.Errorf("Revocation pays exceedingly high fees")
				continue
			}
			revocations[i] = revocation

			watchOutPoints = w.appendRelevantOutpoints(watchOutPoints, dbtx, revocation)
		}
		return nil
	})
	if err != nil {
		log.Errorf("View failed: %v", errors.E(op, err))
	}

	// Remove nil revocations without preserving order.
	for i := 0; i < len(revocations); {
		if revocations[i] == nil {
			revocations[i], revocations[len(revocations)-1] = revocations[len(revocations)-1], revocations[i]
			revocations = revocations[:len(revocations)-1]
			continue
		}
		i++
	}

	revocationRecords := make([]*udb.TxRecord, 0, len(revocations))
	for i := range revocations {
		rec, err := udb.NewTxRecordFromMsgTx(revocations[i], time.Now())
		if err != nil {
			log.Errorf("Failed to create transaction record: %v", err)
			continue
		}
		revocationRecords = append(revocationRecords, rec)
	}
	w.recentlyPublishedMu.Lock()
	for i := range revocationRecords {
		w.recentlyPublished[revocationRecords[i].Hash] = struct{}{}

		log.Infof("Revoked ticket %v with revocation %v", ticketHashes[i],
			&revocationRecords[i].Hash)
	}
	w.recentlyPublishedMu.Unlock()

	// Publish before recording revocations in database.
	err = n.PublishTransactions(ctx, revocations...)
	if err != nil {
		log.Errorf("Failed to send one or more revocations: %v", err)
	}

	if len(watchOutPoints) > 0 {
		err := n.LoadTxFilter(ctx, false, nil, watchOutPoints)
		if err != nil {
			log.Errorf("Failed to watch outpoints: %v", err)
		}
	}

	// w.lockedOutpointMu intentionally not locked.
	err = walletdb.Update(ctx, w.db, func(dbtx walletdb.ReadWriteTx) error {
		for i := range revocationRecords {
			_, err := w.processTransactionRecord(ctx, dbtx, revocationRecords[i], nil, nil)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if n, err := w.NetworkBackend(); err == nil {
		_, err := w.watchHDAddrs(ctx, false, n)
		if err != nil {
			return err
		}
	}
	return nil
}
