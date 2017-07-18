// Copyright (c) 2015-2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package loader

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrrpcclient"
	"github.com/decred/dcrwallet/ticketbuyer"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb" // driver loaded during init
)

const (
	walletDbName = "wallet.db"
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
	chainClient *dcrrpcclient.Client
	chainParams *chaincfg.Params
	dbDirPath   string
	wallet      *wallet.Wallet
	db          walletdb.DB
	mu          sync.Mutex

	purchaseManager *ticketbuyer.PurchaseManager
	ntfnClient      wallet.MainTipChangedNotificationsClient
	stakeOptions    *StakeOptions
	addrIdxScanLen  int
	allowHighFees   bool
	relayFee        float64
}

// StakeOptions contains the various options necessary for stake mining.
type StakeOptions struct {
	TicketPurchasingEnabled bool
	VotingEnabled           bool
	TicketFee               float64
	AddressReuse            bool
	TicketAddress           string
	PoolAddress             string
	PoolFees                float64
	StakePoolColdExtKey     string
}

// NewLoader constructs a Loader.
func NewLoader(chainParams *chaincfg.Params, dbDirPath string, stakeOptions *StakeOptions, addrIdxScanLen int,
	allowHighFees bool, relayFee float64) *Loader {

	return &Loader{
		chainParams:    chainParams,
		dbDirPath:      dbDirPath,
		stakeOptions:   stakeOptions,
		addrIdxScanLen: addrIdxScanLen,
		allowHighFees:  allowHighFees,
		relayFee:       relayFee,
	}
}

// onLoaded executes each added callback and prevents loader from loading any
// additional wallets.  Requires mutex to be locked.
func (l *Loader) onLoaded(w *wallet.Wallet, db walletdb.DB) {
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

// CreateNewWallet creates a new wallet using the provided public and private
// passphrases.  The seed is optional.  If non-nil, addresses are derived from
// this seed.  If nil, a secure random seed is generated.
func (l *Loader) CreateNewWallet(pubPassphrase, privPassphrase, seed []byte) (w *wallet.Wallet, err error) {
	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, ErrWalletLoaded
	}

	// Ensure that the network directory exists.
	if fi, err := os.Stat(l.dbDirPath); err != nil {
		if os.IsNotExist(err) {
			// Attempt data directory creation
			if err = os.MkdirAll(l.dbDirPath, 0700); err != nil {
				return nil, fmt.Errorf("cannot create directory: %s", err)
			}
		} else {
			return nil, fmt.Errorf("error checking directory: %s", err)
		}
	} else {
		if !fi.IsDir() {
			return nil, fmt.Errorf("path '%s' is not a directory", l.dbDirPath)
		}
	}

	dbPath := filepath.Join(l.dbDirPath, walletDbName)
	exists, err := fileExists(dbPath)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrWalletExists
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
	err = wallet.Create(db, pubPassphrase, privPassphrase, seed, l.chainParams)
	if err != nil {
		return nil, err
	}

	// Open the newly-created wallet.
	so := l.stakeOptions
	w, err = wallet.Open(db, pubPassphrase, so.VotingEnabled, so.AddressReuse,
		so.TicketAddress, so.PoolAddress, so.PoolFees, so.TicketFee,
		l.addrIdxScanLen, so.StakePoolColdExtKey, l.allowHighFees,
		l.relayFee, l.chainParams)
	if err != nil {
		return nil, err
	}
	w.Start()

	l.onLoaded(w, db)
	return w, nil
}

// OpenExistingWallet opens the wallet from the loader's wallet database path
// and the public passphrase.  If the loader is being called by a context where
// standard input prompts may be used during wallet upgrades, setting
// canConsolePrompt will enable these prompts.
func (l *Loader) OpenExistingWallet(pubPassphrase []byte) (w *wallet.Wallet, rerr error) {
	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet != nil {
		return nil, ErrWalletLoaded
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

	so := l.stakeOptions
	w, err = wallet.Open(db, pubPassphrase, so.VotingEnabled, so.AddressReuse,
		so.TicketAddress, so.PoolAddress, so.PoolFees, so.TicketFee,
		l.addrIdxScanLen, so.StakePoolColdExtKey, l.allowHighFees,
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
func (l *Loader) LoadedWallet() (*wallet.Wallet, bool) {
	l.mu.Lock()
	w := l.wallet
	l.mu.Unlock()
	return w, w != nil
}

// UnloadWallet stops the loaded wallet, if any, and closes the wallet
// database.  This returns ErrWalletNotLoaded if the wallet has not been loaded
// with CreateNewWallet or LoadExistingWallet.  The Loader may be reused if
// this function returns without error.
func (l *Loader) UnloadWallet() error {
	defer l.mu.Unlock()
	l.mu.Lock()

	if l.wallet == nil {
		return ErrWalletNotLoaded
	}

	// Ignore err already stopped.
	l.stopTicketPurchase()

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

// SetChainClient sets the chain server client.
func (l *Loader) SetChainClient(chainClient *dcrrpcclient.Client) {
	l.mu.Lock()
	l.chainClient = chainClient
	l.mu.Unlock()
}

// StartTicketPurchase launches the ticketbuyer to start purchasing tickets.
func (l *Loader) StartTicketPurchase(passphrase []byte, ticketbuyerCfg *ticketbuyer.Config) error {
	defer l.mu.Unlock()
	l.mu.Lock()

	// Already running?
	if l.purchaseManager != nil {
		return ErrTicketBuyerStarted
	}

	if l.wallet == nil {
		return ErrWalletNotLoaded
	}

	if l.chainClient == nil {
		return ErrNoChainClient
	}

	w := l.wallet
	p, err := ticketbuyer.NewTicketPurchaser(ticketbuyerCfg, l.chainClient, w, l.chainParams)
	if err != nil {
		return err
	}
	n := w.NtfnServer.MainTipChangedNotifications()
	pm := ticketbuyer.NewPurchaseManager(w, p, n.C, passphrase)
	l.ntfnClient = n
	l.purchaseManager = pm
	pm.Start()
	l.wallet.SetTicketPurchasingEnabled(true)
	return nil
}

// stopTicketPurchase stops the ticket purchaser, waiting until it has finished.
// It must be called with the mutex lock held.
func (l *Loader) stopTicketPurchase() error {
	if l.purchaseManager == nil {
		return ErrTicketBuyerStopped
	}

	l.ntfnClient.Done()
	l.purchaseManager.Stop()
	l.purchaseManager.WaitForShutdown()
	l.purchaseManager = nil
	l.wallet.SetTicketPurchasingEnabled(false)
	return nil
}

// StopTicketPurchase stops the ticket purchaser, waiting until it has finished.
// If no ticket purchaser is running, it returns ErrTicketBuyerStopped.
func (l *Loader) StopTicketPurchase() error {
	defer l.mu.Unlock()
	l.mu.Lock()

	return l.stopTicketPurchase()
}

// PurchaseManager returns the ticket purchaser instance. If ticket purchasing
// has been disabled, it returns nil.
func (l *Loader) PurchaseManager() *ticketbuyer.PurchaseManager {
	l.mu.Lock()
	pm := l.purchaseManager
	l.mu.Unlock()
	return pm
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
