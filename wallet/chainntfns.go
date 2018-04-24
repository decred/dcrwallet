// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"bytes"
	"context"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wallet/udb"
	"github.com/decred/dcrwallet/walletdb"
)

func (w *Wallet) extendMainChain(op errors.Op, dbtx walletdb.ReadWriteTx, block *udb.BlockHeaderData, transactions [][]byte) error {
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	log.Infof("Connecting block %v, height %v", block.BlockHash,
		block.SerializedHeader.Height())

	// Propagate the error unless this block is already included in the main
	// chain.
	err := w.TxStore.ExtendMainChain(txmgrNs, block)
	if err != nil && !errors.Is(errors.Exist, err) {
		return errors.E(op, err)
	}

	// Notify interested clients of the connected block.
	var header wire.BlockHeader
	err = header.Deserialize(bytes.NewReader(block.SerializedHeader[:]))
	if err != nil {
		return errors.E(op, errors.E(errors.Op("blockheader.Deserialize"), errors.Encoding, err))
	}
	w.NtfnServer.notifyAttachedBlock(dbtx, &header, &block.BlockHash)

	blockMeta, err := w.TxStore.GetBlockMetaForHash(txmgrNs, &block.BlockHash)
	if err != nil {
		return errors.E(op, err)
	}

	for _, serializedTx := range transactions {
		rec, err := udb.NewTxRecord(serializedTx, time.Now())
		if err != nil {
			return errors.E(op, err)
		}
		err = w.processTransaction(dbtx, rec, &block.SerializedHeader, &blockMeta)
		if err != nil {
			return errors.E(op, err)
		}
	}

	return nil
}

type sideChainBlock struct {
	transactions [][]byte
	headerData   udb.BlockHeaderData
}

// switchToSideChain performs a chain switch, switching the main chain to the
// in-memory side chain.  The old side chain becomes the new main chain.
func (w *Wallet) switchToSideChain(op errors.Op, dbtx walletdb.ReadWriteTx) (*MainTipChangedNotification, error) {
	addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	sideChain := w.sideChain
	if len(sideChain) == 0 {
		return nil, errors.E(op, "no side chain to switch to")
	}

	sideChainForkHeight := sideChain[0].headerData.SerializedHeader.Height()

	_, tipHeight := w.TxStore.MainChainTip(txmgrNs)

	chainTipChanges := &MainTipChangedNotification{
		AttachedBlocks: make([]*chainhash.Hash, len(sideChain)),
		DetachedBlocks: make([]*chainhash.Hash, tipHeight-sideChainForkHeight+1),
		NewHeight:      0, // Must be set by caller before sending
	}

	// Find hashes of removed blocks for notifications.
	for i := tipHeight; i >= sideChainForkHeight; i-- {
		hash, err := w.TxStore.GetMainChainBlockHashForHeight(txmgrNs, i)
		if err != nil {
			return nil, errors.E(op, err)
		}

		// DetachedBlocks contains block hashes in order of increasing heights.
		chainTipChanges.DetachedBlocks[i-sideChainForkHeight] = &hash

		// For transaction notifications, the blocks are notified in reverse
		// height order.
		w.NtfnServer.notifyDetachedBlock(&hash)
	}

	// Remove blocks on the current main chain that are at or above the
	// height of the block that begins the side chain.
	err := w.TxStore.Rollback(txmgrNs, addrmgrNs, sideChainForkHeight)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Extend the main chain with each sidechain block.
	for i := range sideChain {
		scBlock := &sideChain[i]
		err = w.extendMainChain(op, dbtx, &scBlock.headerData, scBlock.transactions)
		if err != nil {
			return nil, err
		}

		// Add the block hash to the notification.
		chainTipChanges.AttachedBlocks[i] = &scBlock.headerData.BlockHash
	}

	return chainTipChanges, nil
}

