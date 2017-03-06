// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

func createBucketError(err error, bucketName string) error {
	return apperrors.E{
		ErrorCode:   apperrors.ErrDatabase,
		Description: "failed to create " + bucketName + " bucket",
		Err:         err,
	}
}

// Initialize prepares an empty database for usage by initializing all buckets
// and key/value pairs.  The database is initialized with the latest version and
// does not require any upgrades to use.
func Initialize(db walletdb.DB, params *chaincfg.Params, seed, pubPass, privPass []byte, unsafeMainNet bool) error {
	err := walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs, err := tx.CreateTopLevelBucket([]byte(waddrmgrBucketKey))
		if err != nil {
			return createBucketError(err, "address manager")
		}
		txmgrNs, err := tx.CreateTopLevelBucket([]byte(wtxmgrBucketKey))
		if err != nil {
			return createBucketError(err, "transaction store")
		}
		stakemgrNs, err := tx.CreateTopLevelBucket([]byte(wstakemgrBucketKey))
		if err != nil {
			return createBucketError(err, "stake store")
		}

		// Create the address manager, transaction store, and stake store.
		err = createAddressManager(addrmgrNs, seed, pubPass, privPass, params, &defaultScryptOptions, unsafeMainNet)
		if err != nil {
			return err
		}
		err = createStore(txmgrNs, params)
		if err != nil {
			return err
		}
		err = initializeEmpty(stakemgrNs)
		if err != nil {
			return err
		}

		// Create the metadata bucket and write the current database version to
		// it.
		metadataBucket, err := tx.CreateTopLevelBucket(unifiedDBMetadata{}.rootBucketKey())
		if err != nil {
			return createBucketError(err, "metadata")
		}
		return unifiedDBMetadata{}.putVersion(metadataBucket, DBVersion)
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

// InitializeWatchOnly prepares an empty database for watching-only wallet usage
// by initializing all buckets and key/value pairs.  The database is initialized
// with the latest version and does not require any upgrades to use.
func InitializeWatchOnly(db walletdb.DB, params *chaincfg.Params, hdPubKey string, pubPass []byte) error {
	err := walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		addrmgrNs, err := tx.CreateTopLevelBucket([]byte(waddrmgrBucketKey))
		if err != nil {
			return createBucketError(err, "address manager")
		}
		txmgrNs, err := tx.CreateTopLevelBucket([]byte(wtxmgrBucketKey))
		if err != nil {
			return createBucketError(err, "transaction store")
		}
		stakemgrNs, err := tx.CreateTopLevelBucket([]byte(wstakemgrBucketKey))
		if err != nil {
			return createBucketError(err, "stake store")
		}

		// Create the address manager, transaction store, and stake store.
		err = createWatchOnly(addrmgrNs, hdPubKey, pubPass, params, &defaultScryptOptions)
		if err != nil {
			return err
		}
		err = createStore(txmgrNs, params)
		if err != nil {
			return err
		}
		err = initializeEmpty(stakemgrNs)
		if err != nil {
			return err
		}

		// Create the metadata bucket and write the current database version to
		// it.
		metadataBucket, err := tx.CreateTopLevelBucket(unifiedDBMetadata{}.rootBucketKey())
		if err != nil {
			return createBucketError(err, "metadata")
		}
		return unifiedDBMetadata{}.putVersion(metadataBucket, DBVersion)
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
