// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"decred.org/dcrwallet/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var (
	vspBucketKey = []byte("vsp")
)

type VSPTicket struct {
	FeeHash chainhash.Hash
	// TODO add vote preferences?
	// VotingPreference map[string]string
}

// SetVSPTicket sets a VSPTicket record into the db.
func SetVSPTicket(dbtx walletdb.ReadWriteTx, ticketHash *chainhash.Hash, record *VSPTicket) error {
	bucket := dbtx.ReadWriteBucket(vspBucketKey)
	serializedRecord := serializeVSPTicket(record)

	// Save the creation date of the store.
	return bucket.Put(ticketHash.CloneBytes(), serializedRecord)
}

// VSPTickets gets all vsp tickets from the db.
func VSPTickets(dbtx walletdb.ReadTx) ([]*VSPTicket, error) {
	bucket := dbtx.ReadBucket(vspBucketKey)
	var tickets []*VSPTicket
	err := bucket.ForEach(func(k, v []byte) error {
		if len(v) == 0 {
			return nil
		}
		ticket := deserializeVSPTicket(v)
		tickets = append(tickets, ticket)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return tickets, nil
}

// GetVSPTicket gets a specific ticket by its hash.
func GetVSPTicket(dbtx walletdb.ReadTx, tickethash chainhash.Hash) (*VSPTicket, error) {
	bucket := dbtx.ReadBucket(vspBucketKey)
	serializedTicket := bucket.Get(tickethash.CloneBytes())
	ticket := deserializeVSPTicket(serializedTicket)

	return ticket, nil
}

// deserializeUserTicket deserializes the passed serialized user
// ticket information.
func deserializeVSPTicket(serializedTicket []byte) *VSPTicket {
	// Cursory check to make sure that the size of the
	// ticket makes sense.
	// if len(serializedTicket)%stakePoolUserTicketSize != 0 {
	// 	return nil, errors.E(errors.IO, "invalid pool ticket record size")
	// }

	curPos := 0

	var feeHash chainhash.Hash

	// Insert the ticket hash into the record.
	feeHash.SetBytes(serializedTicket[curPos : curPos+hashSize])

	vspTicket := &VSPTicket{
		FeeHash: feeHash,
	}
	return vspTicket
}

// serializeUserTicket returns the serialization of a single stake pool
// user ticket.
func serializeVSPTicket(record *VSPTicket) []byte {
	buf := make([]byte, hashSize)
	curPos := 0

	// Write the fee hash.
	copy(buf[curPos:curPos+hashSize], record.FeeHash[:])

	return buf
}
