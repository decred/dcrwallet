// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

// InsertMemPoolTx inserts a memory pool transaction record.  It also marks
// previous outputs referenced by its inputs as spent.  Errors with the
// DoubleSpend code if another unmined transaction is a double spend of this
// transaction.
func (s *Store) InsertMemPoolTx(ns walletdb.ReadWriteBucket, rec *TxRecord) error {
	_, recVal := latestTxRecord(ns, rec.Hash[:])
	if recVal != nil {
		return errors.E(errors.Exist, "transaction already exists mined")
	}

	v := existsRawUnmined(ns, rec.Hash[:])
	if v != nil {
		return nil
	}

	// Check for other unmined transactions which cause this tx to be a double
	// spend.  Unlike mined transactions that double spend an unmined tx,
	// existing unmined txs are not removed when inserting a double spending
	// unmined tx.
	//
	// An exception is made for this rule for votes and revocations that double
	// spend an unmined vote that doesn't vote on the tip block.
	for _, input := range rec.MsgTx.TxIn {
		prevOut := &input.PreviousOutPoint
		k := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
		if v := existsRawUnminedInput(ns, k); v != nil {
			var spenderHash chainhash.Hash
			readRawUnminedInputSpenderHash(v, &spenderHash)

			// A switch is used here instead of an if statement so it can be
			// broken out of to the error below.
		DoubleSpendVoteCheck:
			switch rec.TxType {
			case stake.TxTypeSSGen, stake.TxTypeSSRtx:
				spenderVal := existsRawUnmined(ns, spenderHash[:])
				spenderTxBytes := extractRawUnminedTx(spenderVal)
				var spenderTx wire.MsgTx
				err := spenderTx.Deserialize(bytes.NewReader(spenderTxBytes))
				if err != nil {
					return errors.E(errors.IO, err)
				}
				if stake.DetermineTxType(&spenderTx) != stake.TxTypeSSGen {
					break DoubleSpendVoteCheck
				}
				votedBlock, _ := stake.SSGenBlockVotedOn(&spenderTx)
				tipBlock, _ := s.MainChainTip(ns)
				if votedBlock == tipBlock {
					err := errors.Errorf("vote or revocation %v double spends unmined "+
						"vote %v on the tip block", &rec.Hash, &spenderHash)
					return errors.E(errors.DoubleSpend, err)
				}

				err = s.RemoveUnconfirmed(ns, &spenderTx, &spenderHash)
				if err != nil {
					return err
				}
				continue
			}

			err := errors.Errorf("%v conflicts with %v by double spending %v", &rec.Hash, &spenderHash, prevOut)
			return errors.E(errors.DoubleSpend, err)
		}
	}

	log.Infof("Inserting unconfirmed transaction %v", &rec.Hash)
	v, err := valueTxRecord(rec)
	if err != nil {
		return err
	}
	err = putRawUnmined(ns, rec.Hash[:], v)
	if err != nil {
		return err
	}

	txType := stake.DetermineTxType(&rec.MsgTx)

	for i, input := range rec.MsgTx.TxIn {
		// Skip stakebases for votes.
		if i == 0 && txType == stake.TxTypeSSGen {
			continue
		}
		prevOut := &input.PreviousOutPoint
		k := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
		err = putRawUnminedInput(ns, k, rec.Hash[:])
		if err != nil {
			return err
		}
	}

	// If the transaction is a ticket purchase, record it in the ticket
	// purchases bucket.
	if txType == stake.TxTypeSStx {
		tk := rec.Hash[:]
		tv := existsRawTicketRecord(ns, tk)
		if tv == nil {
			tv = valueTicketRecord(-1)
			err := putRawTicketRecord(ns, tk, tv)
			if err != nil {
				return err
			}
		}
	}

	// TODO: increment credit amount for each credit (but those are unknown
	// here currently).

	return nil
}

