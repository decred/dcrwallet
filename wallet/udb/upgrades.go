// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"crypto/sha256"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/internal/compat"
	"github.com/decred/dcrwallet/wallet/v3/internal/snacl"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
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

	// noEncryptedSeedVersion is the fourth version of the database.  It removes
	// the encrypted seed that earlier versions may have saved in the database
	// (or more commonly, encrypted zeros on mainnet wallets).
	noEncryptedSeedVersion = 4

	// lastReturnedAddressVersion is the fifth version of the database.  It adds
	// additional indexes to each BIP0044 account row that keep track of the
	// index of the last returned child address in the internal and external
	// account branches.  This is used to prevent returning identical addresses
	// across application restarts.
	lastReturnedAddressVersion = 5

	// ticketBucketVersion is the sixth version of the database.  It adds a
	// bucket for recording the hashes of all tickets and provides additional
	// APIs to check the status of tickets and whether they are spent by a vote
	// or revocation.
	ticketBucketVersion = 6

	// slip0044CoinTypeVersion is the seventh version of the database.  It
	// introduces the possibility of the BIP0044 coin type key being either the
	// legacy coin type used by earlier versions of the wallet, or the coin type
	// assigned to Decred in SLIP0044.  The upgrade does not add or remove any
	// required keys (the upgrade is done in a backwards-compatible way) but the
	// database version is bumped to prevent older software from assuming that
	// coin type 20 exists (the upgrade is not forwards-compatible).
	slip0044CoinTypeVersion = 7

	// hasExpiryVersion is the eight version of the database. It adds the
	// hasExpiry field to the credit struct, adds fetchRawCreditHasExpiry
	// helper func and extends sstxchange type utxo checks to only make sstchange
	// with expiries set available to spend after coinbase maturity (16 blocks).
	hasExpiryVersion = 8

	// hasExpiryFixedVersion is the ninth version of the database.  It corrects
	// the previous upgrade by writing the has expiry bit to an unused bit flag
	// rather than in the stake flags and fixes various UTXO selection issues
	// caused by misinterpreting ticket outputs as spendable by regular
	// transactions.
	hasExpiryFixedVersion = 9

	// cfVersion is the tenth version of the database.  It adds a bucket to
	// store compact filters, which are required for Decred's SPV
	// implementation, and a txmgr namespace root key which tracks whether all
	// main chain compact filters were saved.  This version does not begin to
	// save compact filter headers, since the SPV implementation is expected to
	// use header commitments in a later release for validation.
	cfVersion = 10

	// lastProcessedTxsBlockVersion is the eleventh version of the database.  It
	// adds a txmgr namespace root key which records the final hash of all
	// blocks since the genesis block which have been processed for relevant
	// transactions.  This is required to distinguish between the main chain tip
	// (which is advanced during headers fetch) and the point at which a startup
	// rescan should occur.  During upgrade, the current tip block is recorded
	// as this block to avoid an additional or extra long rescan from occurring
	// from properly-synced wallets.
	lastProcessedTxsBlockVersion = 11

	// ticketCommitmentsVersion the twelfth version of the database. It adds
	// the ticketCommitment bucket to the txmgr namespace. This bucket is meant
	// to track outstanding ticket commitment outputs for the purposes of
	// correct balance calculation: it allows non-voting wallets (eg: funding
	// wallets in solo-voting setups or non-voter participants of split tickets)
	// to track their proportional locked funds. In standard (single-voter) VSP
	// setups, it also allows the wallet to discount the pool fee for correct
	// accounting of total locked funds.
	ticketCommitmentsVersion = 12

	// DBVersion is the latest version of the database that is understood by the
	// program.  Databases with recorded versions higher than this will fail to
	// open (meaning any upgrades prevent reverting to older software).
	DBVersion = ticketCommitmentsVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.  Note that there was never a
// version zero so upgrades[0] is nil.
var upgrades = [...]func(walletdb.ReadWriteTx, []byte, *chaincfg.Params) error{
	lastUsedAddressIndexVersion - 1:  lastUsedAddressIndexUpgrade,
	votingPreferencesVersion - 1:     votingPreferencesUpgrade,
	noEncryptedSeedVersion - 1:       noEncryptedSeedUpgrade,
	lastReturnedAddressVersion - 1:   lastReturnedAddressUpgrade,
	ticketBucketVersion - 1:          ticketBucketUpgrade,
	slip0044CoinTypeVersion - 1:      slip0044CoinTypeUpgrade,
	hasExpiryVersion - 1:             hasExpiryUpgrade,
	hasExpiryFixedVersion - 1:        hasExpiryFixedUpgrade,
	cfVersion - 1:                    cfUpgrade,
	lastProcessedTxsBlockVersion - 1: lastProcessedTxsBlockUpgrade,
	ticketCommitmentsVersion - 1:     ticketCommitmentsUpgrade,
}

func lastUsedAddressIndexUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
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
		return errors.E(errors.Invalid, "lastUsedAddressIndexUpgrade inappropriately called")
	}

	masterKeyPubParams, _, err := fetchMasterKeyParams(addrmgrBucket)
	if err != nil {
		return err
	}
	var masterKeyPub snacl.SecretKey
	err = masterKeyPub.Unmarshal(masterKeyPubParams)
	if err != nil {
		return errors.E(errors.IO, errors.Errorf("unmarshal master pubkey params: %v", err))
	}
	err = masterKeyPub.DeriveKey(&publicPassphrase)
	if err != nil {
		return errors.E(errors.Passphrase, "incorrect public passphrase")
	}

	cryptoPubKeyEnc, _, _, err := fetchCryptoKeys(addrmgrBucket)
	if err != nil {
		return err
	}
	cryptoPubKeyCT, err := masterKeyPub.Decrypt(cryptoPubKeyEnc)
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("decrypt public crypto key: %v", err))
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
			return errors.E(errors.Crypto, errors.Errorf("decrypt extended pubkey: %v", err))
		}
		xpub, err := hdkeychain.NewKeyFromString(string(serializedKeyPub), params)
		if err != nil {
			return errors.E(errors.IO, err)
		}
		xpubExtBranch, err := xpub.Child(ExternalBranch)
		if err != nil {
			return err
		}
		xpubIntBranch, err := xpub.Child(InternalBranch)
		if err != nil {
			return err
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
				return err
			}
			// This can't error because the function always passes good input to
			// dcrutil.NewAddressPubKeyHash.  Also, while it looks like a
			// mistake to hardcode the mainnet parameters here, it doesn't make
			// any difference since only the pubkey hash is used.  (Why is there
			// no exported method to just return the serialized public key?)
			addr, _ := compat.HD2Address(xpubChild, params)
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
				return err
			}
			addr, _ := compat.HD2Address(xpubChild, params)
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
			0, 0, lastUsedExtIndex, lastUsedIntIndex, 0, 0, row.name, newVersion)
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
		return errors.E(errors.IO, err)
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func votingPreferencesUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
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
		return errors.E(errors.Invalid, "votingPreferencesUpgrade inappropriately called")
	}

	// Update every ticket purchase with the new database version.  This removes
	// all per-ticket vote bits.
	ticketPurchases := make(map[chainhash.Hash]*sstxRecord)
	c := ticketPurchasesBucket.ReadCursor()
	defer c.Close()
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

func noEncryptedSeedUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 3
	const newVersion = 4

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	addrmgrBucket := tx.ReadWriteBucket(waddrmgrBucketKey)
	mainBucket := addrmgrBucket.NestedReadWriteBucket(mainBucketName)

	// Assert that this function is only called on version 3 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "noEncryptedSeedUpgrade inappropriately called")
	}

	// Remove encrypted seed (or encrypted zeros).
	err = mainBucket.Delete(seedName)
	if err != nil {
		return err
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func lastReturnedAddressUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 4
	const newVersion = 5

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	addrmgrBucket := tx.ReadWriteBucket(waddrmgrBucketKey)

	// Assert that this function is only called on version 4 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "accountAddressCursorsUpgrade inappropriately called")
	}

	upgradeAcct := func(account uint32) error {
		// Load the old account info.
		row, err := fetchAccountInfo(addrmgrBucket, account, oldVersion)
		if err != nil {
			return err
		}

		// Convert account row values to the new serialization format that adds
		// the last returned indexes.  Assume that the last used address is also
		// the last returned address.
		row = bip0044AccountInfo(row.pubKeyEncrypted, row.privKeyEncrypted,
			0, 0, row.lastUsedExternalIndex, row.lastUsedInternalIndex,
			row.lastUsedExternalIndex, row.lastUsedInternalIndex,
			row.name, newVersion)
		return putAccountInfo(addrmgrBucket, account, row)
	}

	// Determine how many BIP0044 accounts have been created.  Each of these
	// accounts must be updated.
	lastAccount, err := fetchLastAccount(addrmgrBucket)
	if err != nil {
		return err
	}

	// Perform account updates on all BIP0044 accounts created thus far.
	for account := uint32(0); account <= lastAccount; account++ {
		err := upgradeAcct(account)
		if err != nil {
			return err
		}
	}

	// Perform upgrade on the imported account, which is also using the BIP0044
	// row serialization.  The last used and last returned indexes are not used
	// by the imported account but the row must be upgraded regardless to avoid
	// deserialization errors due to the row value length checks.
	err = upgradeAcct(ImportedAddrAccount)
	if err != nil {
		return err
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func ticketBucketUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 5
	const newVersion = 6

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	txmgrBucket := tx.ReadWriteBucket(wtxmgrBucketKey)

	// Assert that this function is only called on version 5 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "ticketBucketUpgrade inappropriately called")
	}

	// Create the tickets bucket.
	_, err = txmgrBucket.CreateBucket(bucketTickets)
	if err != nil {
		return err
	}

	// Add an entry in the tickets bucket for every mined and unmined ticket
	// purchase transaction.  Use -1 as the selected height since this value is
	// unknown at this time and the field is not yet being used.
	ticketHashes := make(map[chainhash.Hash]struct{})
	c := txmgrBucket.NestedReadBucket(bucketTxRecords).ReadCursor()
	for k, v := c.First(); v != nil; k, v = c.Next() {
		var hash chainhash.Hash
		err := readRawTxRecordHash(k, &hash)
		if err != nil {
			c.Close()
			return err
		}
		var rec TxRecord
		err = readRawTxRecord(&hash, v, &rec)
		if err != nil {
			c.Close()
			return err
		}
		if stake.IsSStx(&rec.MsgTx) {
			ticketHashes[hash] = struct{}{}
		}
	}
	c.Close()

	c = txmgrBucket.NestedReadBucket(bucketUnmined).ReadCursor()
	for k, v := c.First(); v != nil; k, v = c.Next() {
		var hash chainhash.Hash
		err := readRawUnminedHash(k, &hash)
		if err != nil {
			c.Close()
			return err
		}
		var rec TxRecord
		err = readRawTxRecord(&hash, v, &rec)
		if err != nil {
			c.Close()
			return err
		}
		if stake.IsSStx(&rec.MsgTx) {
			ticketHashes[hash] = struct{}{}
		}
	}
	c.Close()
	for ticketHash := range ticketHashes {
		err := putTicketRecord(txmgrBucket, &ticketHash, -1)
		if err != nil {
			return err
		}
	}

	// Remove previous stakebase input from the unmined inputs bucket, if any
	// was recorded.
	stakebaseOutpoint := canonicalOutPoint(&chainhash.Hash{}, ^uint32(0))
	err = txmgrBucket.NestedReadWriteBucket(bucketUnminedInputs).Delete(stakebaseOutpoint)
	if err != nil {
		return err
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func slip0044CoinTypeUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 6
	const newVersion = 7

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())

	// Assert that this function is only called on version 6 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "slip0044CoinTypeUpgrade inappropriately called")
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func hasExpiryUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 7
	const newVersion = 8
	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	txmgrBucket := tx.ReadWriteBucket(wtxmgrBucketKey)

	// Assert that this function is only called on version 7 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "hasExpiryUpgrade inappropriately called")
	}

	// Iterate through all mined credits
	creditsBucket := txmgrBucket.NestedReadWriteBucket(bucketCredits)
	cursor := creditsBucket.ReadWriteCursor()
	creditsKV := map[string][]byte{}
	for k, v := cursor.First(); v != nil; k, v = cursor.Next() {
		hash := extractRawCreditTxHash(k)
		block, err := fetchBlockRecord(txmgrBucket, extractRawCreditHeight(k))
		if err != nil {
			cursor.Close()
			return err
		}

		_, recV := existsTxRecord(txmgrBucket, &hash, &block.Block)
		record := &TxRecord{}
		err = readRawTxRecord(&hash, recV, record)
		if err != nil {
			cursor.Close()
			return err
		}

		// Only save credits that need their hasExpiry flag updated
		if record.MsgTx.Expiry != wire.NoExpiryValue {
			vCpy := make([]byte, len(v))
			copy(vCpy, v)

			vCpy[8] |= 1 << 4
			creditsKV[string(k)] = vCpy
		}
	}
	cursor.Close()

	for k, v := range creditsKV {
		err = creditsBucket.Put([]byte(k), v)
		if err != nil {
			return err
		}
	}

	// Iterate through all unmined credits
	unminedCreditsBucket := txmgrBucket.NestedReadWriteBucket(bucketUnminedCredits)
	unminedCursor := unminedCreditsBucket.ReadWriteCursor()
	unminedCreditsKV := map[string][]byte{}
	for k, v := unminedCursor.First(); v != nil; k, v = unminedCursor.Next() {
		hash, err := chainhash.NewHash(extractRawUnminedCreditTxHash(k))
		if err != nil {
			unminedCursor.Close()
			return err
		}

		recV := existsRawUnmined(txmgrBucket, hash[:])
		record := &TxRecord{}
		err = readRawTxRecord(hash, recV, record)
		if err != nil {
			unminedCursor.Close()
			return err
		}

		// Only save credits that need their hasExpiry flag updated
		if record.MsgTx.Expiry != wire.NoExpiryValue {
			vCpy := make([]byte, len(v))
			copy(vCpy, v)

			vCpy[8] |= 1 << 4
			unminedCreditsKV[string(k)] = vCpy
		}
	}
	unminedCursor.Close()

	for k, v := range unminedCreditsKV {
		err = unminedCreditsBucket.Put([]byte(k), v)
		if err != nil {
			return err
		}
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func hasExpiryFixedUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 8
	const newVersion = 9
	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	txmgrBucket := tx.ReadWriteBucket(wtxmgrBucketKey)

	// Assert this function is only called on version 8 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "hasExpiryFixedUpgrade inappropriately called")
	}

	// Iterate through all mined credits
	creditsBucket := txmgrBucket.NestedReadWriteBucket(bucketCredits)
	cursor := creditsBucket.ReadCursor()
	creditsKV := map[string][]byte{}
	for k, v := cursor.First(); v != nil; k, v = cursor.Next() {
		hash := extractRawCreditTxHash(k)
		block, err := fetchBlockRecord(txmgrBucket, extractRawCreditHeight(k))
		if err != nil {
			cursor.Close()
			return err
		}

		_, recV := existsTxRecord(txmgrBucket, &hash, &block.Block)
		record := &TxRecord{}
		err = readRawTxRecord(&hash, recV, record)
		if err != nil {
			cursor.Close()
			return err
		}

		// Only save credits that need their hasExpiry flag updated
		if record.MsgTx.Expiry != wire.NoExpiryValue {
			vCpy := make([]byte, len(v))
			copy(vCpy, v)

			vCpy[8] &^= 1 << 4 // Clear bad hasExpiry/OP_SSTXCHANGE flag
			vCpy[8] |= 1 << 6  // Set correct hasExpiry flag
			// Reset OP_SSTXCHANGE flag if this is a ticket purchase
			// OP_SSTXCHANGE output.
			out := record.MsgTx.TxOut[extractRawCreditIndex(k)]
			if stake.IsSStx(&record.MsgTx) &&
				txscript.GetScriptClass(out.Version, out.PkScript) == txscript.StakeSubChangeTy {
				vCpy[8] |= 1 << 4
			}

			creditsKV[string(k)] = vCpy
		}
	}
	cursor.Close()

	for k, v := range creditsKV {
		err = creditsBucket.Put([]byte(k), v)
		if err != nil {
			return err
		}
	}

	// Iterate through all unmined credits
	unminedCreditsBucket := txmgrBucket.NestedReadWriteBucket(bucketUnminedCredits)
	unminedCursor := unminedCreditsBucket.ReadCursor()
	unminedCreditsKV := map[string][]byte{}
	for k, v := unminedCursor.First(); v != nil; k, v = unminedCursor.Next() {
		hash, err := chainhash.NewHash(extractRawUnminedCreditTxHash(k))
		if err != nil {
			unminedCursor.Close()
			return err
		}

		recV := existsRawUnmined(txmgrBucket, hash[:])
		record := &TxRecord{}
		err = readRawTxRecord(hash, recV, record)
		if err != nil {
			unminedCursor.Close()
			return err
		}

		// Only save credits that need their hasExpiry flag updated
		if record.MsgTx.Expiry != wire.NoExpiryValue {
			vCpy := make([]byte, len(v))
			copy(vCpy, v)

			vCpy[8] &^= 1 << 4 // Clear bad hasExpiry/OP_SSTXCHANGE flag
			vCpy[8] |= 1 << 6  // Set correct hasExpiry flag
			// Reset OP_SSTXCHANGE flag if this is a ticket purchase
			// OP_SSTXCHANGE output.
			idx, err := fetchRawUnminedCreditIndex(k)
			if err != nil {
				unminedCursor.Close()
				return err
			}
			out := record.MsgTx.TxOut[idx]
			if stake.IsSStx(&record.MsgTx) &&
				txscript.GetScriptClass(out.Version, out.PkScript) == txscript.StakeSubChangeTy {
				vCpy[8] |= 1 << 4
			}

			unminedCreditsKV[string(k)] = vCpy
		}
	}
	unminedCursor.Close()

	for k, v := range unminedCreditsKV {
		err = unminedCreditsBucket.Put([]byte(k), v)
		if err != nil {
			return err
		}
	}

	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func cfUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 9
	const newVersion = 10

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	txmgrBucket := tx.ReadWriteBucket(wtxmgrBucketKey)

	// Assert that this function is only called on version 9 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "cfUpgrade inappropriately called")
	}

	err = txmgrBucket.Put(rootHaveCFilters, []byte{0})
	if err != nil {
		return errors.E(errors.IO, err)
	}
	_, err = txmgrBucket.CreateBucket(bucketCFilters)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Record cfilter for genesis block.
	f, err := blockcf.Regular(params.GenesisBlock)
	if err != nil {
		return err
	}
	err = putRawCFilter(txmgrBucket, params.GenesisHash[:], f.NBytes())
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Record all cfilters as saved when only the genesis block is saved.
	var tipHash chainhash.Hash
	copy(tipHash[:], txmgrBucket.Get(rootTipBlock))
	if tipHash == params.GenesisHash {
		err = txmgrBucket.Put(rootHaveCFilters, []byte{1})
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func lastProcessedTxsBlockUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 10
	const newVersion = 11

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	txmgrBucket := tx.ReadWriteBucket(wtxmgrBucketKey)

	// Assert that this function is only called on version 10 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "lastProcessedTxsBlockUpgrade inappropriately called")
	}

	// Record the current tip block as the last block since genesis with
	// processed transaction.
	err = txmgrBucket.Put(rootLastTxsBlock, txmgrBucket.Get(rootTipBlock))
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

