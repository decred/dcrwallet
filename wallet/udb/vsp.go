// Copyright (c) 2020-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"

	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var (
	vspBucketKey     = []byte("vsp")
	vspHostBucketKey = []byte("vsphost")
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
	// VSPFeeProcessConfirmed represents the state where the fee has been
	// confirmed by the VSP.
	VSPFeeProcessConfirmed
)

type VSPTicket struct {
	FeeHash     chainhash.Hash
	FeeTxStatus uint32
	VSPHostID   uint32
	Host        string
	PubKey      []byte
}

// SetVSPTicket sets a VSPTicket record into the db.
func SetVSPTicket(dbtx walletdb.ReadWriteTx, ticketHash *chainhash.Hash, record *VSPTicket) error {

	// Check if this VSP is already present in the database.
	vsp, vspHostID, err := FindVSP(dbtx, []byte(record.Host))
	if err != nil {
		return err
	}

	// Create an entry for this VSP if one doesn't already exist.
	if vsp == nil {
		vspHostBucket := dbtx.ReadWriteBucket(vspHostBucketKey)
		v := vspHostBucket.Get(rootVSPHostIndex)
		vspHostID = byteOrder.Uint32(v)
		vspHostID += 1 // Bump current index
		err = SetVSPHost(dbtx, vspHostID, &VSPHost{
			Host:   []byte(record.Host),
			PubKey: record.PubKey,
		})
		if err != nil {
			return err
		}

		vspIndexBytes := make([]byte, 4)
		byteOrder.PutUint32(vspIndexBytes, vspHostID)
		err = vspHostBucket.Put(rootVSPHostIndex, vspIndexBytes)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	record.VSPHostID = vspHostID

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

	host, err := GetVSPHost(dbtx, ticket.VSPHostID)
	if err != nil {
		return nil, err
	}

	ticket.Host = string(host.Host)
	ticket.PubKey = host.PubKey

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
	curPos += 4
	vspTicket.VSPHostID = byteOrder.Uint32(serializedTicket[curPos : curPos+4])

	return vspTicket
}

// serializeUserTicket returns the serialization of a single stake pool
// user ticket.
func serializeVSPTicket(record *VSPTicket) []byte {
	// ticket hash size + fee processment status + host ID
	buf := make([]byte, hashSize+4+4)
	curPos := 0
	// Write the fee hash.
	copy(buf[curPos:curPos+hashSize], record.FeeHash[:])
	curPos += hashSize
	// Write the fee tx status
	byteOrder.PutUint32(buf[curPos:curPos+4], record.FeeTxStatus)
	curPos += 4
	// Write the VSP Host db ID
	byteOrder.PutUint32(buf[curPos:curPos+4], record.VSPHostID)

	return buf
}

type VSPHost struct {
	Host   []byte
	PubKey []byte
}

// FindVSP iterates over all stored VSPs to find one with a matching host.
// Returns zero values if no VSP is found.
func FindVSP(dbtx walletdb.ReadTx, lookingForHost []byte) (*VSPHost, uint32, error) {
	var foundHost *VSPHost
	var foundHostID uint32

	bucket := dbtx.ReadBucket(vspHostBucketKey)
	c := bucket.ReadCursor()
	defer c.Close()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		host := deserializeVSPHost(v)
		if bytes.Equal(host.Host, lookingForHost) {
			foundHost = host
			foundHostID = byteOrder.Uint32(k)
			break
		}
	}

	return foundHost, foundHostID, nil
}

// SetVSPHost sets a VSPHost record into the db.
func SetVSPHost(dbtx walletdb.ReadWriteTx, id uint32, record *VSPHost) error {
	bucket := dbtx.ReadWriteBucket(vspHostBucketKey)
	serializedRecord := serializeVSPHost(record)

	k := make([]byte, 4)
	byteOrder.PutUint32(k, id)

	return bucket.Put(k, serializedRecord)
}

// GetVSPHost gets a specific ticket by the id.
func GetVSPHost(dbtx walletdb.ReadTx, id uint32) (*VSPHost, error) {
	k := make([]byte, 4)
	byteOrder.PutUint32(k, id)
	bucket := dbtx.ReadBucket(vspHostBucketKey)
	serializedHost := bucket.Get(k)
	if serializedHost == nil {
		err := errors.Errorf("no VSP host info for id %v", &id)
		return nil, errors.E(errors.NotExist, err)
	}
	host := deserializeVSPHost(serializedHost)

	return host, nil
}

// deserializeVSPHost deserializes the passed serialized vsp host information.
func deserializeVSPHost(serializedHost []byte) *VSPHost {
	curPos := 0
	VSPHost := &VSPHost{}
	// Read host length.
	hostLength := byteOrder.Uint32(serializedHost[curPos : curPos+4])
	curPos += 4
	// Read host.
	VSPHost.Host = serializedHost[curPos : curPos+int(hostLength)]
	curPos += int(hostLength)
	// Read pubkey length.
	pubkeyLength := byteOrder.Uint32(serializedHost[curPos : curPos+4])
	curPos += 4
	// Read pubkey.
	VSPHost.PubKey = serializedHost[curPos : curPos+int(pubkeyLength)]

	return VSPHost
}

// serializeVSPHost serializes a vsp host record into a byte array.
func serializeVSPHost(record *VSPHost) []byte {
	// host length + host + pubkey length + pubkey
	buf := make([]byte, 4+len(record.Host)+4+len(record.PubKey))
	curPos := 0
	// Write the host length first
	byteOrder.PutUint32(buf[curPos:curPos+4], uint32(len(record.Host)))
	curPos += 4
	// Then write the host based on the length provided.
	copy(buf[curPos:curPos+len(record.Host)], record.Host)
	curPos += len(record.Host)
	// Write the pubkey length.
	byteOrder.PutUint32(buf[curPos:curPos+4], uint32(len(record.PubKey)))
	curPos += 4
	// Then write the pubkey based on the length provided.
	copy(buf[curPos:curPos+len(record.PubKey)], record.PubKey)

	return buf
}
