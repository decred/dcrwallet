// Copyright (c) 2014-2015 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrutil/hdkeychain"
	"github.com/decred/dcrwallet/internal/prompt"
	"github.com/decred/dcrwallet/loader"
	"github.com/decred/dcrwallet/wallet"
	"github.com/decred/dcrwallet/walletdb"
	_ "github.com/decred/dcrwallet/walletdb/bdb"
	"github.com/decred/dcrwallet/walletseed"
)

// networkDir returns the directory name of a network directory to hold wallet
// files.
func networkDir(dataDir string, chainParams *chaincfg.Params) string {
	netname := chainParams.Name

	// For now, we must always name the testnet data directory as "testnet"
	// and not "testnet" or any other version, as the chaincfg testnet
	// paramaters will likely be switched to being named "testnet" in the
	// future.  This is done to future proof that change, and an upgrade
	// plan to move the testnet data directory can be worked out later.
	if chainParams.Net == wire.TestNet {
		netname = "testnet"
	}

	return filepath.Join(dataDir, netname)
}

// createWallet prompts the user for information needed to generate a new wallet
// and generates the wallet accordingly.  The new wallet will reside at the
// provided path. The bool passed back gives whether or not the wallet was
// restored from seed, while the []byte passed is the private password required
// to do the initial sync.
func createWallet(cfg *config) error {
	dbDir := networkDir(cfg.AppDataDir, activeNet.Params)
	stakeOptions := &loader.StakeOptions{
		VoteBits:                cfg.VoteBits,
		VoteBitsExtended:        cfg.VoteBitsExtended,
		TicketPurchasingEnabled: cfg.EnableStakeMining && !cfg.EnableTicketBuyer,
		VotingEnabled:           cfg.EnableVoting,
		BalanceToMaintain:       cfg.BalanceToMaintain.ToCoin(),
		PruneTickets:            cfg.PruneTickets,
		AddressReuse:            cfg.ReuseAddresses,
		TicketAddress:           cfg.TicketAddress,
		TicketMaxPrice:          cfg.tbCfg.MaxPriceAbsolute,
		TicketFee:               cfg.TicketFee.ToCoin(),
	}
	loader := loader.NewLoader(activeNet.Params, dbDir, stakeOptions,
		cfg.UnsafeMainNet, cfg.AddrIdxScanLen, cfg.AllowHighFees,
		cfg.RelayFee.ToCoin())

	reader := bufio.NewReader(os.Stdin)
	privPass, pubPass, seed, err := prompt.Setup(reader,
		[]byte(wallet.InsecurePubPassphrase), []byte(cfg.WalletPass))

	fmt.Println("Creating the wallet...")
	_, err = loader.CreateNewWallet(pubPass, privPass, seed)
	if err != nil {
		return err
	}

	fmt.Println("The wallet has been created successfully.")

	return nil
}

// createSimulationWallet is intended to be called from the rpcclient
// and used to create a wallet for actors involved in simulations.
func createSimulationWallet(cfg *config) error {
	// Simulation wallet password is 'password'.
	privPass := wallet.SimulationPassphrase

	// Public passphrase is the default.
	pubPass := []byte(wallet.InsecurePubPassphrase)

	// Generate a random seed.
	seed, err := hdkeychain.GenerateSeed(hdkeychain.RecommendedSeedLen)
	if err != nil {
		return err
	}

	netDir := networkDir(cfg.AppDataDir, activeNet.Params)

	// Write the seed to disk, so that we can restore it later
	// if need be, for testing purposes.
	seedStr := walletseed.EncodeMnemonic(seed)
	err = ioutil.WriteFile(filepath.Join(netDir, "seed"), []byte(seedStr), 0644)
	if err != nil {
		return err
	}

	// Create the wallet.
	dbPath := filepath.Join(netDir, walletDbName)
	fmt.Println("Creating the wallet...")

	// Create the wallet database backed by bolt db.
	db, err := walletdb.Create("bdb", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// Create the wallet.
	err = wallet.Create(db, pubPass, privPass, seed, activeNet.Params, cfg.UnsafeMainNet)
	if err != nil {
		return err
	}

	fmt.Println("The wallet has been created successfully.")
	return nil
}

// promptHDPublicKey prompts the user for an extended public key.
func promptHDPublicKey(reader *bufio.Reader) (string, error) {
	for {
		fmt.Print("Enter HD wallet public key: ")
		keyString, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}

		keyStringTrimmed := strings.TrimSpace(keyString)

		return keyStringTrimmed, nil
	}
}

// createWatchingOnlyWallet creates a watching only wallet using the passed
// extended public key.
func createWatchingOnlyWallet(cfg *config) error {
	// Get the public key.
	reader := bufio.NewReader(os.Stdin)
	pubKeyString, err := promptHDPublicKey(reader)
	if err != nil {
		return err
	}

	// Ask if the user wants to encrypt the wallet with a password.
	pubPass, err := prompt.PublicPass(reader, []byte{},
		[]byte(wallet.InsecurePubPassphrase), []byte(cfg.WalletPass))
	if err != nil {
		return err
	}

	netDir := networkDir(cfg.AppDataDir, activeNet.Params)

	// Create the wallet.
	dbPath := filepath.Join(netDir, walletDbName)
	fmt.Println("Creating the wallet...")

	// Create the wallet database backed by bolt db.
	db, err := walletdb.Create("bdb", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	err = wallet.CreateWatchOnly(db, pubKeyString, pubPass, activeNet.Params)
	if err != nil {
		errOS := os.Remove(dbPath)
		if errOS != nil {
			fmt.Println(errOS)
		}
		return err
	}

	fmt.Println("The watching only wallet has been created successfully.")
	return nil
}

// checkCreateDir checks that the path exists and is a directory.
// If path does not exist, it is created.
func checkCreateDir(path string) error {
	if fi, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// Attempt data directory creation
			if err = os.MkdirAll(path, 0700); err != nil {
				return fmt.Errorf("cannot create directory: %s", err)
			}
		} else {
			return fmt.Errorf("error checking directory: %s", err)
		}
	} else {
		if !fi.IsDir() {
			return fmt.Errorf("path '%s' is not a directory", path)
		}
	}

	return nil
}
