// Copyright (c) 2014-2016 The btcsuite developers
// Copyright (c) 2015-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrwallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"

	"decred.org/dcrwallet/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/hdkeychain/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/wire"
)

// GetTransaction returns detailed information about a wallet transaction.
func (c *Client) GetTransaction(ctx context.Context, txHash *chainhash.Hash) (*types.GetTransactionResult, error) {
	res := new(types.GetTransactionResult)
	err := c.Call(ctx, "gettransaction", res, txHash.String())
	return res, err
}

// ListTransactions returns a list of the most recent transactions.
//
// See the ListTransactionsCount and ListTransactionsCountFrom to control the
// number of transactions returned and starting point, respectively.
func (c *Client) ListTransactions(ctx context.Context, account string) ([]types.ListTransactionsResult, error) {
	var res []types.ListTransactionsResult
	err := c.Call(ctx, "listtransactions", &res, account)
	return res, err
}

// ListTransactionsCount returns a list of the most recent transactions up
// to the passed count.
//
// See the ListTransactions and ListTransactionsCountFrom functions for
// different options.
func (c *Client) ListTransactionsCount(ctx context.Context, account string, count int) ([]types.ListTransactionsResult, error) {
	var res []types.ListTransactionsResult
	err := c.Call(ctx, "listtransactions", &res, account, count)
	return res, err
}

// ListTransactionsCountFrom returns a list of the most recent transactions up
// to the passed count while skipping the first 'from' transactions.
//
// See the ListTransactions and ListTransactionsCount functions to use defaults.
func (c *Client) ListTransactionsCountFrom(ctx context.Context, account string, count, from int) ([]types.ListTransactionsResult, error) {
	var res []types.ListTransactionsResult
	err := c.Call(ctx, "listtransactions", &res, account, count, from)
	return res, err
}

// ListUnspent returns all unspent transaction outputs known to a wallet, using
// the default number of minimum and maximum number of confirmations as a
// filter (1 and 9999999, respectively).
func (c *Client) ListUnspent(ctx context.Context) ([]types.ListUnspentResult, error) {
	var res []types.ListUnspentResult
	err := c.Call(ctx, "listunspent", &res)
	return res, err
}

// ListUnspentMin returns all unspent transaction outputs known to a wallet,
// using the specified number of minimum conformations and default number of
// maximum confirmations (9999999) as a filter.
func (c *Client) ListUnspentMin(ctx context.Context, minConf int) ([]types.ListUnspentResult, error) {
	var res []types.ListUnspentResult
	err := c.Call(ctx, "listunspent", &res, minConf)
	return res, err
}

// ListUnspentMinMax returns all unspent transaction outputs known to a wallet,
// using the specified number of minimum and maximum number of confirmations as
// a filter.
func (c *Client) ListUnspentMinMax(ctx context.Context, minConf, maxConf int) ([]types.ListUnspentResult, error) {
	var res []types.ListUnspentResult
	err := c.Call(ctx, "listunspent", &res, minConf, maxConf)
	return res, err
}

// ListUnspentMinMaxAddresses returns all unspent transaction outputs that pay
// to any of specified addresses in a wallet using the specified number of
// minimum and maximum number of confirmations as a filter.
func (c *Client) ListUnspentMinMaxAddresses(ctx context.Context, minConf, maxConf int, addrs []dcrutil.Address) ([]types.ListUnspentResult, error) {
	var res []types.ListUnspentResult
	err := c.Call(ctx, "listunspent", &res, minConf, maxConf, marshalAddresses(addrs))
	return res, err
}

// ListSinceBlock returns all transactions added in blocks since the specified
// block hash, or all transactions if it is nil, using the default number of
// minimum confirmations as a filter.
//
// See ListSinceBlockMinConf to override the minimum number of confirmations.
func (c *Client) ListSinceBlock(ctx context.Context, blockHash *chainhash.Hash) (*types.ListSinceBlockResult, error) {
	var blockHashString *string
	if blockHash != nil {
		blockHashString = new(string)
		*blockHashString = blockHash.String()
	}
	res := new(types.ListSinceBlockResult)
	err := c.Call(ctx, "listsinceblock", res, blockHashString)
	return res, err
}

// ListSinceBlockMinConf returns all transactions added in blocks since the
// specified block hash, or all transactions if it is nil, using the specified
// number of minimum confirmations as a filter.
//
// See ListSinceBlock to use the default minimum number of confirmations.
func (c *Client) ListSinceBlockMinConf(ctx context.Context, blockHash *chainhash.Hash, minConfirms int) (*types.ListSinceBlockResult, error) {
	var blockHashString *string
	if blockHash != nil {
		blockHashString = new(string)
		*blockHashString = blockHash.String()
	}
	res := new(types.ListSinceBlockResult)
	err := c.Call(ctx, "listsinceblock", res, blockHashString, minConfirms)
	return res, err
}

