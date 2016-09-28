// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wallet/txauthor"
	"github.com/decred/dcrwallet/wallet/txrules"
	"github.com/decred/dcrwallet/wstakemgr"
	"github.com/decred/dcrwallet/wtxmgr"
)

// handleChainNotifications is the major chain notification handler that
// receives websocket notifications about the blockchain.
func (w *Wallet) handleChainNotifications() {
	chainClient, err := w.requireChainClient()
	if err != nil {
		log.Errorf("handleChainNotifications called without RPC client")
		w.wg.Done()
		return
	}

	// At the moment there is no recourse if the rescan fails for
	// some reason, however, the wallet will not be marked synced
	// and many methods will error early since the wallet is known
	// to be out of date.
	err = w.syncWithChain()
	if err != nil && !w.ShuttingDown() {
		log.Warnf("Unable to synchronize wallet to chain: %v", err)
	}

	for n := range chainClient.Notifications() {
		var err error
		strErrType := ""

		switch n := n.(type) {
		case chain.ClientConnected:
			log.Infof("The client has successfully connected to dcrd and " +
				"is now handling websocket notifications")
		case chain.BlockConnected:
			err = w.connectBlock(wtxmgr.BlockMeta(n))
			strErrType = "BlockConnected"
		case chain.BlockDisconnected:
			err = w.disconnectBlock(wtxmgr.BlockMeta(n))
			strErrType = "BlockDisconnected"
		case chain.Reorganization:
			w.handleReorganizing(n.OldHash, n.OldHeight, n.NewHash, n.NewHeight)
		case chain.RelevantTx:
			err = w.addRelevantTx(n.TxRecord, n.Block)
			strErrType = "RelevantTx"

		// The following are handled by the wallet's rescan
		// goroutines, so just pass them there.
		case *chain.RescanProgress, *chain.RescanFinished:
			w.rescanNotifications <- n
		}
		if err != nil {
			log.Errorf("Cannot handle chain server "+
				"notification %v: %v", strErrType, err)
		}
	}
	w.wg.Done()
}

// handleTicketPurchases autopurchases stake tickets for the wallet
// if stake mining is enabled.
func (w *Wallet) handleTicketPurchases() {
	purchased := 0
	attempts := 0

	// Parse the ticket purchase frequency. Positive numbers mean
	// that many tickets per block. Negative numbers mean to only
	// purchase one ticket once every abs(num) blocks.
	maxTickets := 1
	switch {
	case w.ticketBuyFreq == 0:
		return
	case w.ticketBuyFreq > 1:
		maxTickets = w.ticketBuyFreq
	case w.ticketBuyFreq < 0:
		bs := w.Manager.SyncedTo()
		if int(bs.Height)%w.ticketBuyFreq != 0 {
			return
		}
	}

	sdiff, err := w.StakeDifficulty()
	if err != nil {
		return
	}

	maxToPay := w.GetTicketMaxPrice()
	minBalance := w.BalanceToMaintain()

	if sdiff > maxToPay {
		return
	}

ticketPurchaseLoop:
	for {
		if purchased >= maxTickets {
			break
		}

		_, err := w.PurchaseTickets(minBalance,
			maxToPay,
			0, // No minconf
			w.TicketAddress(),
			waddrmgr.DefaultAccountNum,
			1, // One ticket at a time
			w.PoolAddress(),
			w.PoolFees(),
			0, // No expiry
			w.RelayFee(),
			w.TicketFeeIncrement())
		if err != nil {
			_, insufficientFunds := err.(txauthor.InsufficientFundsError)
			switch {
			case insufficientFunds:
				break ticketPurchaseLoop
			case waddrmgr.IsError(err, waddrmgr.ErrLocked):
				log.Warnf("Ticket purchase for stake mining is enabled, " +
					"but tickets could not be purchased because the " +
					"wallet is currently locked!")
				break ticketPurchaseLoop
			case err == ErrTicketPriceNotSet:
				log.Warnf("Tickets could not be purchased because the " +
					"ticket price could not be established")
				break ticketPurchaseLoop
			default:
				log.Errorf("PurchaseTicket error returned: %v", err)
				break ticketPurchaseLoop
			}
		} else {
			purchased++
		}

		attempts++
	}
}

