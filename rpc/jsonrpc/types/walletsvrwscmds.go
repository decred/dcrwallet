// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import dcrjson "github.com/decred/dcrd/dcrjson/v2"

// NOTE: This file is intended to house the RPC commands that are supported by
// a wallet server, but are only available via websockets.

// CreateEncryptedWalletCmd defines the createencryptedwallet JSON-RPC command.
type CreateEncryptedWalletCmd struct {
	Passphrase string
}

// NewCreateEncryptedWalletCmd returns a new instance which can be used to issue
// a createencryptedwallet JSON-RPC command.
func NewCreateEncryptedWalletCmd(passphrase string) *CreateEncryptedWalletCmd {
	return &CreateEncryptedWalletCmd{
		Passphrase: passphrase,
	}
}

// ExportWatchingWalletCmd defines the exportwatchingwallet JSON-RPC command.
type ExportWatchingWalletCmd struct {
	Account  *string
	Download *bool `jsonrpcdefault:"false"`
}

// NewExportWatchingWalletCmd returns a new instance which can be used to issue
// a exportwatchingwallet JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewExportWatchingWalletCmd(account *string, download *bool) *ExportWatchingWalletCmd {
	return &ExportWatchingWalletCmd{
		Account:  account,
		Download: download,
	}
}

// GetUnconfirmedBalanceCmd defines the getunconfirmedbalance JSON-RPC command.
type GetUnconfirmedBalanceCmd struct {
	Account *string
}

// NewGetUnconfirmedBalanceCmd returns a new instance which can be used to issue
// a getunconfirmedbalance JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetUnconfirmedBalanceCmd(account *string) *GetUnconfirmedBalanceCmd {
	return &GetUnconfirmedBalanceCmd{
		Account: account,
	}
}

// ListAddressTransactionsCmd defines the listaddresstransactions JSON-RPC
// command.
type ListAddressTransactionsCmd struct {
	Addresses []string
	Account   *string
}

// NewListAddressTransactionsCmd returns a new instance which can be used to
// issue a listaddresstransactions JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListAddressTransactionsCmd(addresses []string, account *string) *ListAddressTransactionsCmd {
	return &ListAddressTransactionsCmd{
		Addresses: addresses,
		Account:   account,
	}
}

// ListAllTransactionsCmd defines the listalltransactions JSON-RPC command.
type ListAllTransactionsCmd struct {
	Account *string
}

// NewListAllTransactionsCmd returns a new instance which can be used to issue a
// listalltransactions JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListAllTransactionsCmd(account *string) *ListAllTransactionsCmd {
	return &ListAllTransactionsCmd{
		Account: account,
	}
}

// RecoverAddressesCmd defines the recoveraddresses JSON-RPC command.
type RecoverAddressesCmd struct {
	Account string
	N       int
}

// NewRecoverAddressesCmd returns a new instance which can be used to issue a
// recoveraddresses JSON-RPC command.
func NewRecoverAddressesCmd(account string, n int) *RecoverAddressesCmd {
	return &RecoverAddressesCmd{
		Account: account,
		N:       n,
	}
}

// WalletIsLockedCmd defines the walletislocked JSON-RPC command.
type WalletIsLockedCmd struct{}

// NewWalletIsLockedCmd returns a new instance which can be used to issue a
// walletislocked JSON-RPC command.
func NewWalletIsLockedCmd() *WalletIsLockedCmd {
	return &WalletIsLockedCmd{}
}

func init() {
	// The commands in this file are only usable with a wallet server via
	// websockets.
	flags := dcrjson.UFWalletOnly | dcrjson.UFWebsocketOnly

	dcrjson.MustRegisterCmd("createencryptedwallet", (*CreateEncryptedWalletCmd)(nil), flags)
	dcrjson.MustRegisterCmd("exportwatchingwallet", (*ExportWatchingWalletCmd)(nil), flags)
	dcrjson.MustRegisterCmd("getunconfirmedbalance", (*GetUnconfirmedBalanceCmd)(nil), flags)
	dcrjson.MustRegisterCmd("listaddresstransactions", (*ListAddressTransactionsCmd)(nil), flags)
	dcrjson.MustRegisterCmd("listalltransactions", (*ListAllTransactionsCmd)(nil), flags)
	dcrjson.MustRegisterCmd("recoveraddresses", (*RecoverAddressesCmd)(nil), flags)
	dcrjson.MustRegisterCmd("walletislocked", (*WalletIsLockedCmd)(nil), flags)
}
