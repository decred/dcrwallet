// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// NOTE: This file is intended to house the RPC commands that are supported by
// a wallet server.

package types

import (
	"github.com/decred/dcrd/dcrjson/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types"
)

// Method describes the exact type used when registering methods with dcrjson.
type Method string

// AbandonTransactionCmd describes the command and parameters for performing the
// abandontransaction method.
type AbandonTransactionCmd struct {
	Hash string `json:"hash"`
}

// AccountAddressIndexCmd is a type handling custom marshaling and
// unmarshaling of accountaddressindex JSON wallet extension
// commands.
type AccountAddressIndexCmd struct {
	Account string `json:"account"`
	Branch  int    `json:"branch"`
}

// NewAccountAddressIndexCmd creates a new AccountAddressIndexCmd.
func NewAccountAddressIndexCmd(acct string, branch int) *AccountAddressIndexCmd {
	return &AccountAddressIndexCmd{
		Account: acct,
		Branch:  branch,
	}
}

// AccountSyncAddressIndexCmd is a type handling custom marshaling and
// unmarshaling of accountsyncaddressindex JSON wallet extension
// commands.
type AccountSyncAddressIndexCmd struct {
	Account string `json:"account"`
	Branch  int    `json:"branch"`
	Index   int    `json:"index"`
}

// NewAccountSyncAddressIndexCmd creates a new AccountSyncAddressIndexCmd.
func NewAccountSyncAddressIndexCmd(acct string, branch int,
	idx int) *AccountSyncAddressIndexCmd {
	return &AccountSyncAddressIndexCmd{
		Account: acct,
		Branch:  branch,
		Index:   idx,
	}
}

// AddMultisigAddressCmd defines the addmutisigaddress JSON-RPC command.
type AddMultisigAddressCmd struct {
	NRequired int
	Keys      []string
	Account   *string
}

// NewAddMultisigAddressCmd returns a new instance which can be used to issue a
// addmultisigaddress JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewAddMultisigAddressCmd(nRequired int, keys []string, account *string) *AddMultisigAddressCmd {
	return &AddMultisigAddressCmd{
		NRequired: nRequired,
		Keys:      keys,
		Account:   account,
	}
}

// AddTicketCmd forces a ticket into the wallet stake manager, for example if
// someone made an invalid ticket for a stake pool and the pool manager wanted
// to manually add it.
type AddTicketCmd struct {
	TicketHex string `json:"tickethex"`
}

// NewAddTicketCmd creates a new AddTicketCmd.
func NewAddTicketCmd(ticketHex string) *AddTicketCmd {
	return &AddTicketCmd{TicketHex: ticketHex}
}

// ConsolidateCmd is a type handling custom marshaling and
// unmarshaling of consolidate JSON wallet extension
// commands.
type ConsolidateCmd struct {
	Inputs  int `json:"inputs"`
	Account *string
	Address *string
}

// CreateEncryptedWalletCmd defines the createencryptedwallet JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
type CreateEncryptedWalletCmd struct {
	Passphrase string
}

// NewCreateEncryptedWalletCmd returns a new instance which can be used to issue
// a createencryptedwallet JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
func NewCreateEncryptedWalletCmd(passphrase string) *CreateEncryptedWalletCmd {
	return &CreateEncryptedWalletCmd{
		Passphrase: passphrase,
	}
}

// CreateMultiSigResult models the data returned from the createmultisig
// command.
type CreateMultiSigResult struct {
	Address      string `json:"address"`
	RedeemScript string `json:"redeemScript"`
}

// NewConsolidateCmd creates a new ConsolidateCmd.
func NewConsolidateCmd(inputs int, acct *string, addr *string) *ConsolidateCmd {
	return &ConsolidateCmd{Inputs: inputs, Account: acct, Address: addr}
}

// CreateMultisigCmd defines the createmultisig JSON-RPC command.
type CreateMultisigCmd struct {
	NRequired int
	Keys      []string
}

// NewCreateMultisigCmd returns a new instance which can be used to issue a
// createmultisig JSON-RPC command.
func NewCreateMultisigCmd(nRequired int, keys []string) *CreateMultisigCmd {
	return &CreateMultisigCmd{
		NRequired: nRequired,
		Keys:      keys,
	}
}

// CreateNewAccountCmd defines the createnewaccount JSON-RPC command.
type CreateNewAccountCmd struct {
	Account string
}

// NewCreateNewAccountCmd returns a new instance which can be used to issue a
// createnewaccount JSON-RPC command.
func NewCreateNewAccountCmd(account string) *CreateNewAccountCmd {
	return &CreateNewAccountCmd{
		Account: account,
	}
}

// CreateVotingAccountCmd is a type for handling custom marshaling and
// unmarshalling of createvotingaccount JSON-RPC command.
type CreateVotingAccountCmd struct {
	Name       string
	PubKey     string
	ChildIndex *uint32 `jsonrpcdefault:"0"`
}

// NewCreateVotingAccountCmd creates a new CreateVotingAccountCmd.
func NewCreateVotingAccountCmd(name, pubKey string, childIndex *uint32) *CreateVotingAccountCmd {
	return &CreateVotingAccountCmd{name, pubKey, childIndex}
}

// DropVotingAccountCmd is a type for handling custom marshaling and
// unmarshalling of dropvotingaccount JSON-RPC command
type DropVotingAccountCmd struct {
}

// NewDropVotingAccountCmd creates a new DropVotingAccountCmd.
func NewDropVotingAccountCmd() *DropVotingAccountCmd {
	return &DropVotingAccountCmd{}
}

// DumpPrivKeyCmd defines the dumpprivkey JSON-RPC command.
type DumpPrivKeyCmd struct {
	Address string
}

// NewDumpPrivKeyCmd returns a new instance which can be used to issue a
// dumpprivkey JSON-RPC command.
func NewDumpPrivKeyCmd(address string) *DumpPrivKeyCmd {
	return &DumpPrivKeyCmd{
		Address: address,
	}
}