func copyHeaderSliceToArray(array *udb.RawBlockHeader, slice []byte) error {
	if len(array) != len(udb.RawBlockHeader{}) {
		return errors.New("block header has unexpected size")
	}
	copy(array[:], slice)
	return nil
}

// ConnectBlock attaches a block and relevant wallet transactions to the
// wallet's main chain or side chain depending on whether the wallet is
// reorganizing.
func (w *Wallet) ConnectBlock(serializedBlockHeader []byte, transactions [][]byte) (err error) {
	const op errors.Op = "wallet.ConnectBlock"

	defer func() {
		if err != nil {
			return
		}

		err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
			return w.watchFutureAddresses(tx)
		})
		if err != nil {
			log.Errorf("Failed to watch for future address usage: %v", err)
		}
	}()

	var blockHeader wire.BlockHeader
	err = blockHeader.Deserialize(bytes.NewReader(serializedBlockHeader))
	if err != nil {
		return errors.E(op, errors.Encoding, err)
	}
	block := udb.BlockHeaderData{BlockHash: blockHeader.BlockHash()}
	err = copyHeaderSliceToArray(&block.SerializedHeader, serializedBlockHeader)
	if err != nil {
		return errors.E(op, errors.Invalid, err)
	}

	var chainTipChanges *MainTipChangedNotification

	w.reorganizingLock.Lock()
	reorg, reorgToHash := w.reorganizing, w.reorganizeToHash
	w.reorganizingLock.Unlock()
	if reorg {
		// add to side chain
		scBlock := sideChainBlock{
			transactions: transactions,
			headerData:   block,
		}
		w.sideChain = append(w.sideChain, scBlock)
		log.Infof("Adding block %v (height %v) to sidechain",
			block.BlockHash, block.SerializedHeader.Height())

		if block.BlockHash != reorgToHash {
			// Nothing left to do until the later blocks are
			// received.
			return nil
		}

		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			var err error
			chainTipChanges, err = w.switchToSideChain(op, dbtx)
			return err
		})
		if err != nil {
			return errors.E(op, err)
		}

		w.sideChain = nil
		w.reorganizingLock.Lock()
		w.reorganizing = false
		w.reorganizingLock.Unlock()
		log.Infof("Wallet reorganization to block %v complete", reorgToHash)
	} else {
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			return w.extendMainChain(op, dbtx, &block, transactions)
		})
		// Continue even if the block previously existed.
		if err != nil {
			return err
		}
		chainTipChanges = &MainTipChangedNotification{
			AttachedBlocks: []*chainhash.Hash{&block.BlockHash},
			DetachedBlocks: nil,
			NewHeight:      0, // set below
		}
	}

	height := int32(blockHeader.Height)
	chainTipChanges.NewHeight = height

	// Prune unnmined transactions that don't belong on the extended chain.
	err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		// TODO: The stake difficulty passed here is not correct.  This must be
		// the difficulty of the next block, not the tip block.
		return w.TxStore.PruneUnmined(dbtx, blockHeader.SBits)
	})
	if err != nil {
		log.Errorf("Failed to prune unmined transactions when "+
			"connecting block height %v: %v", height, err)
	}

	w.NtfnServer.notifyMainChainTipChanged(chainTipChanges)
	w.NtfnServer.sendAttachedBlockNotification()

	if voteVersion(w.chainParams) < blockHeader.StakeVersion {
		log.Warnf("Old vote version detected (v%v), please update your "+
			"wallet to the latest version.", voteVersion(w.chainParams))
	}

	return nil
}

