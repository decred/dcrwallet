// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// NOTE: This file is intended to house the RPC websocket notifications that are
// supported by a wallet server.

package types

import "github.com/decred/dcrd/dcrjson/v3"

const (
	// AccountBalanceNtfnMethod is the method used for account balance
	// notifications.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	AccountBalanceNtfnMethod = "accountbalance"

	// DcrdConnectedNtfnMethod is the method used for notifications when
	// a wallet server is connected to a chain server.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	DcrdConnectedNtfnMethod = "dcrdconnected"

	// NewTicketsNtfnMethod is the method of the daemon
	// newtickets notification.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	NewTicketsNtfnMethod = "newtickets"

	// NewTxNtfnMethod is the method used to notify that a wallet server has
	// added a new transaction to the transaction store.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	NewTxNtfnMethod = "newtx"

	// RevocationCreatedNtfnMethod is the method of the dcrwallet
	// revocationcreated notification.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	RevocationCreatedNtfnMethod = "revocationcreated"

	// TicketPurchasedNtfnMethod is the method of the dcrwallet
	// ticketpurchased notification.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	TicketPurchasedNtfnMethod = "ticketpurchased"

	// VoteCreatedNtfnMethod is the method of the dcrwallet
	// votecreated notification.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	VoteCreatedNtfnMethod = "votecreated"

	// WinningTicketsNtfnMethod is the method of the daemon
	// winningtickets notification.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	WinningTicketsNtfnMethod = "winningtickets"

	// WalletLockStateNtfnMethod is the method used to notify the lock state
	// of a wallet has changed.
	//
	// Deprecated: dcrwallet does not provide JSON-RPC notifications
	WalletLockStateNtfnMethod = "walletlockstate"
)

// AccountBalanceNtfn defines the accountbalance JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type AccountBalanceNtfn struct {
	Account   string
	Balance   float64 // In DCR
	Confirmed bool    // Whether Balance is confirmed or unconfirmed.
}

// NewAccountBalanceNtfn returns a new instance which can be used to issue an
// accountbalance JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewAccountBalanceNtfn(account string, balance float64, confirmed bool) *AccountBalanceNtfn {
	return &AccountBalanceNtfn{
		Account:   account,
		Balance:   balance,
		Confirmed: confirmed,
	}
}

// DcrdConnectedNtfn defines the dcrddconnected JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type DcrdConnectedNtfn struct {
	Connected bool
}

// NewDcrdConnectedNtfn returns a new instance which can be used to issue a
// dcrddconnected JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewDcrdConnectedNtfn(connected bool) *DcrdConnectedNtfn {
	return &DcrdConnectedNtfn{
		Connected: connected,
	}
}

// NewTxNtfn defines the newtx JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type NewTxNtfn struct {
	Account string
	Details ListTransactionsResult
}

// NewNewTxNtfn returns a new instance which can be used to issue a newtx
// JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewNewTxNtfn(account string, details ListTransactionsResult) *NewTxNtfn {
	return &NewTxNtfn{
		Account: account,
		Details: details,
	}
}

// TicketPurchasedNtfn is a type handling custom marshaling and
// unmarshaling of ticketpurchased JSON websocket notifications.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type TicketPurchasedNtfn struct {
	TxHash string
	Amount int64 // SStx only
}

// NewTicketPurchasedNtfn creates a new TicketPurchasedNtfn.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewTicketPurchasedNtfn(txHash string, amount int64) *TicketPurchasedNtfn {
	return &TicketPurchasedNtfn{
		TxHash: txHash,
		Amount: amount,
	}
}

// RevocationCreatedNtfn is a type handling custom marshaling and
// unmarshaling of ticketpurchased JSON websocket notifications.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type RevocationCreatedNtfn struct {
	TxHash string
	SStxIn string
}

// NewRevocationCreatedNtfn creates a new RevocationCreatedNtfn.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewRevocationCreatedNtfn(txHash string, sstxIn string) *RevocationCreatedNtfn {
	return &RevocationCreatedNtfn{
		TxHash: txHash,
		SStxIn: sstxIn,
	}
}

// VoteCreatedNtfn is a type handling custom marshaling and
// unmarshaling of ticketpurchased JSON websocket notifications.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type VoteCreatedNtfn struct {
	TxHash    string
	BlockHash string
	Height    int32
	SStxIn    string
	VoteBits  uint16
}

// NewVoteCreatedNtfn creates a new VoteCreatedNtfn.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewVoteCreatedNtfn(txHash string, blockHash string, height int32, sstxIn string, voteBits uint16) *VoteCreatedNtfn {
	return &VoteCreatedNtfn{
		TxHash:    txHash,
		BlockHash: blockHash,
		Height:    height,
		SStxIn:    sstxIn,
		VoteBits:  voteBits,
	}
}

// WalletLockStateNtfn defines the walletlockstate JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
type WalletLockStateNtfn struct {
	Locked bool
}

// NewWalletLockStateNtfn returns a new instance which can be used to issue a
// walletlockstate JSON-RPC notification.
//
// Deprecated: dcrwallet does not provide JSON-RPC notifications
func NewWalletLockStateNtfn(locked bool) *WalletLockStateNtfn {
	return &WalletLockStateNtfn{
		Locked: locked,
	}
}

func init() {
	const dcrjsonv2WalletOnly = 1
	const flags = dcrjsonv2WalletOnly | dcrjson.UFWebsocketOnly | dcrjson.UFNotification

	// Deprecated notifications (only registered with plain string method)
	register := []registeredMethod{
		{AccountBalanceNtfnMethod, (*AccountBalanceNtfn)(nil)},
		{DcrdConnectedNtfnMethod, (*DcrdConnectedNtfn)(nil)},
		{NewTxNtfnMethod, (*NewTxNtfn)(nil)},
		{TicketPurchasedNtfnMethod, (*TicketPurchasedNtfn)(nil)},
		{RevocationCreatedNtfnMethod, (*RevocationCreatedNtfn)(nil)},
		{VoteCreatedNtfnMethod, (*VoteCreatedNtfn)(nil)},
		{WalletLockStateNtfnMethod, (*WalletLockStateNtfn)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(register[i].method, register[i].cmd, flags)
	}
}
