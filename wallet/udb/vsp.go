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
}

// SetVSPTicket sets a VSPTicket record into the db.
func SetVSPTicket(dbtx walletdb.ReadWriteTx, ticketHash *chainhash.Hash, record *VSPTicket) error {
	bucket := dbtx.ReadWriteBucket(vspBucketKey)
	serializedRecord := serializeVSPTicket(record)

	// Save the creation date of the store.
	return bucket.Put(ticketHash[:], serializedRecord)
}

// GetVSPTicket gets a specific ticket by its hash.
func GetVSPTicket(dbtx walletdb.ReadTx, tickethash chainhash.Hash) (*VSPTicket, error) {
	bucket := dbtx.ReadBucket(vspBucketKey)
	serializedTicket := bucket.Get(tickethash[:])
	ticket := deserializeVSPTicket(serializedTicket)

	return ticket, nil
}

// deserializeUserTicket deserializes the passed serialized user
// ticket information.
func deserializeVSPTicket(serializedTicket []byte) *VSPTicket {

	vspTicket := &VSPTicket{}
	copy(vspTicket.FeeHash[:], serializedTicket[:])
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