// LockUnspent marks outputs as locked or unlocked, depending on the value of
// the unlock bool.  When locked, the unspent output will not be selected as
// input for newly created, non-raw transactions, and will not be returned in
// future ListUnspent results, until the output is marked unlocked again.
//
// If unlock is false, each outpoint in ops will be marked locked.  If unlocked
// is true and specific outputs are specified in ops (len != 0), exactly those
// outputs will be marked unlocked.  If unlocked is true and no outpoints are
// specified, all previous locked outputs are marked unlocked.
//
// The locked or unlocked state of outputs are not written to disk and after
// restarting a wallet process, this data will be reset (every output unlocked).
//
// NOTE: While this method would be a bit more readable if the unlock bool was
// reversed (that is, LockUnspent(true, ...) locked the outputs), it has been
// left as unlock to keep compatibility with the reference client API and to
// avoid confusion for those who are already familiar with the lockunspent RPC.
func (c *Client) LockUnspent(ctx context.Context, unlock bool, ops []*wire.OutPoint) error {
	outputs := make([]dcrdtypes.TransactionInput, len(ops))
	for i, op := range ops {
		outputs[i] = dcrdtypes.TransactionInput{
			Txid: op.Hash.String(),
			Vout: op.Index,
			Tree: op.Tree,
		}
	}
	return c.Call(ctx, "lockunspent", nil, unlock, outputs)
}

// ListLockUnspent returns a slice of outpoints for all unspent outputs marked
// as locked by a wallet.  Unspent outputs may be marked locked using
// LockOutput.
func (c *Client) ListLockUnspent(ctx context.Context) ([]*wire.OutPoint, error) {
	var ops []*wire.OutPoint
	err := c.Call(ctx, "listlockunspent", unmarshalOutpoints(&ops))
	return ops, err
}

// SendToAddress sends the passed amount to the given address.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) SendToAddress(ctx context.Context, address dcrutil.Address, amount dcrutil.Amount) (*chainhash.Hash, error) {
	var res *chainhash.Hash
	err := c.Call(ctx, "sendtoaddress", unmarshalHash(&res), address.String(), amount.ToCoin())
	return res, err
}

// SendFrom sends the passed amount to the given address using the provided
// account as a source of funds.  Only funds with the default number of minimum
// confirmations will be used.
//
// See SendFromMinConf for different options.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) SendFrom(ctx context.Context, fromAccount string, toAddress dcrutil.Address, amount dcrutil.Amount) (*chainhash.Hash, error) {
	var res *chainhash.Hash
	err := c.Call(ctx, "sendfrom", unmarshalHash(&res), fromAccount, toAddress.String(),
		amount.ToCoin())
	return res, err
}

// SendFromMinConf sends the passed amount to the given address using the
// provided account as a source of funds.  Only funds with the passed number of
// minimum confirmations will be used.
//
// See SendFrom to use the default number of minimum confirmations.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) SendFromMinConf(ctx context.Context, fromAccount string, toAddress dcrutil.Address, amount dcrutil.Amount, minConfirms int) (*chainhash.Hash, error) {
	var res *chainhash.Hash
	err := c.Call(ctx, "sendfrom", unmarshalHash(&res), fromAccount, toAddress.String(),
		amount.ToCoin(), minConfirms)
	return res, err
}

// SendMany sends multiple amounts to multiple addresses using the provided
// account as a source of funds in a single transaction.  Only funds with the
// default number of minimum confirmations will be used.
//
// See SendManyMinConf for different options.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) SendMany(ctx context.Context, fromAccount string, amounts map[dcrutil.Address]dcrutil.Amount) (*chainhash.Hash, error) {
	amountsObject := make(map[string]float64)
	for addr, amount := range amounts {
		amountsObject[addr.String()] = amount.ToCoin()
	}
	var res *chainhash.Hash
	err := c.Call(ctx, "sendmany", unmarshalHash(&res), fromAccount, amountsObject)
	return res, err
}

// SendManyMinConf sends multiple amounts to multiple addresses using the
// provided account as a source of funds in a single transaction.  Only funds
// with the passed number of minimum confirmations will be used.
//
// See SendMany to use the default number of minimum confirmations.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) SendManyMinConf(ctx context.Context, fromAccount string, amounts map[dcrutil.Address]dcrutil.Amount, minConfirms int) (*chainhash.Hash, error) {
	amountsObject := make(map[string]float64)
	for addr, amount := range amounts {
		amountsObject[addr.String()] = amount.ToCoin()
	}
	var res *chainhash.Hash
	err := c.Call(ctx, "sendmany", unmarshalHash(&res), fromAccount, amountsObject, minConfirms)
	return res, err
}

// PurchaseTicket calls the purchaseticket method.  Starting with the minConf
// parameter, a nil parameter indicates the default value for the optional
// parameter.
func (c *Client) PurchaseTicket(ctx context.Context, fromAccount string,
	spendLimit dcrutil.Amount, minConf *int, ticketAddress dcrutil.Address,
	numTickets *int, poolAddress dcrutil.Address, poolFees *dcrutil.Amount,
	expiry *int, ticketChange *bool, ticketFee *dcrutil.Amount) ([]*chainhash.Hash, error) {

	params := make([]interface{}, 2, 10)
	params[0] = fromAccount
	params[1] = spendLimit.ToCoin()

	var skipped int
	addParam := func(nonNil bool, value func() interface{}) {
		if nonNil {
			for i := 0; i < skipped; i++ {
				params = append(params, nil)
			}
			skipped = 0
			params = append(params, value())
		} else {
			skipped++
		}
	}

	addParam(minConf != nil, func() interface{} { return *minConf })
	addParam(ticketAddress != nil, func() interface{} { return ticketAddress.String() })
	addParam(numTickets != nil, func() interface{} { return *numTickets })
	addParam(poolAddress != nil, func() interface{} { return poolAddress.String() })
	addParam(poolFees != nil, func() interface{} { return poolFees.ToCoin() })
	addParam(expiry != nil, func() interface{} { return *expiry })
	addParam(ticketChange != nil, func() interface{} { return *ticketChange })
	addParam(ticketFee != nil, func() interface{} { return ticketFee.ToCoin() })

	var res []*chainhash.Hash
	err := c.Call(ctx, "purchaseticket", unmarshalHashes(&res), params...)
	return res, err
}