// EstimatePriorityCmd defines the estimatepriority JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
type EstimatePriorityCmd struct {
	NumBlocks int64
}

// NewEstimatePriorityCmd returns a new instance which can be used to issue a
// estimatepriority JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
func NewEstimatePriorityCmd(numBlocks int64) *EstimatePriorityCmd {
	return &EstimatePriorityCmd{
		NumBlocks: numBlocks,
	}
}

// ExportWatchingWalletCmd defines the exportwatchingwallet JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
type ExportWatchingWalletCmd struct {
	Account  *string
	Download *bool `jsonrpcdefault:"false"`
}

// NewExportWatchingWalletCmd returns a new instance which can be used to issue
// a exportwatchingwallet JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
//
// Deprecated: This method is not implemented by the RPC server.
func NewExportWatchingWalletCmd(account *string, download *bool) *ExportWatchingWalletCmd {
	return &ExportWatchingWalletCmd{
		Account:  account,
		Download: download,
	}
}

// FundRawTransactionOptions represents the optional inputs to fund
// a raw transaction.
type FundRawTransactionOptions struct {
	ChangeAddress *string  `json:"changeaddress"`
	FeeRate       *float64 `json:"feerate"`
	ConfTarget    *int32   `json:"conf_target"`
}

// FundRawTransactionCmd is a type handling custom marshaling and
// unmarshaling of fundrawtransaction JSON wallet extension commands.
type FundRawTransactionCmd struct {
	HexString   string
	FundAccount string
	Options     *FundRawTransactionOptions
}

// NewFundRawTransactionCmd returns a new instance which can be used to issue a
// fundrawtransaction JSON-RPC command.
func NewFundRawTransactionCmd(hexString string, fundAccount string, options *FundRawTransactionOptions) *FundRawTransactionCmd {
	return &FundRawTransactionCmd{
		HexString:   hexString,
		FundAccount: fundAccount,
		Options:     options,
	}
}

// GenerateVoteCmd is a type handling custom marshaling and
// unmarshaling of generatevote JSON wallet extension commands.
type GenerateVoteCmd struct {
	BlockHash   string
	Height      int64
	TicketHash  string
	VoteBits    uint16
	VoteBitsExt string
}

// NewGenerateVoteCmd creates a new GenerateVoteCmd.
func NewGenerateVoteCmd(blockhash string, height int64, tickethash string, votebits uint16, voteBitsExt string) *GenerateVoteCmd {
	return &GenerateVoteCmd{
		BlockHash:   blockhash,
		Height:      height,
		TicketHash:  tickethash,
		VoteBits:    votebits,
		VoteBitsExt: voteBitsExt,
	}
}

// GetAccountCmd defines the getaccount JSON-RPC command.
type GetAccountCmd struct {
	Address string
}

// NewGetAccountCmd returns a new instance which can be used to issue a
// getaccount JSON-RPC command.
func NewGetAccountCmd(address string) *GetAccountCmd {
	return &GetAccountCmd{
		Address: address,
	}
}

// GetAccountAddressCmd defines the getaccountaddress JSON-RPC command.
type GetAccountAddressCmd struct {
	Account string
}

// NewGetAccountAddressCmd returns a new instance which can be used to issue a
// getaccountaddress JSON-RPC command.
func NewGetAccountAddressCmd(account string) *GetAccountAddressCmd {
	return &GetAccountAddressCmd{
		Account: account,
	}
}

// GetAddressesByAccountCmd defines the getaddressesbyaccount JSON-RPC command.
type GetAddressesByAccountCmd struct {
	Account string
}

// NewGetAddressesByAccountCmd returns a new instance which can be used to issue
// a getaddressesbyaccount JSON-RPC command.
func NewGetAddressesByAccountCmd(account string) *GetAddressesByAccountCmd {
	return &GetAddressesByAccountCmd{
		Account: account,
	}
}

// GetBalanceCmd defines the getbalance JSON-RPC command.
type GetBalanceCmd struct {
	Account *string
	MinConf *int `jsonrpcdefault:"1"`
}

// NewGetBalanceCmd returns a new instance which can be used to issue a
// getbalance JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetBalanceCmd(account *string, minConf *int) *GetBalanceCmd {
	return &GetBalanceCmd{
		Account: account,
		MinConf: minConf,
	}
}

// GetContractHashCmd defines the getcontracthash JSON-RPC command.
type GetContractHashCmd struct {
	FilePath []string
}

// NewGetContractHashCmd returns a new instance which can be used to issue a
// getcontracthash JSON-RPC command.
func NewGetContractHashCmd(filepaths []string) *GetContractHashCmd {
	return &GetContractHashCmd{FilePath: filepaths}
}

// GetMasterPubkeyCmd is a type handling custom marshaling and unmarshaling of
// getmasterpubkey JSON wallet extension commands.
type GetMasterPubkeyCmd struct {
	Account *string
}

// NewGetMasterPubkeyCmd creates a new GetMasterPubkeyCmd.
func NewGetMasterPubkeyCmd(acct *string) *GetMasterPubkeyCmd {
	return &GetMasterPubkeyCmd{Account: acct}
}

// GetMultisigOutInfoCmd is a type handling custom marshaling and
// unmarshaling of getmultisigoutinfo JSON websocket extension
// commands.
type GetMultisigOutInfoCmd struct {
	Hash  string
	Index uint32
}

// NewGetMultisigOutInfoCmd creates a new GetMultisigOutInfoCmd.
func NewGetMultisigOutInfoCmd(hash string, index uint32) *GetMultisigOutInfoCmd {
	return &GetMultisigOutInfoCmd{hash, index}
}

// GetNewAddressCmd defines the getnewaddress JSON-RPC command.
type GetNewAddressCmd struct {
	Account   *string
	GapPolicy *string
}

