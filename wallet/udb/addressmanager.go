// Copyright (c) 2014-2016 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"crypto/rand"
	"crypto/sha512"
	"fmt"
	"sync"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/chaincfg/v2/chainec"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/internal/zero"
	"github.com/decred/dcrwallet/wallet/v3/internal/compat"
	"github.com/decred/dcrwallet/wallet/v3/internal/snacl"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
	"golang.org/x/crypto/ripemd160"
)

const (
	// MaxAccountNum is the maximum allowed account number.  This value was
	// chosen because accounts are hardened children and therefore must
	// not exceed the hardened child range of extended keys and it provides
	// a reserved account at the top of the range for supporting imported
	// addresses.
	MaxAccountNum = hdkeychain.HardenedKeyStart - 2 // 2^31 - 2

	// MaxAddressesPerAccount is the maximum allowed number of addresses
	// per account number.  This value is based on the limitation of
	// the underlying hierarchical deterministic key derivation.
	MaxAddressesPerAccount = hdkeychain.HardenedKeyStart - 1

	// ImportedAddrAccount is the account number to use for all imported
	// addresses.  This is useful since normal accounts are derived from the
	// root hierarchical deterministic key and imported addresses do not
	// fit into that model.
	ImportedAddrAccount = MaxAccountNum + 1 // 2^31 - 1

	// ImportedAddrAccountName is the name of the imported account.
	ImportedAddrAccountName = "imported"

	// DefaultAccountNum is the number of the default account.
	DefaultAccountNum = 0

	// defaultAccountName is the initial name of the default account.  Note
	// that the default account may be renamed and is not a reserved name,
	// so the default account might not be named "default" and non-default
	// accounts may be named "default".
	//
	// Account numbers never change, so the DefaultAccountNum should be used
	// to refer to (and only to) the default account.
	defaultAccountName = "default"

	// The hierarchy described by BIP0043 is:
	//  m/<purpose>'/*
	// This is further extended by BIP0044 to:
	//  m/44'/<coin type>'/<account>'/<branch>/<address index>
	//
	// The branch is 0 for external addresses and 1 for internal addresses.

	// maxCoinType is the maximum allowed coin type used when structuring
	// the BIP0044 multi-account hierarchy.  This value is based on the
	// limitation of the underlying hierarchical deterministic key
	// derivation.
	maxCoinType = hdkeychain.HardenedKeyStart - 1

	// ExternalBranch is the child number to use when performing BIP0044
	// style hierarchical deterministic key derivation for the external
	// branch.
	ExternalBranch uint32 = 0

	// InternalBranch is the child number to use when performing BIP0044
	// style hierarchical deterministic key derivation for the internal
	// branch.
	InternalBranch uint32 = 1

	// saltSize is the number of bytes of the salt used when hashing
	// private passphrases.
	saltSize = 32
)

// isReservedAccountName returns true if the account name is reserved.  Reserved
// accounts may never be renamed, and other accounts may not be renamed to a
// reserved name.
func isReservedAccountName(name string) bool {
	return name == ImportedAddrAccountName
}

// isReservedAccountNum returns true if the account number is reserved.
// Reserved accounts may not be renamed.
func isReservedAccountNum(acct uint32) bool {
	return acct == ImportedAddrAccount
}

// normalizeAddress normalizes addresses for usage by the address manager.  In
// particular, it converts all pubkeys to pubkey hash addresses so they are
// interchangeable by callers.
func normalizeAddress(addr dcrutil.Address) dcrutil.Address {
	switch addr := addr.(type) {
	case *dcrutil.AddressSecpPubKey:
		return addr.AddressPubKeyHash()
	default:
		return addr
	}
}

// ScryptOptions is used to hold the scrypt parameters needed when deriving new
// passphrase keys.
type ScryptOptions struct {
	N, R, P int
}

// defaultScryptOptions is the default options used with scrypt.
var defaultScryptOptions = ScryptOptions{
	N: 262144, // 2^18
	R: 8,
	P: 1,
}

// accountInfo houses the current state of the internal and external branches
// of an account along with the extended keys needed to derive new keys.  It
// also handles locking by keeping an encrypted version of the serialized
// private extended key so the unencrypted versions can be cleared from memory
// when the address manager is locked.
type accountInfo struct {
	acctName string

	// The account key is used to derive the branches which in turn derive
	// the internal and external addresses.
	// The accountKeyPriv will be nil when the address manager is locked.
	acctKeyEncrypted []byte
	acctKeyPriv      *hdkeychain.ExtendedKey
	acctKeyPub       *hdkeychain.ExtendedKey
}

// AccountProperties contains properties associated with each account, such as
// the account name, number, and the nubmer of derived and imported keys.  If no
// address usage has been recorded on any of the external or internal branches,
// the child index is ^uint32(0).
type AccountProperties struct {
	AccountNumber             uint32
	AccountName               string
	LastUsedExternalIndex     uint32
	LastUsedInternalIndex     uint32
	LastReturnedExternalIndex uint32
	LastReturnedInternalIndex uint32
	ImportedKeyCount          uint32
}

// defaultNewSecretKey returns a new secret key.  See newSecretKey.
func defaultNewSecretKey(passphrase *[]byte, config *ScryptOptions) (*snacl.SecretKey, error) {
	return snacl.NewSecretKey(passphrase, config.N, config.R, config.P)
}

// newSecretKey is used as a way to replace the new secret key generation
// function used so tests can provide a version that fails for testing error
// paths.
var newSecretKey = defaultNewSecretKey

// EncryptorDecryptor provides an abstraction on top of snacl.CryptoKey so that
// our tests can use dependency injection to force the behaviour they need.
type EncryptorDecryptor interface {
	Encrypt(in []byte) ([]byte, error)
	Decrypt(in []byte) ([]byte, error)
	Bytes() []byte
	CopyBytes([]byte)
	Zero()
}

// cryptoKey extends snacl.CryptoKey to implement EncryptorDecryptor.
type cryptoKey struct {
	snacl.CryptoKey
}

// Bytes returns a copy of this crypto key's byte slice.
func (ck *cryptoKey) Bytes() []byte {
	return ck.CryptoKey[:]
}

// CopyBytes copies the bytes from the given slice into this CryptoKey.
func (ck *cryptoKey) CopyBytes(from []byte) {
	copy(ck.CryptoKey[:], from)
}

// defaultNewCryptoKey returns a new CryptoKey.  See newCryptoKey.
func defaultNewCryptoKey() (EncryptorDecryptor, error) {
	key, err := snacl.GenerateCryptoKey()
	if err != nil {
		return nil, err
	}
	return &cryptoKey{*key}, nil
}

// CryptoKeyType is used to differentiate between different kinds of
// crypto keys.
type CryptoKeyType byte

// Crypto key types.
const (
	// CKTPrivate specifies the key that is used for encryption of private
	// key material such as derived extended private keys and imported
	// private keys.
	CKTPrivate CryptoKeyType = iota

	// CKTScript specifies the key that is used for encryption of scripts.
	CKTScript

	// CKTPublic specifies the key that is used for encryption of public
	// key material such as dervied extended public keys and imported public
	// keys.
	CKTPublic
)

// newCryptoKey is used as a way to replace the new crypto key generation
// function used so tests can provide a version that fails for testing error
// paths.
var newCryptoKey = defaultNewCryptoKey

// Manager represents a concurrency safe crypto currency address manager and
// key store.
type Manager struct {
	mtx sync.RWMutex

	// returnedSecretsMu is a read/write mutex that is held for reads when
	// secrets (private keys and redeem scripts) are being used and held for
	// writes when secrets must be zeroed while locking the manager.  It manages
	// every private key in the returnedPrivKeys and returnedScripts maps.  When
	// a private key or redeem script is returned to a caller, an entry is added
	// to the corresponding map (if the key has not yet already been added) and
	// a reader lock is grabbed for the duration of the private key or script
	// usage.  When secrets must be cleared on lock, the writer lock of the
	// mutex is grabbed and each returned secret is cleared.  This means that
	// locking the manager blocks on all private key and redeem script usage,
	// and that callers must be sure to unlock the mutex when they are finished
	// using the secret. We rely on the implementation of sync.RWMutex to
	// prevent new readers when a writer is waiting on the lock to prevent
	// access to secrets when another caller has locked the wallet.
	returnedSecretsMu sync.RWMutex
	returnedPrivKeys  map[[ripemd160.Size]byte]chainec.PrivateKey
	returnedScripts   map[[ripemd160.Size]byte][]byte

	chainParams  *chaincfg.Params
	watchingOnly bool
	locked       bool
	closed       bool

	// acctInfo houses information about accounts including what is needed
	// to generate deterministic chained keys for each created account.
	acctInfo map[uint32]*accountInfo

	// masterKeyPub is the secret key used to secure the cryptoKeyPub key
	// and masterKeyPriv is the secret key used to secure the cryptoKeyPriv
	// key.  This approach is used because it makes changing the passwords
	// much simpler as it then becomes just changing these keys.  It also
	// provides future flexibility.
	//
	// NOTE: This is not the same thing as BIP0032 master node extended
	// key.
	//
	// The underlying master private key will be zeroed when the address
	// manager is locked.
	masterKeyPub  *snacl.SecretKey
	masterKeyPriv *snacl.SecretKey

	// cryptoKeyPub is the key used to encrypt public extended keys and
	// addresses.
	cryptoKeyPub EncryptorDecryptor

	// cryptoKeyPriv is the key used to encrypt private data such as the
	// master hierarchical deterministic extended key.
	//
	// This key will be zeroed when the address manager is locked.
	cryptoKeyPrivEncrypted []byte
	cryptoKeyPriv          EncryptorDecryptor

	// cryptoKeyScript is the key used to encrypt script data.
	//
	// This key will be zeroed when the address manager is locked.
	cryptoKeyScriptEncrypted []byte
	cryptoKeyScript          EncryptorDecryptor

	// privPassphraseSalt and hashedPrivPassphrase allow for the secure
	// detection of a correct passphrase on manager unlock when the
	// manager is already unlocked.  The hash is zeroed each lock.
	privPassphraseSalt   [saltSize]byte
	hashedPrivPassphrase [sha512.Size]byte
}

// lock performs a best try effort to remove and zero all secret keys associated
// with the address manager.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) lock() {
	// Clear all of the account private keys.
	for _, acctInfo := range m.acctInfo {
		if acctInfo.acctKeyPriv != nil {
			acctInfo.acctKeyPriv.Zero()
		}
		acctInfo.acctKeyPriv = nil
	}

	// Remove clear text private keys and scripts from all address entries.
	m.returnedSecretsMu.Lock()
	for _, privKey := range m.returnedPrivKeys {
		zero.BigInt(privKey.GetD())
	}
	for _, script := range m.returnedScripts {
		zero.Bytes(script)
	}
	m.returnedPrivKeys = nil
	m.returnedScripts = nil
	m.returnedSecretsMu.Unlock()

	// Remove clear text private master and crypto keys from memory.
	m.cryptoKeyScript.Zero()
	m.cryptoKeyPriv.Zero()
	m.masterKeyPriv.Zero()

	// Zero the hashed passphrase.
	zero.Bytea64(&m.hashedPrivPassphrase)

	// NOTE: m.cryptoKeyPub is intentionally not cleared here as the address
	// manager needs to be able to continue to read and decrypt public data
	// which uses a separate derived key from the database even when it is
	// locked.

	m.locked = true
}

// zeroSensitivePublicData performs a best try effort to remove and zero all
// sensitive public data associated with the address manager such as
// hierarchical deterministic extended public keys and the crypto public keys.
func (m *Manager) zeroSensitivePublicData() {
	// Clear all of the account private keys.
	for _, acctInfo := range m.acctInfo {
		acctInfo.acctKeyPub.Zero()
		acctInfo.acctKeyPub = nil
	}

	// Remove clear text public master and crypto keys from memory.
	m.cryptoKeyPub.Zero()
	m.masterKeyPub.Zero()
}

// WatchingOnly returns whether or not the wallet is in watching only mode.
func (m *Manager) WatchingOnly() bool {
	return m.watchingOnly
}