// AddMultisigAddress adds a multisignature address that requires the specified
// number of signatures for the provided addresses to the wallet.
func (c *Client) AddMultisigAddress(ctx context.Context, requiredSigs int, addresses []dcrutil.Address, account string) (dcrutil.Address, error) {
	var res dcrutil.Address
	err := c.Call(ctx, "addmultisigaddress", unmarshalAddress(&res, c.net), requiredSigs,
		marshalAddresses(addresses), account)
	return res, err
}

// CreateMultisig creates a multisignature address that requires the specified
// number of signatures for the provided addresses and returns the
// multisignature address and script needed to redeem it.
func (c *Client) CreateMultisig(ctx context.Context, requiredSigs int, addresses []dcrutil.Address) (*types.CreateMultiSigResult, error) {
	res := new(types.CreateMultiSigResult)
	err := c.Call(ctx, "createmultisig", res, requiredSigs, marshalAddresses(addresses))
	return res, err
}

// CreateNewAccount creates a new wallet account.
func (c *Client) CreateNewAccount(ctx context.Context, account string) error {
	return c.Call(ctx, "createnewaccount", nil, account)
}

// GapPolicy defines the policy to use when the BIP0044 unused address gap limit
// would be violated by creating a new address.
type GapPolicy string

// Gap policies that are understood by a wallet JSON-RPC server.  These are
// defined for safety and convenience, but string literals can be used as well.
const (
	GapPolicyError  GapPolicy = "error"
	GapPolicyIgnore GapPolicy = "ignore"
	GapPolicyWrap   GapPolicy = "wrap"
)

// GetNewAddress returns a new address.
func (c *Client) GetNewAddress(ctx context.Context, account string) (dcrutil.Address, error) {
	var addr dcrutil.Address
	err := c.Call(ctx, "getnewaddress", unmarshalAddress(&addr, c.net), account)
	return addr, err
}

// GetNewAddressGapPolicy returns a new address while allowing callers to
// control the BIP0044 unused address gap limit policy.
func (c *Client) GetNewAddressGapPolicy(ctx context.Context, account string, gapPolicy GapPolicy) (dcrutil.Address, error) {
	var addr dcrutil.Address
	err := c.Call(ctx, "getnewaddress", unmarshalAddress(&addr, c.net), account, string(gapPolicy))
	return addr, err
}

// GetRawChangeAddress returns a new address for receiving change that will be
// associated with the provided account.  Note that this is only for raw
// transactions and NOT for normal use.
func (c *Client) GetRawChangeAddress(ctx context.Context, account string, net dcrutil.AddressParams) (dcrutil.Address, error) {
	var addr dcrutil.Address
	err := c.Call(ctx, "getrawchangeaddress", unmarshalAddress(&addr, c.net), account)
	return addr, err
}

// GetAccountAddress returns the current Decred address for receiving payments
// to the specified account.
func (c *Client) GetAccountAddress(ctx context.Context, account string) (dcrutil.Address, error) {
	var addr dcrutil.Address
	err := c.Call(ctx, "getaccountaddress", unmarshalAddress(&addr, c.net), account)
	return addr, err
}

// GetAccount returns the account associated with the passed address.
func (c *Client) GetAccount(ctx context.Context, address dcrutil.Address) (string, error) {
	var res string
	err := c.Call(ctx, "getaccount", &res, address.String())
	return res, err
}

// GetAddressesByAccount returns the list of addresses associated with the
// passed account.
func (c *Client) GetAddressesByAccount(ctx context.Context, account string) ([]dcrutil.Address, error) {
	var addrs []dcrutil.Address
	err := c.Call(ctx, "getaddressesbyaccount", unmarshalAddresses(&addrs, c.net), account)
	return addrs, err
}

// RenameAccount renames an existing wallet account.
func (c *Client) RenameAccount(ctx context.Context, oldAccount, newAccount string) error {
	return c.Call(ctx, "renameaccount", nil, oldAccount, newAccount)
}

// ValidateAddress returns information about the given Decred address.
func (c *Client) ValidateAddress(ctx context.Context, address dcrutil.Address) (*types.ValidateAddressWalletResult, error) {
	res := new(types.ValidateAddressWalletResult)
	err := c.Call(ctx, "validateaddress", res, address.String())
	return res, err
}