// connectBlock handles a chain server notification by marking a wallet
// that's currently in-sync with the chain server as being synced up to
// the passed block.
func (w *Wallet) connectBlock(b wtxmgr.BlockMeta) error {
	bs := waddrmgr.BlockStamp{
		Height: b.Height,
		Hash:   b.Hash,
	}
	if err := w.Manager.SetSyncedTo(&bs); err != nil {
		log.Errorf("Failed to update address manager sync state in "+
			"connect block for hash %v (height %d): %v", b.Hash,
			b.Height, err)
	}
	log.Infof("Connecting block %v, height %v", bs.Hash, bs.Height)

	chainClient, err := w.requireChainClient()
	if err != nil {
		return err
	}

	isReorganizing, topHash := chainClient.GetReorganizing()

	// If we've made it to the height where the reorganization is finished,
	// revert our reorganization state.
	if isReorganizing {
		if bs.Hash.IsEqual(&topHash) {
			log.Infof("Wallet reorganization to block %v complete",
				topHash)
			chainClient.SetReorganizingState(false, chainhash.Hash{})
		}
	}

	if bs.Height >= int32(w.chainParams.CoinbaseMaturity) &&
		w.StakeMiningEnabled &&
		!isReorganizing {
		w.handleTicketPurchases()
	}

	// Insert the block if we haven't already through a relevant tx.
	err = w.TxStore.InsertBlock(&b)
	if err != nil {
		err = fmt.Errorf("Couldn't insert block %v into database: %v",
			b.Hash, err)
		return err
	}

	// Rollback testing for simulation network, if enabled.
	if b.Height < rollbackTestHeight && w.rollbackTesting {
		dbd, err := w.TxStore.DatabaseDump(b.Height, nil)
		if err != nil {
			panicStr := fmt.Sprintf("Failed to dump database at connection "+
				"of block %v (height %v): %v",
				b.Hash,
				b.Height,
				err.Error())
			panic(panicStr)
		}

		if dbd.OneConfBalance != dbd.OneConfCalcBalance {
			log.Warnf("Balance calculations incongruent. The spendable "+
				"balance was %v, but the recalculated spendable balance "+
				"was %v",
				dbd.OneConfBalance,
				dbd.OneConfCalcBalance)
		}

		w.rollbackBlockDB[uint32(b.Height)] = dbd
	}

	// We've reached the height to begin the rollback testing from.
	if b.Height == rollbackTestHeight && w.rollbackTesting {
		log.Infof("Height for rollback testing reached, beginning " +
			"database evaluations.")
		finalHeight := rollbackTestHeight - rollbackTestDepth
		for i := rollbackTestHeight; i >= finalHeight; i-- {
			err := w.TxStore.Rollback(int32(i))
			if err != nil {
				log.Errorf("Error rolling back block at height %v: %v",
					i, err)
			}

			rolledbackDb, err := w.TxStore.DatabaseDump(int32(i-1),
				w.rollbackBlockDB[uint32(i-1)].BucketUnminedInputs)
			if err != nil {
				panicStr := fmt.Sprintf("Failed to dump database at "+
					"disconnection of block height %v: %v",
					i,
					err.Error())
				panic(panicStr)
			}
			is, errStr := w.rollbackBlockDB[uint32(i-1)].Equals(rolledbackDb,
				true)
			if !is {
				log.Errorf("Database incongruencies detected after rolling "+
					"back to block %v!\n"+
					"%v",
					i-1,
					errStr)
			} else {
				log.Infof("Rollback to height %v proceeded without error.",
					i-1)
			}
		}

		w.Stop()
	}

	// Prune all expired transactions and all stake tickets that no longer
	// meet the minimum stake difficulty.
	stakeDifficulty, err := w.StakeDifficulty()
	if err != nil {
		return fmt.Errorf("Failed to get stake difficulty for pruning: %s",
			err.Error())
	}
	err = w.TxStore.PruneUnconfirmed(bs.Height, int64(stakeDifficulty))
	if err != nil {
		err = fmt.Errorf("Failed to prune unconfirmed transactions when "+
			"connecting block height %v: %s", bs.Height, err.Error())
		return err
	}

	// Notify interested clients of the connected block.
	w.NtfnServer.notifyAttachedBlock(&b)
	return nil
}