// Close cleanly shuts down the manager.  It makes a best try effort to remove
// and zero all private key and sensitive public key material associated with
// the address manager from memory.
func (m *Manager) Close() error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Attempt to clear private key material from memory.
	if !m.watchingOnly && !m.locked {
		m.lock()
	}

	// Attempt to clear sensitive public key material from memory too.
	m.zeroSensitivePublicData()

	m.closed = true
	return nil
}

// keyToManaged returns a new managed address for the provided derived key and
// its derivation path which consists of the account, branch, and index.
//
// The passed derivedKey is zeroed after the new address is created.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) keyToManaged(derivedKey *hdkeychain.ExtendedKey, account, branch, index uint32) (ManagedAddress, error) {
	// Create a new managed address based on the public or private key
	// depending on whether the passed key is private.  Also, zero the
	// key after creating the managed address from it.
	ma, err := newManagedAddressFromExtKey(m, account, derivedKey)
	defer derivedKey.Zero()
	if err != nil {
		return nil, err
	}
	if branch == InternalBranch {
		ma.internal = true
	}

	ma.index = index

	return ma, nil
}

// deriveKey returns either a public or private derived extended key based on
// the private flag for the given an account info, branch, and index.
func deriveKey(acctInfo *accountInfo, branch, index uint32, private bool) (*hdkeychain.ExtendedKey, error) {
	// Choose the public or private extended key based on whether or not
	// the private flag was specified.  This, in turn, allows for public or
	// private child derivation.
	acctKey := acctInfo.acctKeyPub
	if private {
		acctKey = acctInfo.acctKeyPriv
	}

	// Derive and return the key.
	branchKey, err := acctKey.Child(branch)
	if err != nil {
		return nil, err
	}
	addressKey, err := branchKey.Child(index)
	branchKey.Zero() // Zero branch key after it's used.
	return addressKey, err
}

// GetMasterPubkey gives the encoded string version of the HD master public key
// for the default account of the wallet.
func (m *Manager) GetMasterPubkey(ns walletdb.ReadBucket, account uint32) (string, error) {
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// The account is either invalid or just wasn't cached, so attempt to
	// load the information from the database.
	row, err := fetchAccountInfo(ns, account, DBVersion)
	if err != nil {
		return "", err
	}

	// Use the crypto public key to decrypt the account public extended key.
	serializedKeyPub, err := m.cryptoKeyPub.Decrypt(row.pubKeyEncrypted)
	if err != nil {
		return "", errors.E(errors.IO, err)
	}

	return string(serializedKeyPub), nil
}

// loadAccountInfo attempts to load and cache information about the given
// account from the database.   This includes what is necessary to derive new
// keys for it and track the state of the internal and external branches.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) loadAccountInfo(ns walletdb.ReadBucket, account uint32) (*accountInfo, error) {
	// Return the account info from cache if it's available.
	if acctInfo, ok := m.acctInfo[account]; ok {
		return acctInfo, nil
	}

	// The account is either invalid or just wasn't cached, so attempt to
	// load the information from the database.
	row, err := fetchAccountInfo(ns, account, DBVersion)
	if err != nil {
		if errors.Is(errors.NotExist, err) {
			return nil, err
		}
		return nil, errors.E(errors.NotExist, errors.Errorf("no account %d", account))
	}

	// Use the crypto public key to decrypt the account public extended key.
	serializedKeyPub, err := m.cryptoKeyPub.Decrypt(row.pubKeyEncrypted)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("decrypt account %d pubkey: %v", account, err))
	}
	acctKeyPub, err := hdkeychain.NewKeyFromString(string(serializedKeyPub), m.chainParams)
	if err != nil {
		return nil, errors.E(errors.IO, err)
	}

	// Create the new account info with the known information.  The rest
	// of the fields are filled out below.
	acctInfo := &accountInfo{
		acctName:         row.name,
		acctKeyEncrypted: row.privKeyEncrypted,
		acctKeyPub:       acctKeyPub,
	}

	if !m.locked {
		// Use the crypto private key to decrypt the account private
		// extended keys.
		decrypted, err := m.cryptoKeyPriv.Decrypt(acctInfo.acctKeyEncrypted)
		if err != nil {
			return nil, errors.E(errors.Crypto, errors.Errorf("decrypt account %d privkey: %v", account, err))
		}

		acctKeyPriv, err := hdkeychain.NewKeyFromString(string(decrypted), m.chainParams)
		if err != nil {
			return nil, errors.E(errors.IO, err)
		}
		acctInfo.acctKeyPriv = acctKeyPriv
	}

	// Add it to the cache and return it when everything is successful.
	m.acctInfo[account] = acctInfo
	return acctInfo, nil
}

// AccountProperties returns properties associated with the account, such as the
// account number, name, and the number of derived and imported keys.
//
// TODO: Instead of opening a second read transaction after making a change, and
// then fetching the account properties with a new read tx, this can be made
// more performant by simply returning the new account properties during the
// change.
func (m *Manager) AccountProperties(ns walletdb.ReadBucket, account uint32) (*AccountProperties, error) {
	defer m.mtx.RUnlock()
	m.mtx.RLock()

	props := &AccountProperties{AccountNumber: account}

	// Until keys can be imported into any account, special handling is
	// required for the imported account.
	//
	// loadAccountInfo errors when using it on the imported account since
	// the accountInfo struct is filled with a BIP0044 account's extended
	// keys, and the imported accounts has none.
	//
	// Since only the imported account allows imports currently, the number
	// of imported keys for any other account is zero, and since the
	// imported account cannot contain non-imported keys, the external and
	// internal key counts for it are zero.
	if account != ImportedAddrAccount {
		acctInfo, err := m.loadAccountInfo(ns, account)
		if err != nil {
			return nil, err
		}
		props.AccountName = acctInfo.acctName
		row, err := fetchAccountInfo(ns, account, DBVersion)
		if err != nil {
			return nil, errors.E(errors.IO, err)
		}
		props.LastUsedExternalIndex = row.lastUsedExternalIndex
		props.LastUsedInternalIndex = row.lastUsedInternalIndex
		props.LastReturnedExternalIndex = row.lastReturnedExternalIndex
		props.LastReturnedInternalIndex = row.lastReturnedInternalIndex
	} else {
		props.AccountName = ImportedAddrAccountName // reserved, nonchangable

		// Could be more efficient if this was tracked by the db.
		var importedKeyCount uint32
		count := func(interface{}) error {
			importedKeyCount++
			return nil
		}
		err := forEachAccountAddress(ns, ImportedAddrAccount, count)
		if err != nil {
			return nil, err
		}
		props.ImportedKeyCount = importedKeyCount
	}

	return props, nil
}

// AccountExtendedPubKey returns the extended public key for an account, which
// can then be used to derive BIP0044 branch keys.
func (m *Manager) AccountExtendedPubKey(dbtx walletdb.ReadTx, account uint32) (*hdkeychain.ExtendedKey, error) {
	ns := dbtx.ReadBucket(waddrmgrBucketKey)
	if account == ImportedAddrAccount {
		return nil, errors.E(errors.Invalid, "imported account has no extended pubkey")
	}
	m.mtx.Lock()
	acctInfo, err := m.loadAccountInfo(ns, account)
	m.mtx.Unlock()
	if err != nil {
		return nil, err
	}
	return acctInfo.acctKeyPub, nil
}

// AccountExtendedPrivKey returns the extended private key for the given
// account. The account must already exist and the address manager must be
// unlocked for this operation to complete.
func (m *Manager) AccountExtendedPrivKey(dbtx walletdb.ReadTx, account uint32) (*hdkeychain.ExtendedKey, error) {
	if account == ImportedAddrAccount {
		return nil, errors.E(errors.Invalid, "imported account has no extended pubkey")
	}

	ns := dbtx.ReadBucket(waddrmgrBucketKey)
	var (
		acctInfo *accountInfo
		err      error
	)

	m.mtx.Lock()
	if m.locked {
		err = errors.E(errors.Locked, "locked address manager cannot fetch extended privkey")
	} else {
		acctInfo, err = m.loadAccountInfo(ns, account)
	}
	m.mtx.Unlock()
	if err != nil {
		return nil, err
	}
	return acctInfo.acctKeyPriv, nil
}

// AccountBranchExtendedPubKey returns the extended public key of an account's
// branch, which then can be used to derive addresses belonging to the account.
func (m *Manager) AccountBranchExtendedPubKey(dbtx walletdb.ReadTx, account, branch uint32) (*hdkeychain.ExtendedKey, error) {
	acctXpub, err := m.AccountExtendedPubKey(dbtx, account)
	if err != nil {
		return nil, err
	}
	return acctXpub.Child(branch)
}

// CoinTypePrivKey returns the coin type private key at the BIP0044 path
// m/44'/<coin type>' (coin type child indexes differ by the network).  The key
// and all derived private keys should be cleared by the caller when finished.
// This method requires the wallet to be unlocked.
func (m *Manager) CoinTypePrivKey(dbtx walletdb.ReadTx) (*hdkeychain.ExtendedKey, error) {
	defer m.mtx.RUnlock()
	m.mtx.RLock()

	if m.locked {
		return nil, errors.E(errors.Locked)
	}
	if m.watchingOnly {
		return nil, errors.E(errors.WatchingOnly)
	}

	ns := dbtx.ReadBucket(waddrmgrBucketKey)

	_, coinTypePrivEnc, err := fetchCoinTypeKeys(ns)
	if err != nil {
		return nil, err
	}
	serializedKeyPriv, err := m.cryptoKeyPriv.Decrypt(coinTypePrivEnc)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("decrypt cointype privkey: %v", err))
	}
	coinTypeKeyPriv, err := hdkeychain.NewKeyFromString(string(serializedKeyPriv), m.chainParams)
	zero.Bytes(serializedKeyPriv)
	if err != nil {
		return nil, errors.E(errors.IO, err)
	}
	return coinTypeKeyPriv, nil
}

// CoinType returns the BIP0044 coin type currently in use.  Early versions of
// the wallet used coin types that conflicted with other coins, preventing use
// of the same seed in multicurrency wallets.  New (not restored) wallets are
// now created using the coin types assigned to Decred in SLIP0044:
//
//  https://github.com/satoshilabs/slips/blob/master/slip-0044.md
//
// The address manager should be upgraded to the SLIP0044 coin type if it is
// currently using the legacy coin type and there are no used accounts or
// addresses.  This procedure must be performed for seed restored wallets which
// save both coin types and, for backwards compatibility reasons, default to the
// legacy coin type.  Both coin types for a network may be queried using the
// CoinTypes func and upgrades are performed using the UpgradeToSLIP0044CoinType
// method.
//
// Watching-only wallets that are created using an account xpub do not save the
// coin type keys and this method will return an error with code
// WatchingOnly on these wallets.
func (m *Manager) CoinType(dbtx walletdb.ReadTx) (uint32, error) {
	ns := dbtx.ReadBucket(waddrmgrBucketKey)
	mainBucket := ns.NestedReadBucket(mainBucketName)

	legacyCoinType, slip0044CoinType := CoinTypes(m.chainParams)

	if mainBucket.Get(coinTypeLegacyPubKeyName) != nil {
		return legacyCoinType, nil
	}
	if mainBucket.Get(coinTypeSLIP0044PubKeyName) != nil {
		return slip0044CoinType, nil
	}

	return 0, errors.E(errors.WatchingOnly, "watching wallets do not record coin type keys")
}

