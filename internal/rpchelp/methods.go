// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build !generate

package rpchelp

import (
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrwallet/rpc/jsonrpc/types"
)

// Common return types.
var (
	returnsBool        = []interface{}{(*bool)(nil)}
	returnsNumber      = []interface{}{(*float64)(nil)}
	returnsString      = []interface{}{(*string)(nil)}
	returnsStringArray = []interface{}{(*[]string)(nil)}
	returnsLTRArray    = []interface{}{(*[]types.ListTransactionsResult)(nil)}
)

// Methods contains all methods and result types that help is generated for,
// for every locale.
var Methods = []struct {
	Method      string
	ResultTypes []interface{}
}{
	{"abandontransaction", nil},
	{"accountaddressindex", []interface{}{(*int)(nil)}},
	{"accountsyncaddressindex", nil},
	{"addmultisigaddress", returnsString},
	{"addticket", nil},
	{"consolidate", returnsString},
	{"createmultisig", []interface{}{(*types.CreateMultiSigResult)(nil)}},
	{"createnewaccount", nil},
	{"dumpprivkey", returnsString},
	{"generatevote", []interface{}{(*types.GenerateVoteResult)(nil)}},
	{"getaccountaddress", returnsString},
	{"getaccount", returnsString},
	{"getaddressesbyaccount", returnsStringArray},
	{"getbalance", []interface{}{(*types.GetBalanceResult)(nil)}},
	{"getbestblockhash", returnsString},
	{"getbestblock", []interface{}{(*dcrdtypes.GetBestBlockResult)(nil)}},
	{"getblockcount", returnsNumber},
	{"getblockhash", returnsString},
	{"getinfo", []interface{}{(*types.InfoWalletResult)(nil)}},
	{"getmasterpubkey", []interface{}{(*string)(nil)}},
	{"getmultisigoutinfo", []interface{}{(*types.GetMultisigOutInfoResult)(nil)}},
	{"getnewaddress", returnsString},
	{"getrawchangeaddress", returnsString},
	{"getreceivedbyaccount", returnsNumber},
	{"getreceivedbyaddress", returnsNumber},
	{"getstakeinfo", []interface{}{(*types.GetStakeInfoResult)(nil)}},
	{"getticketfee", returnsNumber},
	{"gettickets", []interface{}{(*types.GetTicketsResult)(nil)}},
	{"gettransaction", []interface{}{(*types.GetTransactionResult)(nil)}},
	{"getunconfirmedbalance", returnsNumber},
	{"getvotechoices", []interface{}{(*types.GetVoteChoicesResult)(nil)}},
	{"getwalletfee", returnsNumber},
	{"help", append(returnsString, returnsString[0])},
	{"importprivkey", nil},
	{"importscript", nil},
	{"importxpub", nil},
	{"mixaccount", nil},
	{"mixoutput", nil},
	{"listaccounts", []interface{}{(*map[string]float64)(nil)}},
	{"listaddresstransactions", returnsLTRArray},
	{"listalltransactions", returnsLTRArray},
	{"listlockunspent", []interface{}{(*[]dcrdtypes.TransactionInput)(nil)}},
	{"listreceivedbyaccount", []interface{}{(*[]types.ListReceivedByAccountResult)(nil)}},
	{"listreceivedbyaddress", []interface{}{(*[]types.ListReceivedByAddressResult)(nil)}},
	{"listscripts", []interface{}{(*types.ListScriptsResult)(nil)}},
	{"listsinceblock", []interface{}{(*types.ListSinceBlockResult)(nil)}},
	{"listtransactions", returnsLTRArray},
	{"listunspent", []interface{}{(*types.ListUnspentResult)(nil)}},
	{"lockunspent", returnsBool},
	{"purchaseticket", returnsString},
	{"redeemmultisigout", []interface{}{(*types.RedeemMultiSigOutResult)(nil)}},
	{"redeemmultisigouts", []interface{}{(*types.RedeemMultiSigOutResult)(nil)}},
	{"renameaccount", nil},
	{"rescanwallet", nil},
	{"revoketickets", nil},
	{"sendfrom", returnsString},
	{"sendmany", returnsString},
	{"sendtoaddress", returnsString},
	{"sendtomultisig", returnsString},
	{"setticketfee", returnsBool},
	{"settxfee", returnsBool},
	{"setvotechoice", nil},
	{"signmessage", returnsString},
	{"signrawtransaction", []interface{}{(*types.SignRawTransactionResult)(nil)}},
	{"signrawtransactions", []interface{}{(*types.SignRawTransactionsResult)(nil)}},
	{"stakepooluserinfo", []interface{}{(*types.StakePoolUserInfoResult)(nil)}},
	{"sweepaccount", []interface{}{(*types.SweepAccountResult)(nil)}},
	{"ticketsforaddress", returnsBool},
	{"validateaddress", []interface{}{(*types.ValidateAddressWalletResult)(nil)}},
	{"verifymessage", returnsBool},
	{"version", []interface{}{(*map[string]dcrdtypes.VersionResult)(nil)}},
	{"walletinfo", []interface{}{(*types.WalletInfoResult)(nil)}},
	{"walletislocked", returnsBool},
	{"walletlock", nil},
	{"walletpassphrasechange", nil},
	{"walletpassphrase", nil},
}

// HelpDescs contains the locale-specific help strings along with the locale.
var HelpDescs = []struct {
	Locale   string // Actual locale, e.g. en_US
	GoLocale string // Locale used in Go names, e.g. EnUS
	Descs    map[string]string
}{
	{"en_US", "EnUS", helpDescsEnUS}, // helpdescs_en_US.go
}
