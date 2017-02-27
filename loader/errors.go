// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package loader

import "errors"

var (
	// ErrWalletLoaded describes the error condition of attempting to load or
	// create a wallet when the loader has already done so.
	ErrWalletLoaded = errors.New("wallet already loaded")

	// ErrWalletNotLoaded describes the error condition of attempting to close
	// a loaded wallet when a wallet has not been loaded.
	ErrWalletNotLoaded = errors.New("wallet is not loaded")

	// ErrWalletExists describes the error condition of attempting to create a
	// new wallet when one exists already.
	ErrWalletExists = errors.New("wallet already exists")

	// ErrTicketBuyerStarted describes the error condition of attempting to
	// start ticketbuyer when it is already started.
	ErrTicketBuyerStarted = errors.New("ticketbuyer already started")

	// ErrTicketBuyerStopped describes the error condition of attempting to
	// stop ticketbuyer when it is already stopped.
	ErrTicketBuyerStopped = errors.New("ticketbuyer not running")

	// ErrNoChainClient describes the error condition of attempting to start
	// ticketbuyer when no chain client is available.
	ErrNoChainClient = errors.New("chain client not available")
)