// NewGetNewAddressCmd returns a new instance which can be used to issue a
// getnewaddress JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetNewAddressCmd(account *string, gapPolicy *string) *GetNewAddressCmd {
	return &GetNewAddressCmd{
		Account:   account,
		GapPolicy: gapPolicy,
	}
}

// GetPayToContractAddressCmd defines the getpaytocontracthash JSON-RPC command.
type GetPayToContractAddressCmd struct {
	FilePath []string
}

// NewGetPayToContractAddressCmd returns a new instance which can be used to issue a
// getpaytocontractaddress JSON-RPC command.
func NewGetPayToContractAddressCmd(filepaths []string) *GetPayToContractAddressCmd {
	return &GetPayToContractAddressCmd{
		FilePath: filepaths,
	}
}

// GetRawChangeAddressCmd defines the getrawchangeaddress JSON-RPC command.
type GetRawChangeAddressCmd struct {
	Account *string
}

// NewGetRawChangeAddressCmd returns a new instance which can be used to issue a
// getrawchangeaddress JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetRawChangeAddressCmd(account *string) *GetRawChangeAddressCmd {
	return &GetRawChangeAddressCmd{
		Account: account,
	}
}

// GetReceivedByAccountCmd defines the getreceivedbyaccount JSON-RPC command.
type GetReceivedByAccountCmd struct {
	Account string
	MinConf *int `jsonrpcdefault:"1"`
}

// NewGetReceivedByAccountCmd returns a new instance which can be used to issue
// a getreceivedbyaccount JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetReceivedByAccountCmd(account string, minConf *int) *GetReceivedByAccountCmd {
	return &GetReceivedByAccountCmd{
		Account: account,
		MinConf: minConf,
	}
}

// GetReceivedByAddressCmd defines the getreceivedbyaddress JSON-RPC command.
type GetReceivedByAddressCmd struct {
	Address string
	MinConf *int `jsonrpcdefault:"1"`
}

// NewGetReceivedByAddressCmd returns a new instance which can be used to issue
// a getreceivedbyaddress JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetReceivedByAddressCmd(address string, minConf *int) *GetReceivedByAddressCmd {
	return &GetReceivedByAddressCmd{
		Address: address,
		MinConf: minConf,
	}
}

// GetStakeInfoCmd is a type handling custom marshaling and
// unmarshaling of getstakeinfo JSON wallet extension commands.
type GetStakeInfoCmd struct {
}

// NewGetStakeInfoCmd creates a new GetStakeInfoCmd.
func NewGetStakeInfoCmd() *GetStakeInfoCmd {
	return &GetStakeInfoCmd{}
}

// GetTicketFeeCmd is a type handling custom marshaling and
// unmarshaling of getticketfee JSON wallet extension
// commands.
type GetTicketFeeCmd struct {
}

// NewGetTicketFeeCmd creates a new GetTicketFeeCmd.
func NewGetTicketFeeCmd() *GetTicketFeeCmd {
	return &GetTicketFeeCmd{}
}

// GetTicketsCmd is a type handling custom marshaling and
// unmarshaling of gettickets JSON wallet extension
// commands.
type GetTicketsCmd struct {
	IncludeImmature bool
}

// NewGetTicketsCmd returns a new instance which can be used to issue a
// gettickets JSON-RPC command.
func NewGetTicketsCmd(includeImmature bool) *GetTicketsCmd {
	return &GetTicketsCmd{includeImmature}
}

// GetTransactionCmd defines the gettransaction JSON-RPC command.
type GetTransactionCmd struct {
	Txid             string
	IncludeWatchOnly *bool `jsonrpcdefault:"false"`
}

// NewGetTransactionCmd returns a new instance which can be used to issue a
// gettransaction JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewGetTransactionCmd(txHash string, includeWatchOnly *bool) *GetTransactionCmd {
	return &GetTransactionCmd{
		Txid:             txHash,
		IncludeWatchOnly: includeWatchOnly,
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

// GetVoteChoicesCmd returns a new instance which can be used to issue a
// getvotechoices JSON-RPC command.
type GetVoteChoicesCmd struct {
}

// NewGetVoteChoicesCmd returns a new instance which can be used to
// issue a JSON-RPC getvotechoices command.
func NewGetVoteChoicesCmd() *GetVoteChoicesCmd {
	return &GetVoteChoicesCmd{}
}

// GetWalletFeeCmd defines the getwalletfee JSON-RPC command.
type GetWalletFeeCmd struct{}

// NewGetWalletFeeCmd returns a new instance which can be used to issue a
// getwalletfee JSON-RPC command.
//
func NewGetWalletFeeCmd() *GetWalletFeeCmd {
	return &GetWalletFeeCmd{}
}

// ImportPrivKeyCmd defines the importprivkey JSON-RPC command.
type ImportPrivKeyCmd struct {
	PrivKey  string
	Label    *string
	Rescan   *bool `jsonrpcdefault:"true"`
	ScanFrom *int
}

// NewImportPrivKeyCmd returns a new instance which can be used to issue a
// importprivkey JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewImportPrivKeyCmd(privKey string, label *string, rescan *bool, scanFrom *int) *ImportPrivKeyCmd {
	return &ImportPrivKeyCmd{
		PrivKey:  privKey,
		Label:    label,
		Rescan:   rescan,
		ScanFrom: scanFrom,
	}
}

// ImportScriptCmd is a type for handling custom marshaling and
// unmarshaling of importscript JSON wallet extension commands.
type ImportScriptCmd struct {
	Hex      string
	Rescan   *bool `jsonrpcdefault:"true"`
	ScanFrom *int
}

// NewImportScriptCmd creates a new GetImportScriptCmd.
func NewImportScriptCmd(hex string, rescan *bool, scanFrom *int) *ImportScriptCmd {
	return &ImportScriptCmd{hex, rescan, scanFrom}
}

// ImportXpubCmd is a type for handling custom marshaling and unmarshaling of
// importxpub JSON-RPC commands.
type ImportXpubCmd struct {
	Name string
	Xpub string
}