// UpgradeToSLIP0044CoinType upgrades a wallet from using the legacy coin type
// to the coin type registered to Decred as per SLIP0044.  On mainnet, this
// upgrades the coin type from 20 to 42.  On testnet and simnet, the coin type
// is upgraded to 1.  This upgrade is only possible if the SLIP0044 coin type
// private key is saved and there is no address use for keys derived by the
// legacy coin type.
func (m *Manager) UpgradeToSLIP0044CoinType(dbtx walletdb.ReadWriteTx) error {
	coinType, err := m.CoinType(dbtx)
	if err != nil {
		return err
	}
	legacyCoinType, _ := CoinTypes(m.chainParams)
	if coinType != legacyCoinType {
		return errors.E(errors.Invalid, "SLIP0044 coin type upgrade only possible on legacy coin type wallets")
	}

	ns := dbtx.ReadWriteBucket(waddrmgrBucketKey)
	mainBucket := ns.NestedReadWriteBucket(mainBucketName)

	coinTypeSLIP0044PubKeyEnc := mainBucket.Get(coinTypeSLIP0044PubKeyName)
	coinTypeSLIP0044PrivKeyEnc := mainBucket.Get(coinTypeSLIP0044PrivKeyName)
	if coinTypeSLIP0044PubKeyEnc == nil || coinTypeSLIP0044PrivKeyEnc == nil {
		return errors.E(errors.Invalid, "missing keys for SLIP0044 coin type upgrade")
	}

	// Refuse to upgrade the coin type if any accounts or addresses have been
	// used or derived.
	lastAcct, err := m.LastAccount(ns)
	if err != nil {
		return err
	}
	acctProps, err := m.AccountProperties(ns, lastAcct)
	if err != nil {
		return err
	}
	if lastAcct != 0 || acctProps.LastReturnedExternalIndex != ^uint32(0) ||
		acctProps.LastReturnedInternalIndex != ^uint32(0) {
		return errors.E(errors.Invalid, "wallets with returned addresses may not be upgraded to SLIP0044 coin type")
	}

	// Delete the legacy coin type keys.  With these missing, the SLIP0044 coin
	// type keys (which have already been checked to exist) will be used
	// instead.
	err = mainBucket.Delete(coinTypeLegacyPubKeyName)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	err = mainBucket.Delete(coinTypeLegacyPrivKeyName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Rewrite the account 0 row using the SLIP0044 coin type key derivations.
	serializedRow := mainBucket.Get(slip0044Account0RowName)
	if serializedRow == nil {
		return errors.E(errors.IO, "missing SLIP0044 coin type account row")
	}
	accountID := uint32ToBytes(0)
	row, err := deserializeAccountRow(accountID, serializedRow)
	if err != nil {
		return err
	}
	if row.acctType != actBIP0044 {
		return errors.E(errors.IO, errors.Errorf("invalid SLIP0044 account 0 row type %d", row.acctType))
	}
	bip0044Row, err := deserializeBIP0044AccountRow(accountID, row, initialVersion)
	if err != nil {
		return err
	}
	// Keep previous name of account 0
	bip0044Row.name = acctProps.AccountName
	bip0044Row.rawData = serializeBIP0044AccountRow(bip0044Row, DBVersion)
	err = putAccountRow(ns, 0, &bip0044Row.dbAccountRow)
	if err != nil {
		return err
	}
	err = mainBucket.Delete(slip0044Account0RowName)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Acquire the manager mutex for the remainder of the call so that caches
	// can be updated.
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// Check if the account info cache exists and must be updated for the
	// SLIP044 coin type derivations.
	acctInfo, ok := m.acctInfo[0]
	if !ok {
		return nil
	}

	// Decrypt the SLIP0044 coin type account xpub so the in memory account
	// information can be updated.
	acctExtPubKeyStr, err := m.cryptoKeyPub.Decrypt(bip0044Row.pubKeyEncrypted)
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("decrypt SLIP0044 account 0 xpub: %v", err))
	}
	acctExtPubKey, err := hdkeychain.NewKeyFromString(string(acctExtPubKeyStr), m.chainParams)
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// When unlocked, decrypt the SLIP0044 coin type account xpriv as well.
	var acctExtPrivKey *hdkeychain.ExtendedKey
	if !m.locked {
		acctExtPrivKeyStr, err := m.cryptoKeyPriv.Decrypt(bip0044Row.privKeyEncrypted)
		if err != nil {
			return errors.E(errors.Crypto, errors.Errorf("decrypt SLIP0044 account 0 xpriv: %v", err))
		}
		acctExtPrivKey, err = hdkeychain.NewKeyFromString(string(acctExtPrivKeyStr), m.chainParams)
		if err != nil {
			return errors.E(errors.IO, err)
		}
	}

	acctInfo.acctKeyEncrypted = bip0044Row.privKeyEncrypted
	acctInfo.acctKeyPriv = acctExtPrivKey
	acctInfo.acctKeyPub = acctExtPubKey

	return nil
}

// deriveKeyFromPath returns either a public or private derived extended key
// based on the private flag for the given an account, branch, and index.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) deriveKeyFromPath(ns walletdb.ReadBucket, account, branch, index uint32, private bool) (*hdkeychain.ExtendedKey, error) {
	// Look up the account key information.
	acctInfo, err := m.loadAccountInfo(ns, account)
	if err != nil {
		return nil, err
	}

	return deriveKey(acctInfo, branch, index, private)
}

// chainAddressRowToManaged returns a new managed address based on chained
// address data loaded from the database.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) chainAddressRowToManaged(ns walletdb.ReadBucket, row *dbChainAddressRow) (ManagedAddress, error) {
	addressKey, err := m.deriveKeyFromPath(ns, row.account, row.branch,
		row.index, !m.locked)
	if err != nil {
		return nil, err
	}

	return m.keyToManaged(addressKey, row.account, row.branch, row.index)
}

// importedAddressRowToManaged returns a new managed address based on imported
// address data loaded from the database.
func (m *Manager) importedAddressRowToManaged(row *dbImportedAddressRow) (ManagedAddress, error) {
	// Use the crypto public key to decrypt the imported public key.
	pubBytes, err := m.cryptoKeyPub.Decrypt(row.encryptedPubKey)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("decrypt imported pubkey: %v", err))
	}

	pubKey, err := chainec.Secp256k1.ParsePubKey(pubBytes)
	if err != nil {
		return nil, errors.E(errors.IO, err)
	}

	compressed := len(pubBytes) == chainec.Secp256k1.PubKeyBytesLenCompressed()
	ma, err := newManagedAddressWithoutPrivKey(m, row.account, pubKey,
		compressed)
	if err != nil {
		return nil, err
	}
	ma.imported = true

	return ma, nil
}

// scriptAddressRowToManaged returns a new managed address based on script
// address data loaded from the database.
func (m *Manager) scriptAddressRowToManaged(row *dbScriptAddressRow) (ManagedAddress, error) {
	// Use the crypto public key to decrypt the imported script hash.
	scriptHash, err := m.cryptoKeyPub.Decrypt(row.encryptedHash)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("decrypt imported P2SH address: %v", err))
	}

	return newScriptAddress(m, row.account, scriptHash)
}

// rowInterfaceToManaged returns a new managed address based on the given
// address data loaded from the database.  It will automatically select the
// appropriate type.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) rowInterfaceToManaged(ns walletdb.ReadBucket, rowInterface interface{}) (ManagedAddress, error) {
	switch row := rowInterface.(type) {
	case *dbChainAddressRow:
		return m.chainAddressRowToManaged(ns, row)

	case *dbImportedAddressRow:
		return m.importedAddressRowToManaged(row)

	case *dbScriptAddressRow:
		return m.scriptAddressRowToManaged(row)
	}

	return nil, errors.E(errors.Invalid, errors.Errorf("address type %T", rowInterface))
}

// loadAddress attempts to load the passed address from the database.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) loadAddress(ns walletdb.ReadBucket, address dcrutil.Address) (ManagedAddress, error) {
	// Attempt to load the raw address information from the database.
	rowInterface, err := fetchAddress(ns, address.ScriptAddress())
	if err != nil {
		if errors.Is(errors.NotExist, err) {
			return nil, errors.E(errors.NotExist, errors.Errorf("no address %s", address))
		}
		return nil, err
	}

	// Create a new managed address for the specific type of address based
	// on type.
	return m.rowInterfaceToManaged(ns, rowInterface)
}

// Address returns a managed address given the passed address if it is known
// to the address manager.  A managed address differs from the passed address
// in that it also potentially contains extra information needed to sign
// transactions such as the associated private key for pay-to-pubkey and
// pay-to-pubkey-hash addresses and the script associated with
// pay-to-script-hash addresses.
func (m *Manager) Address(ns walletdb.ReadBucket, address dcrutil.Address) (ManagedAddress, error) {
	address = normalizeAddress(address)
	m.mtx.Lock()
	ma, err := m.loadAddress(ns, address)
	m.mtx.Unlock()
	return ma, err
}

// AddrAccount returns the account to which the given address belongs.
func (m *Manager) AddrAccount(ns walletdb.ReadBucket, address dcrutil.Address) (uint32, error) {
	acct, err := fetchAddrAccount(ns, normalizeAddress(address).ScriptAddress())
	if err != nil {
		if errors.Is(errors.NotExist, err) {
			return 0, errors.E(errors.NotExist, errors.Errorf("no address %v", address))
		}
		return 0, err
	}

	return acct, nil
}

// ChangePassphrase changes either the public or private passphrase to the
// provided value depending on the private flag.  In order to change the private
// password, the address manager must not be watching-only.  The new passphrase
// keys are derived using the scrypt parameters in the options, so changing the
// passphrase may be used to bump the computational difficulty needed to brute
// force the passphrase.
func (m *Manager) ChangePassphrase(ns walletdb.ReadWriteBucket, oldPassphrase, newPassphrase []byte, private bool) error {
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// No private passphrase to change for a watching-only address manager.
	if private && m.watchingOnly {
		return errors.E(errors.WatchingOnly)
	}

	// Ensure the provided old passphrase is correct.  This check is done
	// using a copy of the appropriate master key depending on the private
	// flag to ensure the current state is not altered.  The temp key is
	// cleared when done to avoid leaving a copy in memory.
	secretKey := snacl.SecretKey{Key: &snacl.CryptoKey{}}
	if private {
		secretKey.Parameters = m.masterKeyPriv.Parameters
	} else {
		secretKey.Parameters = m.masterKeyPub.Parameters
	}
	if err := secretKey.DeriveKey(&oldPassphrase); err != nil {
		return err
	}
	defer secretKey.Zero()

	// Generate a new master key from the passphrase which is used to secure
	// the actual secret keys.
	newMasterKey, err := newSecretKey(&newPassphrase, &defaultScryptOptions)
	if err != nil {
		return err
	}
	newKeyParams := newMasterKey.Marshal()

	if private {
		// Technically, the locked state could be checked here to only
		// do the decrypts when the address manager is locked as the
		// clear text keys are already available in memory when it is
		// unlocked, but this is not a hot path, decryption is quite
		// fast, and it's less cyclomatic complexity to simply decrypt
		// in either case.

		// Create a new salt that will be used for hashing the new
		// passphrase each unlock.
		var passphraseSalt [saltSize]byte
		_, err := rand.Read(passphraseSalt[:])
		if err != nil {
			return errors.E(errors.IO, err)
		}

		// Re-encrypt the crypto private key using the new master
		// private key.
		decPriv, err := secretKey.Decrypt(m.cryptoKeyPrivEncrypted)
		if err != nil {
			return errors.E(errors.Crypto, errors.Errorf("decrypt crypto privkey: %v", err))
		}
		encPriv, err := newMasterKey.Encrypt(decPriv)
		zero.Bytes(decPriv)
		if err != nil {
			return errors.E(errors.Crypto, errors.Errorf("encrypt crypto privkey: %v", err))
		}

		// Re-encrypt the crypto script key using the new master private
		// key.
		decScript, err := secretKey.Decrypt(m.cryptoKeyScriptEncrypted)
		if err != nil {
			return errors.E(errors.Crypto, errors.Errorf("decrypt crypto script key: %v", err))
		}
		encScript, err := newMasterKey.Encrypt(decScript)
		zero.Bytes(decScript)
		if err != nil {
			return errors.E(errors.Crypto, errors.Errorf("encrypt crypto script key: %v", err))
		}

		// When the manager is locked, ensure the new clear text master
		// key is cleared from memory now that it is no longer needed.
		// If unlocked, create the new passphrase hash with the new
		// passphrase and salt.
		var hashedPassphrase [sha512.Size]byte
		if m.locked {
			newMasterKey.Zero()
		} else {
			saltedPassphrase := append(passphraseSalt[:],
				newPassphrase...)
			hashedPassphrase = sha512.Sum512(saltedPassphrase)
			zero.Bytes(saltedPassphrase)
		}

		// Save the new keys and params to the the db in a single
		// transaction.
		err = putCryptoKeys(ns, nil, encPriv, encScript)
		if err != nil {
			return err
		}

		err = putMasterKeyParams(ns, nil, newKeyParams)
		if err != nil {
			return err
		}

		// Now that the db has been successfully updated, clear the old
		// key and set the new one.
		copy(m.cryptoKeyPrivEncrypted[:], encPriv)
		copy(m.cryptoKeyScriptEncrypted[:], encScript)
		m.masterKeyPriv.Zero() // Clear the old key.
		m.masterKeyPriv = newMasterKey
		m.privPassphraseSalt = passphraseSalt
		m.hashedPrivPassphrase = hashedPassphrase
	} else {
		// Re-encrypt the crypto public key using the new master public
		// key.
		encryptedPub, err := newMasterKey.Encrypt(m.cryptoKeyPub.Bytes())
		if err != nil {
			return errors.E(errors.Crypto, errors.Errorf("encrypt crypto pubkey: %v", err))
		}

		// Save the new keys and params to the the db in a single
		// transaction.
		err = putCryptoKeys(ns, encryptedPub, nil, nil)
		if err != nil {
			return err
		}

		err = putMasterKeyParams(ns, newKeyParams, nil)
		if err != nil {
			return err
		}

		// Now that the db has been successfully updated, clear the old
		// key and set the new one.
		m.masterKeyPub.Zero()
		m.masterKeyPub = newMasterKey
	}

	return nil
}

