// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/wallet/walletdb"
	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var (
	tspendPolicyBucketKey   = []byte("tspendpolicy")   // by tspend hash
	treasuryPolicyBucketKey = []byte("treasurypolicy") // by treasury key
)

// SetTSpendPolicy sets a tspend vote policy for a specific tspend transaction
// hash.
func SetTSpendPolicy(dbtx walletdb.ReadWriteTx, hash *chainhash.Hash,
	policy stake.TreasuryVoteT) error {

	b := dbtx.ReadWriteBucket(tspendPolicyBucketKey)
	if policy == stake.TreasuryVoteInvalid {
		err := b.Delete(hash[:])
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}
	return b.Put(hash[:], []byte{byte(policy)})
}

// TSpendPolicy returns the tspend vote policy for a specific tspend transaction
// hash.
func TSpendPolicy(dbtx walletdb.ReadTx, hash *chainhash.Hash) (stake.TreasuryVoteT, error) {
	b := dbtx.ReadBucket(tspendPolicyBucketKey)
	v := b.Get(hash[:])
	if v == nil {
		return stake.TreasuryVoteInvalid, nil
	}
	if len(v) != 1 {
		err := errors.Errorf("invalid length %v for tspend "+
			"vote policy", len(v))
		return 0, errors.E(errors.IO, err)
	}
	return stake.TreasuryVoteT(v[0]), nil
}

// TSpendPolicies returns all tspend vote policies keyed by a tspend hash.
// Abstaining policies are excluded.
func TSpendPolicies(dbtx walletdb.ReadTx) (map[chainhash.Hash]stake.TreasuryVoteT, error) {
	b := dbtx.ReadBucket(tspendPolicyBucketKey)
	policies := make(map[chainhash.Hash]stake.TreasuryVoteT)
	err := b.ForEach(func(hash, _ []byte) error {
		var tspendHash chainhash.Hash
		copy(tspendHash[:], hash)
		policy, err := TSpendPolicy(dbtx, &tspendHash)
		if err != nil {
			return err
		}
		if policy == stake.TreasuryVoteInvalid {
			return nil
		}
		policies[tspendHash] = policy
		return nil
	})
	return policies, err
}

// SetTreasuryKeyPolicy sets a tspend vote policy for a specific Politeia
// instance key.
func SetTreasuryKeyPolicy(dbtx walletdb.ReadWriteTx, pikey []byte,
	policy stake.TreasuryVoteT) error {

	b := dbtx.ReadWriteBucket(treasuryPolicyBucketKey)
	if policy == stake.TreasuryVoteInvalid {
		err := b.Delete(pikey)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}
	return b.Put(pikey, []byte{byte(policy)})
}

// TreasuryKeyPolicy returns the tspend vote policy for a specific Politeia
// instance key.
func TreasuryKeyPolicy(dbtx walletdb.ReadTx, pikey []byte) (stake.TreasuryVoteT, error) {
	b := dbtx.ReadBucket(treasuryPolicyBucketKey)
	v := b.Get(pikey)
	if v == nil {
		return stake.TreasuryVoteInvalid, nil
	}
	if len(v) != 1 {
		err := errors.Errorf("invalid length %v for treasury "+
			"key policy", len(v))
		return 0, errors.E(errors.IO, err)
	}
	return stake.TreasuryVoteT(v[0]), nil
}

// TreasuryKeyPolicies returns all tspend vote policies keyed by a Politeia
// instance key.  Abstaining policies are excluded.
func TreasuryKeyPolicies(dbtx walletdb.ReadTx) (map[string]stake.TreasuryVoteT, error) {
	b := dbtx.ReadBucket(treasuryPolicyBucketKey)
	policies := make(map[string]stake.TreasuryVoteT)
	err := b.ForEach(func(pikey, _ []byte) error {
		policy, err := TreasuryKeyPolicy(dbtx, pikey)
		if err != nil {
			return err
		}
		if policy == stake.TreasuryVoteInvalid {
			return nil
		}
		policies[string(pikey)] = policy
		return nil
	})
	return policies, err
}