// removeDoubleSpends checks for any unmined transactions which would introduce
// a double spend if tx was added to the store (either as a confirmed or unmined
// transaction).  Each conflicting transaction and all transactions which spend
// it are recursively removed.
func (s *Store) removeDoubleSpends(ns walletdb.ReadWriteBucket, rec *TxRecord) error {
	for _, input := range rec.MsgTx.TxIn {
		prevOut := &input.PreviousOutPoint
		prevOutKey := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
		doubleSpendHash := existsRawUnminedInput(ns, prevOutKey)
		if doubleSpendHash != nil {
			var doubleSpend TxRecord
			doubleSpendVal := existsRawUnmined(ns, doubleSpendHash)
			copy(doubleSpend.Hash[:], doubleSpendHash) // Silly but need an array
			err := readRawTxRecord(&doubleSpend.Hash, doubleSpendVal,
				&doubleSpend)
			if err != nil {
				return err
			}

			log.Debugf("Removing double spending transaction %v",
				doubleSpend.Hash)
			err = s.RemoveUnconfirmed(ns, &doubleSpend.MsgTx, &doubleSpend.Hash)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveUnconfirmed removes an unmined transaction record and all spend chains
// deriving from it from the store.  This is designed to remove transactions
// that would otherwise result in double spend conflicts if left in the store,
// and to remove transactions that spend coinbase transactions on reorgs. It
// can also be used to remove old tickets that do not meet the network difficulty
// and expired transactions.
func (s *Store) RemoveUnconfirmed(ns walletdb.ReadWriteBucket, tx *wire.MsgTx, txHash *chainhash.Hash) error {

	stxType := stake.DetermineTxType(tx)

	// For each potential credit for this record, each spender (if any) must
	// be recursively removed as well.  Once the spenders are removed, the
	// credit is deleted.
	numOuts := uint32(len(tx.TxOut))
	for i := uint32(0); i < numOuts; i++ {
		k := canonicalOutPoint(txHash, i)
		spenderHash := existsRawUnminedInput(ns, k)
		if spenderHash != nil {
			var spender TxRecord
			spenderVal := existsRawUnmined(ns, spenderHash)
			copy(spender.Hash[:], spenderHash) // Silly but need an array
			err := readRawTxRecord(&spender.Hash, spenderVal, &spender)
			if err != nil {
				return err
			}

			log.Debugf("Transaction %v is part of a removed conflict "+
				"chain -- removing as well", spender.Hash)
			err = s.RemoveUnconfirmed(ns, &spender.MsgTx, &spender.Hash)
			if err != nil {
				return err
			}
		}
		err := deleteRawUnminedCredit(ns, k)
		if err != nil {
			return err
		}

		if (stxType == stake.TxTypeSStx) && (i%2 == 1) {
			// An unconfirmed ticket leaving the store means we need to delete
			// the respective commitment and its entry from the unspent
			// commitments index.

			// This assumes that the key to ticket commitment values in the db are
			// canonicalOutPoints. If this ever changes, this needs to be changed to
			// use keyTicketCommitment(...).
			ktc := k
			vtc := existsRawTicketCommitment(ns, ktc)
			if vtc != nil {
				log.Debugf("Removing unconfirmed ticket commitment %s:%d",
					txHash, i)
				err = deleteRawTicketCommitment(ns, ktc)
				if err != nil {
					return err
				}
				err = deleteRawUnspentTicketCommitment(ns, ktc)
				if err != nil {
					return err
				}
			}
		}
	}

	if (stxType == stake.TxTypeSSGen) || (stxType == stake.TxTypeSSRtx) {
		// An unconfirmed vote/revocation leaving the store means we need to
		// unmark the commitments of the respective ticket as unminedSpent.
		s.replaceTicketCommitmentUnminedSpent(ns, stxType, tx, false)
	}

	// If this tx spends any previous credits (either mined or unmined), set
	// each unspent.  Mined transactions are only marked spent by having the
	// output in the unmined inputs bucket.
	for _, input := range tx.TxIn {
		prevOut := &input.PreviousOutPoint
		k := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
		err := deleteRawUnminedInput(ns, k)
		if err != nil {
			return err
		}

		// If a multisig output is recorded for this input, mark it unspent.
		if v := existsMultisigOut(ns, k); v != nil {
			vcopy := make([]byte, len(v))
			copy(vcopy, v)
			setMultisigOutUnSpent(vcopy)
			err := putMultisigOutRawValues(ns, k, vcopy)
			if err != nil {
				return err
			}
			err = putMultisigOutUS(ns, k)
			if err != nil {
				return err
			}
		}
	}

	return deleteRawUnmined(ns, txHash[:])
}

// UnminedTxs returns the underlying transactions for all unmined transactions
// which are not known to have been mined in a block.  Transactions are
// guaranteed to be sorted by their dependency order.
func (s *Store) UnminedTxs(ns walletdb.ReadBucket) ([]*wire.MsgTx, error) {
	recSet, err := s.unminedTxRecords(ns)
	if err != nil {
		return nil, err
	}

	recs := dependencySort(recSet)
	txs := make([]*wire.MsgTx, 0, len(recs))
	for _, rec := range recs {
		txs = append(txs, &rec.MsgTx)
	}
	return txs, nil
}

func (s *Store) unminedTxRecords(ns walletdb.ReadBucket) (map[chainhash.Hash]*TxRecord, error) {
	unmined := make(map[chainhash.Hash]*TxRecord)
	err := ns.NestedReadBucket(bucketUnmined).ForEach(func(k, v []byte) error {
		var txHash chainhash.Hash
		err := readRawUnminedHash(k, &txHash)
		if err != nil {
			return err
		}

		rec := new(TxRecord)
		err = readRawTxRecord(&txHash, v, rec)
		if err != nil {
			return err
		}

		unmined[rec.Hash] = rec
		return nil
	})
	return unmined, err
}

// UnminedTxHashes returns the hashes of all transactions not known to have been
// mined in a block.
func (s *Store) UnminedTxHashes(ns walletdb.ReadBucket) ([]*chainhash.Hash, error) {
	return s.unminedTxHashes(ns)
}

func (s *Store) unminedTxHashes(ns walletdb.ReadBucket) ([]*chainhash.Hash, error) {
	var hashes []*chainhash.Hash
	err := ns.NestedReadBucket(bucketUnmined).ForEach(func(k, v []byte) error {
		hash := new(chainhash.Hash)
		err := readRawUnminedHash(k, hash)
		if err == nil {
			hashes = append(hashes, hash)
		}
		return err
	})
	return hashes, err
}

// PruneUnmined removes unmined transactions that no longer belong in the
// unmined tx set.  This includes:
//
//   * Any transactions past a set expiry
//   * Ticket purchases with a different ticket price than the passed stake
//     difficulty
//   * Votes that do not vote on the tip block
func (s *Store) PruneUnmined(dbtx walletdb.ReadWriteTx, stakeDiff int64) error {
	ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)

	tipHash, tipHeight := s.MainChainTip(ns)

	type removeTx struct {
		tx   wire.MsgTx
		hash *chainhash.Hash
	}
	var toRemove []*removeTx

	c := ns.NestedReadBucket(bucketUnmined).ReadCursor()
	defer c.Close()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var tx wire.MsgTx
		err := tx.Deserialize(bytes.NewReader(extractRawUnminedTx(v)))
		if err != nil {
			return errors.E(errors.IO, err)
		}

		var expired, isTicketPurchase, isVote bool
		switch {
		case tx.Expiry != wire.NoExpiryValue && tx.Expiry <= uint32(tipHeight):
			expired = true
		case stake.IsSStx(&tx):
			isTicketPurchase = true
			if tx.TxOut[0].Value == stakeDiff {
				continue
			}
		case stake.IsSSGen(&tx):
			isVote = true
			// This will never actually error
			votedBlockHash, _ := stake.SSGenBlockVotedOn(&tx)
			if votedBlockHash == tipHash {
				continue
			}
		default:
			continue
		}

		txHash, err := chainhash.NewHash(k)
		if err != nil {
			return errors.E(errors.IO, err)
		}

		if expired {
			log.Infof("Removing expired unmined transaction %v", txHash)
		} else if isTicketPurchase {
			log.Infof("Removing old unmined ticket purchase %v", txHash)
		} else if isVote {
			log.Infof("Removing missed or invalid vote %v", txHash)
		}

		toRemove = append(toRemove, &removeTx{tx, txHash})
	}

	for _, r := range toRemove {
		err := s.RemoveUnconfirmed(ns, &r.tx, r.hash)
		if err != nil {
			return err
		}
	}

	return nil
}