// KeyPoolRefillCmd defines the keypoolrefill JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
type KeyPoolRefillCmd struct {
	NewSize *uint `jsonrpcdefault:"100"`
}

// NewKeyPoolRefillCmd returns a new instance which can be used to issue a
// keypoolrefill JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
//
// Deprecated: This method is not implemented by the RPC server.
func NewKeyPoolRefillCmd(newSize *uint) *KeyPoolRefillCmd {
	return &KeyPoolRefillCmd{
		NewSize: newSize,
	}
}

// ListAccountsCmd defines the listaccounts JSON-RPC command.
type ListAccountsCmd struct {
	MinConf *int `jsonrpcdefault:"1"`
}

// NewListAccountsCmd returns a new instance which can be used to issue a
// listaccounts JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListAccountsCmd(minConf *int) *ListAccountsCmd {
	return &ListAccountsCmd{
		MinConf: minConf,
	}
}

// ListTicketsCmd defines the listtickets JSON-RPC command.
type ListTicketsCmd struct {
}

// NewListTicketsCmd returns a new instance which can be used to issue a
// listtickets JSON-RPC command.
func NewListTicketsCmd() *ListTicketsCmd {
	return &ListTicketsCmd{}
}

// ListLockUnspentCmd defines the listlockunspent JSON-RPC command.
type ListLockUnspentCmd struct{}

// NewListLockUnspentCmd returns a new instance which can be used to issue a
// listlockunspent JSON-RPC command.
func NewListLockUnspentCmd() *ListLockUnspentCmd {
	return &ListLockUnspentCmd{}
}

// ListReceivedByAccountCmd defines the listreceivedbyaccount JSON-RPC command.
type ListReceivedByAccountCmd struct {
	MinConf          *int  `jsonrpcdefault:"1"`
	IncludeEmpty     *bool `jsonrpcdefault:"false"`
	IncludeWatchOnly *bool `jsonrpcdefault:"false"`
}

// NewListReceivedByAccountCmd returns a new instance which can be used to issue
// a listreceivedbyaccount JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListReceivedByAccountCmd(minConf *int, includeEmpty, includeWatchOnly *bool) *ListReceivedByAccountCmd {
	return &ListReceivedByAccountCmd{
		MinConf:          minConf,
		IncludeEmpty:     includeEmpty,
		IncludeWatchOnly: includeWatchOnly,
	}
}

// ListReceivedByAddressCmd defines the listreceivedbyaddress JSON-RPC command.
type ListReceivedByAddressCmd struct {
	MinConf          *int  `jsonrpcdefault:"1"`
	IncludeEmpty     *bool `jsonrpcdefault:"false"`
	IncludeWatchOnly *bool `jsonrpcdefault:"false"`
}

// NewListReceivedByAddressCmd returns a new instance which can be used to issue
// a listreceivedbyaddress JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListReceivedByAddressCmd(minConf *int, includeEmpty, includeWatchOnly *bool) *ListReceivedByAddressCmd {
	return &ListReceivedByAddressCmd{
		MinConf:          minConf,
		IncludeEmpty:     includeEmpty,
		IncludeWatchOnly: includeWatchOnly,
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

// ListScriptsCmd is a type for handling custom marshaling and
// unmarshaling of listscripts JSON wallet extension commands.
type ListScriptsCmd struct {
}

// NewListScriptsCmd returns a new instance which can be used to issue a
// listscripts JSON-RPC command.
func NewListScriptsCmd() *ListScriptsCmd {
	return &ListScriptsCmd{}
}

// ListSinceBlockCmd defines the listsinceblock JSON-RPC command.
type ListSinceBlockCmd struct {
	BlockHash           *string
	TargetConfirmations *int  `jsonrpcdefault:"1"`
	IncludeWatchOnly    *bool `jsonrpcdefault:"false"`
}

// NewListSinceBlockCmd returns a new instance which can be used to issue a
// listsinceblock JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListSinceBlockCmd(blockHash *string, targetConfirms *int, includeWatchOnly *bool) *ListSinceBlockCmd {
	return &ListSinceBlockCmd{
		BlockHash:           blockHash,
		TargetConfirmations: targetConfirms,
		IncludeWatchOnly:    includeWatchOnly,
	}
}

// ListTransactionsCmd defines the listtransactions JSON-RPC command.
type ListTransactionsCmd struct {
	Account          *string
	Count            *int  `jsonrpcdefault:"10"`
	From             *int  `jsonrpcdefault:"0"`
	IncludeWatchOnly *bool `jsonrpcdefault:"false"`
}

// NewListTransactionsCmd returns a new instance which can be used to issue a
// listtransactions JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListTransactionsCmd(account *string, count, from *int, includeWatchOnly *bool) *ListTransactionsCmd {
	return &ListTransactionsCmd{
		Account:          account,
		Count:            count,
		From:             from,
		IncludeWatchOnly: includeWatchOnly,
	}
}

// ListUnspentCmd defines the listunspent JSON-RPC command.
type ListUnspentCmd struct {
	MinConf   *int `jsonrpcdefault:"1"`
	MaxConf   *int `jsonrpcdefault:"9999999"`
	Addresses *[]string
}

// NewListUnspentCmd returns a new instance which can be used to issue a
// listunspent JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewListUnspentCmd(minConf, maxConf *int, addresses *[]string) *ListUnspentCmd {
	return &ListUnspentCmd{
		MinConf:   minConf,
		MaxConf:   maxConf,
		Addresses: addresses,
	}
}

// LockUnspentCmd defines the lockunspent JSON-RPC command.
type LockUnspentCmd struct {
	Unlock       bool
	Transactions []dcrdtypes.TransactionInput
}