// StartReorganize sets the wallet to a reorganizing state where all attached
// blocks will attach to a sidechain until the final block is reached, at which
// point a chain switch occurs.
func (w *Wallet) StartReorganize(oldHash, newHash *chainhash.Hash, oldHeight, newHeight int64) error {
	const op errors.Op = "wallet.StartReorganize"

	w.reorganizingLock.Lock()
	if w.reorganizing {
		reorganizeToHash := w.reorganizeToHash
		w.reorganizingLock.Unlock()

		log.Errorf("Reorg notified for chain tip %v (height %v), but already "+
			"processing a reorg to block %v", newHash, newHeight,
			reorganizeToHash)

		return errors.E(op, errors.Invalid, "reorg in process")
	}

	w.reorganizing = true
	w.reorganizeToHash = *newHash
	w.reorganizingLock.Unlock()

	log.Infof("Reorganization detected!")
	log.Infof("Old top block hash: %v", oldHash)
	log.Infof("Old top block height: %v", oldHeight)
	log.Infof("New top block hash: %v", newHash)
	log.Infof("New top block height: %v", newHeight)
	return nil
}

// evaluateStakePoolTicket evaluates a stake pool ticket to see if it's
// acceptable to the stake pool. The ticket must pay out to the stake
// pool cold wallet, and must have a sufficient fee.
func (w *Wallet) evaluateStakePoolTicket(rec *udb.TxRecord, blockHeight int32, poolUser dcrutil.Address) bool {
	const op errors.Op = "wallet.evaluateStakePoolTicket"

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

	_, exists := w.stakePoolColdAddrs[commitAddr.EncodeAddress()]
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
			tx.TxOut[0].Value), fees, blockHeight, w.PoolFees(),
			w.ChainParams())
		if commitAmt < feeNeeded {
			log.Warnf("User %s submitted ticket %v which "+
				"has less fees than are required to use this "+
				"stake pool and is being skipped (required: %v"+
				", found %v)", commitAddr.EncodeAddress(),
				tx.TxHash(), feeNeeded, commitAmt)

			// Reject the entire transaction if it didn't
			// pay the pool server fees.
			return false
		}
	} else {
		log.Warnf("Unknown pool commitment address %s for ticket %v",
			commitAddr.EncodeAddress(), tx.TxHash())
		return false
	}

	log.Debugf("Accepted valid stake pool ticket %v committing %v in fees",
		tx.TxHash(), tx.TxOut[0].Value)

	return true
}

// AcceptMempoolTx adds a relevant unmined transaction to the wallet.
func (w *Wallet) AcceptMempoolTx(serializedTx []byte) error {
	err := walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		rec, err := udb.NewTxRecord(serializedTx, time.Now())
		if err != nil {
			return err
		}

		// Prevent orphan votes from entering the wallet's unmined transaction
		// set.
		if isVote(&rec.MsgTx) {
			votedBlock, _ := stake.SSGenBlockVotedOn(&rec.MsgTx)
			tipBlock, _ := w.TxStore.MainChainTip(txmgrNs)
			if votedBlock != tipBlock {
				log.Debugf("Rejected unmined orphan vote %v which votes on block %v",
					&rec.Hash, &votedBlock)
				return nil
			}
		}

		return w.processTransaction(dbtx, rec, nil, nil)
	})
	if err == nil {
		err := walletdb.View(w.db, func(tx walletdb.ReadTx) error {
			return w.watchFutureAddresses(tx)
		})
		if err != nil {
			log.Errorf("Failed to watch for future address usage: %v", err)
		}
	}
	return err
}

