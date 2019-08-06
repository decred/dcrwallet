// Copyright (c) 2017-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

type agendaPreferencesTy struct {
}

var agendaPreferences agendaPreferencesTy

var agendaPreferencesRootBucketKey = []byte("agendaprefs")

func (agendaPreferencesTy) rootBucketKey() []byte { return agendaPreferencesRootBucketKey }

func (agendaPreferencesTy) key(version uint32, agendaID string) []byte {
	k := make([]byte, 4+len(agendaID))
	byteOrder.PutUint32(k, version)
	copy(k[4:], agendaID)
	return k
}

func (t agendaPreferencesTy) setPreference(tx walletdb.ReadWriteTx, version uint32, agendaID, choiceID string) error {
	b := tx.ReadWriteBucket(t.rootBucketKey())
	return b.Put(t.key(version, agendaID), []byte(choiceID))
}

func (t agendaPreferencesTy) preference(tx walletdb.ReadTx, version uint32, agendaID string) (choiceID string) {
	b := tx.ReadBucket(t.rootBucketKey())
	v := b.Get(t.key(version, agendaID))
	return string(v)
}

// SetAgendaPreference saves an agenda choice ID for an agenda ID and deployment
// version.
func SetAgendaPreference(tx walletdb.ReadWriteTx, version uint32, agendaID, choiceID string) error {
	err := agendaPreferences.setPreference(tx, version, agendaID, choiceID)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

// AgendaPreference returns the saved choice ID, if any, for an agenda ID and
// deployment version.  If no choice has been saved, this returns the empty
// string.
func AgendaPreference(tx walletdb.ReadTx, version uint32, agendaID string) (choiceID string) {
	return agendaPreferences.preference(tx, version, agendaID)
}
