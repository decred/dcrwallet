// Copyright (c) 2015-2018 The btcsuite developers
// Copyright (c) 2017-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package loader

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3"
	_ "github.com/decred/dcrwallet/wallet/v3/drivers/bdb" // driver loaded during init
)

const (
	walletDbName = "wallet.db"
	driver       = "bdb"
)

// Loader implements the creating of new and opening of existing wallets, while
// providing a callback system for other subsystems to handle the loading of a
// wallet.  This is primarely intended for use by the RPC servers, to enable
// methods and services which require the wallet when the wallet is loaded by
// another subsystem.
//
// Loader is safe for concurrent access.
type Loader struct {
	callbacks   []func(*wallet.Wallet)
	chainParams *chaincfg.Params
	dbDirPath   string
	wallet      *wallet.Wallet
	db          wallet.DB

	stakeOptions            *StakeOptions
	gapLimit                int
	accountGapLimit         int
	disableCoinTypeUpgrades bool
	allowHighFees           bool
	relayFee                float64

	mu sync.Mutex
}

// StakeOptions contains the various options necessary for stake mining.
type StakeOptions struct {
	VotingEnabled       bool
	TicketFee           float64
	AddressReuse        bool
	VotingAddress       dcrutil.Address
	PoolAddress         dcrutil.Address
	PoolFees            float64
	StakePoolColdExtKey string
}

// NewLoader constructs a Loader.
func NewLoader(chainParams *chaincfg.Params, dbDirPath string, stakeOptions *StakeOptions, gapLimit int,
	allowHighFees bool, relayFee float64, accountGapLimit int, disableCoinTypeUpgrades bool) *Loader {

	return &Loader{
		chainParams:             chainParams,
		dbDirPath:               dbDirPath,
		stakeOptions:            stakeOptions,
		gapLimit:                gapLimit,
		accountGapLimit:         accountGapLimit,
		disableCoinTypeUpgrades: disableCoinTypeUpgrades,
		allowHighFees:           allowHighFees,
		relayFee:                relayFee,
	}
}

// onLoaded executes each added callback and prevents loader from loading any
// additional wallets.  Requires mutex to be locked.
func (l *Loader) onLoaded(w *wallet.Wallet, db wallet.DB) {
	for _, fn := range l.callbacks {
		fn(w)
	}

	l.wallet = w
	l.db = db
	l.callbacks = nil // not needed anymore
}

// RunAfterLoad adds a function to be executed when the loader creates or opens
// a wallet.  Functions are executed in a single goroutine in the order they are
// added.
func (l *Loader) RunAfterLoad(fn func(*wallet.Wallet)) {
	l.mu.Lock()
	if l.wallet != nil {
		w := l.wallet
		l.mu.Unlock()
		fn(w)
	} else {
		l.callbacks = append(l.callbacks, fn)
		l.mu.Unlock()
	}
}

