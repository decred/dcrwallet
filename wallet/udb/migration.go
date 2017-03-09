// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

// Old package namespace bucket keys.  These are still used as of the very first
// unified database layout.
var (
	waddrmgrBucketKey  = []byte("waddrmgr")
	wtxmgrBucketKey    = []byte("wtxmgr")
	wstakemgrBucketKey = []byte("wstakemgr")
)

// NeedsMigration checks whether the database needs to be converted to the
// unified database format.
func NeedsMigration(db walletdb.DB) (bool, error) {
	var needsMigration bool
	err := walletdb.View(db, func(tx walletdb.ReadTx) error {
		needsMigration = tx.ReadBucket(unifiedDBMetadata{}.rootBucketKey()) == nil
		return nil
	})
	return needsMigration, err
}

// Migrate converts a database to the first version of the unified database
// format.  If any old upgrades are necessary, they are performed first.
// Upgrades added after the migration was implemented may still need to be
// performed.
func Migrate(db walletdb.DB, params *chaincfg.Params) error {
	return walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs := tx.ReadWriteBucket(waddrmgrBucketKey)
		txmgrNs := tx.ReadWriteBucket(wtxmgrBucketKey)
		stakemgrNs := tx.ReadWriteBucket(wstakemgrBucketKey)

		stakeStoreVersionName := []byte("stakestorever")

		// Perform any necessary upgrades for the old address manager.
		err := upgradeManager(addrmgrNs)
		if err != nil {
			return err
		}

		// Perform any necessary upgrades for the old transaction manager.
		err = upgradeTxDB(txmgrNs, params)
		if err != nil {
			return err
		}

		// The old stake manager had no upgrades, so nothing to do there.

		// Now that all the old managers are upgraded, their versions can be
		// removed and a single unified db version can be written in their
		// place.
		err = addrmgrNs.NestedReadWriteBucket(mainBucketName).Delete(mgrVersionName)
		if err != nil {
			const str = "failed to delete old address manager version"
			return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
		}
		err = txmgrNs.Delete(rootVersion)
		if err != nil {
			const str = "failed to delete old transaction store version"
			return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
		}
		err = stakemgrNs.NestedReadWriteBucket(mainBucketName).Delete(stakeStoreVersionName)
		if err != nil {
			const str = "failed to delete old stake store version"
			return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
		}
		metadataBucket, err := tx.CreateTopLevelBucket(unifiedDBMetadata{}.rootBucketKey())
		if err != nil {
			const str = "failed to create unified database metadata bucket"
			return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
		}
		return unifiedDBMetadata{}.putVersion(metadataBucket, initialVersion)
	})
}
