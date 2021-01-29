// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var (
	vspBucketKey = []byte("vsp")
)

// FeeStatus represents the current fee status of a ticket.
type FeeStatus int

// FeeStatus types
const (
	// VSPFeeProcessStarted represents the state which process has being
	// called but fee still not paid.
	VSPFeeProcessStarted FeeStatus = iota
	// VSPFeeProcessPaid represents the state where the process has being
	// paid, but not published.
	VSPFeeProcessPaid
	VSPFeeProcessErrored
)

type VSPTicket struct {
	FeeHash     chainhash.Hash
	FeeTxStatus uint32
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
	if serializedTicket == nil {
		err := errors.Errorf("no VSP info for ticket %v", &tickethash)
		return nil, errors.E(errors.NotExist, err)
	}
	ticket := deserializeVSPTicket(serializedTicket)

	return ticket, nil
}

// GetVSPTicketsByFeeStatus gets all vsp tickets which have
// FeeTxStatus == feeStatus.
func GetVSPTicketsByFeeStatus(dbtx walletdb.ReadTx, feeStatus int) (map[chainhash.Hash]*VSPTicket, error) {
	bucket := dbtx.ReadBucket(vspBucketKey)
	tickets := make(map[chainhash.Hash]*VSPTicket)
	err := bucket.ForEach(func(k, v []byte) error {
		ticket := deserializeVSPTicket(v)
		if int(ticket.FeeTxStatus) == feeStatus {
			var hash chainhash.Hash
			hash.SetBytes(k)
			tickets[hash] = ticket
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tickets, nil
}

// deserializeUserTicket deserializes the passed serialized user
// ticket information.
func deserializeVSPTicket(serializedTicket []byte) *VSPTicket {
	// ticket stores hash size and an uint32 representing the fee processment
	// status.
	curPos := 0
	vspTicket := &VSPTicket{}
	copy(vspTicket.FeeHash[:], serializedTicket[curPos:hashSize])
	curPos += hashSize
	vspTicket.FeeTxStatus = byteOrder.Uint32(serializedTicket[curPos : curPos+4])

	return vspTicket
}

// serializeUserTicket returns the serialization of a single stake pool
// user ticket.
func serializeVSPTicket(record *VSPTicket) []byte {
	// ticket hash size + fee processment status
	buf := make([]byte, hashSize+4)
	curPos := 0
	// Write the fee hash.
	copy(buf[curPos:curPos+hashSize], record.FeeHash[:])
	curPos += hashSize
	// Write the fee tx status
	byteOrder.PutUint32(buf[curPos:curPos+4], record.FeeTxStatus)

	return buf
}
