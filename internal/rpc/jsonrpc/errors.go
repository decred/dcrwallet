// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2016-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"fmt"

	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrwallet/errors/v2"
)

func convertError(err error) *dcrjson.RPCError {
	if err, ok := err.(*dcrjson.RPCError); ok {
		return err
	}

	code := dcrjson.ErrRPCWallet
	var kind errors.Kind
	if errors.As(err, &kind) {
		switch kind {
		case errors.Bug:
			code = dcrjson.ErrRPCInternal.Code
		case errors.Encoding:
			code = dcrjson.ErrRPCInvalidParameter
		case errors.Locked:
			code = dcrjson.ErrRPCWalletUnlockNeeded
		case errors.Passphrase:
			code = dcrjson.ErrRPCWalletPassphraseIncorrect
		case errors.NoPeers:
			code = dcrjson.ErrRPCClientNotConnected
		case errors.InsufficientBalance:
			code = dcrjson.ErrRPCWalletInsufficientFunds
		}
	}
	return &dcrjson.RPCError{
		Code:    code,
		Message: err.Error(),
	}
}

func rpcError(code dcrjson.RPCErrorCode, err error) *dcrjson.RPCError {
	return &dcrjson.RPCError{
		Code:    code,
		Message: err.Error(),
	}
}

func rpcErrorf(code dcrjson.RPCErrorCode, format string, args ...interface{}) *dcrjson.RPCError {
	return &dcrjson.RPCError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Errors variables that are defined once here to avoid duplication.
var (
	errUnloadedWallet = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCWallet,
		Message: "request requires a wallet but wallet has not loaded yet",
	}

	errRPCClientNotConnected = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCClientNotConnected,
		Message: "disconnected from consensus RPC",
	}

	errNoNetwork = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCClientNotConnected,
		Message: "disconnected from network",
	}

	errAccountNotFound = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCWalletInvalidAccountName,
		Message: "account not found",
	}

	errAddressNotInWallet = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCWallet,
		Message: "address not found in wallet",
	}

	errNotImportedAccount = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCWallet,
		Message: "imported addresses must belong to the imported account",
	}

	errNeedPositiveAmount = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCInvalidParameter,
		Message: "amount must be positive",
	}

	errWalletUnlockNeeded = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCWalletUnlockNeeded,
		Message: "enter the wallet passphrase with walletpassphrase first",
	}

	errReservedAccountName = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCInvalidParameter,
		Message: "account name is reserved by RPC server",
	}
)
