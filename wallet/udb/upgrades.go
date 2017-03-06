// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

const (
	initialVersion = 1

	// DBVersion is the latest version of the database that is understood by the
	// program.  Databases with recorded versions higher than this will fail to
	// open (meaning any upgrades prevent reverting to older software).
	DBVersion = initialVersion
)

// Upgrade checks whether the any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func Upgrade(db walletdb.DB) error {
	var version uint32
	err := walletdb.View(db, func(tx walletdb.ReadTx) error {
		var err error
		metadataBucket := tx.ReadBucket(unifiedDBMetadata{}.rootBucketKey())
		if metadataBucket == nil {
			// This could indicate either an unitialized db or one that hasn't
			// yet been migrated.
			const str = "metadata bucket missing"
			return apperrors.E{ErrorCode: apperrors.ErrNoExist, Description: str, Err: nil}
		}
		version, err = unifiedDBMetadata{}.getVersion(metadataBucket)
		return err
	})
	switch err.(type) {
	case nil:
	case apperrors.E:
		return err
	default:
		const str = "db view failed"
		return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
	}

	if version >= DBVersion {
		// No upgrades necessary.
		return nil
	}

	err = walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		// There are no upgrades to perform yet.
		return nil
	})
	switch err.(type) {
	case nil:
		return nil
	case apperrors.E:
		return err
	default:
		const str = "db update failed"
		return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
	}
}