// CreateWatchingOnlyWallet creates a new watch-only wallet using the provided
// extended public key and public passphrase.
func (l *Loader) CreateWatchingOnlyWallet(extendedPubKey string, pubPass []byte) (w *wallet.Wallet, err error) {
	const op errors.Op = "loader.CreateWatchingOnlyWallet"

	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, errors.E(op, errors.Exist, "wallet already loaded")
	}

	// Ensure that the network directory exists.
	if fi, err := os.Stat(l.dbDirPath); err != nil {
		if os.IsNotExist(err) {
			// Attempt data directory creation
			if err = os.MkdirAll(l.dbDirPath, 0700); err != nil {
				return nil, errors.E(op, err)
			}
		} else {
			return nil, errors.E(op, err)
		}
	} else {
		if !fi.IsDir() {
			return nil, errors.E(op, errors.Invalid, errors.Errorf("%q is not a directory", l.dbDirPath))
		}
	}

	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return nil, errors.E(op, err)
	}
	if exists {
		return nil, errors.E(op, errors.Exist, "wallet already exists")
	}

	// At this point it is asserted that there is no existing database file, and
	// deleting anything won't destroy a wallet in use.  Defer a function that
	// attempts to remove any written database file if this function errors.
	defer func() {
		if err != nil {
			_ = os.Remove(dbPath)
		}
	}()

	// Create the wallet database backed by bolt db.
	err = os.MkdirAll(l.dbDirPath, 0700)
	if err != nil {
		return nil, errors.E(op, err)
	}
	db, err := wallet.CreateDB(driver, dbPath)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Initialize the watch-only database for the wallet before opening.
	err = wallet.CreateWatchOnly(db, extendedPubKey, pubPass, l.chainParams)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Open the watch-only wallet.
	so := l.stakeOptions
	cfg := &wallet.Config{
		DB:                      db,
		PubPassphrase:           pubPass,
		VotingEnabled:           so.VotingEnabled,
		AddressReuse:            so.AddressReuse,
		VotingAddress:           so.VotingAddress,
		PoolAddress:             so.PoolAddress,
		PoolFees:                so.PoolFees,
		TicketFee:               so.TicketFee,
		GapLimit:                l.gapLimit,
		AccountGapLimit:         l.accountGapLimit,
		DisableCoinTypeUpgrades: l.disableCoinTypeUpgrades,
		StakePoolColdExtKey:     so.StakePoolColdExtKey,
		AllowHighFees:           l.allowHighFees,
		RelayFee:                l.relayFee,
		Params:                  l.chainParams,
	}
	w, err = wallet.Open(cfg)
	if err != nil {
		return nil, errors.E(op, err)
	}

	l.onLoaded(w, db)
	return w, nil
}

// CreateNewWallet creates a new wallet using the provided public and private
// passphrases.  The seed is optional.  If non-nil, addresses are derived from
// this seed.  If nil, a secure random seed is generated.
func (l *Loader) CreateNewWallet(pubPassphrase, privPassphrase, seed []byte) (w *wallet.Wallet, err error) {
	const op errors.Op = "loader.CreateNewWallet"

	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, errors.E(op, errors.Exist, "wallet already opened")
	}

	// Ensure that the network directory exists.
	if fi, err := os.Stat(l.dbDirPath); err != nil {
		if os.IsNotExist(err) {
			// Attempt data directory creation
			if err = os.MkdirAll(l.dbDirPath, 0700); err != nil {
				return nil, errors.E(op, err)
			}
		} else {
			return nil, errors.E(op, err)
		}
	} else {
		if !fi.IsDir() {
			return nil, errors.E(op, errors.Errorf("%q is not a directory", l.dbDirPath))
		}
	}

	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return nil, errors.E(op, err)
	}
	if exists {
		return nil, errors.E(op, errors.Exist, "wallet DB exists")
	}

	// At this point it is asserted that there is no existing database file, and
	// deleting anything won't destroy a wallet in use.  Defer a function that
	// attempts to remove any written database file if this function errors.
	defer func() {
		if err != nil {
			_ = os.Remove(dbPath)
		}
	}()

	// Create the wallet database backed by bolt db.
	err = os.MkdirAll(l.dbDirPath, 0700)
	if err != nil {
		return nil, errors.E(op, err)
	}
	db, err := wallet.CreateDB(driver, dbPath)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Initialize the newly created database for the wallet before opening.
	err = wallet.Create(db, pubPassphrase, privPassphrase, seed, l.chainParams)
	if err != nil {
		return nil, errors.E(op, err)
	}

	// Open the newly-created wallet.
	so := l.stakeOptions
	cfg := &wallet.Config{
		DB:                      db,
		PubPassphrase:           pubPassphrase,
		VotingEnabled:           so.VotingEnabled,
		AddressReuse:            so.AddressReuse,
		VotingAddress:           so.VotingAddress,
		PoolAddress:             so.PoolAddress,
		PoolFees:                so.PoolFees,
		TicketFee:               so.TicketFee,
		GapLimit:                l.gapLimit,
		AccountGapLimit:         l.accountGapLimit,
		DisableCoinTypeUpgrades: l.disableCoinTypeUpgrades,
		StakePoolColdExtKey:     so.StakePoolColdExtKey,
		AllowHighFees:           l.allowHighFees,
		RelayFee:                l.relayFee,
		Params:                  l.chainParams,
	}
	w, err = wallet.Open(cfg)
	if err != nil {
		return nil, errors.E(op, err)
	}

	l.onLoaded(w, db)
	return w, nil
}

