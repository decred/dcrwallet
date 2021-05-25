// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// NOTE: This file is intended to house the RPC commands that are supported by
// a wallet server.

package types

import (
	"github.com/decred/dcrd/dcrjson/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
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

// AddTransactionCmd manually adds a single mined transaction to the wallet,
// which may be useful to add a transaction which was mined before a private
// key was imported.
// There is currently no validation that the transaction is indeed mined in
// this block.
type AddTransactionCmd struct {
	BlockHash   string `json:"blockhash"`
	Transaction string `json:"transaction"`
}

// AuditReuseCmd defines the auditreuse JSON-RPC command.
//
// This method returns an object keying reused addresses to two or more outputs
// referencing them, optionally filtering results of address reusage since a
// particular block height.
type AuditReuseCmd struct {
	Since *int32 `json:"since"`
}

// ConsolidateCmd is a type handling custom marshaling and
// unmarshaling of consolidate JSON wallet extension
// commands.
type ConsolidateCmd struct {
	Inputs  int `json:"inputs"`
	Account *string
	Address *string
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

// CreateSignatureCmd defines the createsignature JSON-RPC command.
type CreateSignatureCmd struct {
	Address               string
	InputIndex            int
	HashType              int
	PreviousPkScript      string
	SerializedTransaction string
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
	TicketHash *string
}

// NewGetVoteChoicesCmd returns a new instance which can be used to
// issue a JSON-RPC getvotechoices command.
func NewGetVoteChoicesCmd(tickethash *string) *GetVoteChoicesCmd {
	return &GetVoteChoicesCmd{
		TicketHash: tickethash,
	}
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
	Name string `json:"name"`
	Xpub string `json:"xpub"`
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

// ListLockUnspentCmd defines the listlockunspent JSON-RPC command.
type ListLockUnspentCmd struct {
	Account *string
}

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
	Account   *string
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
	NumTickets    *int `jsonrpcdefault:"1"`
	PoolAddress   *string
	PoolFees      *float64
	Expiry        *int
	Comment       *string
	DontSignTx    *bool
}

// NewPurchaseTicketCmd creates a new PurchaseTicketCmd.
func NewPurchaseTicketCmd(fromAccount string, spendLimit float64, minConf *int,
	ticketAddress *string, numTickets *int, poolAddress *string, poolFees *float64,
	expiry *int, comment *string) *PurchaseTicketCmd {
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
	}
}

// CreateUnsignedTicketResult is returned from PurchaseTicketCmd
// when dontSignTx is true.
type CreateUnsignedTicketResult struct {
	UnsignedTickets []string `json:"unsignedtickets"`
	SplitTx         string   `json:"splittx"`
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

// SendToTreasuryCmd defines the sendtotreasury JSON-RPC command.
type SendToTreasuryCmd struct {
	Amount float64
}

// NewSendToTreasurymd returns a new instance which can be used to issue a
// sendtotreasury JSON-RPC command.
func NewSendToTreasuryCmd(amount float64, comment, commentTo *string) *SendToTreasuryCmd {
	return &SendToTreasuryCmd{
		Amount: amount,
	}
}

// SendFromTreasuryCmd defines the sendfromtreasury JSON-RPC command.
type SendFromTreasuryCmd struct {
	Key     string
	Amounts map[string]float64
}

// NewSendFromTreasurymd returns a new instance which can be used to issue a
// sendfromtreasury JSON-RPC command.
func NewSendFromTreasuryCmd(pubkey string, amounts map[string]float64) *SendFromTreasuryCmd {
	return &SendFromTreasuryCmd{
		Key:     pubkey,
		Amounts: amounts,
	}
}

// TreasuryPolicyCmd defines the parameters for the treasurypolicy JSON-RPC
// command.
type TreasuryPolicyCmd struct {
	Key *string
}

// SetTreasuryPolicyCmd defines the parameters for the settreasurypolicy
// JSON-RPC command.
type SetTreasuryPolicyCmd struct {
	Key    string
	Policy string
}

// TSpendPolicyCmd defines the parameters for the tspendpolicy JSON-RPC
// command.
type TSpendPolicyCmd struct {
	Hash *string
}

// SetTSpendPolicyCmd defines the parameters for the settspendpolicy
// JSON-RPC command.
type SetTSpendPolicyCmd struct {
	Hash   string
	Policy string
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

// SetVoteChoiceCmd defines the parameters to the setvotechoice method.
type SetVoteChoiceCmd struct {
	AgendaID   string
	ChoiceID   string
	TicketHash *string
}

// NewSetVoteChoiceCmd returns a new instance which can be used to issue a
// setvotechoice JSON-RPC command.
func NewSetVoteChoiceCmd(agendaID, choiceID string, tickethash *string) *SetVoteChoiceCmd {
	return &SetVoteChoiceCmd{AgendaID: agendaID, ChoiceID: choiceID, TicketHash: tickethash}
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

// SyncStatusCmd defines the syncstatus JSON-RPC command.
type SyncStatusCmd struct{}

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

// MixAccountCmd defines the mixaccount JSON-RPC command.
type MixAccountCmd struct{}

// MixOutputCmd defines the mixoutput JSON-RPC command.
type MixOutputCmd struct {
	Outpoint string `json:"outpoint"`
}

// DiscoverUsageCmd defines the discoverusage JSON-RPC command.
type DiscoverUsageCmd struct {
	StartBlock       *string `json:"startblock"`
	DiscoverAccounts *bool   `json:"discoveraccounts"`
	GapLimit         *uint32 `json:"gaplimit"`
}

// ValidatePreDCP0005CFCmd defines the validatepredcp0005cf JSON-RPC command.
type ValidatePreDCP0005CFCmd struct{}

// ImportCfiltersV2Cmd defines the importcfiltersv2 JSON-RPC command.
type ImportCFiltersV2Cmd struct {
	StartHeight int32    `json:"startheight"`
	Filters     []string `json:"filters"`
}

// TicketInfoCmd defines the ticketinfo JSON-RPC command.
type TicketInfoCmd struct {
	StartHeight *int32 `json:"startheight" jsonrpcdefault:"0"`
}

// WalletPubPassphraseChangeCmd defines the walletpubpassphrasechange JSON-RPC command.
type WalletPubPassphraseChangeCmd struct {
	OldPassphrase string
	NewPassphrase string
}

// SetAccountPassphraseCmd defines the setaccountpassphrase JSON-RPC command
// arguments.
type SetAccountPassphraseCmd struct {
	Account    string
	Passphrase string
}

// UnlockAccountCmd defines the unlockaccount JSON-RPC command arguments.
type UnlockAccountCmd struct {
	Account    string
	Passphrase string
}

// LockAccountCmd defines the lockaccount JSON-RPC command arguments.
type LockAccountCmd struct {
	Account string
}

// AccountUnlockedCmd defines the accountunlocked JSON-RPC command arguments.
type AccountUnlockedCmd struct {
	Account string
}

type registeredMethod struct {
	method string
	cmd    interface{}
}

type GetCoinjoinsByAcctCmd struct{}

func init() {
	// Wallet-specific methods
	register := []registeredMethod{
		{"abandontransaction", (*AbandonTransactionCmd)(nil)},
		{"accountaddressindex", (*AccountAddressIndexCmd)(nil)},
		{"accountsyncaddressindex", (*AccountSyncAddressIndexCmd)(nil)},
		{"accountunlocked", (*AccountUnlockedCmd)(nil)},
		{"addmultisigaddress", (*AddMultisigAddressCmd)(nil)},
		{"addtransaction", (*AddTransactionCmd)(nil)},
		{"auditreuse", (*AuditReuseCmd)(nil)},
		{"consolidate", (*ConsolidateCmd)(nil)},
		{"createmultisig", (*CreateMultisigCmd)(nil)},
		{"createnewaccount", (*CreateNewAccountCmd)(nil)},
		{"createsignature", (*CreateSignatureCmd)(nil)},
		{"createvotingaccount", (*CreateVotingAccountCmd)(nil)},
		{"discoverusage", (*DiscoverUsageCmd)(nil)},
		{"dumpprivkey", (*DumpPrivKeyCmd)(nil)},
		{"fundrawtransaction", (*FundRawTransactionCmd)(nil)},
		{"generatevote", (*GenerateVoteCmd)(nil)},
		{"getaccount", (*GetAccountCmd)(nil)},
		{"getaccountaddress", (*GetAccountAddressCmd)(nil)},
		{"getaddressesbyaccount", (*GetAddressesByAccountCmd)(nil)},
		{"getbalance", (*GetBalanceCmd)(nil)},
		{"getcoinjoinsbyacct", (*GetCoinjoinsByAcctCmd)(nil)},
		{"getmasterpubkey", (*GetMasterPubkeyCmd)(nil)},
		{"getmultisigoutinfo", (*GetMultisigOutInfoCmd)(nil)},
		{"getnewaddress", (*GetNewAddressCmd)(nil)},
		{"getrawchangeaddress", (*GetRawChangeAddressCmd)(nil)},
		{"getreceivedbyaccount", (*GetReceivedByAccountCmd)(nil)},
		{"getreceivedbyaddress", (*GetReceivedByAddressCmd)(nil)},
		{"getstakeinfo", (*GetStakeInfoCmd)(nil)},
		{"gettickets", (*GetTicketsCmd)(nil)},
		{"gettransaction", (*GetTransactionCmd)(nil)},
		{"getunconfirmedbalance", (*GetUnconfirmedBalanceCmd)(nil)},
		{"getvotechoices", (*GetVoteChoicesCmd)(nil)},
		{"getwalletfee", (*GetWalletFeeCmd)(nil)},
		{"importcfiltersv2", (*ImportCFiltersV2Cmd)(nil)},
		{"importprivkey", (*ImportPrivKeyCmd)(nil)},
		{"importscript", (*ImportScriptCmd)(nil)},
		{"importxpub", (*ImportXpubCmd)(nil)},
		{"listaccounts", (*ListAccountsCmd)(nil)},
		{"listaddresstransactions", (*ListAddressTransactionsCmd)(nil)},
		{"listalltransactions", (*ListAllTransactionsCmd)(nil)},
		{"listlockunspent", (*ListLockUnspentCmd)(nil)},
		{"listreceivedbyaccount", (*ListReceivedByAccountCmd)(nil)},
		{"listreceivedbyaddress", (*ListReceivedByAddressCmd)(nil)},
		{"listsinceblock", (*ListSinceBlockCmd)(nil)},
		{"listtransactions", (*ListTransactionsCmd)(nil)},
		{"listunspent", (*ListUnspentCmd)(nil)},
		{"lockaccount", (*LockAccountCmd)(nil)},
		{"lockunspent", (*LockUnspentCmd)(nil)},
		{"mixaccount", (*MixAccountCmd)(nil)},
		{"mixoutput", (*MixOutputCmd)(nil)},
		{"purchaseticket", (*PurchaseTicketCmd)(nil)},
		{"redeemmultisigout", (*RedeemMultiSigOutCmd)(nil)},
		{"redeemmultisigouts", (*RedeemMultiSigOutsCmd)(nil)},
		{"renameaccount", (*RenameAccountCmd)(nil)},
		{"rescanwallet", (*RescanWalletCmd)(nil)},
		{"revoketickets", (*RevokeTicketsCmd)(nil)},
		{"sendfrom", (*SendFromCmd)(nil)},
		{"sendfromtreasury", (*SendFromTreasuryCmd)(nil)},
		{"sendmany", (*SendManyCmd)(nil)},
		{"sendtoaddress", (*SendToAddressCmd)(nil)},
		{"sendtomultisig", (*SendToMultiSigCmd)(nil)},
		{"sendtotreasury", (*SendToTreasuryCmd)(nil)},
		{"setaccountpassphrase", (*SetAccountPassphraseCmd)(nil)},
		{"settreasurypolicy", (*SetTreasuryPolicyCmd)(nil)},
		{"settspendpolicy", (*SetTSpendPolicyCmd)(nil)},
		{"settxfee", (*SetTxFeeCmd)(nil)},
		{"setvotechoice", (*SetVoteChoiceCmd)(nil)},
		{"signmessage", (*SignMessageCmd)(nil)},
		{"signrawtransaction", (*SignRawTransactionCmd)(nil)},
		{"signrawtransactions", (*SignRawTransactionsCmd)(nil)},
		{"stakepooluserinfo", (*StakePoolUserInfoCmd)(nil)},
		{"sweepaccount", (*SweepAccountCmd)(nil)},
		{"syncstatus", (*SyncStatusCmd)(nil)},
		{"ticketinfo", (*TicketInfoCmd)(nil)},
		{"treasurypolicy", (*TreasuryPolicyCmd)(nil)},
		{"tspendpolicy", (*TSpendPolicyCmd)(nil)},
		{"unlockaccount", (*UnlockAccountCmd)(nil)},
		{"validatepredcp0005cf", (*ValidatePreDCP0005CFCmd)(nil)},
		{"walletinfo", (*WalletInfoCmd)(nil)},
		{"walletislocked", (*WalletIsLockedCmd)(nil)},
		{"walletlock", (*WalletLockCmd)(nil)},
		{"walletpassphrase", (*WalletPassphraseCmd)(nil)},
		{"walletpassphrasechange", (*WalletPassphraseChangeCmd)(nil)},
		{"walletpubpassphrasechange", (*WalletPubPassphraseChangeCmd)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(Method(register[i].method), register[i].cmd, 0)
	}

	// dcrd methods also implemented by dcrwallet
	register = []registeredMethod{
		{"createrawtransaction", (*CreateRawTransactionCmd)(nil)},
		{"getbestblock", (*GetBestBlockCmd)(nil)},
		{"getbestblockhash", (*GetBestBlockHashCmd)(nil)},
		{"getblockcount", (*GetBlockCountCmd)(nil)},
		{"getblockhash", (*GetBlockHashCmd)(nil)},
		{"getblock", (*GetBlockCmd)(nil)},
		{"getcfilterv2", (*GetCFilterV2Cmd)(nil)},
		{"getinfo", (*GetInfoCmd)(nil)},
		{"getpeerinfo", (*GetPeerInfoCmd)(nil)},
		{"gettxout", (*GetTxOutCmd)(nil)},
		{"help", (*HelpCmd)(nil)},
		{"sendrawtransaction", (*SendRawTransactionCmd)(nil)},
		{"ticketsforaddress", (*TicketsForAddressCmd)(nil)},
		{"validateaddress", (*ValidateAddressCmd)(nil)},
		{"verifymessage", (*VerifyMessageCmd)(nil)},
		{"version", (*VersionCmd)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(Method(register[i].method), register[i].cmd, 0)
	}

	// Websocket-specific methods implemented by dcrwallet
	register = []registeredMethod{
		{"authenticate", (*AuthenticateCmd)(nil)},
	}
	for i := range register {
		dcrjson.MustRegister(Method(register[i].method), register[i].cmd,
			dcrjson.UFWebsocketOnly)
	}
}

// newtype definitions of dcrd commands we implement.
type (
	AuthenticateCmd         dcrdtypes.AuthenticateCmd
	CreateRawTransactionCmd dcrdtypes.CreateRawTransactionCmd
	GetBestBlockCmd         dcrdtypes.GetBestBlockCmd
	GetBestBlockHashCmd     dcrdtypes.GetBestBlockHashCmd
	GetBlockCountCmd        dcrdtypes.GetBlockCountCmd
	GetBlockHashCmd         dcrdtypes.GetBlockHashCmd
	GetBlockCmd             dcrdtypes.GetBlockCmd
	GetCFilterV2Cmd         dcrdtypes.GetCFilterV2Cmd
	GetInfoCmd              dcrdtypes.GetInfoCmd
	GetPeerInfoCmd          dcrdtypes.GetPeerInfoCmd
	GetTxOutCmd             dcrdtypes.GetTxOutCmd
	HelpCmd                 dcrdtypes.HelpCmd
	SendRawTransactionCmd   dcrdtypes.SendRawTransactionCmd
	TicketsForAddressCmd    dcrdtypes.TicketsForAddressCmd
	ValidateAddressCmd      dcrdtypes.ValidateAddressCmd
	VerifyMessageCmd        dcrdtypes.VerifyMessageCmd
	VersionCmd              dcrdtypes.VersionCmd
)
