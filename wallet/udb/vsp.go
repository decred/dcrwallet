// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"

	"decred.org/dcrwallet/v3/errors"
	"decred.org/dcrwallet/v3/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

var (
	vspBucketKey       = []byte("vsp")
	vspHostBucketKey   = []byte("vsphost")
	vspPubKeyBucketKey = []byte("vsppubkey")
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
	// Check for Host/Pubkey buckets
	pubkey, err := GetVSPPubKey(dbtx, []byte(record.Host))
	if err != nil && errors.Is(err, errors.NotExist) {
		// Pubkey entry doesn't exist for that entry, so create new one for host
		vspHostBucket := dbtx.ReadWriteBucket(vspHostBucketKey)
		v := vspHostBucket.Get(rootVSPHostIndex)
		vspHostID := byteOrder.Uint32(v)
		vspHostID += 1 // Bump current index
		err = SetVSPHost(dbtx, vspHostID, &VSPHost{
			Host: []byte(record.Host),
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

		pubkey = &VSPPubKey{
			ID:     vspHostID,
			PubKey: record.PubKey,
		}
		err = SetVSPPubKey(dbtx, []byte(record.Host), pubkey)
		if err != nil {
			return err
		}
		record.VSPHostID = vspHostID
	} else if err != nil {
		return err
	}

	// If the pubkey from the record in the request differs from the pubkey
	// in the database that is saved for the host, update the pubkey in the
	// db but keep the vsphost id intact.
	if !bytes.Equal(pubkey.PubKey, record.PubKey) {
		err = SetVSPPubKey(dbtx, []byte(record.Host), &VSPPubKey{
			ID:     pubkey.ID,
			PubKey: record.PubKey,
		})
		if err != nil {
			return err
		}
	}
	record.VSPHostID = pubkey.ID

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
	if err != nil && !errors.Is(err, errors.NotExist) {
		// Only error out if it's not a 'not exist error'
		return nil, err
	}
	if host != nil && len(host.Host) > 0 {
		// If the stored host is not empty then get the saved pubkey as well.
		ticket.Host = string(host.Host)
		pubkey, err := GetVSPPubKey(dbtx, host.Host)
		if err != nil && !errors.Is(err, errors.NotExist) {
			return nil, err
		}
		if pubkey != nil {
			// If pubkey was found then set it, otherwise skip it.
			ticket.PubKey = pubkey.PubKey
		}
	}
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
	Host []byte
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
	hostLength := byteOrder.Uint32(serializedHost[curPos : curPos+4])
	curPos += 4
	VSPHost.Host = serializedHost[curPos : curPos+int(hostLength)]

	return VSPHost
}

// serializeVSPHost serializes a vsp host record into a byte array.
func serializeVSPHost(record *VSPHost) []byte {
	// host length + host
	buf := make([]byte, 4+len(record.Host))
	curPos := 0
	// Write the host length first
	byteOrder.PutUint32(buf[curPos:curPos+4], uint32(len(record.Host)))
	curPos += 4
	// Then write the host based on the length provided.
	copy(buf[curPos:curPos+len(record.Host)], record.Host[:])

	return buf
}

type VSPPubKey struct {
	ID     uint32
	PubKey []byte
}

// SetVSPPubKey sets a VSPPubKey record into the db.
func SetVSPPubKey(dbtx walletdb.ReadWriteTx, host []byte, record *VSPPubKey) error {
	bucket := dbtx.ReadWriteBucket(vspPubKeyBucketKey)
	serializedRecord := serializeVSPPubKey(record)

	return bucket.Put(host[:], serializedRecord)
}

// GetVSPPubKey gets a specific ticket by the host.
func GetVSPPubKey(dbtx walletdb.ReadTx, host []byte) (*VSPPubKey, error) {
	bucket := dbtx.ReadBucket(vspPubKeyBucketKey)
	serializedPubKey := bucket.Get(host[:])
	if serializedPubKey == nil {
		err := errors.Errorf("no VSP pubkey info for host %v", &host)
		return nil, errors.E(errors.NotExist, err)
	}
	pubkey := deserializeVSPPubKey(serializedPubKey)
	return pubkey, nil
}

// deserializeVSPPubKey deserializes the passed serialized vsp host information.
func deserializeVSPPubKey(serializedPubKey []byte) *VSPPubKey {
	curPos := 0
	VSPPubKey := &VSPPubKey{}
	VSPPubKey.ID = byteOrder.Uint32(serializedPubKey[curPos : curPos+4])
	curPos += 4
	pubkeyLength := byteOrder.Uint32(serializedPubKey[curPos : curPos+4])
	curPos += 4
	VSPPubKey.PubKey = serializedPubKey[curPos : curPos+int(pubkeyLength)]

	return VSPPubKey
}

// serializeVSPPubKey serializes a vsp host record into a byte array.
func serializeVSPPubKey(record *VSPPubKey) []byte {
	// id + pubkey length + pubkey
	buf := make([]byte, 4+4+len(record.PubKey))
	curPos := 0
	// Write the id first
	byteOrder.PutUint32(buf[curPos:curPos+4], record.ID)
	curPos += 4
	// Write the pubkey length
	byteOrder.PutUint32(buf[curPos:curPos+4], uint32(len(record.PubKey)))
	curPos += 4
	// Then write the pubkey based on the length provided.
	copy(buf[curPos:curPos+len(record.PubKey)], record.PubKey[:])

	return buf
}