// ConvertToWatchingOnly converts the current address manager to a locked
// watching-only address manager.
//
// WARNING: This function removes private keys from the existing address manager
// which means they will no longer be available.  Typically the caller will make
// a copy of the existing wallet database and modify the copy since otherwise it
// would mean permanent loss of any imported private keys and scripts.
//
// Executing this function on a manager that is already watching-only will have
// no effect.
func (m *Manager) ConvertToWatchingOnly(ns walletdb.ReadWriteBucket) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Exit now if the manager is already watching-only.
	if m.watchingOnly {
		return nil
	}

	// Remove all private key material and mark the new database as watching
	// only.
	err := deletePrivateKeys(ns, DBVersion)
	if err != nil {
		return err
	}

	err = putWatchingOnly(ns, true)
	if err != nil {
		return err
	}

	// Lock the manager to remove all clear text private key material from
	// memory if needed.
	if !m.locked {
		m.lock()
	}

	// This section clears and removes the encrypted private key material
	// that is ordinarily used to unlock the manager.  Since the the manager
	// is being converted to watching-only, the encrypted private key
	// material is no longer needed.

	// Clear and remove all of the encrypted acount private keys.
	for _, acctInfo := range m.acctInfo {
		zero.Bytes(acctInfo.acctKeyEncrypted)
		acctInfo.acctKeyEncrypted = nil
	}

	// Clear and remove encrypted private keys and encrypted scripts from
	// all address entries.
	m.returnedSecretsMu.Lock()
	for _, privKey := range m.returnedPrivKeys {
		zero.BigInt(privKey.GetD())
	}
	for _, script := range m.returnedScripts {
		zero.Bytes(script)
	}
	m.returnedPrivKeys = nil
	m.returnedScripts = nil
	m.returnedSecretsMu.Unlock()

	// Clear and remove encrypted private and script crypto keys.
	zero.Bytes(m.cryptoKeyScriptEncrypted)
	m.cryptoKeyScriptEncrypted = nil
	m.cryptoKeyScript = nil
	zero.Bytes(m.cryptoKeyPrivEncrypted)
	m.cryptoKeyPrivEncrypted = nil
	m.cryptoKeyPriv = nil

	// The master private key is derived from a passphrase when the manager
	// is unlocked, so there is no encrypted version to zero.  However,
	// it is no longer needed, so nil it.
	m.masterKeyPriv = nil

	// Mark the manager watching-only.
	m.watchingOnly = true
	return nil
}

// ExistsHash160 returns whether or not the 20 byte P2PKH or P2SH HASH160 is
// known to the address manager.
func (m *Manager) ExistsHash160(ns walletdb.ReadBucket, hash160 []byte) bool {
	return existsAddress(ns, hash160)
}

// ImportPrivateKey imports a WIF private key into the address manager.  The
// imported address is created using either a compressed or uncompressed
// serialized public key, depending on the CompressPubKey bool of the WIF.
//
// All imported addresses will be part of the account defined by the
// ImportedAddrAccount constant.
//
// NOTE: When the address manager is watching-only, the private key itself will
// not be stored or available since it is private data.  Instead, only the
// public key will be stored.  This means it is paramount the private key is
// kept elsewhere as the watching-only address manager will NOT ever have access
// to it.
//
// This function will return an error if the address manager is locked and not
// watching-only, or not for the same network as the key trying to be imported.
// It will also return an error if the address already exists.  Any other errors
// returned are generally unexpected.
func (m *Manager) ImportPrivateKey(ns walletdb.ReadWriteBucket, wif *dcrutil.WIF) (ManagedPubKeyAddress, error) {
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// The manager must be unlocked to encrypt the imported private key.
	if !m.watchingOnly && m.locked {
		return nil, errors.E(errors.Locked)
	}

	// Prevent duplicates.
	serializedPubKey := wif.SerializePubKey()
	pubKeyHash := dcrutil.Hash160(serializedPubKey)
	if existsAddress(ns, pubKeyHash) {
		return nil, errors.E(errors.Exist, "address for private key already exists")
	}

	// Encrypt public key.
	encryptedPubKey, err := m.cryptoKeyPub.Encrypt(serializedPubKey)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("encrypt imported pubkey: %v", err))
	}

	// Encrypt the private key when not a watching-only address manager.
	var encryptedPrivKey []byte
	if !m.watchingOnly {
		privKeyBytes := wif.PrivKey.Serialize()
		encryptedPrivKey, err = m.cryptoKeyPriv.Encrypt(privKeyBytes)
		zero.Bytes(privKeyBytes)
		if err != nil {
			return nil, errors.E(errors.Crypto, errors.Errorf("encrypt imported privkey: %v", err))
		}
	}

	// Save the new imported address to the db and update start block (if
	// needed) in a single transaction.
	err = putImportedAddress(ns, pubKeyHash, ImportedAddrAccount, ssNone,
		encryptedPubKey, encryptedPrivKey)
	if err != nil {
		return nil, err
	}

	// Create a new managed address based on the imported address.
	managedAddr, err := newManagedAddressWithoutPrivKey(m, ImportedAddrAccount,
		chainec.Secp256k1.NewPublicKey(wif.PrivKey.Public()), true)
	if err != nil {
		return nil, err
	}
	managedAddr.imported = true
	return managedAddr, nil
}

// ImportScript imports a user-provided script into the address manager.  The
// imported script will act as a pay-to-script-hash address.
//
// All imported script addresses will be part of the account defined by the
// ImportedAddrAccount constant.
//
// When the address manager is watching-only, the script itself will not be
// stored or available since it is considered private data.
//
// This function will return an error if the address manager is locked and not
// watching-only, or the address already exists.  Any other errors returned are
// generally unexpected.
func (m *Manager) ImportScript(ns walletdb.ReadWriteBucket, script []byte) (ManagedScriptAddress, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Prevent duplicates.
	scriptHash := dcrutil.Hash160(script)
	if existsAddress(ns, scriptHash) {
		return nil, errors.E(errors.Exist, "script already exists")
	}

	// The manager must be unlocked to encrypt the imported script.
	if !m.watchingOnly && m.locked {
		return nil, errors.E(errors.Locked)
	}

	// Encrypt the script hash using the crypto public key so it is
	// accessible when the address manager is locked or watching-only.
	encryptedHash, err := m.cryptoKeyPub.Encrypt(scriptHash)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("encrypt script hash: %v", err))
	}

	// Encrypt the script for storage in database using the crypto script
	// key when not a watching-only address manager.
	var encryptedScript []byte
	if !m.watchingOnly {
		encryptedScript, err = m.cryptoKeyScript.Encrypt(script)
		if err != nil {
			return nil, errors.E(errors.Crypto, errors.Errorf("encrypt script: %v", err))
		}
	}

	// Save the new imported address to the db and update start block (if
	// needed) in a single transaction.
	err = putScriptAddress(ns, scriptHash, ImportedAddrAccount,
		ssNone, encryptedHash, encryptedScript)
	if err != nil {
		return nil, err
	}

	// Create a new managed address based on the imported script.
	return newScriptAddress(m, ImportedAddrAccount, scriptHash)
}

// IsLocked returns whether or not the address managed is locked.  When it is
// unlocked, the decryption key needed to decrypt private keys used for signing
// is in memory.
func (m *Manager) IsLocked() bool {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	return m.locked
}

// Lock performs a best try effort to remove and zero all secret keys associated
// with the address manager.
//
// This function will return an error if invoked on a watching-only address
// manager.
func (m *Manager) Lock() error {
	// A watching-only address manager can't be locked.
	if m.watchingOnly {
		return errors.E(errors.WatchingOnly)
	}

	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Error on attempt to lock an already locked manager.
	if m.locked {
		return errors.E(errors.Locked)
	}

	m.lock()
	return nil
}

// LookupAccount loads account number stored in the manager for the given
// account name
func (m *Manager) LookupAccount(ns walletdb.ReadBucket, name string) (uint32, error) {
	// Mutex does not need to be held here as this does not read or write to any
	// of the manager's members.
	return fetchAccountByName(ns, name)
}

// UnlockedWithPassphrase returns nil when the wallet is currently unlocked with a
// matching passphrase and errors with the following codes otherwise:
//   WatchingOnly: The wallet is watching-only and can never be unlocked
//   Locked: The wallet is currently locked
//   Passphrase: The wallet is unlocked but the provided passphrase is incorrect
func (m *Manager) UnlockedWithPassphrase(passphrase []byte) error {
	defer m.mtx.RUnlock()
	m.mtx.RLock()

	if m.watchingOnly {
		return errors.E(errors.WatchingOnly, "watching wallets can not be unlocked")
	}

	if m.locked {
		return errors.E(errors.Locked)
	}

	saltedPassphrase := append(m.privPassphraseSalt[:], passphrase...)
	hashedPassphrase := sha512.Sum512(saltedPassphrase)
	zero.Bytes(saltedPassphrase)
	if hashedPassphrase != m.hashedPrivPassphrase {
		return errors.E(errors.Passphrase)
	}

	return nil
}

// Unlock derives the master private key from the specified passphrase.  An
// invalid passphrase will return an error.  Otherwise, the derived secret key
// is stored in memory until the address manager is locked.  Any failures that
// occur during this function will result in the address manager being locked,
// even if it was already unlocked prior to calling this function.
//
// This function will return an error if invoked on a watching-only address
// manager.
func (m *Manager) Unlock(ns walletdb.ReadBucket, passphrase []byte) error {
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// A watching-only address manager can't be unlocked.
	if m.watchingOnly {
		return errors.E(errors.WatchingOnly, "cannot unlock watching wallet")
	}

	// Avoid actually unlocking if the manager is already unlocked
	// and the passphrases match.
	if !m.locked {
		saltedPassphrase := append(m.privPassphraseSalt[:],
			passphrase...)
		hashedPassphrase := sha512.Sum512(saltedPassphrase)
		zero.Bytes(saltedPassphrase)
		if hashedPassphrase != m.hashedPrivPassphrase {
			m.lock()
			return errors.E(errors.Passphrase)
		}
		return nil
	}

	// Derive the master private key using the provided passphrase.
	if err := m.masterKeyPriv.DeriveKey(&passphrase); err != nil {
		m.lock()
		return err
	}

	// Use the master private key to decrypt the crypto private key.
	decryptedKey, err := m.masterKeyPriv.Decrypt(m.cryptoKeyPrivEncrypted)
	if err != nil {
		m.lock()
		return errors.E(errors.Crypto, errors.Errorf("decrypt crypto privkey: %v", err))
	}
	m.cryptoKeyPriv.CopyBytes(decryptedKey)
	zero.Bytes(decryptedKey)

	// Use the crypto private key to decrypt all of the account private
	// extended keys.
	for account, acctInfo := range m.acctInfo {
		decrypted, err := m.cryptoKeyPriv.Decrypt(acctInfo.acctKeyEncrypted)
		if err != nil {
			m.lock()
			return errors.E(errors.Crypto, errors.Errorf("decrypt account %d privkey: %v", account, err))
		}

		acctKeyPriv, err := hdkeychain.NewKeyFromString(string(decrypted), m.chainParams)
		zero.Bytes(decrypted)
		if err != nil {
			m.lock()
			return errors.E(errors.IO, err)
		}
		acctInfo.acctKeyPriv = acctKeyPriv
	}

	m.locked = false
	saltedPassphrase := append(m.privPassphraseSalt[:], passphrase...)
	m.hashedPrivPassphrase = sha512.Sum512(saltedPassphrase)
	zero.Bytes(saltedPassphrase)
	return nil
}