// disconnectBlock handles a chain server reorganize by rolling back all
// block history from the reorged block for a wallet in-sync with the chain
// server.
func (w *Wallet) disconnectBlock(b wtxmgr.BlockMeta) error {
	bs := waddrmgr.BlockStamp{
		Height: b.Height,
		Hash:   b.Hash,
	}
	log.Infof("Disconnecting block %v, height %v", bs.Hash, bs.Height)

	// Disconnect the last seen block from the manager if it matches the
	// removed block.
	err := w.TxStore.Rollback(b.Height)
	if err != nil {
		return err
	}
	prev, err := w.TxStore.GetBlockHash(b.Height - 1)
	if err != nil {
		return err
	}
	w.Manager.SetSyncedTo(&waddrmgr.BlockStamp{Hash: prev, Height: b.Height - 1})

	// Notify interested clients of the disconnected block.
	w.NtfnServer.notifyDetachedBlock(&b.Hash)

	return nil
}

// handleReorganizing handles a blockchain reorganization notification. It
// sets the chain server to indicate that currently the wallet state is in
// reorganizing, and what the final block of the reorganization is by hash.
func (w *Wallet) handleReorganizing(oldHash *chainhash.Hash, oldHeight int64,
	newHash *chainhash.Hash, newHeight int64) {
	log.Infof("Reorganization detected!")
	log.Infof("Old top block hash: %v", oldHash)
	log.Infof("Old top block height: %v", oldHeight)
	log.Infof("New top block hash: %v", newHash)
	log.Infof("New top block height: %v", newHeight)

	chainClient, err := w.requireChainClient()
	if err != nil {
		log.Error(err)
		return
	}

	chainClient.SetReorganizingState(true, *newHash)
}

// evaluateStakePoolTicket evaluates a stake pool ticket to see if it's
// acceptable to the stake pool. The ticket must pay out to the stake
// pool cold wallet, and must have a sufficient fee.
func (w *Wallet) evaluateStakePoolTicket(rec *wtxmgr.TxRecord,
	block *wtxmgr.BlockMeta, poolUser dcrutil.Address) (bool, error) {
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
		return false, fmt.Errorf("Failed to parse commit out addr: %s",
			err.Error())
	}

	// Extract the fee from the ticket.
	in := dcrutil.Amount(0)
	for i := range tx.TxOut {
		if i%2 != 0 {
			commitAmt, err := stake.AmountFromSStxPkScrCommitment(
				tx.TxOut[i].PkScript)
			if err != nil {
				return false, fmt.Errorf("Failed to parse commit "+
					"out amt for commit in vout %v: %s", i, err.Error())
			}
			in += dcrutil.Amount(commitAmt)
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
			return false, fmt.Errorf("failed to parse commit "+
				"out amt: %s", err.Error())
		}

		// Calculate the fee required based on the current
		// height and the required amount from the pool.
		feeNeeded := txrules.StakePoolTicketFee(dcrutil.Amount(
			tx.TxOut[0].Value), fees, block.Height, w.PoolFees(),
			w.ChainParams())
		if commitAmt < feeNeeded {
			log.Warnf("User %s submitted ticket %v which "+
				"has less fees than are required to use this "+
				"stake pool and is being skipped (required: %v"+
				", found %v)", commitAddr.EncodeAddress(),
				tx.TxSha(), feeNeeded, commitAmt)

			// Reject the entire transaction if it didn't
			// pay the pool server fees.
			return false, nil
		}
	} else {
		log.Warnf("Unknown pool commitment address %s for ticket %v",
			commitAddr.EncodeAddress(), tx.TxSha())
		return false, nil
	}

	log.Debugf("Accepted valid stake pool ticket %v committing %v in fees",
		tx.TxSha(), tx.TxOut[0].Value)

	return true, nil
}