// NewLockUnspentCmd returns a new instance which can be used to issue a
// lockunspent JSON-RPC command.
func NewLockUnspentCmd(unlock bool, transactions []dcrdtypes.TransactionInput) *LockUnspentCmd {
	return &LockUnspentCmd{
		Unlock:       unlock,
		Transactions: transactions,
	}
}

// PurchaseTicketCmd is a type handling custom marshaling and
// unmarshaling of purchaseticket JSON RPC commands.
type PurchaseTicketCmd struct {
	FromAccount   string
	SpendLimit    float64 // In Coins
	MinConf       *int    `jsonrpcdefault:"1"`
	TicketAddress *string
	NumTickets    *int
	PoolAddress   *string
	PoolFees      *float64
	Expiry        *int
	Comment       *string
	TicketFee     *float64
}

// NewPurchaseTicketCmd creates a new PurchaseTicketCmd.
func NewPurchaseTicketCmd(fromAccount string, spendLimit float64, minConf *int,
	ticketAddress *string, numTickets *int, poolAddress *string, poolFees *float64,
	expiry *int, comment *string, ticketFee *float64) *PurchaseTicketCmd {
	return &PurchaseTicketCmd{
		FromAccount:   fromAccount,
		SpendLimit:    spendLimit,
		MinConf:       minConf,
		TicketAddress: ticketAddress,
		NumTickets:    numTickets,
		PoolAddress:   poolAddress,
		PoolFees:      poolFees,
		Expiry:        expiry,
		Comment:       comment,
		TicketFee:     ticketFee,
	}
}

// RecoverAddressesCmd defines the recoveraddresses JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
type RecoverAddressesCmd struct {
	Account string
	N       int
}

// NewRecoverAddressesCmd returns a new instance which can be used to issue a
// recoveraddresses JSON-RPC command.
//
// Deprecated: This method is not implemented by the RPC server.
func NewRecoverAddressesCmd(account string, n int) *RecoverAddressesCmd {
	return &RecoverAddressesCmd{
		Account: account,
		N:       n,
	}
}

// RedeemMultiSigOutCmd is a type handling custom marshaling and
// unmarshaling of redeemmultisigout JSON RPC commands.
type RedeemMultiSigOutCmd struct {
	Hash    string
	Index   uint32
	Tree    int8
	Address *string
}

// NewRedeemMultiSigOutCmd creates a new RedeemMultiSigOutCmd.
func NewRedeemMultiSigOutCmd(hash string, index uint32, tree int8,
	address *string) *RedeemMultiSigOutCmd {
	return &RedeemMultiSigOutCmd{
		Hash:    hash,
		Index:   index,
		Tree:    tree,
		Address: address,
	}
}

// RedeemMultiSigOutsCmd is a type handling custom marshaling and
// unmarshaling of redeemmultisigout JSON RPC commands.
type RedeemMultiSigOutsCmd struct {
	FromScrAddress string
	ToAddress      *string
	Number         *int
}

// NewRedeemMultiSigOutsCmd creates a new RedeemMultiSigOutsCmd.
func NewRedeemMultiSigOutsCmd(from string, to *string,
	number *int) *RedeemMultiSigOutsCmd {
	return &RedeemMultiSigOutsCmd{
		FromScrAddress: from,
		ToAddress:      to,
		Number:         number,
	}
}

// RenameAccountCmd defines the renameaccount JSON-RPC command.
type RenameAccountCmd struct {
	OldAccount string
	NewAccount string
}

// NewRenameAccountCmd returns a new instance which can be used to issue a
// renameaccount JSON-RPC command.
func NewRenameAccountCmd(oldAccount, newAccount string) *RenameAccountCmd {
	return &RenameAccountCmd{
		OldAccount: oldAccount,
		NewAccount: newAccount,
	}
}

// RescanWalletCmd describes the rescanwallet JSON-RPC request and parameters.
type RescanWalletCmd struct {
	BeginHeight *int `jsonrpcdefault:"0"`
}

// RevokeTicketsCmd describes the revoketickets JSON-RPC request and parameters.
type RevokeTicketsCmd struct {
}

// NewRevokeTicketsCmd creates a new RevokeTicketsCmd.
func NewRevokeTicketsCmd() *RevokeTicketsCmd {
	return &RevokeTicketsCmd{}
}

// SendFromCmd defines the sendfrom JSON-RPC command.
type SendFromCmd struct {
	FromAccount string
	ToAddress   string
	Amount      float64 // In DCR
	MinConf     *int    `jsonrpcdefault:"1"`
	Comment     *string
	CommentTo   *string
}

// NewSendFromCmd returns a new instance which can be used to issue a sendfrom
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSendFromCmd(fromAccount, toAddress string, amount float64, minConf *int, comment, commentTo *string) *SendFromCmd {
	return &SendFromCmd{
		FromAccount: fromAccount,
		ToAddress:   toAddress,
		Amount:      amount,
		MinConf:     minConf,
		Comment:     comment,
		CommentTo:   commentTo,
	}
}

// SendManyCmd defines the sendmany JSON-RPC command.
type SendManyCmd struct {
	FromAccount string
	Amounts     map[string]float64 `jsonrpcusage:"{\"address\":amount,...}"` // In DCR
	MinConf     *int               `jsonrpcdefault:"1"`
	Comment     *string
}

// NewSendManyCmd returns a new instance which can be used to issue a sendmany
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSendManyCmd(fromAccount string, amounts map[string]float64, minConf *int, comment *string) *SendManyCmd {
	return &SendManyCmd{
		FromAccount: fromAccount,
		Amounts:     amounts,
		MinConf:     minConf,
		Comment:     comment,
	}
}

// SendToAddressCmd defines the sendtoaddress JSON-RPC command.
type SendToAddressCmd struct {
	Address   string
	Amount    float64
	Comment   *string
	CommentTo *string
}

// NewSendToAddressCmd returns a new instance which can be used to issue a
// sendtoaddress JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSendToAddressCmd(address string, amount float64, comment, commentTo *string) *SendToAddressCmd {
	return &SendToAddressCmd{
		Address:   address,
		Amount:    amount,
		Comment:   comment,
		CommentTo: commentTo,
	}
}