func maxUint32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

// MarkUsed updates usage statistics of a BIP0044 account address so that the
// last used address index can be tracked.  There is no effect when called on
// P2SH addresses or any imported addresses.
func (m *Manager) MarkUsed(ns walletdb.ReadWriteBucket, address dcrutil.Address) error {
	// Version 1 of the database used a bucket that recorded the usage status of
	// every used address in the wallet.  This was changed in version 2 to
	// record the last used address of a BIP0044 account's external and internal
	// branches in the account row itself.  The database also no longer tracks
	// usage of non-BIP0044 derived addresses, as there is no need for this
	// data.
	address = normalizeAddress(address)
	dbAddr, err := fetchAddress(ns, address.Hash160()[:])
	if err != nil {
		return err
	}
	bip0044Addr, ok := dbAddr.(*dbChainAddressRow)
	if !ok {
		return nil
	}
	row, err := fetchAccountInfo(ns, bip0044Addr.account, DBVersion)
	if err != nil {
		return errors.E(errors.IO, errors.Errorf("missing account %d", bip0044Addr.account))
	}
	lastUsedExtIndex := row.lastUsedExternalIndex
	lastUsedIntIndex := row.lastUsedInternalIndex
	switch bip0044Addr.branch {
	case ExternalBranch:
		lastUsedExtIndex = bip0044Addr.index
	case InternalBranch:
		lastUsedIntIndex = bip0044Addr.index
	default:
		return errors.E(errors.IO, errors.Errorf("invalid account branch %d", bip0044Addr.branch))
	}

	if lastUsedExtIndex+1 < row.lastUsedExternalIndex+1 ||
		lastUsedIntIndex+1 < row.lastUsedInternalIndex+1 {
		// More recent addresses have already been marked used, nothing to
		// update.
		return nil
	}

	// The last returned indexes should never be less than the last used.  The
	// weird addition and subtraction makes this calculation work correctly even
	// when any of of the indexes are ^uint32(0).
	lastRetExtIndex := maxUint32(lastUsedExtIndex+1, row.lastReturnedExternalIndex+1) - 1
	lastRetIntIndex := maxUint32(lastUsedIntIndex+1, row.lastReturnedInternalIndex+1) - 1

	row = bip0044AccountInfo(row.pubKeyEncrypted, row.privKeyEncrypted, 0, 0,
		lastUsedExtIndex, lastUsedIntIndex, lastRetExtIndex, lastRetIntIndex,
		row.name, DBVersion)
	return putAccountRow(ns, bip0044Addr.account, &row.dbAccountRow)
}

// MarkUsedChildIndex marks a BIP0044 account branch child as used.
func (m *Manager) MarkUsedChildIndex(tx walletdb.ReadWriteTx, account, branch, child uint32) error {
	ns := tx.ReadWriteBucket(waddrmgrBucketKey)

	row, err := fetchAccountInfo(ns, account, DBVersion)
	if err != nil {
		return err
	}
	lastUsedExtIndex := row.lastUsedExternalIndex
	lastUsedIntIndex := row.lastUsedInternalIndex
	switch branch {
	case ExternalBranch:
		lastUsedExtIndex = child
	case InternalBranch:
		lastUsedIntIndex = child
	default:
		return errors.E(errors.Invalid, errors.Errorf("account branch %d", branch))
	}

	if lastUsedExtIndex+1 < row.lastUsedExternalIndex+1 ||
		lastUsedIntIndex+1 < row.lastUsedInternalIndex+1 {
		// More recent addresses have already been marked used, nothing to
		// update.
		return nil
	}

	// The last returned indexes should never be less than the last used.  The
	// weird addition and subtraction makes this calculation work correctly even
	// when any of of the indexes are ^uint32(0).
	lastRetExtIndex := maxUint32(lastUsedExtIndex+1, row.lastReturnedExternalIndex+1) - 1
	lastRetIntIndex := maxUint32(lastUsedIntIndex+1, row.lastReturnedInternalIndex+1) - 1

	row = bip0044AccountInfo(row.pubKeyEncrypted, row.privKeyEncrypted, 0, 0,
		lastUsedExtIndex, lastUsedIntIndex, lastRetExtIndex, lastRetIntIndex,
		row.name, DBVersion)
	return putAccountRow(ns, account, &row.dbAccountRow)
}

// MarkReturnedChildIndex marks a BIP0044 account branch child as returned to a
// caller.  This method will write returned child indexes that are lower than
// the currently-recorded last returned indexes, but these indexes will never be
// lower than the last used index.
func (m *Manager) MarkReturnedChildIndex(tx walletdb.ReadWriteTx, account, branch, child uint32) error {
	ns := tx.ReadWriteBucket(waddrmgrBucketKey)

	row, err := fetchAccountInfo(ns, account, DBVersion)
	if err != nil {
		return err
	}
	lastRetExtIndex := row.lastReturnedExternalIndex
	lastRetIntIndex := row.lastReturnedInternalIndex
	switch branch {
	case ExternalBranch:
		lastRetExtIndex = child
	case InternalBranch:
		lastRetIntIndex = child
	default:
		return errors.E(errors.Invalid, errors.Errorf("account branch %d", branch))
	}

	// The last returned indexes should never be less than the last used.  The
	// weird addition and subtraction makes this calculation work correctly even
	// when any of of the indexes are ^uint32(0).
	lastRetExtIndex = maxUint32(row.lastUsedExternalIndex+1, lastRetExtIndex+1) - 1
	lastRetIntIndex = maxUint32(row.lastUsedInternalIndex+1, lastRetIntIndex+1) - 1

	row = bip0044AccountInfo(row.pubKeyEncrypted, row.privKeyEncrypted, 0, 0,
		row.lastUsedExternalIndex, row.lastUsedInternalIndex,
		lastRetExtIndex, lastRetIntIndex, row.name, DBVersion)
	return putAccountRow(ns, account, &row.dbAccountRow)
}

// ChainParams returns the chain parameters for this address manager.
func (m *Manager) ChainParams() *chaincfg.Params {
	// NOTE: No need for mutex here since the net field does not change
	// after the manager instance is created.

	return m.chainParams
}

// syncAccountToAddrIndex takes an account, branch, and index and synchronizes
// the waddrmgr account to it.
//
// This function MUST be called with the manager lock held for writes.
func (m *Manager) syncAccountToAddrIndex(ns walletdb.ReadWriteBucket, account uint32, syncToIndex uint32, branch uint32) error {
	// Unfortunately the imported account is saved as a BIP0044 account type so
	// the next db fetch will not error. Therefore we need an explicit check
	// that it is not being modified.
	if account == ImportedAddrAccount {
		return errors.E(errors.Invalid, "cannot sync imported account branch index")
	}

	// The next address can only be generated for accounts that have already
	// been created.  This also enforces that the account is a BIP0044 account.
	// While imported accounts are also saved as BIP0044 account types, the
	// above check prevents this from this code ever continuing on imported
	// accounts.
	acctInfo, err := m.loadAccountInfo(ns, account)
	if err != nil {
		return err
	}

	// Derive the account branch xpub, and if the account is unlocked, also
	// derive the xpriv.
	var xpubBranch, xprivBranch *hdkeychain.ExtendedKey
	switch branch {
	case ExternalBranch, InternalBranch:
		xpubBranch, err = acctInfo.acctKeyPub.Child(branch)
		if err != nil {
			return err
		}
		if m.locked {
			break
		}
		xprivBranch, err = acctInfo.acctKeyPriv.Child(branch)
		if err != nil {
			return err
		}
		defer xprivBranch.Zero()
	default:
		return errors.E(errors.Invalid, errors.Errorf("account branch %d", branch))
	}

	// Ensure the requested index to sync to doesn't exceed the maximum
	// allowed for this account.
	if syncToIndex > MaxAddressesPerAccount {
		return errors.E(errors.Invalid, errors.Errorf("child index %d exceeds max", syncToIndex))
	}

	// Because the database does not track the last generated address for each
	// account (only the address usage in public transactions), child addresses
	// must be generated and saved in reverse, down to child index 0.  For each
	// derived address, a check is performed to see if the address has already
	// been recorded.  As soon as any already-saved address is found, the loop
	// can end, because we know that all addresses before that child have also
	// been created.
	for child := syncToIndex; ; child-- {
		xpubChild, err := xpubBranch.Child(child)
		if err == hdkeychain.ErrInvalidChild {
			continue
		}
		if err != nil {
			return err
		}
		// This can't error as only good input is passed to
		// dcrutil.NewAddressPubKeyHash.
		addr, _ := compat.HD2Address(xpubChild, m.chainParams)
		hash160 := addr.Hash160()[:]
		if existsAddress(ns, hash160) {
			// address was found and there are no more to generate
			break
		}

		err = putChainedAddress(ns, hash160, account, ssFull, branch, child)
		if err != nil {
			return err
		}

		if child == 0 {
			break
		}
	}

	return nil
}

// SyncAccountToAddrIndex returns the specified number of next chained addresses
// that are intended for internal use such as change from the address manager.
func (m *Manager) SyncAccountToAddrIndex(ns walletdb.ReadWriteBucket, account uint32, syncToIndex uint32, branch uint32) error {
	// Enforce maximum account number.
	if account > MaxAccountNum {
		return errors.E(errors.Invalid, errors.Errorf("account %d", account))
	}

	m.mtx.Lock()
	err := m.syncAccountToAddrIndex(ns, account, syncToIndex, branch)
	m.mtx.Unlock()
	return err
}

// ValidateAccountName validates the given account name and returns an error,
// if any.
func ValidateAccountName(name string) error {
	if name == "" {
		return errors.E(errors.Invalid, "accounts may not be named the empty string")
	}
	if isReservedAccountName(name) {
		return errors.E(errors.Invalid, "reserved account name")
	}
	return nil
}