// ListAccounts returns a map of account names and their associated balances
// using the default number of minimum confirmations.
//
// See ListAccountsMinConf to override the minimum number of confirmations.
func (c *Client) ListAccounts(ctx context.Context) (map[string]dcrutil.Amount, error) {
	res := make(map[string]dcrutil.Amount)
	err := c.Call(ctx, "listaccounts", unmarshalListAccounts(res))
	return res, err
}

// ListAccountsMinConf returns a map of account names and their associated
// balances using the specified number of minimum confirmations.
//
// See ListAccounts to use the default minimum number of confirmations.
func (c *Client) ListAccountsMinConf(ctx context.Context, minConfirms int) (map[string]dcrutil.Amount, error) {
	res := make(map[string]dcrutil.Amount)
	err := c.Call(ctx, "listaccounts", unmarshalListAccounts(res), minConfirms)
	return res, err
}

// GetMasterPubkey returns a pointer to the master extended public key for account.
func (c *Client) GetMasterPubkey(ctx context.Context, account string) (*hdkeychain.ExtendedKey, error) {
	var res *hdkeychain.ExtendedKey
	err := c.Call(ctx, "getmasterpubkey", unmarshalHDKey(&res, c.net), account)
	return res, err
}

// GetBalance returns the available balance from the server for the specified
// account using the default number of minimum confirmations.  The account may
// be "*" for all accounts.
//
// See GetBalanceMinConf to override the minimum number of confirmations.
func (c *Client) GetBalance(ctx context.Context, account string) (*types.GetBalanceResult, error) {
	res := new(types.GetBalanceResult)
	err := c.Call(ctx, "getbalance", res, account)
	return res, err
}

// GetBalanceMinConf returns the available balance from the server for the
// specified account using the specified number of minimum confirmations.  The
// account may be "*" for all accounts.
//
// See GetBalance to use the default minimum number of confirmations.
func (c *Client) GetBalanceMinConf(ctx context.Context, account string, minConfirms int) (*types.GetBalanceResult, error) {
	res := new(types.GetBalanceResult)
	err := c.Call(ctx, "getbalance", res, account, minConfirms)
	return res, err
}

// GetReceivedByAccount returns the total amount received with the specified
// account with at least the default number of minimum confirmations.
//
// See GetReceivedByAccountMinConf to override the minimum number of
// confirmations.
func (c *Client) GetReceivedByAccount(ctx context.Context, account string) (dcrutil.Amount, error) {
	var res dcrutil.Amount
	err := c.Call(ctx, "getreceivedbyaccount", unmarshalAmount(&res), account)
	return res, err
}

// GetReceivedByAccountMinConf returns the total amount received with the
// specified account with at least the specified number of minimum
// confirmations.
//
// See GetReceivedByAccount to use the default minimum number of confirmations.
func (c *Client) GetReceivedByAccountMinConf(ctx context.Context, account string, minConfirms int) (dcrutil.Amount, error) {
	var res dcrutil.Amount
	err := c.Call(ctx, "getreceivedbyaccount", unmarshalAmount(&res), account, minConfirms)
	return res, err
}

// GetUnconfirmedBalance returns the unconfirmed balance from the server for
// the specified account.
func (c *Client) GetUnconfirmedBalance(ctx context.Context, account string) (dcrutil.Amount, error) {
	var res dcrutil.Amount
	err := c.Call(ctx, "getunconfirmedbalance", unmarshalAmount(&res), account)
	return res, err
}

// GetReceivedByAddress returns the total amount received by the specified
// address with at least the default number of minimum confirmations.
//
// See GetReceivedByAddressMinConf to override the minimum number of
// confirmations.
func (c *Client) GetReceivedByAddress(ctx context.Context, address dcrutil.Address) (dcrutil.Amount, error) {
	var res dcrutil.Amount
	err := c.Call(ctx, "getreceivedbyaddress", unmarshalAmount(&res), address.String())
	return res, err
}

// GetReceivedByAddressMinConf returns the total amount received by the specified
// address with at least the specified number of minimum confirmations.
//
// See GetReceivedByAddress to use the default minimum number of confirmations.
func (c *Client) GetReceivedByAddressMinConf(ctx context.Context, address dcrutil.Address, minConfirms int) (dcrutil.Amount, error) {
	var res dcrutil.Amount
	err := c.Call(ctx, "getreceivedbyaddress", unmarshalAmount(&res), address.String(), minConfirms)
	return res, err
}

// ListReceivedByAccount lists balances by account using the default number
// of minimum confirmations and including accounts that haven't received any
// payments.
//
// See ListReceivedByAccountMinConf to override the minimum number of
// confirmations and ListReceivedByAccountIncludeEmpty to filter accounts that
// haven't received any payments from the results.
func (c *Client) ListReceivedByAccount(ctx context.Context) ([]types.ListReceivedByAccountResult, error) {
	var res []types.ListReceivedByAccountResult
	err := c.Call(ctx, "listreceivedbyacccount", &res)
	return res, err
}

// ListReceivedByAccountMinConf lists balances by account using the specified
// number of minimum confirmations not including accounts that haven't received
// any payments.
//
// See ListReceivedByAccount to use the default minimum number of confirmations
// and ListReceivedByAccountIncludeEmpty to also filter accounts that haven't
// received any payments from the results.
func (c *Client) ListReceivedByAccountMinConf(ctx context.Context, minConfirms int) ([]types.ListReceivedByAccountResult, error) {
	var res []types.ListReceivedByAccountResult
	err := c.Call(ctx, "listreceivedbyacccount", &res, minConfirms)
	return res, err
}

