// Copyright (c) 2017-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

type agendaPreferencesTy struct {
}

var agendaPreferences agendaPreferencesTy

var (
	defaultAgendaPrefsBucketKey = []byte("agendaprefs")
	ticketsAgendaPrefsBucketKey = []byte("ticketsagendaprefs")
)

func (agendaPreferencesTy) key(version uint32, agendaID string) []byte {
	k := make([]byte, 4+len(agendaID))
	byteOrder.PutUint32(k, version)
	copy(k[4:], agendaID)
	return k
}

func (agendaPreferencesTy) defaultBucketKey() []byte { return defaultAgendaPrefsBucketKey }

func (agendaPreferencesTy) ticketsBucketKey() []byte { return ticketsAgendaPrefsBucketKey }

func (t agendaPreferencesTy) setDefaultPreference(tx walletdb.ReadWriteTx, version uint32, agendaID, choiceID string) error {
	b := tx.ReadWriteBucket(t.defaultBucketKey())
	return b.Put(t.key(version, agendaID), []byte(choiceID))
}

func (t agendaPreferencesTy) setTicketPreference(tx walletdb.ReadWriteTx, txHash *chainhash.Hash, version uint32, agendaID, choiceID string) error {
	b, err := tx.ReadWriteBucket(t.ticketsBucketKey()).CreateBucketIfNotExists(txHash[:])
	if err != nil {
		return err
	}
	return b.Put(t.key(version, agendaID), []byte(choiceID))
}

func (t agendaPreferencesTy) defaultPreference(dbtx walletdb.ReadTx, version uint32, agendaID string) (choiceID string) {
	b := dbtx.ReadBucket(t.defaultBucketKey())
	v := b.Get(t.key(version, agendaID))
	return string(v)
}

func (t agendaPreferencesTy) ticketPreference(dbtx walletdb.ReadTx, ticketHash *chainhash.Hash, version uint32, agendaID string) (choiceID string) {
	ticketBucket := dbtx.ReadBucket(t.ticketsBucketKey()).NestedReadBucket(ticketHash[:])
	if ticketBucket == nil {
		return ""
	}
	v := ticketBucket.Get(t.key(version, agendaID))
	return string(v)
}

// SetDefaultAgendaPreference saves a default agenda choice ID for an agenda ID
// and deployment version.
func SetDefaultAgendaPreference(dbtx walletdb.ReadWriteTx, version uint32, agendaID, choiceID string) error {
	err := agendaPreferences.setDefaultPreference(dbtx, version, agendaID, choiceID)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// SetTicketAgendaPreference saves a ticket-specific agenda choice ID for an
// agenda ID and deployment version.
func SetTicketAgendaPreference(dbtx walletdb.ReadWriteTx, txHash *chainhash.Hash, version uint32, agendaID, choiceID string) error {
	err := agendaPreferences.setTicketPreference(dbtx, txHash, version, agendaID, choiceID)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// DefaultAgendaPreference returns the saved default choice ID, if any, for an
// agenda ID and deployment version.  If no choice has been saved, this returns
// the empty string.
func DefaultAgendaPreference(dbtx walletdb.ReadTx, version uint32, agendaID string) (choiceID string) {
	return agendaPreferences.defaultPreference(dbtx, version, agendaID)
}

// TicketAgendaPreference returns a ticket's saved choice ID, if any, for an agenda
// ID and deployment version.  If no choice has been saved, this returns the empty
// string.
func TicketAgendaPreference(dbtx walletdb.ReadTx, ticketHash *chainhash.Hash, version uint32, agendaID string) (choiceID string) {
	return agendaPreferences.ticketPreference(dbtx, ticketHash, version, agendaID)
}