// NewAccount creates and returns a new account stored in the manager based
// on the given account name.  If an account with the same name already exists,
// ErrDuplicateAccount will be returned.  Since creating a new account requires
// access to the cointype keys (from which extended account keys are derived),
// it requires the manager to be unlocked.
func (m *Manager) NewAccount(ns walletdb.ReadWriteBucket, name string) (uint32, error) {
	defer m.mtx.Unlock()
	m.mtx.Lock()

	if m.watchingOnly {
		return 0, errors.E(errors.WatchingOnly)
	}

	if m.locked {
		return 0, errors.E(errors.Locked)
	}

	// Validate account name
	if err := ValidateAccountName(name); err != nil {
		return 0, err
	}

	// Check that account with the same name does not exist
	_, err := fetchAccountByName(ns, name)
	if err == nil {
		return 0, errors.E(errors.Exist, errors.Errorf("account named %q already exists", name))
	}

	// Fetch latest account, and create a new account in the same transaction
	// Fetch the latest account number to generate the next account number
	account, err := fetchLastAccount(ns)
	if err != nil {
		return 0, err
	}
	account++
	// Fetch the cointype key which will be used to derive the next account
	// extended keys
	_, coinTypePrivEnc, err := fetchCoinTypeKeys(ns)
	if err != nil {
		return 0, err
	}

	// Decrypt the cointype key
	serializedKeyPriv, err := m.cryptoKeyPriv.Decrypt(coinTypePrivEnc)
	if err != nil {
		return 0, errors.E(errors.Crypto, errors.Errorf("decrypt cointype privkey: %v", err))
	}
	coinTypeKeyPriv, err :=
		hdkeychain.NewKeyFromString(string(serializedKeyPriv), m.chainParams)
	zero.Bytes(serializedKeyPriv)
	if err != nil {
		return 0, errors.E(errors.IO, err)
	}

	// Derive the account key using the cointype key
	acctKeyPriv, err := deriveAccountKey(coinTypeKeyPriv, account)
	coinTypeKeyPriv.Zero()
	if err != nil {
		return 0, err
	}
	acctKeyPub, err := acctKeyPriv.Neuter()
	if err != nil {
		return 0, err
	}
	// Encrypt the default account keys with the associated crypto keys.
	apes := acctKeyPub.String()
	acctPubEnc, err := m.cryptoKeyPub.Encrypt([]byte(apes))
	if err != nil {
		return 0, errors.E(errors.Crypto, errors.Errorf("encrypt account pubkey: %v", err))
	}
	apes = acctKeyPriv.String()
	acctPrivEnc, err := m.cryptoKeyPriv.Encrypt([]byte(apes))
	if err != nil {
		return 0, errors.E(errors.Crypto, errors.Errorf("encrypt account privkey: %v", err))
	}
	// We have the encrypted account extended keys, so save them to the
	// database
	row := bip0044AccountInfo(acctPubEnc, acctPrivEnc, 0, 0,
		^uint32(0), ^uint32(0), ^uint32(0), ^uint32(0), name, DBVersion)
	err = putAccountInfo(ns, account, row)
	if err != nil {
		return 0, err
	}

	// Save last account metadata
	if err := putLastAccount(ns, account); err != nil {
		return 0, err
	}

	return account, nil
}

// RenameAccount renames an account stored in the manager based on the
// given account number with the given name.  If an account with the same name
// already exists, ErrDuplicateAccount will be returned.
func (m *Manager) RenameAccount(ns walletdb.ReadWriteBucket, account uint32, name string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Ensure that a reserved account is not being renamed.
	if isReservedAccountNum(account) {
		return errors.E(errors.Invalid, "reserved account")
	}

	// Check that account with the new name does not exist
	_, err := fetchAccountByName(ns, name)
	if err == nil {
		return errors.E(errors.Exist, errors.Errorf("account named %q already exists", name))
	}
	// Validate account name
	if err := ValidateAccountName(name); err != nil {
		return err
	}

	row, err := fetchAccountInfo(ns, account, DBVersion)
	if err != nil {
		return err
	}

	// Remove the old name key from the accout id index
	if err = deleteAccountIDIndex(ns, account); err != nil {
		return err
	}
	// Remove the old name key from the account name index
	if err = deleteAccountNameIndex(ns, row.name); err != nil {
		return err
	}
	row = bip0044AccountInfo(row.pubKeyEncrypted, row.privKeyEncrypted,
		0, 0, row.lastUsedExternalIndex, row.lastUsedInternalIndex,
		row.lastReturnedExternalIndex, row.lastReturnedInternalIndex,
		name, DBVersion)
	err = putAccountInfo(ns, account, row)
	if err != nil {
		return err
	}

	// Update in-memory account info with new name if cached and the db
	// write was successful.
	if acctInfo, ok := m.acctInfo[account]; ok {
		acctInfo.acctName = name
	}
	return nil
}

// AccountName returns the account name for the given account number
// stored in the manager.
func (m *Manager) AccountName(ns walletdb.ReadBucket, account uint32) (string, error) {
	return fetchAccountName(ns, account)
}

// ForEachAccount calls the given function with each account stored in the
// manager, breaking early on error.
func (m *Manager) ForEachAccount(ns walletdb.ReadBucket, fn func(account uint32) error) error {
	return forEachAccount(ns, fn)
}

// LastAccount returns the last account stored in the manager.
func (m *Manager) LastAccount(ns walletdb.ReadBucket) (uint32, error) {
	return fetchLastAccount(ns)
}

// ForEachAccountAddress calls the given function with each address of
// the given account stored in the manager, breaking early on error.
func (m *Manager) ForEachAccountAddress(ns walletdb.ReadBucket, account uint32, fn func(maddr ManagedAddress) error) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	addrFn := func(rowInterface interface{}) error {
		managedAddr, err := m.rowInterfaceToManaged(ns, rowInterface)
		if err != nil {
			return err
		}
		return fn(managedAddr)
	}
	return forEachAccountAddress(ns, account, addrFn)
}

// ForEachActiveAccountAddress calls the given function with each active
// address of the given account stored in the manager, breaking early on
// error.
// TODO(tuxcanfly): actually return only active addresses
func (m *Manager) ForEachActiveAccountAddress(ns walletdb.ReadBucket, account uint32, fn func(maddr ManagedAddress) error) error {
	return m.ForEachAccountAddress(ns, account, fn)
}

// ForEachActiveAddress calls the given function with each active address
// stored in the manager, breaking early on error.
func (m *Manager) ForEachActiveAddress(ns walletdb.ReadBucket, fn func(addr dcrutil.Address) error) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	addrFn := func(rowInterface interface{}) error {
		managedAddr, err := m.rowInterfaceToManaged(ns, rowInterface)
		if err != nil {
			return err
		}
		return fn(managedAddr.Address())
	}

	return forEachActiveAddress(ns, addrFn)
}

// PrivateKey retreives the private key for a P2PK or P2PKH address.  If this
// function returns without error, the returned 'done' function must be called
// when the private key is no longer being used.  Failure to do so will cause
// deadlocks when the manager is locked.
func (m *Manager) PrivateKey(ns walletdb.ReadBucket, addr dcrutil.Address) (key chainec.PrivateKey, done func(), err error) {
	// Lock the manager mutex for writes.  This protects read access to m.locked
	// and write access to m.returnedPrivKeys and the cached accounts.
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// No private keys are available for a watching-only address manager.
	if m.watchingOnly {
		return nil, nil, errors.E(errors.WatchingOnly)
	}

	if m.locked {
		return nil, nil, errors.E(errors.Locked)
	}

	// If the private key for this address' hash160 has already been returned,
	// return it again.
	if m.returnedPrivKeys != nil {
		key, ok := m.returnedPrivKeys[*addr.Hash160()]
		if ok {
			m.returnedSecretsMu.RLock()
			return key, m.returnedSecretsMu.RUnlock, nil
		}
	}

	// At this point, there are two types of addresses that must be handled:
	// those that are derived from a BIP0044 account and addresses for imported
	// keys.  For BIP0044 addresses, the private key must be derived using the
	// account xpriv with the correct branch and child indexes.  For imported
	// keys, the encrypted private key is simply retreived from the database and
	// decrypted.
	addrInterface, err := fetchAddress(ns, addr.Hash160()[:])
	if err != nil {
		return nil, nil, err
	}
	switch a := addrInterface.(type) {
	case *dbChainAddressRow:
		xpriv, err := m.deriveKeyFromPath(ns, a.account, a.branch, a.index, true)
		if err != nil {
			return nil, nil, err
		}
		key, err = xpriv.ECPrivKey()
		// hdkeychain.ExtendedKey.ECPrivKey creates a copy of the private key,
		// and therefore the extended private key should be zeroed.
		xpriv.Zero()
		if err != nil {
			return nil, nil, err
		}

	case *dbImportedAddressRow:
		privKeyBytes, err := m.cryptoKeyPriv.Decrypt(a.encryptedPrivKey)
		if err != nil {
			return nil, nil, errors.E(errors.Crypto, errors.Errorf("decrypt imported privkey: %v", err))
		}
		key, _ = chainec.Secp256k1.PrivKeyFromBytes(privKeyBytes)
		// PrivKeyFromBytes creates a copy of the private key, and therefore
		// the decrypted private key bytes must be zeroed now.
		zero.Bytes(privKeyBytes)

	case *dbScriptAddressRow:
		return nil, nil, errors.E(errors.Invalid, "no private key for P2SH address")

	default:
		return nil, nil, errors.E(errors.Invalid, errors.Errorf("address row type %T", addrInterface))
	}

	// Lock the RWMutex for reads for the caller and prepare to return the
	// function to unlock. Clearing private keys first grabs the write lock and
	// no private keys will be cleared while the caller is holding the read
	// lock.  This prevents zeroing keys (and data racing while doing so) when
	// the caller is still using them.
	m.returnedSecretsMu.RLock()
	done = m.returnedSecretsMu.RUnlock

	// Add the key to the manager so it can be zeroed on wallet lock.
	if m.returnedPrivKeys == nil {
		m.returnedPrivKeys = make(map[[ripemd160.Size]byte]chainec.PrivateKey)
	}
	m.returnedPrivKeys[*addr.Hash160()] = key

	return key, done, nil
}

// RedeemScript retreives the redeem script to redeem an output paid to a P2SH
// address.  If this function returns without error, the returned 'done'
// function must be called when the script is no longer being used. Failure to
// do so will cause deadlocks when the manager is locked.
func (m *Manager) RedeemScript(ns walletdb.ReadBucket, addr dcrutil.Address) (script []byte, done func(), err error) {
	// Lock the manager mutex for writes.  This protects read access to m.locked
	// and write access to m.returnedScripts and the cached accounts.
	defer m.mtx.Unlock()
	m.mtx.Lock()

	// No scripts are available for a watching-only address manager.
	if m.watchingOnly {
		return nil, nil, errors.E(errors.WatchingOnly)
	}

	if m.locked {
		return nil, nil, errors.E(errors.Locked)
	}

	// If the script for this address' hash160 has already been returned, return
	// it again.
	if m.returnedScripts != nil {
		script, ok := m.returnedScripts[*addr.Hash160()]
		if ok {
			m.returnedSecretsMu.RLock()
			return script, m.returnedSecretsMu.RUnlock, nil
		}
	}

	addrInterface, err := fetchAddress(ns, addr.Hash160()[:])
	if err != nil {
		return nil, nil, err
	}
	switch a := addrInterface.(type) {
	case *dbScriptAddressRow:
		script, err = m.cryptoKeyScript.Decrypt(a.encryptedScript)
		if err != nil {
			return nil, nil, errors.E(errors.Crypto, errors.Errorf("decrypt imported script: %v", err))
		}

	case *dbChainAddressRow, *dbImportedAddressRow:
		return nil, nil, errors.E(errors.Invalid, "redeem script lookup requires P2SH address")

	default:
		return nil, nil, errors.E(errors.Invalid, errors.Errorf("address row type %T", addrInterface))
	}

	// Lock the RWMutex for reads for the caller and prepare to return the
	// function to unlock. Clearing scripts first grabs the write lock and no
	// scripts will be cleared while the caller is holding the read lock.  This
	// prevents zeroing scripts (and data racing while doing so) when the caller
	// is still using them.
	m.returnedSecretsMu.RLock()
	done = m.returnedSecretsMu.RUnlock

	// Add the script to the manager so it can be zeroed on wallet lock.
	if m.returnedScripts == nil {
		m.returnedScripts = make(map[[ripemd160.Size]byte][]byte)
	}
	m.returnedScripts[*addr.Hash160()] = script

	return script, done, nil
}