// ListReceivedByAccountIncludeEmpty lists balances by account using the
// specified number of minimum confirmations and including accounts that
// haven't received any payments depending on specified flag.
//
// See ListReceivedByAccount and ListReceivedByAccountMinConf to use defaults.
func (c *Client) ListReceivedByAccountIncludeEmpty(ctx context.Context, minConfirms int, includeEmpty bool) ([]types.ListReceivedByAccountResult, error) {
	var res []types.ListReceivedByAccountResult
	err := c.Call(ctx, "listreceivedbyacccount", &res, minConfirms, includeEmpty)
	return res, err
}

// ListReceivedByAddress lists balances by address using the default number
// of minimum confirmations not including addresses that haven't received any
// payments or watching only addresses.
//
// See ListReceivedByAddressMinConf to override the minimum number of
// confirmations and ListReceivedByAddressIncludeEmpty to also include addresses
// that haven't received any payments in the results.
func (c *Client) ListReceivedByAddress(ctx context.Context) ([]types.ListReceivedByAddressResult, error) {
	var res []types.ListReceivedByAddressResult
	err := c.Call(ctx, "listreceivedbyaddress", &res)
	return res, err
}

// ListReceivedByAddressMinConf lists balances by address using the specified
// number of minimum confirmations not including addresses that haven't received
// any payments.
//
// See ListReceivedByAddress to use the default minimum number of confirmations
// and ListReceivedByAddressIncludeEmpty to also include addresses that haven't
// received any payments in the results.
func (c *Client) ListReceivedByAddressMinConf(ctx context.Context, minConfirms int) ([]types.ListReceivedByAddressResult, error) {
	var res []types.ListReceivedByAddressResult
	err := c.Call(ctx, "listreceivedbyaddress", &res, minConfirms)
	return res, err
}

// ListReceivedByAddressIncludeEmpty lists balances by address using the
// specified number of minimum confirmations and including addresses that
// haven't received any payments depending on specified flag.
//
// See ListReceivedByAddress and ListReceivedByAddressMinConf to use defaults.
func (c *Client) ListReceivedByAddressIncludeEmpty(ctx context.Context, minConfirms int, includeEmpty bool) ([]types.ListReceivedByAddressResult, error) {
	var res []types.ListReceivedByAddressResult
	err := c.Call(ctx, "listreceivedbyaddress", &res, minConfirms, includeEmpty)
	return res, err
}

// WalletLock locks the wallet by removing the encryption key from memory.
//
// After calling this function, the WalletPassphrase function must be used to
// unlock the wallet prior to calling any other function which requires the
// wallet to be unlocked.
func (c *Client) WalletLock(ctx context.Context) error {
	return c.Call(ctx, "walletlock", nil)
}

// WalletPassphrase unlocks the wallet by using the passphrase to derive the
// decryption key which is then stored in memory for the specified timeout
// (in seconds).
func (c *Client) WalletPassphrase(ctx context.Context, passphrase string, timeoutSecs int64) error {
	return c.Call(ctx, "walletpassphrase", nil, passphrase, timeoutSecs)
}

// WalletPassphraseChange changes the wallet passphrase from the specified old
// to new passphrase.
func (c *Client) WalletPassphraseChange(ctx context.Context, old, new string) error {
	return c.Call(ctx, "walletpassphrasechange", nil, old, new)
}

// SignMessage signs a message with the private key of the specified address.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) SignMessage(ctx context.Context, address dcrutil.Address, message string) (string, error) {
	var res string
	err := c.Call(ctx, "signmessage", &res, address.String(), message)
	return res, err
}

// VerifyMessage verifies a signed message.
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) VerifyMessage(ctx context.Context, address dcrutil.Address, signature, message string) (bool, error) {
	var res bool
	err := c.Call(ctx, "verifymessage", &res, address.String(), signature, message)
	return res, err
}

// DumpPrivKey gets the private key corresponding to the passed address encoded
// in the wallet import format (WIF).
//
// NOTE: This function requires to the wallet to be unlocked.  See the
// WalletPassphrase function for more details.
func (c *Client) DumpPrivKey(ctx context.Context, address dcrutil.Address) (*dcrutil.WIF, error) {
	var res *dcrutil.WIF
	err := c.Call(ctx, "dumpprivkey", unmarshalWIF(&res, c.net), address.String())
	return res, err
}

// ImportPrivKey imports the passed private key which must be the wallet import
// format (WIF).
func (c *Client) ImportPrivKey(ctx context.Context, privKeyWIF *dcrutil.WIF) error {
	return c.Call(ctx, "importprivkey", nil, privKeyWIF.String())
}

// ImportPrivKeyLabel imports the passed private key which must be the wallet import
// format (WIF). It sets the account label to the one provided.
func (c *Client) ImportPrivKeyLabel(ctx context.Context, privKeyWIF *dcrutil.WIF, label string) error {
	return c.Call(ctx, "importprivkey", nil, privKeyWIF.String(), label)
}

