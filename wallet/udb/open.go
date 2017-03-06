// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

// Open opens the database and returns various "manager" types that must be used
// to access and modify data in the database.
//
// A apperrors.E with an error code of ErrNoExist will be returned if the
// database has not been initialized.  The recorded database version must match
// exactly with DBVersion.  If the version is too low, the erorr code
// ErrNeedsUpgrade is used and the database must be upgraded.  If the version is
// greater than the one understood by this code, the error code will be
// ErrUnknownVersion.
func Open(db walletdb.DB, params *chaincfg.Params, pubPass []byte) (addrMgr *Manager, txStore *Store, stakeStore *StakeStore, err error) {
	err = walletdb.View(db, func(tx walletdb.ReadTx) error {
		// Verify the database exists and the recorded version is supported by
		// this software version.
		metadataBucket := tx.ReadBucket(unifiedDBMetadata{}.rootBucketKey())
		if metadataBucket == nil {
			const str = "database has not been initialized"
			return apperrors.E{ErrorCode: apperrors.ErrNoExist, Description: str}
		}
		dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
		if err != nil {
			return err
		}
		if dbVersion < DBVersion {
			const str = "database upgrade required"
			return apperrors.E{ErrorCode: apperrors.ErrNeedsUpgrade, Description: str}
		}
		if dbVersion > DBVersion {
			const str = "database has been upgraded to an unknown newer version"
			return apperrors.E{ErrorCode: apperrors.ErrUnknownVersion, Description: str}
		}

		addrmgrNs := tx.ReadBucket([]byte(waddrmgrBucketKey))
		stakemgrNs := tx.ReadBucket([]byte(wstakemgrBucketKey))

		addrMgr, err = loadManager(addrmgrNs, pubPass, params)
		if err != nil {
			return err
		}
		txStore = &Store{
			chainParams:    params,
			acctLookupFunc: addrMgr.AddrAccount,
		}
		stakeStore, err = openStakeStore(stakemgrNs, addrMgr, params)
		return err
	})
	switch err.(type) {
	case nil, apperrors.E:
	default:
		const str = "database view failed"
		err = apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
	}
	return
}