// selectCryptoKey selects the appropriate crypto key based on the key type. An
// error is returned when an invalid key type is specified or the requested key
// requires the manager to be unlocked when it isn't.
//
// This function MUST be called with the manager lock held for reads.
func (m *Manager) selectCryptoKey(keyType CryptoKeyType) (EncryptorDecryptor, error) {
	if keyType == CKTPrivate || keyType == CKTScript {
		// The manager must be unlocked to work with the private keys.
		if m.locked || m.watchingOnly {
			return nil, errors.E(errors.Locked)
		}
	}

	var cryptoKey EncryptorDecryptor
	switch keyType {
	case CKTPrivate:
		cryptoKey = m.cryptoKeyPriv
	case CKTScript:
		cryptoKey = m.cryptoKeyScript
	case CKTPublic:
		cryptoKey = m.cryptoKeyPub
	default:
		return nil, errors.E(errors.Invalid, errors.Errorf("crypto key kind %d", keyType))
	}

	return cryptoKey, nil
}

// Encrypt in using the crypto key type specified by keyType.
func (m *Manager) Encrypt(keyType CryptoKeyType, in []byte) ([]byte, error) {
	// Encryption must be performed under the manager mutex since the
	// keys are cleared when the manager is locked.
	m.mtx.Lock()
	defer m.mtx.Unlock()

	cryptoKey, err := m.selectCryptoKey(keyType)
	if err != nil {
		return nil, err
	}

	encrypted, err := cryptoKey.Encrypt(in)
	if err != nil {
		return nil, errors.E(errors.Crypto, err)
	}
	return encrypted, nil
}

// Decrypt in using the crypto key type specified by keyType.
func (m *Manager) Decrypt(keyType CryptoKeyType, in []byte) ([]byte, error) {
	// Decryption must be performed under the manager mutex since the
	// keys are cleared when the manager is locked.
	m.mtx.Lock()
	defer m.mtx.Unlock()

	cryptoKey, err := m.selectCryptoKey(keyType)
	if err != nil {
		return nil, err
	}

	decrypted, err := cryptoKey.Decrypt(in)
	if err != nil {
		return nil, errors.E(errors.Crypto, err)
	}
	return decrypted, nil
}

// newManager returns a new locked address manager with the given parameters.
func newManager(chainParams *chaincfg.Params, masterKeyPub *snacl.SecretKey,
	masterKeyPriv *snacl.SecretKey, cryptoKeyPub EncryptorDecryptor,
	cryptoKeyPrivEncrypted, cryptoKeyScriptEncrypted []byte,
	privPassphraseSalt [saltSize]byte) *Manager {

	return &Manager{
		chainParams:              chainParams,
		locked:                   true,
		acctInfo:                 make(map[uint32]*accountInfo),
		masterKeyPub:             masterKeyPub,
		masterKeyPriv:            masterKeyPriv,
		cryptoKeyPub:             cryptoKeyPub,
		cryptoKeyPrivEncrypted:   cryptoKeyPrivEncrypted,
		cryptoKeyPriv:            &cryptoKey{},
		cryptoKeyScriptEncrypted: cryptoKeyScriptEncrypted,
		cryptoKeyScript:          &cryptoKey{},
		privPassphraseSalt:       privPassphraseSalt,
	}
}

// deriveCoinTypeKey derives the cointype key which can be used to derive the
// extended key for an account according to the hierarchy described by BIP0044
// given the coin type key.
//
// In particular this is the hierarchical deterministic extended key path:
// m/44'/<coin type>'
func deriveCoinTypeKey(masterNode *hdkeychain.ExtendedKey, coinType uint32) (*hdkeychain.ExtendedKey, error) {
	// Enforce maximum coin type.
	if coinType > maxCoinType {
		return nil, errors.E(errors.Invalid, errors.Errorf("coin type %d", coinType))
	}

	// The hierarchy described by BIP0043 is:
	//  m/<purpose>'/*
	// This is further extended by BIP0044 to:
	//  m/44'/<coin type>'/<account>'/<branch>/<address index>
	//
	// The branch is 0 for external addresses and 1 for internal addresses.

	// Derive the purpose key as a child of the master node.
	purpose, err := masterNode.Child(44 + hdkeychain.HardenedKeyStart)
	if err != nil {
		return nil, err
	}

	// Derive the coin type key as a child of the purpose key.
	coinTypeKey, err := purpose.Child(coinType + hdkeychain.HardenedKeyStart)
	if err != nil {
		return nil, err
	}

	return coinTypeKey, nil
}

// deriveAccountKey derives the extended key for an account according to the
// hierarchy described by BIP0044 given the master node.
//
// In particular this is the hierarchical deterministic extended key path:
//   m/44'/<coin type>'/<account>'
func deriveAccountKey(coinTypeKey *hdkeychain.ExtendedKey, account uint32) (*hdkeychain.ExtendedKey, error) {
	// Enforce maximum account number.
	if account > MaxAccountNum {
		return nil, errors.E(errors.Invalid, errors.Errorf("account %d", account))
	}

	// Derive the account key as a child of the coin type key.
	return coinTypeKey.Child(account + hdkeychain.HardenedKeyStart)
}

// checkBranchKeys ensures deriving the extended keys for the internal and
// external branches given an account key does not result in an invalid child
// error which means the chosen seed is not usable.  This conforms to the
// hierarchy described by BIP0044 so long as the account key is already derived
// accordingly.
//
// In particular this is the hierarchical deterministic extended key path:
//   m/44'/<coin type>'/<account>'/<branch>
//
// The branch is 0 for external addresses and 1 for internal addresses.
func checkBranchKeys(acctKey *hdkeychain.ExtendedKey) error {
	// Derive the external branch as the first child of the account key.
	if _, err := acctKey.Child(ExternalBranch); err != nil {
		return err
	}

	// Derive the external branch as the second child of the account key.
	_, err := acctKey.Child(InternalBranch)
	return err
}

// loadManager returns a new address manager that results from loading it from
// the passed opened database.  The public passphrase is required to decrypt the
// public keys.
func loadManager(ns walletdb.ReadBucket, pubPassphrase []byte, chainParams *chaincfg.Params) (*Manager, error) {
	// Load whether or not the manager is watching-only from the db.
	watchingOnly, err := fetchWatchingOnly(ns)
	if err != nil {
		return nil, err
	}

	// Load the master key params from the db.
	masterKeyPubParams, masterKeyPrivParams, err := fetchMasterKeyParams(ns)
	if err != nil {
		return nil, err
	}

	// Load the crypto keys from the db.
	cryptoKeyPubEnc, cryptoKeyPrivEnc, cryptoKeyScriptEnc, err :=
		fetchCryptoKeys(ns)
	if err != nil {
		return nil, err
	}

	// When not a watching-only manager, set the master private key params,
	// but don't derive it now since the manager starts off locked.
	var masterKeyPriv snacl.SecretKey
	if !watchingOnly {
		err := masterKeyPriv.Unmarshal(masterKeyPrivParams)
		if err != nil {
			return nil, errors.E(errors.IO, errors.Errorf("unmarshal master privkey: %v", err))
		}
	}

	// Derive the master public key using the serialized params and provided
	// passphrase.
	var masterKeyPub snacl.SecretKey
	if err := masterKeyPub.Unmarshal(masterKeyPubParams); err != nil {
		return nil, errors.E(errors.IO, errors.Errorf("unmarshal master pubkey: %v", err))
	}
	if err := masterKeyPub.DeriveKey(&pubPassphrase); err != nil {
		return nil, errors.E(errors.Passphrase)
	}

	// Use the master public key to decrypt the crypto public key.
	cryptoKeyPub := &cryptoKey{snacl.CryptoKey{}}
	cryptoKeyPubCT, err := masterKeyPub.Decrypt(cryptoKeyPubEnc)
	if err != nil {
		return nil, errors.E(errors.Crypto, errors.Errorf("decrypt crypto pubkey: %v", err))
	}
	cryptoKeyPub.CopyBytes(cryptoKeyPubCT)
	zero.Bytes(cryptoKeyPubCT)

	// Generate private passphrase salt.
	var privPassphraseSalt [saltSize]byte
	_, err = rand.Read(privPassphraseSalt[:])
	if err != nil {
		return nil, errors.E(errors.IO, err)
	}

	// Create new address manager with the given parameters.  Also, override
	// the defaults for the additional fields which are not specified in the
	// call to new with the values loaded from the database.
	mgr := newManager(chainParams, &masterKeyPub, &masterKeyPriv,
		cryptoKeyPub, cryptoKeyPrivEnc, cryptoKeyScriptEnc,
		privPassphraseSalt)
	mgr.watchingOnly = watchingOnly
	return mgr, nil
}

// CoinTypes returns the legacy and SLIP0044 coin types for the chain
// parameters.  At the moment, the parameters have not been upgraded for the new
// coin types.
func CoinTypes(params *chaincfg.Params) (legacyCoinType, slip0044CoinType uint32) {
	return params.LegacyCoinType, params.SLIP0044CoinType
}