// ImportPrivKeyRescan imports the passed private key which must be the wallet import
// format (WIF). It sets the account label to the one provided. When rescan is true,
// the block history is scanned for transactions addressed to provided privKey.
func (c *Client) ImportPrivKeyRescan(ctx context.Context, privKeyWIF *dcrutil.WIF, label string, rescan bool) error {
	return c.Call(ctx, "importprivkey", nil, privKeyWIF.String(), label, rescan)
}

// ImportPrivKeyRescanFrom imports the passed private key which must be the wallet
// import format (WIF). It sets the account label to the one provided. When rescan
// is true, the block history from block scanFrom is scanned for transactions
// addressed to provided privKey.
func (c *Client) ImportPrivKeyRescanFrom(ctx context.Context, privKeyWIF *dcrutil.WIF, label string, rescan bool, scanFrom int) error {
	return c.Call(ctx, "importprivkey", nil, privKeyWIF.String(), label, rescan, scanFrom)
}

// ImportScript attempts to import a byte code script into wallet.
func (c *Client) ImportScript(ctx context.Context, script []byte) error {
	return c.Call(ctx, "importscript", nil, hex.EncodeToString(script))
}

// ImportScriptRescan attempts to import a byte code script into wallet. It also
// allows the user to choose whether or not they do a rescan.
func (c *Client) ImportScriptRescan(ctx context.Context, script []byte, rescan bool) error {
	return c.Call(ctx, "importscript", nil, hex.EncodeToString(script), rescan)
}

// ImportScriptRescanFrom attempts to import a byte code script into wallet. It
// also allows the user to choose whether or not they do a rescan, and which
// height to rescan from.
func (c *Client) ImportScriptRescanFrom(ctx context.Context, script []byte, rescan bool, scanFrom int) error {
	return c.Call(ctx, "importscript", nil, hex.EncodeToString(script), rescan, scanFrom)
}

// AccountAddressIndex returns the address index for a given account's branch.
func (c *Client) AccountAddressIndex(ctx context.Context, account string, branch uint32) (int, error) {
	var res int
	err := c.Call(ctx, "accountaddressindex", &res, account, branch)
	return res, err
}

// AccountSyncAddressIndex synchronizes an account branch to the passed address
// index.
func (c *Client) AccountSyncAddressIndex(ctx context.Context, account string, branch uint32, index int) error {
	return c.Call(ctx, "accountsyncaddressindex", nil, account, branch, index)
}

// RevokeTickets triggers the wallet to issue revocations for any missed tickets that
// have not yet been revoked.
func (c *Client) RevokeTickets(ctx context.Context) error {
	return c.Call(ctx, "revoketickets", nil)
}

// AddTicket manually adds a new ticket to the wallet stake manager. This is used
// to override normal security settings to insert tickets which would not
// otherwise be added to the wallet.
func (c *Client) AddTicket(ctx context.Context, ticket *dcrutil.Tx) error {
	return c.Call(ctx, "addticket", nil, marshalTx(ticket.MsgTx()))
}

// FundRawTransaction adds inputs to a transaction until it has enough in value
// to meet its out value.
func (c *Client) FundRawTransaction(ctx context.Context, rawhex string, fundAccount string, options types.FundRawTransactionOptions) (*types.FundRawTransactionResult, error) {
	res := new(types.FundRawTransactionResult)
	err := c.Call(ctx, "fundrawtransaction", res, rawhex, fundAccount, options)
	return res, err
}

// GenerateVote returns hex of an SSGen.
func (c *Client) GenerateVote(ctx context.Context, blockHash *chainhash.Hash, height int64, sstxHash *chainhash.Hash, voteBits uint16, voteBitsExt string) (*types.GenerateVoteResult, error) {
	res := new(types.GenerateVoteResult)
	err := c.Call(ctx, "generatevote", res, blockHash.String(), height, sstxHash.String(),
		voteBits, voteBitsExt)
	return res, err
}

// GetInfoWallet calls the getinfo method.  It is named differently to avoid a
// naming clash for dcrd clients with a GetInfo method.
func (c *Client) GetInfo(ctx context.Context) (*types.InfoWalletResult, error) {
	res := new(types.InfoWalletResult)
	err := c.Call(ctx, "getinfo", res)
	return res, err
}

// GetStakeInfo returns stake mining info from a given wallet. This includes
// various statistics on tickets it owns and votes it has produced.
func (c *Client) GetStakeInfo(ctx context.Context) (*types.GetStakeInfoResult, error) {
	res := new(types.GetStakeInfoResult)
	err := c.Call(ctx, "getstakeinfo", res)
	return res, err
}

// GetTickets returns a list of the tickets owned by the wallet, partially
// or in full. The flag includeImmature is used to indicate if non mature
// tickets should also be returned.
func (c *Client) GetTickets(ctx context.Context, includeImmature bool) ([]*chainhash.Hash, error) {
	var hashes []*chainhash.Hash
	var res = struct {
		Hashes json.Unmarshaler `json:"hashes"`
	}{
		Hashes: unmarshalHashes(&hashes),
	}
	err := c.Call(ctx, "gettickets", &res, includeImmature)
	return hashes, err
}