// SendToMultiSigCmd is a type handling custom marshaling and
// unmarshaling of sendtomultisig JSON RPC commands.
type SendToMultiSigCmd struct {
	FromAccount string
	Amount      float64
	Pubkeys     []string
	NRequired   *int `jsonrpcdefault:"1"`
	MinConf     *int `jsonrpcdefault:"1"`
	Comment     *string
}

// NewSendToMultiSigCmd creates a new SendToMultiSigCmd.
func NewSendToMultiSigCmd(fromaccount string, amount float64, pubkeys []string,
	nrequired *int, minConf *int, comment *string) *SendToMultiSigCmd {
	return &SendToMultiSigCmd{
		FromAccount: fromaccount,
		Amount:      amount,
		Pubkeys:     pubkeys,
		NRequired:   nrequired,
		MinConf:     minConf,
		Comment:     comment,
	}
}

// SetTxFeeCmd defines the settxfee JSON-RPC command.
type SetTxFeeCmd struct {
	Amount float64 // In DCR
}

// NewSetTxFeeCmd returns a new instance which can be used to issue a settxfee
// JSON-RPC command.
func NewSetTxFeeCmd(amount float64) *SetTxFeeCmd {
	return &SetTxFeeCmd{
		Amount: amount,
	}
}

// SetTicketFeeCmd is a type handling custom marshaling and
// unmarshaling of setticketfee JSON RPC commands.
type SetTicketFeeCmd struct {
	Fee float64
}

// NewSetTicketFeeCmd creates a new instance of the setticketfee
// command.
func NewSetTicketFeeCmd(fee float64) *SetTicketFeeCmd {
	return &SetTicketFeeCmd{
		Fee: fee,
	}
}

// SetVoteChoiceCmd defines the parameters to the setvotechoice method.
type SetVoteChoiceCmd struct {
	AgendaID string
	ChoiceID string
}

// NewSetVoteChoiceCmd returns a new instance which can be used to issue a
// setvotechoice JSON-RPC command.
func NewSetVoteChoiceCmd(agendaID, choiceID string) *SetVoteChoiceCmd {
	return &SetVoteChoiceCmd{AgendaID: agendaID, ChoiceID: choiceID}
}

// SignMessageCmd defines the signmessage JSON-RPC command.
type SignMessageCmd struct {
	Address string
	Message string
}

// NewSignMessageCmd returns a new instance which can be used to issue a
// signmessage JSON-RPC command.
func NewSignMessageCmd(address, message string) *SignMessageCmd {
	return &SignMessageCmd{
		Address: address,
		Message: message,
	}
}

// RawTxInput models the data needed for raw transaction input that is used in
// the SignRawTransactionCmd struct.  Contains Decred additions.
type RawTxInput struct {
	Txid         string `json:"txid"`
	Vout         uint32 `json:"vout"`
	Tree         int8   `json:"tree"`
	ScriptPubKey string `json:"scriptPubKey"`
	RedeemScript string `json:"redeemScript"`
}

// SignRawTransactionCmd defines the signrawtransaction JSON-RPC command.
type SignRawTransactionCmd struct {
	RawTx    string
	Inputs   *[]RawTxInput
	PrivKeys *[]string
	Flags    *string `jsonrpcdefault:"\"ALL\""`
}

// NewSignRawTransactionCmd returns a new instance which can be used to issue a
// signrawtransaction JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewSignRawTransactionCmd(hexEncodedTx string, inputs *[]RawTxInput, privKeys *[]string, flags *string) *SignRawTransactionCmd {
	return &SignRawTransactionCmd{
		RawTx:    hexEncodedTx,
		Inputs:   inputs,
		PrivKeys: privKeys,
		Flags:    flags,
	}
}

// SignRawTransactionsCmd defines the signrawtransactions JSON-RPC command.
type SignRawTransactionsCmd struct {
	RawTxs []string
	Send   *bool `jsonrpcdefault:"true"`
}

// NewSignRawTransactionsCmd returns a new instance which can be used to issue a
// signrawtransactions JSON-RPC command.
func NewSignRawTransactionsCmd(hexEncodedTxs []string,
	send *bool) *SignRawTransactionsCmd {
	return &SignRawTransactionsCmd{
		RawTxs: hexEncodedTxs,
		Send:   send,
	}
}

// StakePoolUserInfoCmd defines the stakepooluserinfo JSON-RPC command.
type StakePoolUserInfoCmd struct {
	User string
}

// NewStakePoolUserInfoCmd returns a new instance which can be used to issue a
// signrawtransactions JSON-RPC command.
func NewStakePoolUserInfoCmd(user string) *StakePoolUserInfoCmd {
	return &StakePoolUserInfoCmd{
		User: user,
	}
}

// StartAutoBuyerCmd is a type handling custom marshaling and
// unmarshaling of startautobuyer JSON RPC commands.
//
// Deprecated: This method is not implemented by the RPC server.
type StartAutoBuyerCmd struct {
	Account           string
	Passphrase        string
	BalanceToMaintain *int64
	MaxFeePerKb       *int64
	MaxPriceRelative  *float64
	MaxPriceAbsolute  *int64
	VotingAddress     *string
	PoolAddress       *string
	PoolFees          *float64
	MaxPerBlock       *int64
}

// NewStartAutoBuyerCmd creates a new StartAutoBuyerCmd.
//
// Deprecated: This method is not implemented by the RPC server.
func NewStartAutoBuyerCmd(account string, passphrase string, balanceToMaintain *int64, maxFeePerKb *int64, maxPriceRelative *float64, maxPriceAbsolute *int64, votingAddress *string, poolAddress *string, poolFees *float64,
	maxPerBlock *int64) *StartAutoBuyerCmd {
	return &StartAutoBuyerCmd{
		Account:           account,
		Passphrase:        passphrase,
		BalanceToMaintain: balanceToMaintain,
		MaxFeePerKb:       maxFeePerKb,
		MaxPriceRelative:  maxPriceRelative,
		MaxPriceAbsolute:  maxPriceAbsolute,
		VotingAddress:     votingAddress,
		PoolAddress:       poolAddress,
		PoolFees:          poolFees,
		MaxPerBlock:       maxPerBlock,
	}
}