// createAddressManager creates a new address manager in the given namespace.
// The seed must conform to the standards described in hdkeychain.NewMaster and
// will be used to create the master root node from which all hierarchical
// deterministic addresses are derived.  This allows all chained addresses in
// the address manager to be recovered by using the same seed.
//
// All private and public keys and information are protected by secret keys
// derived from the provided private and public passphrases.  The public
// passphrase is required on subsequent opens of the address manager, and the
// private passphrase is required to unlock the address manager in order to gain
// access to any private keys and information.
func createAddressManager(ns walletdb.ReadWriteBucket, seed, pubPassphrase, privPassphrase []byte, chainParams *chaincfg.Params, config *ScryptOptions) error {
	// Return an error if the manager has already been created in the given
	// database namespace.
	if managerExists(ns) {
		return errors.E(errors.Exist, "address manager already exists")
	}

	// Ensure the private passphrase is not empty.
	if len(privPassphrase) == 0 {
		return errors.E(errors.Invalid, "private passphrase may not be empty")
	}

	// Perform the initial bucket creation and database namespace setup.
	if err := createManagerNS(ns); err != nil {
		return err
	}

	// Generate the BIP0044 HD key structure to ensure the provided seed
	// can generate the required structure with no issues.

	// Derive the master extended key from the seed.
	root, err := hdkeychain.NewMaster(seed, chainParams)
	if err != nil {
		return err
	}

	// Derive the cointype keys according to BIP0044.
	legacyCoinType, slip0044CoinType := CoinTypes(chainParams)
	coinTypeLegacyKeyPriv, err := deriveCoinTypeKey(root, legacyCoinType)
	if err != nil {
		return err
	}
	defer coinTypeLegacyKeyPriv.Zero()
	coinTypeSLIP0044KeyPriv, err := deriveCoinTypeKey(root, slip0044CoinType)
	if err != nil {
		return err
	}
	defer coinTypeSLIP0044KeyPriv.Zero()

	// Derive the account key for the first account according to BIP0044.
	acctKeyLegacyPriv, err := deriveAccountKey(coinTypeLegacyKeyPriv, 0)
	if err != nil {
		// The seed is unusable if the any of the children in the
		// required hierarchy can't be derived due to invalid child.
		if err == hdkeychain.ErrInvalidChild {
			return errors.E(errors.Seed, hdkeychain.ErrUnusableSeed)
		}

		return err
	}
	acctKeySLIP0044Priv, err := deriveAccountKey(coinTypeSLIP0044KeyPriv, 0)
	if err != nil {
		// The seed is unusable if the any of the children in the
		// required hierarchy can't be derived due to invalid child.
		if err == hdkeychain.ErrInvalidChild {
			return errors.E(errors.Seed, hdkeychain.ErrUnusableSeed)
		}

		return err
	}

	// Ensure the branch keys can be derived for the provided seed according
	// to BIP0044.
	if err := checkBranchKeys(acctKeyLegacyPriv); err != nil {
		// The seed is unusable if the any of the children in the
		// required hierarchy can't be derived due to invalid child.
		if err == hdkeychain.ErrInvalidChild {
			return errors.E(errors.Seed, hdkeychain.ErrUnusableSeed)
		}

		return err
	}
	if err := checkBranchKeys(acctKeySLIP0044Priv); err != nil {
		// The seed is unusable if the any of the children in the
		// required hierarchy can't be derived due to invalid child.
		if err == hdkeychain.ErrInvalidChild {
			return errors.E(errors.Seed, hdkeychain.ErrUnusableSeed)
		}

		return err
	}

	// The address manager needs the public extended key for the account.
	acctKeyLegacyPub, err := acctKeyLegacyPriv.Neuter()
	if err != nil {
		return err
	}
	acctKeySLIP0044Pub, err := acctKeySLIP0044Priv.Neuter()
	if err != nil {
		return err
	}

	// Generate new master keys.  These master keys are used to protect the
	// crypto keys that will be generated next.
	masterKeyPub, err := newSecretKey(&pubPassphrase, config)
	if err != nil {
		return err
	}
	masterKeyPriv, err := newSecretKey(&privPassphrase, config)
	if err != nil {
		return err
	}
	defer masterKeyPriv.Zero()

	// Generate the private passphrase salt.  This is used when hashing
	// passwords to detect whether an unlock can be avoided when the manager
	// is already unlocked.
	var privPassphraseSalt [saltSize]byte
	_, err = rand.Read(privPassphraseSalt[:])
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Generate new crypto public, private, and script keys.  These keys are
	// used to protect the actual public and private data such as addresses,
	// extended keys, and scripts.
	cryptoKeyPub, err := newCryptoKey()
	if err != nil {
		return err
	}
	cryptoKeyPriv, err := newCryptoKey()
	if err != nil {
		return err
	}
	defer cryptoKeyPriv.Zero()

	cryptoKeyScript, err := newCryptoKey()
	if err != nil {
		return err
	}
	defer cryptoKeyScript.Zero()

	// Encrypt the crypto keys with the associated master keys.
	cryptoKeyPubEnc, err := masterKeyPub.Encrypt(cryptoKeyPub.Bytes())
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt crypto pubkey: %v", err))
	}
	cryptoKeyPrivEnc, err := masterKeyPriv.Encrypt(cryptoKeyPriv.Bytes())
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt crypto privkey: %v", err))
	}
	cryptoKeyScriptEnc, err := masterKeyPriv.Encrypt(cryptoKeyScript.Bytes())
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt crypto script key: %v", err))
	}

	// Encrypt the legacy cointype keys with the associated crypto keys.
	coinTypeLegacyKeyPub, err := coinTypeLegacyKeyPriv.Neuter()
	if err != nil {
		return err
	}
	ctpes := coinTypeLegacyKeyPub.String()
	coinTypeLegacyPubEnc, err := cryptoKeyPub.Encrypt([]byte(ctpes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt legacy cointype pubkey: %v", err))
	}
	ctpes = coinTypeLegacyKeyPriv.String()
	coinTypeLegacyPrivEnc, err := cryptoKeyPriv.Encrypt([]byte(ctpes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt legacy cointype privkey: %v", err))
	}

	// Encrypt the SLIP0044 cointype keys with the associated crypto keys.
	coinTypeSLIP0044KeyPub, err := coinTypeSLIP0044KeyPriv.Neuter()
	if err != nil {
		return err
	}
	ctpes = coinTypeSLIP0044KeyPub.String()
	coinTypeSLIP0044PubEnc, err := cryptoKeyPub.Encrypt([]byte(ctpes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt SLIP0044 cointype pubkey: %v", err))
	}
	ctpes = coinTypeSLIP0044KeyPriv.String()
	coinTypeSLIP0044PrivEnc, err := cryptoKeyPriv.Encrypt([]byte(ctpes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt SLIP0044 cointype privkey: %v", err))
	}

	// Encrypt the default account keys with the associated crypto keys.
	apes := acctKeyLegacyPub.String()
	acctPubLegacyEnc, err := cryptoKeyPub.Encrypt([]byte(apes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt account 0 pubkey: %v", err))
	}
	apes = acctKeyLegacyPriv.String()
	acctPrivLegacyEnc, err := cryptoKeyPriv.Encrypt([]byte(apes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt account 0 privkey: %v", err))
	}
	apes = acctKeySLIP0044Pub.String()
	acctPubSLIP0044Enc, err := cryptoKeyPub.Encrypt([]byte(apes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt account 0 pubkey: %v", err))
	}
	apes = acctKeySLIP0044Priv.String()
	acctPrivSLIP0044Enc, err := cryptoKeyPriv.Encrypt([]byte(apes))
	if err != nil {
		return errors.E(errors.Crypto, fmt.Errorf("encrypt account 0 privkey: %v", err))
	}

	// Save the master key params to the database.
	pubParams := masterKeyPub.Marshal()
	privParams := masterKeyPriv.Marshal()
	err = putMasterKeyParams(ns, pubParams, privParams)
	if err != nil {
		return err
	}

	// Save the encrypted crypto keys to the database.
	err = putCryptoKeys(ns, cryptoKeyPubEnc, cryptoKeyPrivEnc,
		cryptoKeyScriptEnc)
	if err != nil {
		return err
	}

	// Save the encrypted legacy cointype keys to the database.
	err = putCoinTypeLegacyKeys(ns, coinTypeLegacyPubEnc, coinTypeLegacyPrivEnc)
	if err != nil {
		return err
	}

	// Save the encrypted SLIP0044 cointype keys.
	err = putCoinTypeSLIP0044Keys(ns, coinTypeSLIP0044PubEnc, coinTypeSLIP0044PrivEnc)
	if err != nil {
		return err
	}

	// Save the fact this is not a watching-only address manager to the
	// database.
	err = putWatchingOnly(ns, false)
	if err != nil {
		return err
	}

	// Set the next to use addresses as empty for the address pool.
	err = putNextToUseAddrPoolIdx(ns, false, DefaultAccountNum, 0)
	if err != nil {
		return err
	}
	err = putNextToUseAddrPoolIdx(ns, true, DefaultAccountNum, 0)
	if err != nil {
		return err
	}

	// Save the information for the imported account to the database.  Even
	// though the imported account is a special and restricted account, the
	// database used a BIP0044 row type for it.
	importedRow := bip0044AccountInfo(nil, nil, 0, 0, 0, 0, 0, 0,
		ImportedAddrAccountName, initialVersion)
	err = putAccountInfo(ns, ImportedAddrAccount, importedRow)
	if err != nil {
		return err
	}

	// Save the information for the default account to the database.  This
	// account is derived from the legacy coin type.
	defaultRow := bip0044AccountInfo(acctPubLegacyEnc, acctPrivLegacyEnc,
		0, 0, 0, 0, 0, 0, defaultAccountName, initialVersion)
	err = putAccountInfo(ns, DefaultAccountNum, defaultRow)
	if err != nil {
		return err
	}

	// Save the account row for the 0th account derived from the coin type
	// 42 key.
	slip0044Account0Row := bip0044AccountInfo(acctPubSLIP0044Enc, acctPrivSLIP0044Enc,
		0, 0, 0, 0, 0, 0, defaultAccountName, initialVersion)
	mainBucket := ns.NestedReadWriteBucket(mainBucketName)
	err = mainBucket.Put(slip0044Account0RowName, serializeAccountRow(&slip0044Account0Row.dbAccountRow))
	if err != nil {
		return errors.E(errors.IO, err)
	}

	return nil
}

// createWatchOnly creates a watching-only address manager in the given
// namespace.
//
// All public keys and information are protected by secret keys derived from the
// provided public passphrase.  The public passphrase is required on subsequent
// opens of the address manager.
func createWatchOnly(ns walletdb.ReadWriteBucket, hdPubKey string, pubPassphrase []byte, chainParams *chaincfg.Params, config *ScryptOptions) (err error) {
	// Return an error if the manager has already been created in the given
	// database namespace.
	if managerExists(ns) {
		return errors.E(errors.Exist, "address manager already exists")
	}

	// Perform the initial bucket creation and database namespace setup.
	if err := createManagerNS(ns); err != nil {
		return err
	}

	// Load the passed public key.
	acctKeyPub, err := hdkeychain.NewKeyFromString(hdPubKey, chainParams)
	if err != nil {
		// The seed is unusable if the any of the children in the
		// required hierarchy can't be derived due to invalid child.
		if err == hdkeychain.ErrInvalidChild {
			return errors.E(errors.Seed, hdkeychain.ErrUnusableSeed)
		}

		return err
	}

	// Ensure the branch keys can be derived for the provided seed according
	// to BIP0044.
	if err := checkBranchKeys(acctKeyPub); err != nil {
		// The seed is unusable if the any of the children in the
		// required hierarchy can't be derived due to invalid child.
		if err == hdkeychain.ErrInvalidChild {
			return errors.E(errors.Seed, hdkeychain.ErrUnusableSeed)
		}

		return err
	}

	// Generate new master keys.  These master keys are used to protect the
	// crypto keys that will be generated next.
	masterKeyPub, err := newSecretKey(&pubPassphrase, config)
	if err != nil {
		return err
	}
	masterKeyPriv, err := newSecretKey(&pubPassphrase, config)
	if err != nil {
		return err
	}
	defer masterKeyPriv.Zero()

	// Generate the private passphrase salt.  This is used when hashing
	// passwords to detect whether an unlock can be avoided when the manager
	// is already unlocked.
	var privPassphraseSalt [saltSize]byte
	_, err = rand.Read(privPassphraseSalt[:])
	if err != nil {
		return errors.E(errors.IO, err)
	}

	// Generate new crypto public, private, and script keys.  These keys are
	// used to protect the actual public and private data such as addresses,
	// extended keys, and scripts.
	cryptoKeyPub, err := newCryptoKey()
	if err != nil {
		return err
	}
	cryptoKeyPriv, err := newCryptoKey()
	if err != nil {
		return err
	}
	defer cryptoKeyPriv.Zero()
	cryptoKeyScript, err := newCryptoKey()
	if err != nil {
		return err
	}
	defer cryptoKeyScript.Zero()

	// Encrypt the crypto keys with the associated master keys.
	cryptoKeyPubEnc, err := masterKeyPub.Encrypt(cryptoKeyPub.Bytes())
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt crypto pubkey: %v", err))
	}
	cryptoKeyPrivEnc, err := masterKeyPriv.Encrypt(cryptoKeyPriv.Bytes())
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt crypto privkey: %v", err))
	}
	cryptoKeyScriptEnc, err := masterKeyPriv.Encrypt(cryptoKeyScript.Bytes())
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt crypto script key: %v", err))
	}

	// Encrypt the default account keys with the associated crypto keys.
	apes := acctKeyPub.String()
	acctPubEnc, err := cryptoKeyPub.Encrypt([]byte(apes))
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt account 0 pubkey: %v", err))
	}
	apes = acctKeyPub.String()
	acctPrivEnc, err := cryptoKeyPriv.Encrypt([]byte(apes))
	if err != nil {
		return errors.E(errors.Crypto, errors.Errorf("encrypt account 0 privkey: %v", err))
	}

	// Save the master key params to the database.
	pubParams := masterKeyPub.Marshal()
	privParams := masterKeyPriv.Marshal()
	err = putMasterKeyParams(ns, pubParams, privParams)
	if err != nil {
		return err
	}

	// Save the encrypted crypto keys to the database.
	err = putCryptoKeys(ns, cryptoKeyPubEnc, cryptoKeyPrivEnc,
		cryptoKeyScriptEnc)
	if err != nil {
		return err
	}

	// Save the fact this is a watching-only address manager to the database.
	err = putWatchingOnly(ns, true)
	if err != nil {
		return err
	}

	// Set the next to use addresses as empty for the address pool.
	err = putNextToUseAddrPoolIdx(ns, false, DefaultAccountNum, 0)
	if err != nil {
		return err
	}
	err = putNextToUseAddrPoolIdx(ns, true, DefaultAccountNum, 0)
	if err != nil {
		return err
	}

	// Save the information for the imported account to the database.
	importedRow := bip0044AccountInfo(nil, nil, 0, 0, 0, 0, 0, 0,
		ImportedAddrAccountName, initialVersion)
	err = putAccountInfo(ns, ImportedAddrAccount, importedRow)
	if err != nil {
		return err
	}

	// Save the information for the default account to the database.
	defaultRow := bip0044AccountInfo(acctPubEnc, acctPrivEnc, 0, 0, 0, 0, 0, 0,
		defaultAccountName, initialVersion)
	return putAccountInfo(ns, DefaultAccountNum, defaultRow)
}
