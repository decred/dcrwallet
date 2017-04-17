// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"crypto/sha256"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/snacl"
	"github.com/decred/dcrwallet/walletdb"
)

// Note: all manager functions always use the latest version of the database.
// Therefore it is extremely important when adding database upgrade code to
// never call any methods of the managers and instead only use the db primitives
// with the correct version passed as parameters.

const (
	initialVersion = 1

	// lastUsedAddressIndexVersion is the second version of the database.  It
	// adds indexes for the last used address of BIP0044 accounts, removes the
	// next to use address indexes, removes all references to address pools, and
	// removes all per-address usage tracking.
	//
	// See lastUsedAddressIndexUpgrade for the code that implements the upgrade
	// path.
	lastUsedAddressIndexVersion = 2

	// votingPreferencesVersion is the third version of the database.  It
	// removes all per-ticket vote bits, replacing them with vote preferences
	// for choices on individual agendas from the current stake version.
	votingPreferencesVersion = 3

	// DBVersion is the latest version of the database that is understood by the
	// program.  Databases with recorded versions higher than this will fail to
	// open (meaning any upgrades prevent reverting to older software).
	DBVersion = votingPreferencesVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(walletdb.ReadWriteTx, []byte) error{
	initialVersion:              lastUsedAddressIndexUpgrade,
	lastUsedAddressIndexVersion: votingPreferencesUpgrade,
}

func lastUsedAddressIndexUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte) error {
	const oldVersion = 1
	const newVersion = 2

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	addrmgrBucket := tx.ReadWriteBucket(waddrmgrBucketKey)
	addressBucket := addrmgrBucket.NestedReadBucket(addrBucketName)
	usedAddrBucket := addrmgrBucket.NestedReadBucket(usedAddrBucketName)

	addressKey := func(hash160 []byte) []byte {
		sha := sha256.Sum256(hash160)
		return sha[:]
	}

	// Assert that this function is only called on version 1 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		const str = "lastUsedAddressIndexUpgrade inappropriately called"
		return apperrors.E{ErrorCode: apperrors.ErrUpgrade, Description: str, Err: nil}
	}

	masterKeyPubParams, _, err := fetchMasterKeyParams(addrmgrBucket)
	if err != nil {
		return err
	}
	var masterKeyPub snacl.SecretKey
	err = masterKeyPub.Unmarshal(masterKeyPubParams)
	if err != nil {
		const str = "failed to unmarshal master public key parameters"
		return apperrors.E{ErrorCode: apperrors.ErrData, Description: str, Err: err}
	}
	err = masterKeyPub.DeriveKey(&publicPassphrase)
	if err != nil {
		str := "invalid passphrase for master public key"
		return apperrors.E{ErrorCode: apperrors.ErrWrongPassphrase, Description: str, Err: nil}
	}

	cryptoPubKeyEnc, _, _, err := fetchCryptoKeys(addrmgrBucket)
	if err != nil {
		return err
	}
	cryptoPubKeyCT, err := masterKeyPub.Decrypt(cryptoPubKeyEnc)
	if err != nil {
		const str = "failed to decrypt public data crypto key using master key"
		return apperrors.E{ErrorCode: apperrors.ErrCrypto, Description: str, Err: err}
	}
	cryptoPubKey := &cryptoKey{snacl.CryptoKey{}}
	copy(cryptoPubKey.CryptoKey[:], cryptoPubKeyCT)

	// Determine how many BIP0044 accounts have been created.  Each of these
	// accounts must be updated.
	lastAccount, err := fetchLastAccount(addrmgrBucket)
	if err != nil {
		return err
	}

	// Perform account updates on all BIP0044 accounts created thus far.
	for account := uint32(0); account <= lastAccount; account++ {
		// Load the old account info.
		row, err := fetchAccountInfo(addrmgrBucket, account, oldVersion)
		if err != nil {
			return err
		}

		// Use the crypto public key to decrypt the account public extended key
		// and each branch key.
		serializedKeyPub, err := cryptoPubKey.Decrypt(row.pubKeyEncrypted)
		if err != nil {
			const str = "failed to decrypt extended public key"
			return apperrors.E{ErrorCode: apperrors.ErrCrypto, Description: str, Err: err}
		}
		xpub, err := hdkeychain.NewKeyFromString(string(serializedKeyPub))
		if err != nil {
			const str = "failed to create extended public key"
			return apperrors.E{ErrorCode: apperrors.ErrKeyChain, Description: str, Err: err}
		}
		xpubExtBranch, err := xpub.Child(ExternalBranch)
		if err != nil {
			const str = "failed to derive external branch extended public key"
			return apperrors.E{ErrorCode: apperrors.ErrKeyChain, Description: str, Err: err}
		}
		xpubIntBranch, err := xpub.Child(InternalBranch)
		if err != nil {
			const str = "failed to derive internal branch extended public key"
			return apperrors.E{ErrorCode: apperrors.ErrKeyChain, Description: str, Err: err}
		}

		// Determine the last used internal and external address indexes.  The
		// sentinel value ^uint32(0) means that there has been no usage at all.
		lastUsedExtIndex := ^uint32(0)
		lastUsedIntIndex := ^uint32(0)
		for child := uint32(0); child < hdkeychain.HardenedKeyStart; child++ {
			xpubChild, err := xpubExtBranch.Child(child)
			if err == hdkeychain.ErrInvalidChild {
				continue
			}
			if err != nil {
				const str = "unexpected error deriving child key"
				return apperrors.E{ErrorCode: apperrors.ErrKeyChain, Description: str, Err: err}
			}
			// This can't error because the function always passes good input to
			// dcrutil.NewAddressPubKeyHash.  Also, while it looks like a
			// mistake to hardcode the mainnet parameters here, it doesn't make
			// any difference since only the pubkey hash is used.  (Why is there
			// no exported method to just return the serialized public key?)
			addr, _ := xpubChild.Address(&chaincfg.MainNetParams)
			if addressBucket.Get(addressKey(addr.Hash160()[:])) == nil {
				// No more recorded addresses for this account.
				break
			}
			if usedAddrBucket.Get(addressKey(addr.Hash160()[:])) != nil {
				lastUsedExtIndex = child
			}
		}
		for child := uint32(0); child < hdkeychain.HardenedKeyStart; child++ {
			// Same as above but search the internal branch.
			xpubChild, err := xpubIntBranch.Child(child)
			if err == hdkeychain.ErrInvalidChild {
				continue
			}
			if err != nil {
				const str = "unexpected error deriving child key"
				return apperrors.E{ErrorCode: apperrors.ErrKeyChain, Description: str, Err: err}
			}
			addr, _ := xpubChild.Address(&chaincfg.MainNetParams)
			if addressBucket.Get(addressKey(addr.Hash160()[:])) == nil {
				break
			}
			if usedAddrBucket.Get(addressKey(addr.Hash160()[:])) != nil {
				lastUsedIntIndex = child
			}
		}

		// Convert account row values to the new serialization format that
		// replaces the next to use indexes with the last used indexes.
		row = bip0044AccountInfo(row.pubKeyEncrypted, row.privKeyEncrypted,
			0, 0, lastUsedExtIndex, lastUsedIntIndex, row.name, newVersion)
		err = putAccountInfo(addrmgrBucket, account, row)
		if err != nil {
			return err
		}

		// Remove all data saved for address pool handling.
		addrmgrMetaBucket := addrmgrBucket.NestedReadWriteBucket(metaBucketName)
		err = addrmgrMetaBucket.Delete(accountNumberToAddrPoolKey(false, account))
		if err != nil {
			return err
		}
		err = addrmgrMetaBucket.Delete(accountNumberToAddrPoolKey(true, account))
		if err != nil {
			return err
		}
	}

	// Remove the used address tracking bucket.
	err = addrmgrBucket.DeleteNestedBucket(usedAddrBucketName)
	if err != nil {
		const str = "failed to remove used address tracking bucket"
		return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func votingPreferencesUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte) error {
	const oldVersion = 2
	const newVersion = 3

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	stakemgrBucket := tx.ReadWriteBucket(wstakemgrBucketKey)
	ticketPurchasesBucket := stakemgrBucket.NestedReadWriteBucket(sstxRecordsBucketName)

	// Assert that this function is only called on version 2 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		const str = "votingPreferencesUpgrade inappropriately called"
		return apperrors.E{ErrorCode: apperrors.ErrUpgrade, Description: str, Err: nil}
	}

	// Update every ticket purchase with the new database version.  This removes
	// all per-ticket vote bits.
	ticketPurchases := make(map[chainhash.Hash]*sstxRecord)
	c := ticketPurchasesBucket.ReadCursor()
	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		var hash chainhash.Hash
		copy(hash[:], k)
		ticketPurchase, err := fetchSStxRecord(stakemgrBucket, &hash, oldVersion)
		if err != nil {
			return err
		}
		ticketPurchases[hash] = ticketPurchase
	}
	for _, ticketPurchase := range ticketPurchases {
		err := putSStxRecord(stakemgrBucket, ticketPurchase, newVersion)
		if err != nil {
			return err
		}
	}

	// Create the top level bucket for agenda preferences.
	_, err = tx.CreateTopLevelBucket(agendaPreferences.rootBucketKey())
	if err != nil {
		return err
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

// Upgrade checks whether the any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func Upgrade(db walletdb.DB, publicPassphrase []byte) error {
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
		// Execute all necessary upgrades in order.
		for _, upgrade := range upgrades[version:] {
			err := upgrade(tx, publicPassphrase)
			if err != nil {
				return err
			}
		}
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