// SetTxFee sets the transaction fee per KB amount.
func (c *Client) SetTxFee(ctx context.Context, fee dcrutil.Amount) error {
	return c.Call(ctx, "settxfee", nil, fee.ToCoin())
}

// GetVoteChoices returns the currently-set vote choices for each agenda in the
// latest supported stake version.
func (c *Client) GetVoteChoices(ctx context.Context) (*types.GetVoteChoicesResult, error) {
	res := new(types.GetVoteChoicesResult)
	err := c.Call(ctx, "getvotechoices", res)
	return res, err
}

// SetVoteChoice sets a voting choice preference for an agenda.
func (c *Client) SetVoteChoice(ctx context.Context, agendaID, choiceID string) error {
	return c.Call(ctx, "setvotechoice", nil, agendaID, choiceID)
}

// TicketsForAddress returns a list of tickets paying to the passed address.
// If the daemon server is queried, it returns a search of tickets in the
// live ticket pool. If the wallet server is queried, it searches all tickets
// owned by the wallet.
func (c *Client) TicketsForAddress(ctx context.Context, addr dcrutil.Address) (*dcrdtypes.TicketsForAddressResult, error) {
	res := new(dcrdtypes.TicketsForAddressResult)
	err := c.Call(ctx, "ticketsforaddress", res, addr.String())
	return res, err
}

// WalletInfo returns wallet global state info for a given wallet.
func (c *Client) WalletInfo(ctx context.Context) (*types.WalletInfoResult, error) {
	res := new(types.WalletInfoResult)
	err := c.Call(ctx, "walletinfo", res)
	return res, err
}

// ListAddressTransactions returns information about all transactions associated
// with the provided addresses.
func (c *Client) ListAddressTransactions(ctx context.Context, addresses []dcrutil.Address, account string) ([]types.ListTransactionsResult, error) {
	var res []types.ListTransactionsResult
	err := c.Call(ctx, "listaddresstransactions", &res, marshalAddresses(addresses), account)
	return res, err
}

// SigHashType enumerates the available signature hashing types that the
// SignRawTransaction function accepts.
type SigHashType string

// Constants used to indicate the signature hash type for SignRawTransaction.
const (
	// SigHashAll indicates ALL of the outputs should be signed.
	SigHashAll SigHashType = "ALL"

	// SigHashNone indicates NONE of the outputs should be signed.  This
	// can be thought of as specifying the signer does not care where the
	// bitcoins go.
	SigHashNone SigHashType = "NONE"

	// SigHashSingle indicates that a SINGLE output should be signed.  This
	// can be thought of specifying the signer only cares about where ONE of
	// the outputs goes, but not any of the others.
	SigHashSingle SigHashType = "SINGLE"

	// SigHashAllAnyoneCanPay indicates that signer does not care where the
	// other inputs to the transaction come from, so it allows other people
	// to add inputs.  In addition, it uses the SigHashAll signing method
	// for outputs.
	SigHashAllAnyoneCanPay SigHashType = "ALL|ANYONECANPAY"

	// SigHashNoneAnyoneCanPay indicates that signer does not care where the
	// other inputs to the transaction come from, so it allows other people
	// to add inputs.  In addition, it uses the SigHashNone signing method
	// for outputs.
	SigHashNoneAnyoneCanPay SigHashType = "NONE|ANYONECANPAY"

	// SigHashSingleAnyoneCanPay indicates that signer does not care where
	// the other inputs to the transaction come from, so it allows other
	// people to add inputs.  In addition, it uses the SigHashSingle signing
	// method for outputs.
	SigHashSingleAnyoneCanPay SigHashType = "SINGLE|ANYONECANPAY"
)

// String returns the SighHashType in human-readable form.
func (s SigHashType) String() string {
	return string(s)
}

// SignatureErrors implements the error interface for the "errors" field of a
// signrawtransaction response.
type SignatureErrors []types.SignRawTransactionError

func (e SignatureErrors) Error() string {
	if len(e) == 0 {
		return "<nil>"
	}
	if len(e) == 1 {
		return e[0].Error
	}
	var s strings.Builder
	s.WriteString("multiple signature errors: [")
	for i := range e {
		if i != 0 {
			s.WriteString("; ")
		}
		s.WriteString(`"`)
		s.WriteString(e[i].Error)
		s.WriteString(`"`)
	}
	s.WriteString("]")
	return s.String()
}

// SignRawTransaction signs inputs for the passed transaction and returns the
// signed transaction as well as whether or not all inputs are now signed.
//
// This function assumes the RPC server already knows the input transactions and
// private keys for the passed transaction which needs to be signed and uses the
// default signature hash type.  Use one of the SignRawTransaction# variants to
// specify that information if needed.
//
// If the "errors" field of the response is set, the error return value will be
// of type SignatureErrors.  This does not indicate that no signatures were
// added, and the partially signed transaction is still returned.
func (c *Client) SignRawTransaction(ctx context.Context, tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
	var signedTx *wire.MsgTx
	var res = struct {
		Tx              json.Unmarshaler `json:"hex"`
		Complete        bool             `json:"complete"`
		SignatureErrors SignatureErrors  `json:"errors"`
	}{
		Tx: unmarshalTx(&signedTx),
	}
	err := c.Call(ctx, "signrawtransaction", &res, marshalTx(tx))
	if err == nil && len(res.SignatureErrors) > 0 {
		err = res.SignatureErrors
	}
	return signedTx, res.Complete, err
}