func (w *Wallet) addRelevantTx(rec *wtxmgr.TxRecord,
	block *wtxmgr.BlockMeta) error {
	// TODO: The transaction store and address manager need to be updated
	// together, but each operate under different namespaces and are changed
	// under new transactions.  This is not error safe as we lose
	// transaction semantics.
	//
	// I'm unsure of the best way to solve this.  Some possible solutions
	// and drawbacks:
	//
	//   1. Open write transactions here and pass the handle to every
	//      waddrmr and wtxmgr method.  This complicates the caller code
	//      everywhere, however.
	//
	//   2. Move the wtxmgr namespace into the waddrmgr namespace, likely
	//      under its own bucket.  This entire function can then be moved
	//      into the waddrmgr package, which updates the nested wtxmgr.
	//      This removes some of separation between the components.
	//
	//   3. Use multiple wtxmgrs, one for each account, nested in the
	//      waddrmgr namespace.  This still provides some sort of logical
	//      separation (transaction handling remains in another package, and
	//      is simply used by waddrmgr), but may result in duplicate
	//      transactions being saved if they are relevant to multiple
	//      accounts.
	//
	//   4. Store wtxmgr-related details under the waddrmgr namespace, but
	//      solve the drawback of #3 by splitting wtxmgr to save entire
	//      transaction records globally for all accounts, with
	//      credit/debit/balance tracking per account.  Each account would
	//      also save the relevant transaction hashes and block incidence so
	//      the full transaction can be loaded from the waddrmgr
	//      transactions bucket.  This currently seems like the best
	//      solution.

	// At the moment all notified transactions are assumed to actually be
	// relevant.  This assumption will not hold true when SPV support is
	// added, but until then, simply insert the transaction because there
	// should either be one or more relevant inputs or outputs.
	//
	// TODO This function is pretty bad corruption wise, it's very easy
	// to corrupt the wallet if you ctrl+c while in this function. This
	// needs desperate refactoring.

	tx := dcrutil.NewTx(&rec.MsgTx)
	txHash := rec.Hash

	// Handle incoming SStx; store them in the stake manager if we own
	// the OP_SSTX tagged out, except if we're operating as a stake pool
	// server. In that case, additionally consider the first commitment
	// output as well.
	if is, _ := stake.IsSStx(tx); is {
		// Errors don't matter here.  If addrs is nil, the range below
		// does nothing.
		txOut := tx.MsgTx().TxOut[0]

		_, addrs, _, _ := txscript.ExtractPkScriptAddrs(txOut.Version,
			txOut.PkScript, w.chainParams)
		insert := false
		for _, addr := range addrs {
			_, err := w.Manager.Address(addr)
			if err == nil {
				// We own the voting output pubkey or script and we're
				// not operating as a stake pool, so simply insert this
				// ticket now.
				if !w.stakePoolEnabled {
					insert = true
					break
				} else {
					// We are operating as a stake pool. The below
					// function will ONLY add the ticket into the
					// stake pool if it has been found within a
					// block.
					if block == nil {
						break
					}

					valid, errEval := w.evaluateStakePoolTicket(rec, block,
						addr)
					if valid {
						// Be sure to insert this into the user's stake
						// pool entry into the stake manager.
						poolTicket := &wstakemgr.PoolTicket{
							Ticket:       txHash,
							HeightTicket: uint32(block.Height),
							Status:       wstakemgr.TSImmatureOrLive,
						}
						errUpdate := w.StakeMgr.UpdateStakePoolUserTickets(addr,
							poolTicket)
						if errUpdate != nil {
							log.Warnf("Failed to insert stake pool "+
								"user ticket: %s", err.Error())
						}
						log.Debugf("Inserted stake pool ticket %v for user %v "+
							"into the stake store database", txHash, addr)

						insert = true
						break
					}

					// Log errors if there were any. At this point the ticket
					// must be invalid, so insert it into the list of invalid
					// user tickets.
					if errEval != nil {
						log.Warnf("Ticket %v failed ticket evaluation for "+
							"the stake pool: %s", rec.Hash, err.Error())
					}
					errUpdate := w.StakeMgr.UpdateStakePoolUserInvalTickets(addr,
						&rec.Hash)
					if errUpdate != nil {
						log.Warnf("Failed to update pool user %v with "+
							"invalid ticket %v", addr.EncodeAddress(),
							rec.Hash)
					}
				}
			}
		}

		if insert {
			err := w.StakeMgr.InsertSStx(tx, w.VoteBits)
			if err != nil {
				log.Errorf("Failed to insert SStx %v"+
					"into the stake store.", tx.Sha())
			}
		}
	}

	// Handle incoming SSGen; store them if we own
	// the ticket used to purchase them.
	if is, _ := stake.IsSSGen(tx); is {
		if block != nil {
			txInHash := tx.MsgTx().TxIn[1].PreviousOutPoint.Hash
			if w.StakeMgr.CheckHashInStore(&txInHash) {
				w.StakeMgr.InsertSSGen(&block.Hash,
					int64(block.Height),
					&txHash,
					w.VoteBits,
					&txInHash)
			}

			// If we're running as a stake pool, insert
			// the stake pool user ticket update too.
			if w.stakePoolEnabled {
				txInHeight := tx.MsgTx().TxIn[1].BlockHeight
				poolTicket := &wstakemgr.PoolTicket{
					Ticket:       txInHash,
					HeightTicket: txInHeight,
					Status:       wstakemgr.TSVoted,
					SpentBy:      txHash,
					HeightSpent:  uint32(block.Height),
				}

				poolUser, err := w.StakeMgr.SStxAddress(&txInHash)
				if err != nil {
					log.Warnf("Failed to fetch stake pool user for "+
						"ticket %v (voted ticket)", txInHash)
				} else {
					err = w.StakeMgr.UpdateStakePoolUserTickets(poolUser,
						poolTicket)
					if err != nil {
						log.Warnf("Failed to update stake pool ticket for "+
							"stake pool user %s after voting",
							poolUser.EncodeAddress())
					} else {
						log.Debugf("Updated voted stake pool ticket %v "+
							"for user %v into the stake store database ("+
							"vote hash: %v)", txInHash, poolUser, txHash)
					}
				}
			}
		} else {
			// If there's no associated block, it's potentially a
			// doublespent SSGen. Just ignore it and wait for it
			// to later get into a block.
			return nil
		}
	}

	// Handle incoming SSRtx; store them if we own
	// the ticket used to purchase them.
	if is, _ := stake.IsSSRtx(tx); is {
		if block != nil {
			txInHash := tx.MsgTx().TxIn[0].PreviousOutPoint.Hash

			if w.StakeMgr.CheckHashInStore(&txInHash) {
				w.StakeMgr.InsertSSRtx(&block.Hash,
					int64(block.Height),
					&txHash,
					&txInHash)
			}

			// If we're running as a stake pool, insert
			// the stake pool user ticket update too.
			if w.stakePoolEnabled {
				txInHeight := tx.MsgTx().TxIn[0].BlockHeight
				poolTicket := &wstakemgr.PoolTicket{
					Ticket:       txInHash,
					HeightTicket: txInHeight,
					Status:       wstakemgr.TSMissed,
					SpentBy:      txHash,
					HeightSpent:  uint32(block.Height),
				}

				poolUser, err := w.StakeMgr.SStxAddress(&txInHash)
				if err != nil {
					log.Warnf("failed to fetch stake pool user for "+
						"ticket %v (missed ticket)", txInHash)
				} else {
					err = w.StakeMgr.UpdateStakePoolUserTickets(poolUser,
						poolTicket)
					if err != nil {
						log.Warnf("failed to update stake pool ticket for "+
							"stake pool user %s after revoking",
							poolUser.EncodeAddress())
					} else {
						log.Debugf("Updated missed stake pool ticket %v "+
							"for user %v into the stake store database ("+
							"revocation hash: %v)", txInHash, poolUser, txHash)
					}
				}
			}
		}
	}

	err := w.TxStore.InsertTx(rec, block)
	if err != nil {
		return err
	}

	// Handle input scripts that contain P2PKs that we care about.
	for i, input := range rec.MsgTx.TxIn {
		if txscript.IsMultisigSigScript(input.SignatureScript) {
			rs, err :=
				txscript.MultisigRedeemScriptFromScriptSig(
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
				_, err := w.Manager.Address(addr)
				if err == nil {
					isRelevant = true
					err = w.Manager.MarkUsed(addr)
					if err != nil {
						return err
					}
					log.Debugf("Marked address %v used", addr)
				} else {
					// Missing addresses are skipped.  Other errors should
					// be propagated.
					if !waddrmgr.IsError(err, waddrmgr.ErrAddressNotFound) {
						return err
					}
				}
			}

			// Add the script to the script databases.
			// TODO Markused script address? cj
			if isRelevant {
				err = w.TxStore.InsertTxScript(rs)
				if err != nil {
					return err
				}
				var blockToUse *waddrmgr.BlockStamp
				if block != nil {
					blockToUse = &waddrmgr.BlockStamp{
						Height: block.Height,
						Hash:   block.Hash,
					}
				}
				mscriptaddr, err := w.Manager.ImportScript(rs, blockToUse)
				if err != nil {
					switch {
					// Don't care if it's already there.
					case waddrmgr.IsError(err, waddrmgr.ErrDuplicateAddress):
						break
					case waddrmgr.IsError(err, waddrmgr.ErrLocked):
						log.Warnf("failed to attempt script importation "+
							"of incoming tx script %x because addrmgr "+
							"was locked", rs)
						break
					default:
						return err
					}
				} else {
					// This is the first time seeing this script address
					// belongs to us, so do a rescan and see if there are
					// any other outputs to this address.
					job := &RescanJob{
						Addrs:     []dcrutil.Address{mscriptaddr.Address()},
						OutPoints: nil,
						BlockStamp: waddrmgr.BlockStamp{
							Height: 0,
							Hash:   *w.chainParams.GenesisHash,
						},
					}

					// Submit rescan job and log when the import has completed.
					// Do not block on finishing the rescan.  The rescan success
					// or failure is logged elsewhere, and the channel is not
					// required to be read, so discard the return value.
					_ = w.SubmitRescan(job)
				}
			}

			// If we're spending a multisig outpoint we know about,
			// update the outpoint. Inefficient because you deserialize
			// the entire multisig output info. Consider a specific
			// exists function in wtxmgr. The error here is skipped
			// because the absence of an multisignature output for
			// some script can not always be considered an error. For
			// example, the wallet might be rescanning as called from
			// the above function and so does not have the output
			// included yet.
			mso, err := w.TxStore.GetMultisigOutput(&input.PreviousOutPoint)
			if mso != nil && err == nil {
				w.TxStore.SpendMultisigOut(&input.PreviousOutPoint,
					rec.Hash,
					uint32(i))
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
				log.Errorf("Unknown stake output subclass parse error "+
					"encountered: %v", err)
				continue
			}
		}

		for _, addr := range addrs {
			ma, err := w.Manager.Address(addr)
			if err == nil {
				// TODO: Credits should be added with the
				// account they belong to, so wtxmgr is able to
				// track per-account balances.
				err = w.TxStore.AddCredit(rec, block, uint32(i),
					ma.Internal(), ma.Account())
				if err != nil {
					return err
				}
				err = w.Manager.MarkUsed(addr)
				if err != nil {
					return err
				}
				log.Debugf("Marked address %v used", addr)
				continue
			}

			// Missing addresses are skipped.  Other errors should
			// be propagated.
			if !waddrmgr.IsError(err, waddrmgr.ErrAddressNotFound) {
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
				expandedScript, err =
					w.TxStore.GetTxScript(addr.ScriptAddress())
				if err != nil {
					return err
				}

				if expandedScript == nil {
					scrAddr, err := w.Manager.Address(addr)
					if err == nil {
						sa, ok := scrAddr.(waddrmgr.ManagedScriptAddress)
						if !ok {
							log.Warnf("address %v is not a script"+
								" address (type %T)",
								scrAddr.Address().EncodeAddress(),
								scrAddr.Address())
							continue
						}
						retrievedScript, err := sa.Script()
						if err != nil {
							log.Errorf("failed to decode redeemscript for "+
								"address %v: %v", addr.EncodeAddress(),
								err.Error())
							continue
						}
						expandedScript = retrievedScript

					} else {
						// We can't find this redeem script anywhere.
						// Skip this output.
						log.Debugf("failed to find redeemscript for "+
							"address %v in address manager: %v",
							addr.EncodeAddress(), err.Error())
						continue
					}
				}
			}

			// Otherwise, extract the actual addresses and
			// see if any belong to us.
			expClass, multisigAddrs, _, err := txscript.ExtractPkScriptAddrs(
				txscript.DefaultScriptVersion,
				expandedScript,
				w.chainParams)
			if err != nil {
				return err
			}

			// Skip non-multisig scripts.
			if expClass != txscript.MultiSigTy {
				continue
			}

			for _, maddr := range multisigAddrs {
				_, err := w.Manager.Address(maddr)
				// An address we own; handle accordingly.
				if err == nil {
					errStore := w.TxStore.AddMultisigOut(rec, block, uint32(i))
					if errStore != nil {
						// This will throw if there are multiple private keys
						// for this multisignature output owned by the wallet,
						// so it's routed to debug.
						log.Debugf("unable to add multisignature output: %v",
							errStore.Error())
					}
				}
			}
		}
	}

	// Send notification of mined or unmined transaction to any interested
	// clients.
	//
	// TODO: Avoid the extra db hits.
	if block == nil {
		details, err := w.TxStore.UniqueTxDetails(&rec.Hash, nil)
		if err != nil {
			log.Errorf("Cannot query transaction details for notifiation: %v",
				err)
		} else {
			w.NtfnServer.notifyUnminedTransaction(details)
		}
	} else {
		details, err := w.TxStore.UniqueTxDetails(&rec.Hash, &block.Block)
		if err != nil {
			log.Errorf("Cannot query transaction details for notifiation: %v",
				err)
		} else {
			w.NtfnServer.notifyMinedTransaction(details, block)
		}
	}

	return nil
}

