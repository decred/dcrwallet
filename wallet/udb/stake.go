// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/decred/dcrd/blockchain/stake"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainec"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

func stakeStoreError(code apperrors.Code, str string, err error) error {
	return apperrors.E{ErrorCode: code, Description: str, Err: err}
}

// sstxRecord is the structure for a stored SStx.
type sstxRecord struct {
	tx          *dcrutil.Tx
	ts          time.Time
	voteBitsSet bool   // Removed in version 3
	voteBits    uint16 // Removed in version 3
	voteBitsExt []byte // Removed in version 3
}

// ssgenRecord is the structure for a stored SSGen tx. There's no
// real reason to store the actual transaction I don't think,
// the inputs and outputs are all predetermined from the block
// height and the original SStx it references.
//
// TODO Store the extended votebits, too.
type ssgenRecord struct {
	blockHash   chainhash.Hash
	blockHeight uint32
	txHash      chainhash.Hash
	voteBits    uint16
	ts          time.Time
}

// ssrtxRecord is the structure for a stored SSRtx. While the
// ssrtx itself does not include the block hash or block height,
// we still preserve that so that we know the block ntfn that
// informed us that the sstx was missed.
type ssrtxRecord struct {
	blockHash   chainhash.Hash
	blockHeight uint32
	txHash      chainhash.Hash
	ts          time.Time
}

// TicketStatus is the current status of a stake pool ticket.
type TicketStatus uint8

const (
	// TSImmatureOrLive indicates that the ticket is either
	// live or immature.
	TSImmatureOrLive = iota

	// TSVoted indicates that the ticket was spent as a vote.
	TSVoted

	// TSMissed indicates that the ticket was spent as a
	// revocation.
	TSMissed
)

// PoolTicket is a 73-byte struct that is used to preserve user's
// ticket information when they have an account at the stake pool.
type PoolTicket struct {
	Ticket       chainhash.Hash
	HeightTicket uint32
	Status       TicketStatus
	HeightSpent  uint32
	SpentBy      chainhash.Hash
}

// StakePoolUser is a list of tickets for a given user (P2SH
// address) in the stake pool.
type StakePoolUser struct {
	Tickets        []*PoolTicket
	InvalidTickets []*chainhash.Hash
}

// StakeStore represents a safely accessible database of
// stake transactions.
type StakeStore struct {
	Params  *chaincfg.Params
	Manager *Manager

	ownedSStxs map[chainhash.Hash]struct{}
	mtx        sync.RWMutex // only protects ownedSStxs
}

// checkHashInStore checks if a hash exists in ownedSStxs.
func (s *StakeStore) checkHashInStore(hash *chainhash.Hash) bool {
	_, exists := s.ownedSStxs[*hash]
	return exists
}

// OwnTicket returns whether the ticket is tracked by the stake manager.
func (s *StakeStore) OwnTicket(hash *chainhash.Hash) bool {
	s.mtx.RLock()
	owned := s.checkHashInStore(hash)
	s.mtx.RUnlock()
	return owned
}

// addHashToStore adds a hash into ownedSStxs.
func (s *StakeStore) addHashToStore(hash *chainhash.Hash) {
	s.ownedSStxs[*hash] = struct{}{}
}

// insertSStx inserts an SStx into the store.
func (s *StakeStore) insertSStx(ns walletdb.ReadWriteBucket, sstx *dcrutil.Tx) error {
	// If we already have the SStx, no need to
	// try to include twice.
	exists := s.checkHashInStore(sstx.Hash())
	if exists {
		log.Tracef("Attempted to insert SStx %v into the stake store, "+
			"but the SStx already exists.", sstx.Hash())
		return nil
	}
	record := &sstxRecord{
		tx: sstx,
		ts: time.Now(),
	}

	// Add the SStx to the database.
	err := putSStxRecord(ns, record, DBVersion)
	if err != nil {
		return err
	}

	// Add the SStx's hash to the internal list in the store.
	s.addHashToStore(sstx.Hash())

	return nil
}