func ticketCommitmentsUpgrade(tx walletdb.ReadWriteTx, publicPassphrase []byte, params *chaincfg.Params) error {
	const oldVersion = 11
	const newVersion = 12

	metadataBucket := tx.ReadWriteBucket(unifiedDBMetadata{}.rootBucketKey())
	txmgrBucket := tx.ReadWriteBucket(wtxmgrBucketKey)
	addrmgrBucket := tx.ReadWriteBucket(waddrmgrBucketKey)

	// Assert that this function is only called on version 11 databases.
	dbVersion, err := unifiedDBMetadata{}.getVersion(metadataBucket)
	if err != nil {
		return err
	}
	if dbVersion != oldVersion {
		return errors.E(errors.Invalid, "ticketCommitmentsUpgrade inappropriately called")
	}

	_, err = txmgrBucket.CreateBucket(bucketTicketCommitments)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	_, err = txmgrBucket.CreateBucket(bucketTicketCommitmentsUsp)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Helper function to handle the details of a single (mined or unmined)
	// transaction.
	handleTxRecCommitments := func(txrec *TxRecord, unmined bool) error {
		for i, txo := range txrec.MsgTx.TxOut {
			if txrec.TxType == stake.TxTypeSStx {
				if i%2 != 1 {
					// Ignore non ticket commitment outputs.
					continue
				}

				// Decode the address stored in the commitment.
				addr, err := stake.AddrFromSStxPkScrCommitment(txo.PkScript, params)
				if err != nil {
					return errors.E(errors.IO, err)
				}

				acct, err := fetchAddrAccount(addrmgrBucket,
					normalizeAddress(addr).ScriptAddress())
				if err != nil && errors.Is(errors.NotExist, err) {
					// If this address does not have an account associated
					// with it, it means it's not owned by the wallet.
					continue
				} else if err != nil {
					return errors.E(errors.IO, err)
				}

				// Decode the amount stored in the commitment.
				amount, err := stake.AmountFromSStxPkScrCommitment(txo.PkScript)
				if err != nil {
					return errors.E(errors.IO, err)
				}

				log.Debugf("Adding ticket commitment %s:%d (%s) for account %d",
					txrec.Hash, i, amount, acct)

				// Store both the ticket commitment info and an entry in the
				// unspent index.
				k := keyTicketCommitment(txrec.Hash, uint32(i))
				v := valueTicketCommitment(amount, acct)
				err = putRawTicketCommitment(txmgrBucket, k, v)
				if err != nil {
					return errors.E(errors.IO, err)
				}

				v = valueUnspentTicketCommitment(false)
				err = putRawUnspentTicketCommitment(txmgrBucket, k, v)
				if err != nil {
					return errors.E(errors.IO, err)
				}
			} else if (txrec.TxType == stake.TxTypeSSGen) || (txrec.TxType == stake.TxTypeSSRtx) {
				// txoIdx is the original output index of the commitment, given
				// that "i" is an output index on the spender transaction.
				txoIdx := uint32(i*2 + 1)

				// ticketHashIdx is the index of the input on the spender
				// transaction (txrec) where the original ticket hash can be
				// found.
				ticketHashIdx := uint32(0)
				if txrec.TxType == stake.TxTypeSSGen {
					if i < 2 {
						// Ignore previous block hash and vote bits outputs.
						continue
					}

					// To find the original output index on votes, we skip the
					// first two outputs (previous block and vote bits) and to
					// find the original input index we skip the first input
					// (stakebase).
					txoIdx = uint32((i-2)*2 + 1)
					ticketHashIdx = 1
				}

				ticketHash := txrec.MsgTx.TxIn[ticketHashIdx].PreviousOutPoint.Hash

				k := keyTicketCommitment(ticketHash, txoIdx)
				v := existsRawTicketCommitment(txmgrBucket, k)
				if v == nil {
					// Ignore commitments we were not originally tracking.
					continue
				}

				if unmined {
					// An unmined vote/revocation only marks the ticket
					// commitment as unminedSpent.
					log.Debugf("Marking ticket commitment %s:%d unmined spent",
						ticketHash, txoIdx)

					v = valueUnspentTicketCommitment(true)
					err = putRawUnspentTicketCommitment(txmgrBucket, k, v)
				} else {
					// A mined vote/revocation removes the entry from the
					// unspent ticket commitment index.
					log.Debugf("Removing unspent ticket commitment %s:%d",
						ticketHash, txoIdx)

					err = deleteRawUnspentTicketCommitment(txmgrBucket, k)
				}
				if err != nil {
					return errors.E(errors.IO, err)
				}
			}
		}

		return nil
	}

	// Rescan the database for stake transactions, creating the commitments when
	// a ticket is found and deleting it when a vote/revocation is found.
	it := makeReadBlockIterator(txmgrBucket, 0)
	var txrec TxRecord
	for it.next() {
		for _, txh := range it.elem.transactions {
			_, v := latestTxRecord(txmgrBucket, txh[:])
			err = readRawTxRecord(&txh, v, &txrec)
			if err != nil {
				return errors.E(errors.IO, err)
			}

			err = handleTxRecCommitments(&txrec, false)
			if err != nil {
				return err
			}
		}
	}

	// Rescan unmined transactions for tickets and votes.
	err = txmgrBucket.NestedReadBucket(bucketUnmined).ForEach(func(uk, uv []byte) error {
		var txHash chainhash.Hash
		var rec TxRecord

		err := readRawUnminedHash(uk, &txHash)
		if err != nil {
			return err
		}

		err = readRawTxRecord(&txHash, uv, &rec)
		if err != nil {
			return err
		}

		return handleTxRecCommitments(&rec, true)
	})
	if err != nil {
		return errors.E(errors.IO, err)
	}

	log.Debug("Ticket commitments db upgrade done")

	// Write the new database version.
	return unifiedDBMetadata{}.putVersion(metadataBucket, newVersion)
}

// Upgrade checks whether the any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func Upgrade(db walletdb.DB, publicPassphrase []byte, params *chaincfg.Params) error {
	var version uint32
	err := walletdb.View(db, func(tx walletdb.ReadTx) error {
		var err error
		metadataBucket := tx.ReadBucket(unifiedDBMetadata{}.rootBucketKey())
		if metadataBucket == nil {
			// This could indicate either an unitialized db or one that hasn't
			// yet been migrated.
			return errors.E(errors.IO, "missing metadata bucket")
		}
		version, err = unifiedDBMetadata{}.getVersion(metadataBucket)
		return err
	})
	if err != nil {
		return err
	}

	if version >= DBVersion {
		// No upgrades necessary.
		return nil
	}

	log.Infof("Upgrading database from version %d to %d", version, DBVersion)

	return walletdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		// Execute all necessary upgrades in order.
		for _, upgrade := range upgrades[version:] {
			err := upgrade(tx, publicPassphrase, params)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
