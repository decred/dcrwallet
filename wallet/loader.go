// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrwallet/internal/prompt"
	"github.com/decred/dcrwallet/waddrmgr"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb" // driver loaded during init
)

const (
	walletDbName = "wallet.db"
)

var (
	// ErrLoaded describes the error condition of attempting to load or
	// create a wallet when the loader has already done so.
	ErrLoaded = errors.New("wallet already loaded")

	// ErrNotLoaded describes the error condition of attempting to close a
	// loaded wallet when a wallet has not been loaded.
	ErrNotLoaded = errors.New("wallet is not loaded")

	// ErrExists describes the error condition of attempting to create a new
	// wallet when one exists already.
	ErrExists = errors.New("wallet already exists")
)

// Loader implements the creating of new and opening of existing wallets, while
// providing a callback system for other subsystems to handle the loading of a
// wallet.  This is primarely intended for use by the RPC servers, to enable
// methods and services which require the wallet when the wallet is loaded by
// another subsystem.
//
// Loader is safe for concurrent access.
type Loader struct {
	callbacks   []func(*Wallet)
	chainParams *chaincfg.Params
	dbDirPath   string
	wallet      *Wallet
	db          walletdb.DB
	mu          sync.Mutex

	stakeOptions   *StakeOptions
	autoRepair     bool
	unsafeMainNet  bool
	addrIdxScanLen int
	allowHighFees  bool
	relayFee       float64
}

// StakeOptions contains the various options necessary for stake mining.
type StakeOptions struct {
	VoteBits                uint16
	VoteBitsExtended        string
	TicketPurchasingEnabled bool
	VotingEnabled           bool
	BalanceToMaintain       float64
	TicketFee               float64
	PruneTickets            bool
	AddressReuse            bool
	TicketAddress           string
	TicketMaxPrice          float64
	TicketBuyFreq           int
	PoolAddress             string
	PoolFees                float64
	StakePoolColdExtKey     string
}

// NewLoader constructs a Loader.
func NewLoader(chainParams *chaincfg.Params, dbDirPath string,
	stakeOptions *StakeOptions, autoRepair bool, unsafeMainNet bool,
	addrIdxScanLen int, allowHighFees bool, relayFee float64) *Loader {
	return &Loader{
		chainParams:    chainParams,
		dbDirPath:      dbDirPath,
		stakeOptions:   stakeOptions,
		autoRepair:     autoRepair,
		unsafeMainNet:  unsafeMainNet,
		addrIdxScanLen: addrIdxScanLen,
		allowHighFees:  allowHighFees,
		relayFee:       relayFee,
	}
}

// onLoaded executes each added callback and prevents loader from loading any
// additional wallets.  Requires mutex to be locked.
func (l *Loader) onLoaded(w *Wallet, db walletdb.DB) {
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
func (l *Loader) RunAfterLoad(fn func(*Wallet)) {
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

// CreateNewWallet creates a new wallet using the provided public and private
// passphrases.  The seed is optional.  If non-nil, addresses are derived from
// this seed.  If nil, a secure random seed is generated.
func (l *Loader) CreateNewWallet(pubPassphrase, privPassphrase, seed []byte) (w *Wallet, err error) {
	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, ErrLoaded
	}

	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrExists
	}

	// Create the wallet database backed by bolt db.
	err = os.MkdirAll(l.dbDirPath, 0700)
	if err != nil {
		return nil, err
	}
	db, err := walletdb.Create("bdb", dbPath)
	if err != nil {
		return nil, err
	}
	// Attempt to remove database file if this function errors.
	defer func() {
		if err != nil {
			_ = os.Remove(dbPath)
		}
	}()

	// Initialize the newly created database for the wallet before opening.
	err = Create(db, pubPassphrase, privPassphrase, seed, l.chainParams,
		l.unsafeMainNet)
	if err != nil {
		return nil, err
	}

	// Open the newly-created wallet.
	so := l.stakeOptions
	w, err = Open(db, pubPassphrase, nil, so.VoteBits, so.VoteBitsExtended,
		so.TicketPurchasingEnabled, so.VotingEnabled, so.BalanceToMaintain,
		so.AddressReuse, so.PruneTickets, so.TicketAddress, so.TicketMaxPrice,
		so.TicketBuyFreq, so.PoolAddress, so.PoolFees, so.TicketFee,
		l.addrIdxScanLen, so.StakePoolColdExtKey, l.autoRepair,
		l.allowHighFees, l.relayFee, l.chainParams)
	if err != nil {
		return nil, err
	}
	w.Start()

	l.onLoaded(w, db)
	return w, nil
}