// InsertSStx is the exported version of insertSStx that is safe for concurrent
// access.
func (s *StakeStore) InsertSStx(ns walletdb.ReadWriteBucket, sstx *dcrutil.Tx) error {
	s.mtx.Lock()
	err := s.insertSStx(ns, sstx)
	s.mtx.Unlock()
	return err
}

// dumpSStxHashes dumps the hashes of all owned SStxs. Note
// that this doesn't use the DB.
func (s *StakeStore) dumpSStxHashes() []chainhash.Hash {
	// Copy the hash list of sstxs. You could pass the pointer
	// directly but you risk that the size of the internal
	// ownedSStxs is later modified while the end user is
	// working with the returned list.
	ownedSStxs := make([]chainhash.Hash, len(s.ownedSStxs))

	itr := 0
	for hash := range s.ownedSStxs {
		ownedSStxs[itr] = hash
		itr++
	}

	return ownedSStxs
}

// DumpSStxHashes returns the hashes of all wallet ticket purchase transactions.
func (s *StakeStore) DumpSStxHashes() []chainhash.Hash {
	defer s.mtx.RUnlock()
	s.mtx.RLock()

	return s.dumpSStxHashes()
}

// dumpSStxHashes dumps the hashes of all owned SStxs for some address.
func (s *StakeStore) dumpSStxHashesForAddress(ns walletdb.ReadBucket, addr dcrutil.Address) ([]chainhash.Hash, error) {
	// Extract the HASH160 script hash; if it's not 20 bytes
	// long, return an error.
	hash160 := addr.ScriptAddress()
	if len(hash160) != 20 {
		str := "stake store is closed"
		return nil, stakeStoreError(apperrors.ErrInput, str, nil)
	}
	_, addrIsP2SH := addr.(*dcrutil.AddressScriptHash)

	allTickets := s.dumpSStxHashes()
	var ticketsForAddr []chainhash.Hash

	// Access the database and store the result locally.
	for _, h := range allTickets {
		thisHash160, p2sh, err := fetchSStxRecordSStxTicketHash160(ns, &h, DBVersion)
		if err != nil {
			str := "failure getting ticket 0th out script hashes from db"
			return nil, stakeStoreError(apperrors.ErrDatabase, str, err)
		}
		if addrIsP2SH != p2sh {
			continue
		}

		if bytes.Equal(hash160, thisHash160) {
			ticketsForAddr = append(ticketsForAddr, h)
		}
	}

	return ticketsForAddr, nil
}

// DumpSStxHashesForAddress returns the hashes of all wallet ticket purchase
// transactions for an address.
func (s *StakeStore) DumpSStxHashesForAddress(ns walletdb.ReadBucket, addr dcrutil.Address) ([]chainhash.Hash, error) {
	defer s.mtx.RUnlock()
	s.mtx.RLock()

	return s.dumpSStxHashesForAddress(ns, addr)
}

// sstxAddress returns the address for a given ticket.
func (s *StakeStore) sstxAddress(ns walletdb.ReadBucket, hash *chainhash.Hash) (dcrutil.Address, error) {
	// Access the database and store the result locally.
	thisHash160, p2sh, err := fetchSStxRecordSStxTicketHash160(ns, hash, DBVersion)
	if err != nil {
		str := "failure getting ticket 0th out script hashes from db"
		return nil, stakeStoreError(apperrors.ErrDatabase, str, err)
	}
	var addr dcrutil.Address
	if p2sh {
		addr, err = dcrutil.NewAddressScriptHashFromHash(thisHash160, s.Params)
	} else {
		addr, err = dcrutil.NewAddressPubKeyHash(thisHash160, s.Params, chainec.ECTypeSecp256k1)
	}
	if err != nil {
		str := "failure getting ticket 0th out script hashes from db"
		return nil, stakeStoreError(apperrors.ErrDatabase, str, err)
	}

	return addr, nil
}