func (w *Wallet) handleChainVotingNotifications() {
	chainClient, err := w.requireChainClient()
	if err != nil {
		log.Error(err)
		w.wg.Done()
		return
	}
	for n := range chainClient.NotificationsVoting() {
		var err error
		strErrType := ""

		switch n := n.(type) {
		case chain.WinningTickets:
			err = w.handleWinningTickets(n.BlockHash, n.BlockHeight, n.Tickets)
			strErrType = "WinningTickets"
		case chain.MissedTickets:
			err = w.handleMissedTickets(n.BlockHash, n.BlockHeight, n.Tickets)
			strErrType = "MissedTickets"
		default:
			err = fmt.Errorf("voting handler received unknown ntfn type")
		}
		if err != nil {
			log.Errorf("Cannot handle chain server voting "+
				"notification %v: %v", strErrType, err)
		}
	}
	w.wg.Done()
}

// handleWinningTickets receives a list of hashes and some block information
// and submits it to the wstakemgr to handle SSGen production.
func (w *Wallet) handleWinningTickets(blockHash *chainhash.Hash,
	blockHeight int64,
	tickets []*chainhash.Hash) error {
	topBlockStamp := w.Manager.SyncedTo()

	// Even if stake voting is disabled, we should still store eligible
	// tickets for the current top block.
	// TODO The behaviour of this is not quite right if tons of blocks
	// are coming in quickly, because the address manager will end up
	// out of sync with the voting channel here. This should probably
	// be fixed somehow, but this should be stable for networks that
	// are voting at normal block speeds.
	if blockHeight >= w.chainParams.StakeValidationHeight-1 &&
		topBlockStamp.Hash.IsEqual(blockHash) {
		w.SetCurrentVotingInfo(blockHash, blockHeight, tickets)
	}

	if blockHeight >= w.chainParams.StakeValidationHeight-1 &&
		w.StakeMiningEnabled {
		ntfns, err := w.StakeMgr.HandleWinningTicketsNtfn(blockHash,
			blockHeight,
			tickets,
			w.VoteBits,
			w.AllowHighFees,
		)

		if ntfns != nil {
			// Send notifications for newly created votes by the RPC.
			for _, ntfn := range ntfns {
				// Inform the console that we've voted, too.
				log.Infof("Voted on block %v (height %v) using ticket %v "+
					"(vote hash: %v)",
					ntfn.BlockHash,
					ntfn.Height,
					ntfn.SStxIn,
					ntfn.TxHash)
			}
		}

		return err
	}

	return nil
}

// handleMissedTickets receives a list of hashes and some block information
// and submits it to the wstakemgr to handle SSRtx production.
func (w *Wallet) handleMissedTickets(blockHash *chainhash.Hash,
	blockHeight int64,
	tickets []*chainhash.Hash) error {

	if !w.StakeMiningEnabled {
		return nil
	}

	if blockHeight >= w.chainParams.StakeValidationHeight+1 &&
		w.StakeMiningEnabled {
		ntfns, err := w.StakeMgr.HandleMissedTicketsNtfn(blockHash,
			blockHeight,
			tickets, w.AllowHighFees)

		if ntfns != nil {
			// Send notifications for newly created revocations by the RPC.
			for _, ntfn := range ntfns {
				if ntfn != nil {
					// Inform the console that we've revoked our ticket.
					log.Infof("Revoked missed ticket %v (tx hash: %v)",
						ntfn.SStxIn,
						ntfn.TxHash)
				}
			}
		}

		return err
	}

	return nil
}
