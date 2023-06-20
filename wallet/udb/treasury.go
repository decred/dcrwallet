// Copyright (c) 2020-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/blockchain/stake/v5"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var (
	tspendPolicyBucketKey   = []byte("tspendpolicy")   // by tspend hash
	treasuryPolicyBucketKey = []byte("treasurypolicy") // by treasury key

	vspTspendPolicyBucketKey   = []byte("vsptspendpolicy")   // by ticket | tspend hash
	vspTreasuryPolicyBucketKey = []byte("vsptreasurypolicy") // by ticket | treasury key
)

type VSPTSpend struct {
	Ticket, TSpend chainhash.Hash
}

type VSPTreasuryKey struct {
	Ticket      chainhash.Hash
	TreasuryKey string
}

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
		return nil
	}
	return b.Put(hash[:], []byte{byte(policy)})
}

// SetVSPTSpendPolicy sets a tspend vote policy for a specific tspend
// transaction hash for a VSP customer's ticket hash.
func SetVSPTSpendPolicy(dbtx walletdb.ReadWriteTx, ticketHash, tspendHash *chainhash.Hash,
	policy stake.TreasuryVoteT) error {

	k := make([]byte, 64)
	copy(k, ticketHash[:])
	copy(k[32:], tspendHash[:])

	b := dbtx.ReadWriteBucket(vspTspendPolicyBucketKey)
	if policy == stake.TreasuryVoteInvalid {
		err := b.Delete(k)
		if err != nil {
			return errors.E(errors.IO, err)
		}
		return nil
	}
	return b.Put(k, []byte{byte(policy)})
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

// VSPTSpendPolicy returns the tspend vote policy for a specific tspend
// transaction hash for a VSP customer's ticket hash.
func VSPTSpendPolicy(dbtx walletdb.ReadTx, ticketHash,
	tspendHash *chainhash.Hash) (stake.TreasuryVoteT, error) {

	key := make([]byte, 64)
	copy(key, ticketHash[:])
	copy(key[32:], tspendHash[:])

	b := dbtx.ReadBucket(vspTspendPolicyBucketKey)
	v := b.Get(key)
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

// VSPTSpendPolicies returns all tspend vote policies keyed by a tspend hash and
// a VSP customer's ticket hash.  Abstaining policies are excluded.
func VSPTSpendPolicies(dbtx walletdb.ReadTx) (map[VSPTSpend]stake.TreasuryVoteT, error) {
	b := dbtx.ReadBucket(vspTspendPolicyBucketKey)
	policies := make(map[VSPTSpend]stake.TreasuryVoteT)
	err := b.ForEach(func(k, _ []byte) error {
		var key VSPTSpend
		copy(key.Ticket[:], k)
		copy(key.TSpend[:], k[32:])

		policy, err := VSPTSpendPolicy(dbtx, &key.Ticket, &key.TSpend)
		if err != nil {
			return err
		}
		if policy == stake.TreasuryVoteInvalid {
			return nil
		}
		policies[key] = policy
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
		return nil
	}
	return b.Put(pikey, []byte{byte(policy)})
}

// SetVSPTreasuryKeyPolicy sets a tspend vote policy for a specific Politeia
// instance key for a VSP customer's ticket.
func SetVSPTreasuryKeyPolicy(dbtx walletdb.ReadWriteTx, ticket *chainhash.Hash,
	pikey []byte, policy stake.TreasuryVoteT) error {

	k := make([]byte, 0, chainhash.HashSize+len(pikey))
	k = append(k, ticket[:]...)
	k = append(k, pikey...)

	b := dbtx.ReadWriteBucket(vspTreasuryPolicyBucketKey)
	if policy == stake.TreasuryVoteInvalid {
		err := b.Delete(k)
		if err != nil {
			return errors.E(errors.IO, err)
		}
		return nil
	}
	return b.Put(k, []byte{byte(policy)})
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

// VSPTreasuryKeyPolicy returns the tspend vote policy for a specific Politeia
// instance key for a VSP customer's ticket.
func VSPTreasuryKeyPolicy(dbtx walletdb.ReadTx, ticket *chainhash.Hash,
	pikey []byte) (stake.TreasuryVoteT, error) {

	k := make([]byte, 0, chainhash.HashSize+len(pikey))
	k = append(k, ticket[:]...)
	k = append(k, pikey...)

	b := dbtx.ReadBucket(vspTreasuryPolicyBucketKey)
	v := b.Get(k)
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

// VSPTreasuryKeyPolicies returns all tspend vote policies keyed by a Politeia
// instance key for a VSP customer's ticket.  Abstaining policies are excluded.
func VSPTreasuryKeyPolicies(dbtx walletdb.ReadTx) (map[VSPTreasuryKey]stake.TreasuryVoteT, error) {
	b := dbtx.ReadBucket(vspTreasuryPolicyBucketKey)
	policies := make(map[VSPTreasuryKey]stake.TreasuryVoteT)
	err := b.ForEach(func(k, _ []byte) error {
		var key VSPTreasuryKey
		pikey := k[chainhash.HashSize:]
		copy(key.Ticket[:], k)
		key.TreasuryKey = string(pikey)

		policy, err := VSPTreasuryKeyPolicy(dbtx, &key.Ticket, pikey)
		if err != nil {
			return err
		}
		if policy == stake.TreasuryVoteInvalid {
			return nil
		}
		policies[key] = policy
		return nil
	})
	return policies, err
}