// SStxAddress is the exported, concurrency safe version of sstxAddress.
func (s *StakeStore) SStxAddress(ns walletdb.ReadBucket, hash *chainhash.Hash) (dcrutil.Address, error) {
	return s.sstxAddress(ns, hash)
}

// dumpSSGenHashes fetches and returns the entire list of votes generated by
// this wallet, including votes that were produced but were never included in
// the blockchain.
func (s *StakeStore) dumpSSGenHashes(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	var voteList []chainhash.Hash

	// Open the vite records database.
	bucket := ns.NestedReadBucket(ssgenRecordsBucketName)

	// Store each hash sequentially.
	err := bucket.ForEach(func(k []byte, v []byte) error {
		recs, errDeser := deserializeSSGenRecords(v)
		if errDeser != nil {
			return errDeser
		}

		for _, rec := range recs {
			voteList = append(voteList, rec.txHash)
		}
		return nil
	})
	return voteList, err
}

// DumpSSGenHashes is the exported version of dumpSSGenHashes that is safe
// for concurrent access.
func (s *StakeStore) DumpSSGenHashes(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	return s.dumpSSGenHashes(ns)
}

// dumpSSRtxTickets fetches the entire list of tickets spent as revocations
// by this wallet.
func (s *StakeStore) dumpSSRtxTickets(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	var ticketList []chainhash.Hash

	// Open the revocation records database.
	bucket := ns.NestedReadBucket(ssrtxRecordsBucketName)

	// Store each hash sequentially.
	err := bucket.ForEach(func(k []byte, v []byte) error {
		ticket, errDeser := chainhash.NewHash(k)
		if errDeser != nil {
			return errDeser
		}

		ticketList = append(ticketList, *ticket)
		return nil
	})
	return ticketList, err
}

// DumpSSRtxTickets is the exported version of dumpSSRtxTickets that is safe
// for concurrent access.
func (s *StakeStore) DumpSSRtxTickets(ns walletdb.ReadBucket) ([]chainhash.Hash, error) {
	return s.dumpSSRtxTickets(ns)
}

// insertSSGen inserts an SSGen record into the DB (keyed to the SStx it
// spends.
func insertSSGen(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64,
	ssgenHash *chainhash.Hash, voteBits uint16, sstxHash *chainhash.Hash) error {

	if blockHeight <= 0 {
		return fmt.Errorf("invalid SSGen block height")
	}

	record := &ssgenRecord{
		*blockHash,
		uint32(blockHeight),
		*ssgenHash,
		voteBits,
		time.Now(),
	}

	// Add the SSGen to the database.
	return putSSGenRecord(ns, sstxHash, record)
}

// InsertSSGen is the exported version of insertSSGen that is safe for
// concurrent access.
func (s *StakeStore) InsertSSGen(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64, ssgenHash *chainhash.Hash, voteBits uint16, sstxHash *chainhash.Hash) error {
	return insertSSGen(ns, blockHash, blockHeight, ssgenHash, voteBits, sstxHash)
}

// TicketPurchase returns the ticket purchase transaction recorded in the "stake
// manager" portion of the DB.
//
// TODO: This is redundant and should be looked up in from the transaction
// manager.  Left for now for compatibility.
func (s *StakeStore) TicketPurchase(dbtx walletdb.ReadTx, hash *chainhash.Hash) (*wire.MsgTx, error) {
	ns := dbtx.ReadBucket(wstakemgrBucketKey)

	ticketRecord, err := fetchSStxRecord(ns, hash, DBVersion)
	if err != nil {
		return nil, err
	}
	return ticketRecord.tx.MsgTx(), nil
}