func (w *Wallet) processTransaction(dbtx walletdb.ReadWriteTx, rec *udb.TxRecord,
	serializedHeader *udb.RawBlockHeader, blockMeta *udb.BlockMeta) error {

	const op errors.Op = "wallet.processTransaction"

	addrmgrNs := dbtx.ReadWriteBucket(waddrmgrNamespaceKey)
	stakemgrNs := dbtx.ReadWriteBucket(wstakemgrNamespaceKey)
	txmgrNs := dbtx.ReadWriteBucket(wtxmgrNamespaceKey)

	height := int32(-1)
	if serializedHeader != nil {
		height = serializedHeader.Height()
	}

	// At the moment all notified transactions are assumed to actually be
	// relevant.  This assumption will not hold true when SPV support is
	// added, but until then, simply insert the transaction because there
	// should either be one or more relevant inputs or outputs.
	var err error
	if serializedHeader == nil {
		err = w.TxStore.InsertMemPoolTx(txmgrNs, rec)
		if errors.Is(errors.Exist, err) {
			log.Warnf("Refusing to add unmined transaction %v since same "+
				"transaction already exists mined", &rec.Hash)
			return nil
		}
	} else {
		err = w.TxStore.InsertMinedTx(txmgrNs, addrmgrNs, rec, &blockMeta.Hash)
	}
	if err != nil {
		return errors.E(op, err)
	}

	// Handle incoming SStx; store them in the stake manager if we own
	// the OP_SSTX tagged out, except if we're operating as a stake pool
	// server. In that case, additionally consider the first commitment
	// output as well.
	if stake.IsSStx(&rec.MsgTx) {
		// Errors don't matter here.  If addrs is nil, the range below
		// does nothing.
		txOut := rec.MsgTx.TxOut[0]
		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(txOut.Version,
			txOut.PkScript, w.chainParams)
		insert := false
		for _, addr := range addrs {
			if !w.Manager.ExistsHash160(addrmgrNs, addr.Hash160()[:]) {
				continue
			}
			// We own the voting output pubkey or script and we're
			// not operating as a stake pool, so simply insert this
			// ticket now.
			if !w.stakePoolEnabled {
				insert = true
				break
			}

			// We are operating as a stake pool. The below
			// function will ONLY add the ticket into the
			// stake pool if it has been found within a
			// block.
			if serializedHeader == nil {
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
				err := w.StakeMgr.UpdateStakePoolUserTickets(
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
			err := w.StakeMgr.UpdateStakePoolUserInvalTickets(
				stakemgrNs, addr, &rec.Hash)
			if err != nil {
				log.Warnf("Failed to update pool user %v with "+
					"invalid ticket %v", addr.EncodeAddress(),
					rec.Hash)
			}
		}

		if insert {
			err := w.StakeMgr.InsertSStx(stakemgrNs, dcrutil.NewTx(&rec.MsgTx))
			if err != nil {
				log.Errorf("Failed to insert SStx %v"+
					"into the stake store.", &rec.Hash)
			}
		}
	}

	// Handle incoming mined votes (only in stakepool mode)
	if w.stakePoolEnabled && isVote(&rec.MsgTx) && serializedHeader != nil {
		ticketHash := &rec.MsgTx.TxIn[1].PreviousOutPoint.Hash
		txInHeight := rec.MsgTx.TxIn[1].BlockHeight
		poolTicket := &udb.PoolTicket{
			Ticket:       *ticketHash,
			HeightTicket: txInHeight,
			Status:       udb.TSVoted,
			SpentBy:      rec.Hash,
			HeightSpent:  uint32(height),
		}

		poolUser, err := w.StakeMgr.SStxAddress(stakemgrNs, ticketHash)
		if err != nil {
			log.Warnf("Failed to fetch stake pool user for "+
				"ticket %v (voted ticket): %v", ticketHash, err)
		} else {
			err = w.StakeMgr.UpdateStakePoolUserTickets(
				stakemgrNs, poolUser, poolTicket)
			if err != nil {
				log.Warnf("Failed to update stake pool ticket for "+
					"stake pool user %s after voting",
					poolUser.EncodeAddress())
			} else {
				log.Debugf("Updated voted stake pool ticket %v "+
					"for user %v into the stake store database ("+
					"vote hash: %v)", ticketHash, poolUser, &rec.Hash)
			}
		}
	}

	// Handle incoming mined revocations (only in stakepool mode)
	if w.stakePoolEnabled && isRevocation(&rec.MsgTx) && serializedHeader != nil {
		txInHash := &rec.MsgTx.TxIn[0].PreviousOutPoint.Hash
		txInHeight := rec.MsgTx.TxIn[0].BlockHeight
		poolTicket := &udb.PoolTicket{
			Ticket:       *txInHash,
			HeightTicket: txInHeight,
			Status:       udb.TSMissed,
			SpentBy:      rec.Hash,
			HeightSpent:  uint32(height),
		}

		poolUser, err := w.StakeMgr.SStxAddress(stakemgrNs, txInHash)
		if err != nil {
			log.Warnf("failed to fetch stake pool user for "+
				"ticket %v (missed ticket)", txInHash)
		} else {
			err = w.StakeMgr.UpdateStakePoolUserTickets(
				stakemgrNs, poolUser, poolTicket)
			if err != nil {
				log.Warnf("failed to update stake pool ticket for "+
					"stake pool user %s after revoking",
					poolUser.EncodeAddress())
			} else {
				log.Debugf("Updated missed stake pool ticket %v "+
					"for user %v into the stake store database ("+
					"revocation hash: %v)", txInHash, poolUser, &rec.Hash)
			}
		}
	}

	// Handle input scripts that contain P2PKs that we care about.
	for i, input := range rec.MsgTx.TxIn {
		if txscript.IsMultisigSigScript(input.SignatureScript) {
			rs, err := txscript.MultisigRedeemScriptFromScriptSig(
				input.SignatureScript)
			if err != nil {
				return err
			}

			class, addrs, _, err := txscript.ExtractPkScriptAddrs(
				txscript.DefaultScriptVersion, rs, w.chainParams)
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
				ma, err := w.Manager.Address(addrmgrNs, addr)
				if err != nil {
					// Missing addresses are skipped.  Other errors should be
					// propagated.
					if errors.Is(errors.NotExist, err) {
						continue
					}
					return errors.E(op, err)
				}
				isRelevant = true
				err = w.markUsedAddress(op, dbtx, ma)
				if err != nil {
					return err
				}
				log.Debugf("Marked address %v used", addr)
			}

			// Add the script to the script databases.
			// TODO Markused script address? cj
			if isRelevant {
				err = w.TxStore.InsertTxScript(txmgrNs, rs)
				if err != nil {
					return errors.E(op, err)
				}
				mscriptaddr, err := w.Manager.ImportScript(addrmgrNs, rs)
				switch {
				case errors.Is(errors.Exist, err): // Don't care if it's already there.
				case errors.Is(errors.Locked, err):
					log.Warnf("failed to attempt script importation "+
						"of incoming tx script %x because addrmgr "+
						"was locked", rs)
				case err == nil:
					if n, err := w.NetworkBackend(); err == nil {
						addr := mscriptaddr.Address()
						err := n.LoadTxFilter(context.TODO(),
							false, []dcrutil.Address{addr}, nil)
						if err != nil {
							return errors.E(op, err)
						}
					}
				default:
					return errors.E(op, err)
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
			mso, err := w.TxStore.GetMultisigOutput(txmgrNs, &input.PreviousOutPoint)
			if mso != nil && err == nil {
				err = w.TxStore.SpendMultisigOut(txmgrNs, &input.PreviousOutPoint,
					rec.Hash, uint32(i))
				if err != nil {
					return errors.E(op, err)
				}
			}
		}
	}

	// Check every output to determine whether it is controlled by a wallet
	// key.  If so, mark the output as a credit.
	for i, output := range rec.MsgTx.TxOut {
		// Ignore unspendable outputs.
		if output.Value == 0 {
			continue
		}

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

		for _, addr := range addrs {
			ma, err := w.Manager.Address(addrmgrNs, addr)
			if err == nil {
				err = w.TxStore.AddCredit(txmgrNs, rec, blockMeta,
					uint32(i), ma.Internal(), ma.Account())
				if err != nil {
					return errors.E(op, err)
				}
				err = w.markUsedAddress(op, dbtx, ma)
				if err != nil {
					return err
				}
				log.Debugf("Marked address %v used", addr)
				continue
			}

			// Missing addresses are skipped.  Other errors should
			// be propagated.
			if !errors.Is(errors.NotExist, err) {
				return err
			}
		}

		// Handle P2SH addresses that are multisignature scripts
		// with keys that we own.
		if class == txscript.ScriptHashTy {
			var expandedScript []byte
			for _, addr := range addrs {
				// Search both the script store in the tx store
				// and the address manager for the redeem script.
				var err error
				expandedScript, err = w.TxStore.GetTxScript(txmgrNs,
					addr.ScriptAddress())
				if err != nil {
					return errors.E(op, err)
				}

				if expandedScript == nil {
					script, done, err := w.Manager.RedeemScript(addrmgrNs, addr)
					if err != nil {
						log.Debugf("failed to find redeemscript for "+
							"address %v in address manager: %v",
							addr.EncodeAddress(), err)
						continue
					}
					defer done()
					expandedScript = script
				}
			}

			// Otherwise, extract the actual addresses and
			// see if any belong to us.
			expClass, multisigAddrs, _, err := txscript.ExtractPkScriptAddrs(
				txscript.DefaultScriptVersion,
				expandedScript,
				w.chainParams)
			if err != nil {
				return errors.E(op, errors.E(errors.Op("txscript.ExtractPkScriptAddrs"), err))
			}

			// Skip non-multisig scripts.
			if expClass != txscript.MultiSigTy {
				continue
			}

			for _, maddr := range multisigAddrs {
				_, err := w.Manager.Address(addrmgrNs, maddr)
				// An address we own; handle accordingly.
				if err == nil {
					err := w.TxStore.AddMultisigOut(
						txmgrNs, rec, blockMeta, uint32(i))
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

	// Send notification of mined or unmined transaction to any interested
	// clients.
	//
	// TODO: Avoid the extra db hits.
	if serializedHeader == nil {
		details, err := w.TxStore.UniqueTxDetails(txmgrNs, &rec.Hash, nil)
		if err != nil {
			log.Errorf("Cannot query transaction details for notifiation: %v", err)
		} else {
			w.NtfnServer.notifyUnminedTransaction(dbtx, details)
		}
	} else {
		details, err := w.TxStore.UniqueTxDetails(txmgrNs, &rec.Hash, &blockMeta.Block)
		if err != nil {
			log.Errorf("Cannot query transaction details for notifiation: %v", err)
		} else {
			w.NtfnServer.notifyMinedTransaction(dbtx, details, blockMeta)
		}
	}

	return nil
}

// selectOwnedTickets returns a slice of tickets hashes from the tickets
// argument that are owned by the wallet.
//
// Because votes must be created for tickets tracked by both the transaction
// manager and the stake manager, this function checks both.
func selectOwnedTickets(w *Wallet, dbtx walletdb.ReadTx, tickets []*chainhash.Hash) []*chainhash.Hash {
	var owned []*chainhash.Hash
	for _, ticketHash := range tickets {
		if w.TxStore.OwnTicket(dbtx, ticketHash) || w.StakeMgr.OwnTicket(ticketHash) {
			owned = append(owned, ticketHash)
		}
	}
	return owned
}

// VoteOnOwnedTickets creates and publishes vote transactions for all owned
// tickets in the winningTicketHashes slice if wallet voting is enabled.  The
// vote is only valid when voting on the block described by the passed block
// hash and height.
func (w *Wallet) VoteOnOwnedTickets(winningTicketHashes []*chainhash.Hash,
	blockHash *chainhash.Hash, blockHeight int32) error {

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
	voteBits := w.VoteBits()
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Only consider tickets owned by this wallet.
		ticketHashes = selectOwnedTickets(w, dbtx, winningTicketHashes)
		if len(ticketHashes) == 0 {
			return nil
		}

		votes = make([]*wire.MsgTx, len(ticketHashes))

		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		for i, ticketHash := range ticketHashes {
			ticketPurchase, err := w.TxStore.Tx(txmgrNs, ticketHash)
			if err != nil || ticketPurchase == nil {
				ticketPurchase, err = w.StakeMgr.TicketPurchase(dbtx, ticketHash)
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

			vote, err := createUnsignedVote(ticketHash, ticketPurchase,
				blockHeight, blockHash, voteBits, w.subsidyCache, w.chainParams)
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
		}
		return nil
	})
	if err != nil {
		log.Errorf("View failed: %v", errors.E(op, err))
	}

	for i, vote := range votes {
		if vote == nil {
			continue
		}
		txRec, err := udb.NewTxRecordFromMsgTx(vote, time.Now())
		if err != nil {
			log.Errorf("Failed to create transaction record for vote %v: %v",
				ticketHashes[i], err)
			continue
		}
		voteHash := &txRec.Hash
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			err := w.processTransaction(dbtx, txRec, nil, nil)
			if err != nil {
				return err
			}
			return n.PublishTransaction(context.TODO(), vote)
		})
		if err != nil {
			rpcErr, ok := err.(*dcrjson.RPCError)
			if ok {
				if rpcErr.Code != dcrjson.ErrRPCDuplicateTx {
					log.Errorf("Failed to send vote for ticket hash %v: %v",
						ticketHashes[i], err)
					continue
				}
				// log duplicate vote transactions as info, not errors
				log.Infof("Vote tx for ticket %v already in mempool, "+
					"rejected duplicate. Voted on block %v (height %v) "+
					"using ticket %v (vote hash: %v bits: %v)", ticketHashes[i], blockHash,
					blockHeight, ticketHashes[i], voteHash, voteBits.Bits)
				continue
			}
		}
		log.Infof("Voted on block %v (height %v) using ticket %v "+
			"(vote hash: %v bits: %v)", blockHash, blockHeight,
			ticketHashes[i], voteHash, voteBits.Bits)
	}

	return nil
}

// RevokeOwnedTickets revokes any owned tickets specified in the
// missedTicketHashes slice.
func (w *Wallet) RevokeOwnedTickets(missedTicketHashes []*chainhash.Hash) error {
	const op errors.Op = "wallet.RevokeOwnedTickets"

	n, err := w.NetworkBackend()
	if err != nil {
		return errors.E(op, err)
	}

	var ticketHashes []*chainhash.Hash
	var revocations []*wire.MsgTx
	relayFee := w.RelayFee()
	err = walletdb.View(w.db, func(dbtx walletdb.ReadTx) error {
		txmgrNs := dbtx.ReadBucket(wtxmgrNamespaceKey)

		// Only consider tickets owned by this wallet.
		ticketHashes = selectOwnedTickets(w, dbtx, missedTicketHashes)
		if len(ticketHashes) == 0 {
			return nil
		}

		revocations = make([]*wire.MsgTx, len(ticketHashes))

		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		for i, ticketHash := range ticketHashes {
			ticketPurchase, err := w.TxStore.Tx(txmgrNs, ticketHash)
			if err != nil || ticketPurchase == nil {
				ticketPurchase, err = w.StakeMgr.TicketPurchase(dbtx, ticketHash)
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
		}
		return nil
	})
	if err != nil {
		log.Errorf("View failed: %v", errors.E(op, err))
	}

	for i, revocation := range revocations {
		if revocation == nil {
			continue
		}
		txRec, err := udb.NewTxRecordFromMsgTx(revocation, time.Now())
		if err != nil {
			log.Errorf("Failed to create transaction record for revocation %v: %v",
				ticketHashes[i], err)
			continue
		}
		revocationHash := &txRec.Hash
		err = walletdb.Update(w.db, func(dbtx walletdb.ReadWriteTx) error {
			err := w.processTransaction(dbtx, txRec, nil, nil)
			if err != nil {
				return err
			}
			return n.PublishTransaction(context.TODO(), revocation)
		})
		if err != nil {
			log.Errorf("Failed to send revocation %v for ticket hash %v: %v",
				revocationHash, ticketHashes[i], err)
			continue
		}
		log.Infof("Revoked ticket %v with revocation %v", ticketHashes[i],
			revocationHash)
	}

	return nil
}
