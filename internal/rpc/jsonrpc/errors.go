// Copyright (c) 2013-2015 The btcsuite developers
// Copyright (c) 2016-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package jsonrpc

import (
	"fmt"

	"decred.org/dcrwallet/v5/errors"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/jrick/wsrpc/v2"
)

func convertError(err error) *dcrjson.RPCError {
	switch err := err.(type) {
	case *dcrjson.RPCError:
		return err
	case *wsrpc.Error:
		return &dcrjson.RPCError{
			Code:    dcrjson.RPCErrorCode(err.Code),
			Message: err.Message,
		}
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

func rpcErrorf(code dcrjson.RPCErrorCode, format string, args ...any) *dcrjson.RPCError {
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
		Message: "wallet or account locked; use walletpassphrase or unlockaccount first",
	}

	errReservedAccountName = &dcrjson.RPCError{
		Code:    dcrjson.ErrRPCInvalidParameter,
		Message: "account name is reserved by RPC server",
	}
)