// StoreVoteInfo records information about a vote transaction.
//
// TODO: Much of this is redundant and we probably don't want to track it here
// anyways.  Would be better to handle this in the transaction manager.
func (s *StakeStore) StoreVoteInfo(dbtx walletdb.ReadWriteTx, ticketHash, voteHash, blockHash *chainhash.Hash,
	blockHeight int32, voteBits stake.VoteBits) error {

	ns := dbtx.ReadWriteBucket(wstakemgrBucketKey)
	return insertSSGen(ns, blockHash, int64(blockHeight), voteHash, voteBits.Bits,
		ticketHash)
}

// insertSSRtx inserts an SSRtx record into the DB (keyed to the SStx it
// spends.
func (s *StakeStore) insertSSRtx(ns walletdb.ReadWriteBucket, blockHash *chainhash.Hash, blockHeight int64, ssrtxHash *chainhash.Hash, sstxHash *chainhash.Hash) error {

	if blockHeight <= 0 {
		return fmt.Errorf("invalid SSRtx block height")
	}

	record := &ssrtxRecord{
		*blockHash,
		uint32(blockHeight),
		*ssrtxHash,
		time.Now(),
	}

	// Add the SSRtx to the database.
	return putSSRtxRecord(ns, sstxHash, record)
}

// StoreRevocationInfo records information about a revocation transaction.
//
// TODO: Much of this is redundant and we probably don't want to track it here
// anyways.  Would be better to handle this in the transaction manager.
func (s *StakeStore) StoreRevocationInfo(dbtx walletdb.ReadWriteTx, ticketHash, revocationHash *chainhash.Hash,
	blockHash *chainhash.Hash, blockHeight int32) error {

	ns := dbtx.ReadWriteBucket(wstakemgrBucketKey)
	return s.insertSSRtx(ns, blockHash, int64(blockHeight), revocationHash,
		ticketHash)
}

// updateStakePoolUserTickets updates a stake pool ticket for a given user.
// If the ticket does not currently exist in the database, it adds it. If it
// does exist (the ticket hash exists), it replaces the old record.
func (s *StakeStore) updateStakePoolUserTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address, ticket *PoolTicket) error {
	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	return updateStakePoolUserTickets(ns, *scriptHash, ticket)
}

// UpdateStakePoolUserTickets is the exported and concurrency safe form of
// updateStakePoolUserTickets.
func (s *StakeStore) UpdateStakePoolUserTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address, ticket *PoolTicket) error {
	return s.updateStakePoolUserTickets(ns, user, ticket)
}

// removeStakePoolUserInvalTickets prepares the user.Address and asks stakedb
// to remove the formerly invalid tickets.
func (s *StakeStore) removeStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address,
	ticket *chainhash.Hash) error {

	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	return removeStakePoolInvalUserTickets(ns, *scriptHash, ticket)
}

// RemoveStakePoolUserInvalTickets is the exported and concurrency safe form of
// removetStakePoolUserInvalTickets.
func (s *StakeStore) RemoveStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address,
	ticket *chainhash.Hash) error {
	return s.removeStakePoolUserInvalTickets(ns, user, ticket)
}

// updateStakePoolUserInvalTickets updates the list of invalid stake pool
// tickets for a given user. If the ticket does not currently exist in the
// database, it adds it.
func (s *StakeStore) updateStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address, ticket *chainhash.Hash) error {
	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	return updateStakePoolInvalUserTickets(ns, *scriptHash, ticket)
}

// UpdateStakePoolUserInvalTickets is the exported and concurrency safe form of
// updateStakePoolUserInvalTickets.
func (s *StakeStore) UpdateStakePoolUserInvalTickets(ns walletdb.ReadWriteBucket, user dcrutil.Address, ticket *chainhash.Hash) error {
	return s.updateStakePoolUserInvalTickets(ns, user, ticket)
}