// SignRawTransaction2 signs inputs for the passed transaction given the list
// of information about the input transactions needed to perform the signing
// process.
//
// This only input transactions that need to be specified are ones the
// RPC server does not already know.  Already known input transactions will be
// merged with the specified transactions.
//
// See SignRawTransaction if the RPC server already knows the input
// transactions.
func (c *Client) SignRawTransaction2(ctx context.Context, tx *wire.MsgTx, inputs []types.RawTxInput) (*wire.MsgTx, bool, error) {
	var signedTx *wire.MsgTx
	var res = struct {
		Tx              json.Unmarshaler `json:"hex"`
		Complete        bool             `json:"complete"`
		SignatureErrors SignatureErrors  `json:"errors"`
	}{
		Tx: unmarshalTx(&signedTx),
	}
	err := c.Call(ctx, "signrawtransaction", &res, marshalTx(tx), inputs)
	if err == nil && len(res.SignatureErrors) > 0 {
		err = res.SignatureErrors
	}
	return signedTx, res.Complete, err
}

// SignRawTransaction3 signs inputs for the passed transaction given the list
// of information about extra input transactions and a list of private keys
// needed to perform the signing process.  The private keys must be in wallet
// import format (WIF).
//
// This only input transactions that need to be specified are ones the
// RPC server does not already know.  Already known input transactions will be
// merged with the specified transactions.  This means the list of transaction
// inputs can be nil if the RPC server already knows them all.
//
// NOTE: Unlike the merging functionality of the input transactions, ONLY the
// specified private keys will be used, so even if the server already knows some
// of the private keys, they will NOT be used.
//
// See SignRawTransaction if the RPC server already knows the input
// transactions and private keys or SignRawTransaction2 if it already knows the
// private keys.
func (c *Client) SignRawTransaction3(ctx context.Context, tx *wire.MsgTx,
	inputs []types.RawTxInput,
	privKeysWIF []string) (*wire.MsgTx, bool, error) {

	var signedTx *wire.MsgTx
	var res = struct {
		Tx              json.Unmarshaler `json:"hex"`
		Complete        bool             `json:"complete"`
		SignatureErrors SignatureErrors  `json:"errors"`
	}{
		Tx: unmarshalTx(&signedTx),
	}
	err := c.Call(ctx, "signrawtransaction", &res, marshalTx(tx), inputs, privKeysWIF)
	if err == nil && len(res.SignatureErrors) > 0 {
		err = res.SignatureErrors
	}
	return signedTx, res.Complete, err
}

// SignRawTransaction4 signs inputs for the passed transaction using the
// the specified signature hash type given the list of information about extra
// input transactions and a potential list of private keys needed to perform
// the signing process.  The private keys, if specified, must be in wallet
// import format (WIF).
//
// The only input transactions that need to be specified are ones the RPC server
// does not already know.  This means the list of transaction inputs can be nil
// if the RPC server already knows them all.
//
// NOTE: Unlike the merging functionality of the input transactions, ONLY the
// specified private keys will be used, so even if the server already knows some
// of the private keys, they will NOT be used.  The list of private keys can be
// nil in which case any private keys the RPC server knows will be used.
//
// This function should only used if a non-default signature hash type is
// desired.  Otherwise, see SignRawTransaction if the RPC server already knows
// the input transactions and private keys, SignRawTransaction2 if it already
// knows the private keys, or SignRawTransaction3 if it does not know both.
func (c *Client) SignRawTransaction4(ctx context.Context, tx *wire.MsgTx,
	inputs []types.RawTxInput, privKeysWIF []string,
	hashType SigHashType) (*wire.MsgTx, bool, error) {

	var signedTx *wire.MsgTx
	var res = struct {
		Tx              json.Unmarshaler `json:"hex"`
		Complete        bool             `json:"complete"`
		SignatureErrors SignatureErrors  `json:"errors"`
	}{
		Tx: unmarshalTx(&signedTx),
	}
	err := c.Call(ctx, "signrawtransaction", &res, marshalTx(tx), inputs, privKeysWIF, hashType.String())
	if err == nil && len(res.SignatureErrors) > 0 {
		err = res.SignatureErrors
	}
	return signedTx, res.Complete, err
}

// SignRawSSGenTx signs inputs for the passed transaction using the
// the specified signature hash type given the list of information about extra
// input transactions and a potential list of private keys needed to perform
// the signing process.  The private keys, if specified, must be in wallet
// import format (WIF).
//
// The only input transactions that need to be specified are ones the RPC server
// does not already know.  This means the list of transaction inputs can be nil
// if the RPC server already knows them all.
func (c *Client) SignRawSSGenTx(ctx context.Context, tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
	var signedTx *wire.MsgTx
	var res = struct {
		Tx       json.Unmarshaler `json:"hex"`
		Complete bool             `json:"complete"`
	}{
		Tx: unmarshalTx(&signedTx),
	}
	err := c.Call(ctx, "signrawtransaction", &res, marshalTx(tx), nil, nil, "ssgen")
	return signedTx, res.Complete, err
}