// OpenExistingWallet opens the wallet from the loader's wallet database path
// and the public passphrase.  If the loader is being called by a context where
// standard input prompts may be used during wallet upgrades, setting
// canConsolePrompt will enable these prompts.
func (l *Loader) OpenExistingWallet(pubPassphrase []byte) (w *wallet.Wallet, rerr error) {
	const op errors.Op = "loader.OpenExistingWallet"

	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, errors.E(op, errors.Exist, "wallet already opened")
	}

	// Open the database using the boltdb backend.
	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	l.mu.Unlock()
	db, err := wallet.OpenDB(driver, dbPath)
	l.mu.Lock()

	if err != nil {
		log.Errorf("Failed to open database: %v", err)
		return nil, errors.E(op, err)
	}
	// If this function does not return to completion the database must be
	// closed.  Otherwise, because the database is locked on opens, any
	// other attempts to open the wallet will hang, and there is no way to
	// recover since this db handle would be leaked.
	defer func() {
		if rerr != nil {
			db.Close()
		}
	}()

	so := l.stakeOptions
	cfg := &wallet.Config{
		DB:                      db,
		PubPassphrase:           pubPassphrase,
		VotingEnabled:           so.VotingEnabled,
		AddressReuse:            so.AddressReuse,
		VotingAddress:           so.VotingAddress,
		PoolAddress:             so.PoolAddress,
		PoolFees:                so.PoolFees,
		TicketFee:               so.TicketFee,
		GapLimit:                l.gapLimit,
		AccountGapLimit:         l.accountGapLimit,
		DisableCoinTypeUpgrades: l.disableCoinTypeUpgrades,
		StakePoolColdExtKey:     so.StakePoolColdExtKey,
		AllowHighFees:           l.allowHighFees,
		RelayFee:                l.relayFee,
		Params:                  l.chainParams,
	}
	w, err = wallet.Open(cfg)
	if err != nil {
		return nil, errors.E(op, err)
	}

	l.onLoaded(w, db)
	return w, nil
}

// DbDirPath returns the Loader's database directory path
func (l *Loader) DbDirPath() string {
	return l.dbDirPath
}

// WalletExists returns whether a file exists at the loader's database path.
// This may return an error for unexpected I/O failures.
func (l *Loader) WalletExists() (bool, error) {
	const op errors.Op = "loader.WalletExists"
	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return false, errors.E(op, err)
	}
	return exists, nil
}

// LoadedWallet returns the loaded wallet, if any, and a bool for whether the
// wallet has been loaded or not.  If true, the wallet pointer should be safe to
// dereference.
func (l *Loader) LoadedWallet() (*wallet.Wallet, bool) {
	l.mu.Lock()
	w := l.wallet
	l.mu.Unlock()
	return w, w != nil
}

// UnloadWallet stops the loaded wallet, if any, and closes the wallet database.
// Returns with errors.Invalid if the wallet has not been loaded with
// CreateNewWallet or LoadExistingWallet.  The Loader may be reused if this
// function returns without error.
func (l *Loader) UnloadWallet() error {
	const op errors.Op = "loader.UnloadWallet"

	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet == nil {
		return errors.E(op, errors.Invalid, "wallet is unopened")
	}

	err := l.db.Close()
	if err != nil {
		return errors.E(op, err)
	}

	l.wallet = nil
	l.db = nil
	return nil
}

// NetworkBackend returns the associated wallet network backend, if any, and a
// bool describing whether a non-nil network backend was set.
func (l *Loader) NetworkBackend() (n wallet.NetworkBackend, ok bool) {
	l.mu.Lock()
	if l.wallet != nil {
		n, _ = l.wallet.NetworkBackend()
	}
	l.mu.Unlock()
	return n, n != nil
}

func fileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