// StopAutoBuyerCmd is a type handling custom marshaling and
// unmarshaling of stopautobuyer JSON RPC commands.
//
// Deprecated: This method is not implemented by the RPC server.
type StopAutoBuyerCmd struct {
}

// NewStopAutoBuyerCmd creates a new StopAutoBuyerCmd.
//
// Deprecated: This method is not implemented by the RPC server.
func NewStopAutoBuyerCmd() *StopAutoBuyerCmd {
	return &StopAutoBuyerCmd{}
}

// SweepAccountCmd defines the sweep account JSON-RPC command.
type SweepAccountCmd struct {
	SourceAccount         string
	DestinationAddress    string
	RequiredConfirmations *uint32
	FeePerKb              *float64
}

// NewSweepAccountCmd returns a new instance which can be used to issue a JSON-RPC SweepAccountCmd command.
func NewSweepAccountCmd(sourceAccount string, destinationAddress string, requiredConfs *uint32, feePerKb *float64) *SweepAccountCmd {
	return &SweepAccountCmd{
		SourceAccount:         sourceAccount,
		DestinationAddress:    destinationAddress,
		RequiredConfirmations: requiredConfs,
		FeePerKb:              feePerKb,
	}
}

// VerifySeedCmd defines the verifyseed JSON-RPC command.
type VerifySeedCmd struct {
	Seed    string
	Account *uint32
}

// NewVerifySeedCmd returns a new instance which can be used to issue a
// walletlock JSON-RPC command.
//
// The parameters which are pointers indicate that they are optional. Passing
// nil for the optional parameters will use the default value.
func NewVerifySeedCmd(seed string, account *uint32) *VerifySeedCmd {
	return &VerifySeedCmd{
		Seed:    seed,
		Account: account,
	}
}

// WalletInfoCmd defines the walletinfo JSON-RPC command.
type WalletInfoCmd struct {
}

// NewWalletInfoCmd returns a new instance which can be used to issue a
// walletinfo JSON-RPC command.
func NewWalletInfoCmd() *WalletInfoCmd {
	return &WalletInfoCmd{}
}

// WalletIsLockedCmd defines the walletislocked JSON-RPC command.
type WalletIsLockedCmd struct{}

// NewWalletIsLockedCmd returns a new instance which can be used to issue a
// walletislocked JSON-RPC command.
func NewWalletIsLockedCmd() *WalletIsLockedCmd {
	return &WalletIsLockedCmd{}
}

// WalletLockCmd defines the walletlock JSON-RPC command.
type WalletLockCmd struct{}

// NewWalletLockCmd returns a new instance which can be used to issue a
// walletlock JSON-RPC command.
func NewWalletLockCmd() *WalletLockCmd {
	return &WalletLockCmd{}
}

// WalletPassphraseCmd defines the walletpassphrase JSON-RPC command.
type WalletPassphraseCmd struct {
	Passphrase string
	Timeout    int64
}

// NewWalletPassphraseCmd returns a new instance which can be used to issue a
// walletpassphrase JSON-RPC command.
func NewWalletPassphraseCmd(passphrase string, timeout int64) *WalletPassphraseCmd {
	return &WalletPassphraseCmd{
		Passphrase: passphrase,
		Timeout:    timeout,
	}
}

// WalletPassphraseChangeCmd defines the walletpassphrase JSON-RPC command.
type WalletPassphraseChangeCmd struct {
	OldPassphrase string
	NewPassphrase string
}

// NewWalletPassphraseChangeCmd returns a new instance which can be used to
// issue a walletpassphrasechange JSON-RPC command.
func NewWalletPassphraseChangeCmd(oldPassphrase, newPassphrase string) *WalletPassphraseChangeCmd {
	return &WalletPassphraseChangeCmd{
		OldPassphrase: oldPassphrase,
		NewPassphrase: newPassphrase,
	}
}

type registeredMethod struct {
	method string
	cmd    interface{}
}

