/*
 * Copyright (c) 2013-2015 The btcsuite developers
 * Copyright (c) 2015 The Decred developers
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package wallet

import (
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/txscript"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/chain"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/wtxmgr"
)

type TicketsNotification struct {
	blockHash   *chainhash.Hash
	blockHeight int64
	tickets     []*chainhash.Hash
}

func (w *Wallet) handleChainNotifications() {
	sync := func(w *Wallet) {
		// At the moment there is no recourse if the rescan fails for
		// some reason, however, the wallet will not be marked synced
		// and many methods will error early since the wallet is known
		// to be out of date.
		err := w.syncWithChain()
		if err != nil && !w.ShuttingDown() {
			log.Warnf("Unable to synchronize wallet to chain: %v", err)
		}

		// Spin up the address pools.
		err = w.internalPool.initialize(waddrmgr.InternalBranch, w)
		if err != nil {
			log.Errorf("Failed to start the default internal branch address "+
				"pool: %v", err)
		}
		err = w.externalPool.initialize(waddrmgr.ExternalBranch, w)
		if err != nil {
			log.Errorf("Failed to start the default external branch address "+
				"pool: %v", err)
		}
	}

	for n := range w.chainSvr.Notifications() {
		var err error
		strErrType := ""

		switch n := n.(type) {
		case chain.ClientConnected:
			go sync(w)
		case chain.BlockConnected:
			w.connectBlock(wtxmgr.BlockMeta(n))
		case chain.BlockDisconnected:
			err = w.disconnectBlock(wtxmgr.BlockMeta(n))
		case chain.Reorganization:
			w.handleReorganizing(n.OldHash, n.OldHeight, n.NewHash, n.NewHeight)
		case chain.StakeDifficulty:
			err = w.handleStakeDifficulty(n.BlockHash, n.BlockHeight, n.StakeDiff)
			strErrType = "StakeDifficulty"
		case chain.RelevantTx:
			err = w.addRelevantTx(n.TxRecord, n.Block)

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
	maxTickets := int(w.chainParams.MaxFreshStakePerBlock)
	maxAttempts := 20 // Sane-ish?

	sdiff := dcrutil.Amount(w.GetStakeDifficulty().StakeDifficulty)
	maxToPay := w.GetTicketMaxPrice()
	if sdiff > maxToPay {
		return
	}

ticketPurchaseLoop:
	for {
		if purchased >= maxTickets {
			break
		}

		if attempts >= maxAttempts {
			break
		}

		// eligible may also be the tx hash as a string; however, for the
		// too many inputs error, the list of eligible Credits from
		// wtxmgr is instead returned. We can use this to compress the
		// amount to the ticket price, thus avoiding more costly db
		// lookups.
		eligible, err := w.CreatePurchaseTicket(w.BalanceToMaintain, -1,
			0, nil)
		if err != nil {
			switch {
			case err == ErrSStxNotEnoughFunds:
				break ticketPurchaseLoop
			case err == ErrSStxInputOverflow:
				switch v := eligible.(type) {
				case string:
					log.Errorf("Was given a string instead of eligible credits!")
					continue
				case []wtxmgr.Credit:
					err := w.compressEligible(v)
					if err != nil {
						log.Errorf("Failed to compress outputs: %v", err.Error())
					}
					attempts++
					continue
				}
			case waddrmgr.IsError(err, waddrmgr.ErrLocked):
				log.Warnf("Ticket purchase for stake mining is enabled, " +
					"but tickets could not be purchased because the " +
					"wallet is currently locked!")
				break ticketPurchaseLoop
			case err == ErrTicketPriceNotSet:
				// TODO make this trigger a request to the daemon
				// through chainsvr to get the latest ticket price.
				// The current behaviour simply waits for a block
				// to be connected to get the stake difficulty.
				// Probably need a retrigger for the ntfn like
				// "rebroadcaststakediff"
				log.Warnf("Ticket prices not yet established because the " +
					"client was recently connected; aborting ticket purchase " +
					"attempts")
				break ticketPurchaseLoop
			case err == ErrClientPurchaseTicket:
				log.Warnf("A chainSvr error was returned attempting to " +
					"purchase a ticket; ticket purchases aborted.")
				break ticketPurchaseLoop
			default:
				log.Errorf("PurchaseTicket error returned: %v", err)
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
func (w *Wallet) connectBlock(b wtxmgr.BlockMeta) {
	if !w.ChainSynced() {
		return
	}

	bs := waddrmgr.BlockStamp{
		Height: b.Height,
		Hash:   b.Hash,
	}
	if err := w.Manager.SetSyncedTo(&bs); err != nil {
		log.Errorf("Failed to update address manager sync state in "+
			"connect block for hash %v (height %d): %v", b.Hash,
			b.Height, err)
	}
	w.notifyConnectedBlock(b)
	log.Infof("Connecting block %v, height %v", bs.Hash, bs.Height)

	w.notifyBalances(bs.Height, wtxmgr.BFBalanceSpendable)

	isReorganizing, topHash := w.chainSvr.GetReorganizing()

	// If we've made it to the height where the reorganization is finished,
	// revert our reorganization state.
	if isReorganizing {
		if bs.Hash.IsEqual(&topHash) {
			log.Infof("Wallet reorganization to block %v complete",
				topHash)
			w.chainSvr.SetReorganizingState(false, chainhash.Hash{})
		}
	}

	if bs.Height >= int32(w.chainParams.CoinbaseMaturity) &&
		w.StakeMiningEnabled &&
		!isReorganizing {
		w.handleTicketPurchases()
	}

	// Insert the block if we haven't already through a relevant tx.
	err := w.TxStore.InsertBlock(&b)
	if err != nil {
		log.Errorf("Couldn't insert block %v into database: %v",
			b.Hash, err)
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
}

// disconnectBlock handles a chain server reorganize by rolling back all
// block history from the reorged block for a wallet in-sync with the chain
// server.
func (w *Wallet) disconnectBlock(b wtxmgr.BlockMeta) error {
	if !w.ChainSynced() {
		return nil
	}
	bs := waddrmgr.BlockStamp{
		Height: b.Height,
		Hash:   b.Hash,
	}
	log.Infof("Disconnecting block %v, height %v", bs.Hash, bs.Height)

	// Disconnect the last seen block from the manager if it matches the
	// removed block.
	iter := w.Manager.NewIterateRecentBlocks()
	if iter != nil && iter.BlockStamp().Hash == b.Hash {
		if iter.Prev() {
			prev := iter.BlockStamp()
			w.Manager.SetSyncedTo(&prev)
			err := w.TxStore.Rollback(prev.Height + 1)
			if err != nil {
				return err
			}
		} else {
			// The reorg is farther back than the recently-seen list
			// of blocks has recorded, so set it to unsynced which
			// will in turn lead to a rescan from either the
			// earliest blockstamp the addresses in the manager are
			// known to have been created.
			w.Manager.SetSyncedTo(nil)
			// Rollback everything but the genesis block.
			err := w.TxStore.Rollback(1)
			if err != nil {
				return err
			}
		}
	}

	w.notifyDisconnectedBlock(b)
	w.notifyBalances(b.Height-1, wtxmgr.BFBalanceSpendable)

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

	w.chainSvr.SetReorganizingState(true, *newHash)
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

	// Handle incoming SStx; store them in the stake manager if we own
	// the OP_SSTX tagged out.
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
				insert = true
				break
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
					tx.Sha(),
					w.VoteBits,
					&txInHash)
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
					tx.Sha(),
					&txInHash)
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
					blockToUse = &waddrmgr.BlockStamp{block.Height, block.Hash}
				}
				mscriptaddr, err := w.Manager.ImportScript(rs, blockToUse)
				if err != nil {
					switch {
					// Don't care if it's already there.
					case waddrmgr.IsError(err, waddrmgr.ErrDuplicateAddress):
						break
					case waddrmgr.IsError(err, waddrmgr.ErrLocked):
						log.Debugf("failed to attempt script importation " +
							"of incoming tx because addrmgr was locked")
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
							0,
							*w.chainParams.GenesisHash,
						},
					}

					// Submit rescan job and log when the import has completed.
					// Do not block on finishing the rescan.  The rescan success
					// or failure is logged elsewhere, and the channel is not
					// required to be read, so discard the return value.
					_ = w.SubmitRescan(job)
				}
			}

			// If we're spending a multisig outpoint we
			// know about, update the outpoint.
			// Inefficient because you deserialize the
			// entire multisig output info, consider
			// a specific exists function in wtxmgr. cj
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
				log.Errorf("Unknown stake output subclass encountered")
				continue
			}
		}
		switch {
		case class == txscript.PubKeyHashTy:
			for _, addr := range addrs {
				ma, err := w.Manager.Address(addr)
				if err == nil {
					// TODO: Credits should be added with the
					// account they belong to, so wtxmgr is able to
					// track per-account balances.
					err = w.TxStore.AddCredit(rec, block, uint32(i),
						ma.Internal())
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
		case class == txscript.ScriptHashTy:
			var expandedScript []byte
			for _, addr := range addrs {
				var err error
				expandedScript, err =
					w.TxStore.GetTxScript(addr.ScriptAddress())
				if err != nil {
					return err
				}

				// TODO make this work, the type conversion is broken cj
				//scrAddr, err := w.Manager.Address(addr)
				//if err == nil {
				//	addrTyped := scrAddr.(*waddrmgr.ManagedScriptAddress)
				//	retrievedScript, err := addrTyped.Script()
				//	if err == nil {
				//		expandedScript = retrievedScript
				//	}
				//}
			}

			// We don't have the script for this hash, skip.
			if expandedScript == nil {
				continue
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

	// TODO: Notify connected clients of the added transaction.

	bs, err := w.chainSvr.BlockStamp()
	if err == nil {
		w.notifyBalances(bs.Height, wtxmgr.BFBalanceSpendable)
	}

	return nil
}

// handleStakeDifficulty receives a stake difficulty and some block information
// and submits uses it to update the current stake difficulty in wallet.
func (w *Wallet) handleStakeDifficulty(blockHash *chainhash.Hash,
	blockHeight int64,
	StakeDifficulty int64) error {

	w.SetStakeDifficulty(&StakeDifficultyInfo{
		blockHash,
		blockHeight,
		StakeDifficulty,
	})

	return nil
}

func (w *Wallet) notifyBalances(curHeight int32, balanceFlag wtxmgr.BehaviorFlags) {
	// Don't notify unless wallet is synced to the chain server.
	if !w.ChainSynced() {
		return
	}

	// Notify any potential changes to the balance.
	confirmed, err := w.TxStore.Balance(1, curHeight, balanceFlag)
	if err != nil {
		log.Errorf("Cannot determine 1-conf balance: %v", err)
		return
	}
	w.notifyConfirmedBalance(confirmed)
	unconfirmed, err := w.TxStore.Balance(0, curHeight, balanceFlag)
	if err != nil {
		log.Errorf("Cannot determine 0-conf balance: %v", err)
		return
	}
	w.notifyUnconfirmedBalance(unconfirmed - confirmed)
}

func (w *Wallet) handleChainVotingNotifications() {
	for n := range w.chainSvr.NotificationsVoting() {
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
			w.VoteBits)

		if ntfns != nil {
			// Send notifications for newly created votes by the RPC.
			for _, ntfn := range ntfns {
				if ntfn != nil {
					w.notifyVoteCreated(*ntfn)
				}

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
			tickets)

		if ntfns != nil {
			// Send notifications for newly created revocations by the RPC.
			for _, ntfn := range ntfns {
				if ntfn != nil {
					w.notifyRevocationCreated(*ntfn)

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
