// Copyright (c) 2015 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//+build !generate

package rpchelp

import (
	"decred.org/dcrwallet/rpc/jsonrpc/types"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
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
	{"addtransaction", nil},
	{"auditreuse", []interface{}{(*map[string][]string)(nil)}},
	{"consolidate", returnsString},
	{"createmultisig", []interface{}{(*types.CreateMultiSigResult)(nil)}},
	{"createnewaccount", nil},
	{"createrawtransaction", returnsString},
	{"createsignature", []interface{}{(*types.CreateSignatureResult)(nil)}},
	{"discoverusage", nil},
	{"dumpprivkey", returnsString},
	{"fundrawtransaction", []interface{}{(*types.FundRawTransactionResult)(nil)}},
	{"generatevote", []interface{}{(*types.GenerateVoteResult)(nil)}},
	{"getaccount", returnsString},
	{"getaccountaddress", returnsString},
	{"getaddressesbyaccount", returnsStringArray},
	{"getbalance", []interface{}{(*types.GetBalanceResult)(nil)}},
	{"getbestblock", []interface{}{(*dcrdtypes.GetBestBlockResult)(nil)}},
	{"getbestblockhash", returnsString},
	{"getblockcount", returnsNumber},
	{"getblockhash", returnsString},
	{"getcoinjoinsbyacct", []interface{}{(*map[string]uint32)(nil)}},
	{"getinfo", []interface{}{(*types.InfoWalletResult)(nil)}},
	{"getmasterpubkey", []interface{}{(*string)(nil)}},
	{"getmultisigoutinfo", []interface{}{(*types.GetMultisigOutInfoResult)(nil)}},
	{"getnewaddress", returnsString},
	{"getpeerinfo", []interface{}{(*types.GetPeerInfoResult)(nil)}},
	{"getrawchangeaddress", returnsString},
	{"getreceivedbyaccount", returnsNumber},
	{"getreceivedbyaddress", returnsNumber},
	{"getstakeinfo", []interface{}{(*types.GetStakeInfoResult)(nil)}},
	{"gettickets", []interface{}{(*types.GetTicketsResult)(nil)}},
	{"gettransaction", []interface{}{(*types.GetTransactionResult)(nil)}},
	{"getunconfirmedbalance", returnsNumber},
	{"getvotechoices", []interface{}{(*types.GetVoteChoicesResult)(nil)}},
	{"getwalletfee", returnsNumber},
	{"help", append(returnsString, returnsString[0])},
	{"importcfiltersv2", nil},
	{"importprivkey", nil},
	{"importscript", nil},
	{"importxpub", nil},
	{"listaccounts", []interface{}{(*map[string]float64)(nil)}},
	{"listaddresstransactions", returnsLTRArray},
	{"listalltransactions", returnsLTRArray},
	{"listlockunspent", []interface{}{(*[]dcrdtypes.TransactionInput)(nil)}},
	{"listreceivedbyaccount", []interface{}{(*[]types.ListReceivedByAccountResult)(nil)}},
	{"listreceivedbyaddress", []interface{}{(*[]types.ListReceivedByAddressResult)(nil)}},
	{"listsinceblock", []interface{}{(*types.ListSinceBlockResult)(nil)}},
	{"listtransactions", returnsLTRArray},
	{"listunspent", []interface{}{(*types.ListUnspentResult)(nil)}},
	{"lockaccount", nil},
	{"lockunspent", returnsBool},
	{"mixaccount", nil},
	{"mixoutput", nil},
	{"purchaseticket", returnsString},
	{"redeemmultisigout", []interface{}{(*types.RedeemMultiSigOutResult)(nil)}},
	{"redeemmultisigouts", []interface{}{(*types.RedeemMultiSigOutResult)(nil)}},
	{"renameaccount", nil},
	{"rescanwallet", nil},
	{"revoketickets", nil},
	{"sendfrom", returnsString},
	{"sendfromtreasury", returnsString},
	{"sendmany", returnsString},
	{"sendrawtransaction", returnsString},
	{"sendtoaddress", returnsString},
	{"sendtomultisig", returnsString},
	{"sendtotreasury", returnsString},
	{"setaccountpassphrase", nil},
	{"settreasurypolicy", nil},
	{"settspendpolicy", nil},
	{"settxfee", returnsBool},
	{"setvotechoice", nil},
	{"signmessage", returnsString},
	{"signrawtransaction", []interface{}{(*types.SignRawTransactionResult)(nil)}},
	{"signrawtransactions", []interface{}{(*types.SignRawTransactionsResult)(nil)}},
	{"stakepooluserinfo", []interface{}{(*types.StakePoolUserInfoResult)(nil)}},
	{"sweepaccount", []interface{}{(*types.SweepAccountResult)(nil)}},
	{"ticketinfo", []interface{}{(*[]types.TicketInfoResult)(nil)}},
	{"ticketsforaddress", returnsBool},
	{"treasurypolicy", []interface{}{(*[]types.TreasuryPolicyResult)(nil), (*types.TreasuryPolicyResult)(nil)}},
	{"tspendpolicy", []interface{}{(*[]types.TSpendPolicyResult)(nil), (*types.TSpendPolicyResult)(nil)}},
	{"unlockaccount", nil},
	{"validateaddress", []interface{}{(*types.ValidateAddressWalletResult)(nil)}},
	{"validatepredcp0005cf", returnsBool},
	{"verifymessage", returnsBool},
	{"version", []interface{}{(*map[string]dcrdtypes.VersionResult)(nil)}},
	{"walletinfo", []interface{}{(*types.WalletInfoResult)(nil)}},
	{"walletislocked", returnsBool},
	{"walletlock", nil},
	{"walletpassphrase", nil},
	{"walletpassphrasechange", nil},
	{"walletpubpassphrasechange", nil},
}

// HelpDescs contains the locale-specific help strings along with the locale.
var HelpDescs = []struct {
	Locale   string // Actual locale, e.g. en_US
	GoLocale string // Locale used in Go names, e.g. EnUS
	Descs    map[string]string
}{
	{"en_US", "EnUS", helpDescsEnUS}, // helpdescs_en_US.go
}