var errNoConsole = errors.New("db upgrade requires console access for additional input")

func noConsole() ([]byte, error) {
	return nil, errNoConsole
}

// OpenExistingWallet opens the wallet from the loader's wallet database path
// and the public passphrase.  If the loader is being called by a context where
// standard input prompts may be used during wallet upgrades, setting
// canConsolePrompt will enable these prompts.
func (l *Loader) OpenExistingWallet(pubPassphrase []byte, canConsolePrompt bool) (w *Wallet, rerr error) {
	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, ErrLoaded
	}

	// Ensure that the network directory exists.
	if err := checkCreateDir(l.dbDirPath); err != nil {
		return nil, err
	}

	// Open the database using the boltdb backend.
	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	db, err := walletdb.Open("bdb", dbPath)
	if err != nil {
		log.Errorf("Failed to open database: %v", err)
		return nil, err
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

	var cbs *waddrmgr.OpenCallbacks
	if canConsolePrompt {
		cbs = &waddrmgr.OpenCallbacks{
			ObtainSeed:        prompt.ProvideSeed,
			ObtainPrivatePass: prompt.ProvidePrivPassphrase,
		}
	} else {
		cbs = &waddrmgr.OpenCallbacks{
			ObtainSeed:        noConsole,
			ObtainPrivatePass: noConsole,
		}
	}
	so := l.stakeOptions
	w, err = Open(db, pubPassphrase, cbs, so.VoteBits, so.VoteBitsExtended,
		so.TicketPurchasingEnabled, so.VotingEnabled, so.BalanceToMaintain,
		so.AddressReuse, so.PruneTickets, so.TicketAddress, so.TicketMaxPrice,
		so.TicketBuyFreq, so.PoolAddress, so.PoolFees, so.TicketFee,
		l.addrIdxScanLen, so.StakePoolColdExtKey, l.autoRepair, l.allowHighFees,
		l.relayFee, l.chainParams)
	if err != nil {
		return nil, err
	}

	w.Start()
	l.onLoaded(w, db)
	return w, nil
}

// WalletExists returns whether a file exists at the loader's database path.
// This may return an error for unexpected I/O failures.
func (l *Loader) WalletExists() (bool, error) {
	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	return fileExists(dbPath)
}

// LoadedWallet returns the loaded wallet, if any, and a bool for whether the
// wallet has been loaded or not.  If true, the wallet pointer should be safe to
// dereference.
func (l *Loader) LoadedWallet() (*Wallet, bool) {
	l.mu.Lock()
	w := l.wallet
	l.mu.Unlock()
	return w, w != nil
}

// UnloadWallet stops the loaded wallet, if any, and closes the wallet database.
// This returns ErrNotLoaded if the wallet has not been loaded with
// CreateNewWallet or LoadExistingWallet.  The Loader may be reused if this
// function returns without error.
func (l *Loader) UnloadWallet() error {
	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet == nil {
		return ErrNotLoaded
	}

	l.wallet.Stop()
	l.wallet.WaitForShutdown()
	err := l.db.Close()
	if err != nil {
		return err
	}

	l.wallet = nil
	l.db = nil
	return nil
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