func stakePoolUserInfo(ns walletdb.ReadBucket, user dcrutil.Address) (*StakePoolUser, error) {
	_, isScriptHash := user.(*dcrutil.AddressScriptHash)
	_, isP2PKH := user.(*dcrutil.AddressPubKeyHash)
	if !(isScriptHash || isP2PKH) {
		str := fmt.Sprintf("user %v is invalid", user.EncodeAddress())
		return nil, stakeStoreError(apperrors.ErrBadPoolUserAddr, str, nil)
	}
	scriptHashB := user.ScriptAddress()
	scriptHash := new([20]byte)
	copy(scriptHash[:], scriptHashB)

	stakePoolUser := new(StakePoolUser)

	// Catch missing user errors below and blank out the stake
	// pool user information for the section if the user has
	// no entries.
	missingValidTickets, missingInvalidTickets := false, false

	userTickets, fetchErrVal := fetchStakePoolUserTickets(ns, *scriptHash)
	if fetchErrVal != nil {
		stakeMgrErr, is := fetchErrVal.(apperrors.E)
		if is {
			missingValidTickets = stakeMgrErr.ErrorCode ==
				apperrors.ErrPoolUserTicketsNotFound
		} else {
			return nil, fetchErrVal
		}
	}
	if missingValidTickets {
		userTickets = make([]*PoolTicket, 0)
	}

	invalTickets, fetchErrInval := fetchStakePoolUserInvalTickets(ns,
		*scriptHash)
	if fetchErrInval != nil {
		stakeMgrErr, is := fetchErrInval.(apperrors.E)
		if is {
			missingInvalidTickets = stakeMgrErr.ErrorCode ==
				apperrors.ErrPoolUserInvalTcktsNotFound
		} else {
			return nil, fetchErrInval
		}
	}
	if missingInvalidTickets {
		invalTickets = make([]*chainhash.Hash, 0)
	}

	stakePoolUser.Tickets = userTickets
	stakePoolUser.InvalidTickets = invalTickets

	return stakePoolUser, nil
}

// StakePoolUserInfo returns the stake pool user information for a given stake
// pool user, keyed to their P2SH voting address.
func (s *StakeStore) StakePoolUserInfo(ns walletdb.ReadBucket, user dcrutil.Address) (*StakePoolUser, error) {
	return stakePoolUserInfo(ns, user)
}

// loadManager returns a new stake manager that results from loading it from
// the passed opened database.  The public passphrase is required to decrypt the
// public keys.
func (s *StakeStore) loadOwnedSStxs(ns walletdb.ReadBucket) error {
	// Regenerate the list of tickets.
	// Perform all database lookups in a read-only view.
	ticketList := make(map[chainhash.Hash]struct{})

	// Open the sstx records database.
	bucket := ns.NestedReadBucket(sstxRecordsBucketName)

	// Store each key sequentially.
	err := bucket.ForEach(func(k []byte, v []byte) error {
		var errNewHash error
		var hash *chainhash.Hash

		hash, errNewHash = chainhash.NewHash(k)
		if errNewHash != nil {
			return errNewHash
		}
		ticketList[*hash] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}

	s.ownedSStxs = ticketList
	return nil
}

// newStakeStore initializes a new stake store with the given parameters.
func newStakeStore(params *chaincfg.Params, manager *Manager) *StakeStore {
	return &StakeStore{
		Params:     params,
		Manager:    manager,
		ownedSStxs: make(map[chainhash.Hash]struct{}),
	}
}

// openStakeStore loads an existing stake manager from the given namespace,
// waddrmgr, and network parameters.
//
// A ManagerError with an error code of ErrNoExist will be returned if the
// passed manager does not exist in the specified namespace.
func openStakeStore(ns walletdb.ReadBucket, manager *Manager, params *chaincfg.Params) (*StakeStore, error) {
	// Return an error if the manager has NOT already been created in the
	// given database namespace.
	exists := stakeStoreExists(ns)
	if !exists {
		str := "the specified stake store/manager does not exist in db"
		return nil, stakeStoreError(apperrors.ErrNoExist, str, nil)
	}

	ss := newStakeStore(params, manager)

	err := ss.loadOwnedSStxs(ns)
	if err != nil {
		return nil, err
	}

	return ss, nil
}
