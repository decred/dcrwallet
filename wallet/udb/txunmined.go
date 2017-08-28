// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"fmt"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

// InsertMemPoolTx inserts a memory pool transaction record.  It also marks
// previous outputs referenced by its inputs as spent.  Errors with the
// apperrors.ErrDoubleSpend code if another unmined transaction is a double
// spend of this transaction.
func (s *Store) InsertMemPoolTx(ns walletdb.ReadWriteBucket, rec *TxRecord) error {
	_, recVal := latestTxRecord(ns, rec.Hash[:])
	if recVal != nil {
		const str = "transaction already exists mined"
		return apperrors.New(apperrors.ErrDuplicate, str)
	}

	v := existsRawUnmined(ns, rec.Hash[:])
	if v != nil {
		// TODO: compare serialized txs to ensure this isn't a hash collision?
		return nil
	}

	// Check for other unmined transactions which cause this tx to be a double
	// spend.  Unlike mined transactions that double spend an unmined tx,
	// existing unmined txs are not removed when inserting a double spending
	// unmined tx.
	//
	// An exception is made for this rule for revocations that double spend an
	// unmined vote that doesn't vote on the tip block.
	for _, input := range rec.MsgTx.TxIn {
		prevOut := &input.PreviousOutPoint
		k := canonicalOutPoint(&prevOut.Hash, prevOut.Index)
		if v := existsRawUnminedInput(ns, k); v != nil {
			var spenderHash chainhash.Hash
			readRawUnminedInputSpenderHash(v, &spenderHash)

			// A switch is used here instead of an if statement so it can be
			// broken out of to the error below.
		RevocationCheck:
			switch {
			case rec.TxType == stake.TxTypeSSRtx:
				spenderVal := existsRawUnmined(ns, spenderHash[:])
				spenderTxBytes := extractRawUnminedTx(spenderVal)
				var spenderTx wire.MsgTx
				err := spenderTx.Deserialize(bytes.NewReader(spenderTxBytes))
				if err != nil {
					str := fmt.Sprintf("failed to deserialize unmined "+
						"transaction %v", &spenderHash)
					return apperrors.Wrap(err, apperrors.ErrData, str)
				}
				if stake.DetermineTxType(&spenderTx) != stake.TxTypeSSGen {
					break RevocationCheck
				}
				votedBlock, _, err := stake.SSGenBlockVotedOn(&spenderTx)
				if err != nil {
					const str = "failed to determine voted block"
					return apperrors.Wrap(err, apperrors.ErrData, str)
				}
				tipBlock, _ := s.MainChainTip(ns)
				if votedBlock == tipBlock {
					const str = "revocation double spends unmined vote on " +
						"the tip block"
					return apperrors.New(apperrors.ErrDoubleSpend, str)
				}

				err = s.removeUnconfirmed(ns, &spenderTx, &spenderHash)
				if err != nil {
					return err
				}
				continue
			}

			str := fmt.Sprintf("unmined transaction %v is a double spend of "+
				"unmined transaction %v", &rec.Hash, &spenderHash)
			return apperrors.New(apperrors.ErrDoubleSpend, str)
		}
	}

	log.Infof("Inserting unconfirmed transaction %v", rec.Hash)
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
			err = s.removeUnconfirmed(ns, &doubleSpend.MsgTx, &doubleSpend.Hash)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// removeUnconfirmed removes an unmined transaction record and all spend chains
// deriving from it from the store.  This is designed to remove transactions
// that would otherwise result in double spend conflicts if left in the store,
// and to remove transactions that spend coinbase transactions on reorgs. It
// can also be used to remove old tickets that do not meet the network difficulty
// and expired transactions.
func (s *Store) removeUnconfirmed(ns walletdb.ReadWriteBucket, tx *wire.MsgTx, txHash *chainhash.Hash) error {
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
			err = s.removeUnconfirmed(ns, &spender.MsgTx, &spender.Hash)
			if err != nil {
				return err
			}
		}
		err := deleteRawUnminedCredit(ns, k)
		if err != nil {
			return err
		}
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