func init() {
	const dcrjsonv2WalletOnly = 1

	// Wallet-specific methods
	register := []registeredMethod{
		{"abandontransaction", (*AbandonTransactionCmd)(nil)},
		{"accountaddressindex", (*AccountAddressIndexCmd)(nil)},
		{"accountsyncaddressindex", (*AccountSyncAddressIndexCmd)(nil)},
		{"addmultisigaddress", (*AddMultisigAddressCmd)(nil)},
		{"addticket", (*AddTicketCmd)(nil)},
		{"consolidate", (*ConsolidateCmd)(nil)},
		{"createmultisig", (*CreateMultisigCmd)(nil)},
		{"createnewaccount", (*CreateNewAccountCmd)(nil)},
		{"createvotingaccount", (*CreateVotingAccountCmd)(nil)},
		{"dropvotingaccount", (*DropVotingAccountCmd)(nil)},
		{"dumpprivkey", (*DumpPrivKeyCmd)(nil)},
		{"fundrawtransaction", (*FundRawTransactionCmd)(nil)},
		{"generatevote", (*GenerateVoteCmd)(nil)},
		{"getaccount", (*GetAccountCmd)(nil)},
		{"getaccountaddress", (*GetAccountAddressCmd)(nil)},
		{"getaddressesbyaccount", (*GetAddressesByAccountCmd)(nil)},
		{"getbalance", (*GetBalanceCmd)(nil)},
		{"getcontracthash", (*GetContractHashCmd)(nil)},
		{"getmasterpubkey", (*GetMasterPubkeyCmd)(nil)},
		{"getmultisigoutinfo", (*GetMultisigOutInfoCmd)(nil)},
		{"getnewaddress", (*GetNewAddressCmd)(nil)},
		{"getpaytocontractaddress", (*GetPayToContractAddressCmd)(nil)},
		{"getrawchangeaddress", (*GetRawChangeAddressCmd)(nil)},
		{"getreceivedbyaccount", (*GetReceivedByAccountCmd)(nil)},
		{"getreceivedbyaddress", (*GetReceivedByAddressCmd)(nil)},
		{"getstakeinfo", (*GetStakeInfoCmd)(nil)},
		{"getticketfee", (*GetTicketFeeCmd)(nil)},
		{"gettickets", (*GetTicketsCmd)(nil)},
		{"gettransaction", (*GetTransactionCmd)(nil)},
		{"getunconfirmedbalance", (*GetUnconfirmedBalanceCmd)(nil)},
		{"getvotechoices", (*GetVoteChoicesCmd)(nil)},
		{"getwalletfee", (*GetWalletFeeCmd)(nil)},
		{"importprivkey", (*ImportPrivKeyCmd)(nil)},
		{"importscript", (*ImportScriptCmd)(nil)},
		{"importxpub", (*ImportXpubCmd)(nil)},
		{"listaccounts", (*ListAccountsCmd)(nil)},
		{"listaddresstransactions", (*ListAddressTransactionsCmd)(nil)},
		{"listalltransactions", (*ListAllTransactionsCmd)(nil)},
		{"listlockunspent", (*ListLockUnspentCmd)(nil)},
		{"listreceivedbyaccount", (*ListReceivedByAccountCmd)(nil)},
		{"listreceivedbyaddress", (*ListReceivedByAddressCmd)(nil)},
		{"listscripts", (*ListScriptsCmd)(nil)},
		{"listsinceblock", (*ListSinceBlockCmd)(nil)},
		{"listtickets", (*ListTicketsCmd)(nil)},
		{"listtransactions", (*ListTransactionsCmd)(nil)},
		{"listunspent", (*ListUnspentCmd)(nil)},
		{"lockunspent", (*LockUnspentCmd)(nil)},
		{"purchaseticket", (*PurchaseTicketCmd)(nil)},
		{"redeemmultisigout", (*RedeemMultiSigOutCmd)(nil)},
		{"redeemmultisigouts", (*RedeemMultiSigOutsCmd)(nil)},
		{"renameaccount", (*RenameAccountCmd)(nil)},
		{"rescanwallet", (*RescanWalletCmd)(nil)},
		{"revoketickets", (*RevokeTicketsCmd)(nil)},
		{"sendfrom", (*SendFromCmd)(nil)},
		{"sendmany", (*SendManyCmd)(nil)},
		{"sendtoaddress", (*SendToAddressCmd)(nil)},
		{"sendtomultisig", (*SendToMultiSigCmd)(nil)},
		{"settxfee", (*SetTxFeeCmd)(nil)},
		{"setticketfee", (*SetTicketFeeCmd)(nil)},
		{"setvotechoice", (*SetVoteChoiceCmd)(nil)},
		{"signmessage", (*SignMessageCmd)(nil)},
		{"signrawtransaction", (*SignRawTransactionCmd)(nil)},
		{"signrawtransactions", (*SignRawTransactionsCmd)(nil)},
		{"stakepooluserinfo", (*StakePoolUserInfoCmd)(nil)},
		{"sweepaccount", (*SweepAccountCmd)(nil)},
		{"verifyseed", (*VerifySeedCmd)(nil)},
		{"walletinfo", (*WalletInfoCmd)(nil)},
		{"walletislocked", (*WalletIsLockedCmd)(nil)},
		{"walletlock", (*WalletLockCmd)(nil)},
		{"walletpassphrase", (*WalletPassphraseCmd)(nil)},
		{"walletpassphrasechange", (*WalletPassphraseChangeCmd)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(register[i].method, register[i].cmd, dcrjsonv2WalletOnly)
		dcrjson.MustRegister(Method(register[i].method), register[i].cmd, 0)
	}

	// dcrd methods also implemented by dcrwallet
	register = []registeredMethod{
		{"authenticate", (*dcrdtypes.AuthenticateCmd)(nil)},
		{"getbestblock", (*dcrdtypes.GetBestBlockCmd)(nil)},
		{"getbestblockhash", (*dcrdtypes.GetBestBlockHashCmd)(nil)},
		{"getblockcount", (*dcrdtypes.GetBlockCountCmd)(nil)},
		{"getblockhash", (*dcrdtypes.GetBlockHashCmd)(nil)},
		{"getinfo", (*dcrdtypes.GetInfoCmd)(nil)},
		{"help", (*dcrdtypes.HelpCmd)(nil)},
		{"ticketsforaddress", (*dcrdtypes.TicketsForAddressCmd)(nil)},
		{"validateaddress", (*dcrdtypes.ValidateAddressCmd)(nil)},
		{"verifymessage", (*dcrdtypes.VerifyMessageCmd)(nil)},
		{"version", (*dcrdtypes.VersionCmd)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(Method(register[i].method), register[i].cmd, 0)
	}

	// Deprecated methods (only registered with plain string method)
	register = []registeredMethod{
		{"createencryptedwallet", (*CreateEncryptedWalletCmd)(nil)},
		{"estimatepriority", (*EstimatePriorityCmd)(nil)},
		{"exportwatchingwallet", (*ExportWatchingWalletCmd)(nil)},
		{"keypoolrefill", (*KeyPoolRefillCmd)(nil)},
		{"recoveraddresses", (*RecoverAddressesCmd)(nil)},
		{"startautobuyer", (*StartAutoBuyerCmd)(nil)},
		{"stopautobuyer", (*StopAutoBuyerCmd)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(register[i].method, register[i].cmd, dcrjsonv2WalletOnly)
	}
}
