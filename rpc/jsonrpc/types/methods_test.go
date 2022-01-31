// Copyright (c) 2014 The btcsuite developers
// Copyright (c) 2015-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/decred/dcrd/dcrjson/v4"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
)

// TestWalletSvrCmds tests all of the wallet server commands marshal and
// unmarshal into valid results include handling of optional fields being
// omitted in the marshalled command, while optional fields with defaults have
// the default assigned on unmarshalled commands.
func TestWalletSvrCmds(t *testing.T) {
	t.Parallel()

	testID := int(1)
	tests := []struct {
		name         string
		newCmd       func() (interface{}, error)
		staticCmd    func() interface{}
		marshalled   string
		unmarshalled interface{}
	}{
		{
			name: "addmultisigaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("addmultisigaddress"), 2, []string{"031234", "035678"})
			},
			staticCmd: func() interface{} {
				keys := []string{"031234", "035678"}
				return NewAddMultisigAddressCmd(2, keys, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"addmultisigaddress","params":[2,["031234","035678"]],"id":1}`,
			unmarshalled: &AddMultisigAddressCmd{
				NRequired: 2,
				Keys:      []string{"031234", "035678"},
				Account:   nil,
			},
		},
		{
			name: "addmultisigaddress optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("addmultisigaddress"), 2, []string{"031234", "035678"}, "test")
			},
			staticCmd: func() interface{} {
				keys := []string{"031234", "035678"}
				return NewAddMultisigAddressCmd(2, keys, dcrjson.String("test"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"addmultisigaddress","params":[2,["031234","035678"],"test"],"id":1}`,
			unmarshalled: &AddMultisigAddressCmd{
				NRequired: 2,
				Keys:      []string{"031234", "035678"},
				Account:   dcrjson.String("test"),
			},
		},
		{
			name: "createmultisig",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("createmultisig"), 2, []string{"031234", "035678"})
			},
			staticCmd: func() interface{} {
				keys := []string{"031234", "035678"}
				return NewCreateMultisigCmd(2, keys)
			},
			marshalled: `{"jsonrpc":"1.0","method":"createmultisig","params":[2,["031234","035678"]],"id":1}`,
			unmarshalled: &CreateMultisigCmd{
				NRequired: 2,
				Keys:      []string{"031234", "035678"},
			},
		},
		{
			name: "createnewaccount",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("createnewaccount"), "acct")
			},
			staticCmd: func() interface{} {
				return NewCreateNewAccountCmd("acct")
			},
			marshalled: `{"jsonrpc":"1.0","method":"createnewaccount","params":["acct"],"id":1}`,
			unmarshalled: &CreateNewAccountCmd{
				Account: "acct",
			},
		},
		{
			name: "dumpprivkey",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("dumpprivkey"), "1Address")
			},
			staticCmd: func() interface{} {
				return NewDumpPrivKeyCmd("1Address")
			},
			marshalled: `{"jsonrpc":"1.0","method":"dumpprivkey","params":["1Address"],"id":1}`,
			unmarshalled: &DumpPrivKeyCmd{
				Address: "1Address",
			},
		},
		{
			name: "getaccount",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getaccount"), "1Address")
			},
			staticCmd: func() interface{} {
				return NewGetAccountCmd("1Address")
			},
			marshalled: `{"jsonrpc":"1.0","method":"getaccount","params":["1Address"],"id":1}`,
			unmarshalled: &GetAccountCmd{
				Address: "1Address",
			},
		},
		{
			name: "getaccountaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getaccountaddress"), "acct")
			},
			staticCmd: func() interface{} {
				return NewGetAccountAddressCmd("acct")
			},
			marshalled: `{"jsonrpc":"1.0","method":"getaccountaddress","params":["acct"],"id":1}`,
			unmarshalled: &GetAccountAddressCmd{
				Account: "acct",
			},
		},
		{
			name: "getaddressesbyaccount",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getaddressesbyaccount"), "acct")
			},
			staticCmd: func() interface{} {
				return NewGetAddressesByAccountCmd("acct")
			},
			marshalled: `{"jsonrpc":"1.0","method":"getaddressesbyaccount","params":["acct"],"id":1}`,
			unmarshalled: &GetAddressesByAccountCmd{
				Account: "acct",
			},
		},
		{
			name: "getbalance",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getbalance"))
			},
			staticCmd: func() interface{} {
				return NewGetBalanceCmd(nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"getbalance","params":[],"id":1}`,
			unmarshalled: &GetBalanceCmd{
				Account: nil,
				MinConf: dcrjson.Int(1),
			},
		},
		{
			name: "getbalance optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getbalance"), "acct")
			},
			staticCmd: func() interface{} {
				return NewGetBalanceCmd(dcrjson.String("acct"), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"getbalance","params":["acct"],"id":1}`,
			unmarshalled: &GetBalanceCmd{
				Account: dcrjson.String("acct"),
				MinConf: dcrjson.Int(1),
			},
		},
		{
			name: "getbalance optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getbalance"), "acct", 6)
			},
			staticCmd: func() interface{} {
				return NewGetBalanceCmd(dcrjson.String("acct"), dcrjson.Int(6))
			},
			marshalled: `{"jsonrpc":"1.0","method":"getbalance","params":["acct",6],"id":1}`,
			unmarshalled: &GetBalanceCmd{
				Account: dcrjson.String("acct"),
				MinConf: dcrjson.Int(6),
			},
		},
		{
			name: "getnewaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getnewaddress"))
			},
			staticCmd: func() interface{} {
				return NewGetNewAddressCmd(nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"getnewaddress","params":[],"id":1}`,
			unmarshalled: &GetNewAddressCmd{
				Account:   nil,
				GapPolicy: nil,
			},
		},
		{
			name: "getnewaddress optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getnewaddress"), "acct", "ignore")
			},
			staticCmd: func() interface{} {
				return NewGetNewAddressCmd(dcrjson.String("acct"), dcrjson.String("ignore"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"getnewaddress","params":["acct","ignore"],"id":1}`,
			unmarshalled: &GetNewAddressCmd{
				Account:   dcrjson.String("acct"),
				GapPolicy: dcrjson.String("ignore"),
			},
		},
		{
			name: "getrawchangeaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getrawchangeaddress"))
			},
			staticCmd: func() interface{} {
				return NewGetRawChangeAddressCmd(nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"getrawchangeaddress","params":[],"id":1}`,
			unmarshalled: &GetRawChangeAddressCmd{
				Account: nil,
			},
		},
		{
			name: "getrawchangeaddress optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getrawchangeaddress"), "acct")
			},
			staticCmd: func() interface{} {
				return NewGetRawChangeAddressCmd(dcrjson.String("acct"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"getrawchangeaddress","params":["acct"],"id":1}`,
			unmarshalled: &GetRawChangeAddressCmd{
				Account: dcrjson.String("acct"),
			},
		},
		{
			name: "getreceivedbyaccount",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getreceivedbyaccount"), "acct")
			},
			staticCmd: func() interface{} {
				return NewGetReceivedByAccountCmd("acct", nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"getreceivedbyaccount","params":["acct"],"id":1}`,
			unmarshalled: &GetReceivedByAccountCmd{
				Account: "acct",
				MinConf: dcrjson.Int(1),
			},
		},
		{
			name: "getreceivedbyaccount optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getreceivedbyaccount"), "acct", 6)
			},
			staticCmd: func() interface{} {
				return NewGetReceivedByAccountCmd("acct", dcrjson.Int(6))
			},
			marshalled: `{"jsonrpc":"1.0","method":"getreceivedbyaccount","params":["acct",6],"id":1}`,
			unmarshalled: &GetReceivedByAccountCmd{
				Account: "acct",
				MinConf: dcrjson.Int(6),
			},
		},
		{
			name: "getreceivedbyaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getreceivedbyaddress"), "1Address")
			},
			staticCmd: func() interface{} {
				return NewGetReceivedByAddressCmd("1Address", nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"getreceivedbyaddress","params":["1Address"],"id":1}`,
			unmarshalled: &GetReceivedByAddressCmd{
				Address: "1Address",
				MinConf: dcrjson.Int(1),
			},
		},
		{
			name: "getreceivedbyaddress optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("getreceivedbyaddress"), "1Address", 6)
			},
			staticCmd: func() interface{} {
				return NewGetReceivedByAddressCmd("1Address", dcrjson.Int(6))
			},
			marshalled: `{"jsonrpc":"1.0","method":"getreceivedbyaddress","params":["1Address",6],"id":1}`,
			unmarshalled: &GetReceivedByAddressCmd{
				Address: "1Address",
				MinConf: dcrjson.Int(6),
			},
		},
		{
			name: "gettransaction",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("gettransaction"), "123")
			},
			staticCmd: func() interface{} {
				return NewGetTransactionCmd("123", nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"gettransaction","params":["123"],"id":1}`,
			unmarshalled: &GetTransactionCmd{
				Txid:             "123",
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "gettransaction optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("gettransaction"), "123", true)
			},
			staticCmd: func() interface{} {
				return NewGetTransactionCmd("123", dcrjson.Bool(true))
			},
			marshalled: `{"jsonrpc":"1.0","method":"gettransaction","params":["123",true],"id":1}`,
			unmarshalled: &GetTransactionCmd{
				Txid:             "123",
				IncludeWatchOnly: dcrjson.Bool(true),
			},
		},
		{
			name: "importprivkey",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("importprivkey"), "abc")
			},
			staticCmd: func() interface{} {
				return NewImportPrivKeyCmd("abc", nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"importprivkey","params":["abc"],"id":1}`,
			unmarshalled: &ImportPrivKeyCmd{
				PrivKey: "abc",
				Label:   nil,
				Rescan:  dcrjson.Bool(true),
			},
		},
		{
			name: "importprivkey optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("importprivkey"), "abc", "label")
			},
			staticCmd: func() interface{} {
				return NewImportPrivKeyCmd("abc", dcrjson.String("label"), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"importprivkey","params":["abc","label"],"id":1}`,
			unmarshalled: &ImportPrivKeyCmd{
				PrivKey: "abc",
				Label:   dcrjson.String("label"),
				Rescan:  dcrjson.Bool(true),
			},
		},
		{
			name: "importprivkey optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("importprivkey"), "abc", "label", false)
			},
			staticCmd: func() interface{} {
				return NewImportPrivKeyCmd("abc", dcrjson.String("label"), dcrjson.Bool(false), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"importprivkey","params":["abc","label",false],"id":1}`,
			unmarshalled: &ImportPrivKeyCmd{
				PrivKey: "abc",
				Label:   dcrjson.String("label"),
				Rescan:  dcrjson.Bool(false),
			},
		},
		{
			name: "importprivkey optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("importprivkey"), "abc", "label", false, 12345)
			},
			staticCmd: func() interface{} {
				return NewImportPrivKeyCmd("abc", dcrjson.String("label"), dcrjson.Bool(false), dcrjson.Int(12345))
			},
			marshalled: `{"jsonrpc":"1.0","method":"importprivkey","params":["abc","label",false,12345],"id":1}`,
			unmarshalled: &ImportPrivKeyCmd{
				PrivKey:  "abc",
				Label:    dcrjson.String("label"),
				Rescan:   dcrjson.Bool(false),
				ScanFrom: dcrjson.Int(12345),
			},
		},
		{
			name: "listaccounts",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listaccounts"))
			},
			staticCmd: func() interface{} {
				return NewListAccountsCmd(nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listaccounts","params":[],"id":1}`,
			unmarshalled: &ListAccountsCmd{
				MinConf: dcrjson.Int(1),
			},
		},
		{
			name: "listaccounts optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listaccounts"), 6)
			},
			staticCmd: func() interface{} {
				return NewListAccountsCmd(dcrjson.Int(6))
			},
			marshalled: `{"jsonrpc":"1.0","method":"listaccounts","params":[6],"id":1}`,
			unmarshalled: &ListAccountsCmd{
				MinConf: dcrjson.Int(6),
			},
		},
		{
			name: "listlockunspent",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listlockunspent"))
			},
			staticCmd: func() interface{} {
				return NewListLockUnspentCmd()
			},
			marshalled:   `{"jsonrpc":"1.0","method":"listlockunspent","params":[],"id":1}`,
			unmarshalled: &ListLockUnspentCmd{},
		},
		{
			name: "listreceivedbyaccount",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaccount"))
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAccountCmd(nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaccount","params":[],"id":1}`,
			unmarshalled: &ListReceivedByAccountCmd{
				MinConf:          dcrjson.Int(1),
				IncludeEmpty:     dcrjson.Bool(false),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaccount optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaccount"), 6)
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAccountCmd(dcrjson.Int(6), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaccount","params":[6],"id":1}`,
			unmarshalled: &ListReceivedByAccountCmd{
				MinConf:          dcrjson.Int(6),
				IncludeEmpty:     dcrjson.Bool(false),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaccount optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaccount"), 6, true)
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAccountCmd(dcrjson.Int(6), dcrjson.Bool(true), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaccount","params":[6,true],"id":1}`,
			unmarshalled: &ListReceivedByAccountCmd{
				MinConf:          dcrjson.Int(6),
				IncludeEmpty:     dcrjson.Bool(true),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaccount optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaccount"), 6, true, false)
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAccountCmd(dcrjson.Int(6), dcrjson.Bool(true), dcrjson.Bool(false))
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaccount","params":[6,true,false],"id":1}`,
			unmarshalled: &ListReceivedByAccountCmd{
				MinConf:          dcrjson.Int(6),
				IncludeEmpty:     dcrjson.Bool(true),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaddress"))
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAddressCmd(nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaddress","params":[],"id":1}`,
			unmarshalled: &ListReceivedByAddressCmd{
				MinConf:          dcrjson.Int(1),
				IncludeEmpty:     dcrjson.Bool(false),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaddress optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaddress"), 6)
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAddressCmd(dcrjson.Int(6), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaddress","params":[6],"id":1}`,
			unmarshalled: &ListReceivedByAddressCmd{
				MinConf:          dcrjson.Int(6),
				IncludeEmpty:     dcrjson.Bool(false),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaddress optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaddress"), 6, true)
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAddressCmd(dcrjson.Int(6), dcrjson.Bool(true), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaddress","params":[6,true],"id":1}`,
			unmarshalled: &ListReceivedByAddressCmd{
				MinConf:          dcrjson.Int(6),
				IncludeEmpty:     dcrjson.Bool(true),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listreceivedbyaddress optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listreceivedbyaddress"), 6, true, false)
			},
			staticCmd: func() interface{} {
				return NewListReceivedByAddressCmd(dcrjson.Int(6), dcrjson.Bool(true), dcrjson.Bool(false))
			},
			marshalled: `{"jsonrpc":"1.0","method":"listreceivedbyaddress","params":[6,true,false],"id":1}`,
			unmarshalled: &ListReceivedByAddressCmd{
				MinConf:          dcrjson.Int(6),
				IncludeEmpty:     dcrjson.Bool(true),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listsinceblock",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listsinceblock"))
			},
			staticCmd: func() interface{} {
				return NewListSinceBlockCmd(nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listsinceblock","params":[],"id":1}`,
			unmarshalled: &ListSinceBlockCmd{
				BlockHash:           nil,
				TargetConfirmations: dcrjson.Int(1),
				IncludeWatchOnly:    dcrjson.Bool(false),
			},
		},
		{
			name: "listsinceblock optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listsinceblock"), "123")
			},
			staticCmd: func() interface{} {
				return NewListSinceBlockCmd(dcrjson.String("123"), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listsinceblock","params":["123"],"id":1}`,
			unmarshalled: &ListSinceBlockCmd{
				BlockHash:           dcrjson.String("123"),
				TargetConfirmations: dcrjson.Int(1),
				IncludeWatchOnly:    dcrjson.Bool(false),
			},
		},
		{
			name: "listsinceblock optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listsinceblock"), "123", 6)
			},
			staticCmd: func() interface{} {
				return NewListSinceBlockCmd(dcrjson.String("123"), dcrjson.Int(6), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listsinceblock","params":["123",6],"id":1}`,
			unmarshalled: &ListSinceBlockCmd{
				BlockHash:           dcrjson.String("123"),
				TargetConfirmations: dcrjson.Int(6),
				IncludeWatchOnly:    dcrjson.Bool(false),
			},
		},
		{
			name: "listsinceblock optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listsinceblock"), "123", 6, true)
			},
			staticCmd: func() interface{} {
				return NewListSinceBlockCmd(dcrjson.String("123"), dcrjson.Int(6), dcrjson.Bool(true))
			},
			marshalled: `{"jsonrpc":"1.0","method":"listsinceblock","params":["123",6,true],"id":1}`,
			unmarshalled: &ListSinceBlockCmd{
				BlockHash:           dcrjson.String("123"),
				TargetConfirmations: dcrjson.Int(6),
				IncludeWatchOnly:    dcrjson.Bool(true),
			},
		},
		{
			name: "listtransactions",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listtransactions"))
			},
			staticCmd: func() interface{} {
				return NewListTransactionsCmd(nil, nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listtransactions","params":[],"id":1}`,
			unmarshalled: &ListTransactionsCmd{
				Account:          nil,
				Count:            dcrjson.Int(10),
				From:             dcrjson.Int(0),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listtransactions optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listtransactions"), "acct")
			},
			staticCmd: func() interface{} {
				return NewListTransactionsCmd(dcrjson.String("acct"), nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listtransactions","params":["acct"],"id":1}`,
			unmarshalled: &ListTransactionsCmd{
				Account:          dcrjson.String("acct"),
				Count:            dcrjson.Int(10),
				From:             dcrjson.Int(0),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listtransactions optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listtransactions"), "acct", 20)
			},
			staticCmd: func() interface{} {
				return NewListTransactionsCmd(dcrjson.String("acct"), dcrjson.Int(20), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listtransactions","params":["acct",20],"id":1}`,
			unmarshalled: &ListTransactionsCmd{
				Account:          dcrjson.String("acct"),
				Count:            dcrjson.Int(20),
				From:             dcrjson.Int(0),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listtransactions optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listtransactions"), "acct", 20, 1)
			},
			staticCmd: func() interface{} {
				return NewListTransactionsCmd(dcrjson.String("acct"), dcrjson.Int(20),
					dcrjson.Int(1), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listtransactions","params":["acct",20,1],"id":1}`,
			unmarshalled: &ListTransactionsCmd{
				Account:          dcrjson.String("acct"),
				Count:            dcrjson.Int(20),
				From:             dcrjson.Int(1),
				IncludeWatchOnly: dcrjson.Bool(false),
			},
		},
		{
			name: "listtransactions optional4",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listtransactions"), "acct", 20, 1, true)
			},
			staticCmd: func() interface{} {
				return NewListTransactionsCmd(dcrjson.String("acct"), dcrjson.Int(20),
					dcrjson.Int(1), dcrjson.Bool(true))
			},
			marshalled: `{"jsonrpc":"1.0","method":"listtransactions","params":["acct",20,1,true],"id":1}`,
			unmarshalled: &ListTransactionsCmd{
				Account:          dcrjson.String("acct"),
				Count:            dcrjson.Int(20),
				From:             dcrjson.Int(1),
				IncludeWatchOnly: dcrjson.Bool(true),
			},
		},
		{
			name: "listunspent",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listunspent"))
			},
			staticCmd: func() interface{} {
				return NewListUnspentCmd(nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listunspent","params":[],"id":1}`,
			unmarshalled: &ListUnspentCmd{
				MinConf:   dcrjson.Int(1),
				MaxConf:   dcrjson.Int(9999999),
				Addresses: nil,
			},
		},
		{
			name: "listunspent optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listunspent"), 6)
			},
			staticCmd: func() interface{} {
				return NewListUnspentCmd(dcrjson.Int(6), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listunspent","params":[6],"id":1}`,
			unmarshalled: &ListUnspentCmd{
				MinConf:   dcrjson.Int(6),
				MaxConf:   dcrjson.Int(9999999),
				Addresses: nil,
			},
		},
		{
			name: "listunspent optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listunspent"), 6, 100)
			},
			staticCmd: func() interface{} {
				return NewListUnspentCmd(dcrjson.Int(6), dcrjson.Int(100), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"listunspent","params":[6,100],"id":1}`,
			unmarshalled: &ListUnspentCmd{
				MinConf:   dcrjson.Int(6),
				MaxConf:   dcrjson.Int(100),
				Addresses: nil,
			},
		},
		{
			name: "listunspent optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("listunspent"), 6, 100, []string{"1Address", "1Address2"})
			},
			staticCmd: func() interface{} {
				return NewListUnspentCmd(dcrjson.Int(6), dcrjson.Int(100),
					&[]string{"1Address", "1Address2"})
			},
			marshalled: `{"jsonrpc":"1.0","method":"listunspent","params":[6,100,["1Address","1Address2"]],"id":1}`,
			unmarshalled: &ListUnspentCmd{
				MinConf:   dcrjson.Int(6),
				MaxConf:   dcrjson.Int(100),
				Addresses: &[]string{"1Address", "1Address2"},
			},
		},
		{
			name: "lockunspent",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("lockunspent"), true, `[{"txid":"123","vout":1}]`)
			},
			staticCmd: func() interface{} {
				txInputs := []dcrdtypes.TransactionInput{
					{Txid: "123", Vout: 1},
				}
				return NewLockUnspentCmd(true, txInputs)
			},
			marshalled: `{"jsonrpc":"1.0","method":"lockunspent","params":[true,[{"txid":"123","vout":1,"tree":0}]],"id":1}`,
			unmarshalled: &LockUnspentCmd{
				Unlock: true,
				Transactions: []dcrdtypes.TransactionInput{
					{Txid: "123", Vout: 1},
				},
			},
		},
		{
			name: "renameaccount",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("renameaccount"), "oldacct", "newacct")
			},
			staticCmd: func() interface{} {
				return NewRenameAccountCmd("oldacct", "newacct")
			},
			marshalled: `{"jsonrpc":"1.0","method":"renameaccount","params":["oldacct","newacct"],"id":1}`,
			unmarshalled: &RenameAccountCmd{
				OldAccount: "oldacct",
				NewAccount: "newacct",
			},
		},
		{
			name: "sendfrom",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendfrom"), "from", "1Address", 0.5)
			},
			staticCmd: func() interface{} {
				return NewSendFromCmd("from", "1Address", 0.5, nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendfrom","params":["from","1Address",0.5],"id":1}`,
			unmarshalled: &SendFromCmd{
				FromAccount: "from",
				ToAddress:   "1Address",
				Amount:      0.5,
				MinConf:     dcrjson.Int(1),
				Comment:     nil,
				CommentTo:   nil,
			},
		},
		{
			name: "sendfrom optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendfrom"), "from", "1Address", 0.5, 6)
			},
			staticCmd: func() interface{} {
				return NewSendFromCmd("from", "1Address", 0.5, dcrjson.Int(6), nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendfrom","params":["from","1Address",0.5,6],"id":1}`,
			unmarshalled: &SendFromCmd{
				FromAccount: "from",
				ToAddress:   "1Address",
				Amount:      0.5,
				MinConf:     dcrjson.Int(6),
				Comment:     nil,
				CommentTo:   nil,
			},
		},
		{
			name: "sendfrom optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendfrom"), "from", "1Address", 0.5, 6, "comment")
			},
			staticCmd: func() interface{} {
				return NewSendFromCmd("from", "1Address", 0.5, dcrjson.Int(6),
					dcrjson.String("comment"), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendfrom","params":["from","1Address",0.5,6,"comment"],"id":1}`,
			unmarshalled: &SendFromCmd{
				FromAccount: "from",
				ToAddress:   "1Address",
				Amount:      0.5,
				MinConf:     dcrjson.Int(6),
				Comment:     dcrjson.String("comment"),
				CommentTo:   nil,
			},
		},
		{
			name: "sendfrom optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendfrom"), "from", "1Address", 0.5, 6, "comment", "commentto")
			},
			staticCmd: func() interface{} {
				return NewSendFromCmd("from", "1Address", 0.5, dcrjson.Int(6),
					dcrjson.String("comment"), dcrjson.String("commentto"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendfrom","params":["from","1Address",0.5,6,"comment","commentto"],"id":1}`,
			unmarshalled: &SendFromCmd{
				FromAccount: "from",
				ToAddress:   "1Address",
				Amount:      0.5,
				MinConf:     dcrjson.Int(6),
				Comment:     dcrjson.String("comment"),
				CommentTo:   dcrjson.String("commentto"),
			},
		},
		{
			name: "sendmany",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendmany"), "from", `{"1Address":0.5}`)
			},
			staticCmd: func() interface{} {
				amounts := map[string]float64{"1Address": 0.5}
				return NewSendManyCmd("from", amounts, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendmany","params":["from",{"1Address":0.5}],"id":1}`,
			unmarshalled: &SendManyCmd{
				FromAccount: "from",
				Amounts:     map[string]float64{"1Address": 0.5},
				MinConf:     dcrjson.Int(1),
				Comment:     nil,
			},
		},
		{
			name: "sendmany optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendmany"), "from", `{"1Address":0.5}`, 6)
			},
			staticCmd: func() interface{} {
				amounts := map[string]float64{"1Address": 0.5}
				return NewSendManyCmd("from", amounts, dcrjson.Int(6), nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendmany","params":["from",{"1Address":0.5},6],"id":1}`,
			unmarshalled: &SendManyCmd{
				FromAccount: "from",
				Amounts:     map[string]float64{"1Address": 0.5},
				MinConf:     dcrjson.Int(6),
				Comment:     nil,
			},
		},
		{
			name: "sendmany optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendmany"), "from", `{"1Address":0.5}`, 6, "comment")
			},
			staticCmd: func() interface{} {
				amounts := map[string]float64{"1Address": 0.5}
				return NewSendManyCmd("from", amounts, dcrjson.Int(6), dcrjson.String("comment"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendmany","params":["from",{"1Address":0.5},6,"comment"],"id":1}`,
			unmarshalled: &SendManyCmd{
				FromAccount: "from",
				Amounts:     map[string]float64{"1Address": 0.5},
				MinConf:     dcrjson.Int(6),
				Comment:     dcrjson.String("comment"),
			},
		},
		{
			name: "sendtoaddress",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendtoaddress"), "1Address", 0.5)
			},
			staticCmd: func() interface{} {
				return NewSendToAddressCmd("1Address", 0.5, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendtoaddress","params":["1Address",0.5],"id":1}`,
			unmarshalled: &SendToAddressCmd{
				Address:   "1Address",
				Amount:    0.5,
				Comment:   nil,
				CommentTo: nil,
			},
		},
		{
			name: "sendtoaddress optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendtoaddress"), "1Address", 0.5, "comment", "commentto")
			},
			staticCmd: func() interface{} {
				return NewSendToAddressCmd("1Address", 0.5, dcrjson.String("comment"),
					dcrjson.String("commentto"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendtoaddress","params":["1Address",0.5,"comment","commentto"],"id":1}`,
			unmarshalled: &SendToAddressCmd{
				Address:   "1Address",
				Amount:    0.5,
				Comment:   dcrjson.String("comment"),
				CommentTo: dcrjson.String("commentto"),
			},
		},
		{
			name: "sendfromtreasury",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendfromtreasury"), "key", `{"1Address":0.5}`)
			},
			staticCmd: func() interface{} {
				var debits = map[string]float64{"1Address": 0.5}
				return NewSendFromTreasuryCmd("key", debits)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendfromtreasury","params":["key",{"1Address":0.5}],"id":1}`,
			unmarshalled: &SendFromTreasuryCmd{
				Key:     "key",
				Amounts: map[string]float64{"1Address": 0.5},
			},
		},
		{
			name: "sendtotreasury",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sendtotreasury"), 0.5)
			},
			staticCmd: func() interface{} {
				return NewSendToTreasuryCmd(0.5, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sendtotreasury","params":[0.5],"id":1}`,
			unmarshalled: &SendToTreasuryCmd{
				Amount: 0.5,
			},
		},
		{
			name: "settspendpolicy",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("settspendpolicy"), "hash", "policy")
			},
			staticCmd: func() interface{} {
				return NewSetTSpendPolicyCmd("hash", "policy", nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"settspendpolicy","params":["hash","policy"],"id":1}`,
			unmarshalled: &SetTSpendPolicyCmd{
				Hash:   "hash",
				Policy: "policy",
				Ticket: nil,
			},
		},
		{
			name: "settspendpolicy optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("settspendpolicy"), "hash", "policy", "ticket")
			},
			staticCmd: func() interface{} {
				return NewSetTSpendPolicyCmd("hash", "policy", dcrjson.String("ticket"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"settspendpolicy","params":["hash","policy","ticket"],"id":1}`,
			unmarshalled: &SetTSpendPolicyCmd{
				Hash:   "hash",
				Policy: "policy",
				Ticket: dcrjson.String("ticket"),
			},
		},
		{
			name: "settreasurypolicy",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("settreasurypolicy"), "key", "policy")
			},
			staticCmd: func() interface{} {
				return NewSetTreasuryPolicyCmd("key", "policy", nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"settreasurypolicy","params":["key","policy"],"id":1}`,
			unmarshalled: &SetTreasuryPolicyCmd{
				Key:    "key",
				Policy: "policy",
				Ticket: nil,
			},
		},
		{
			name: "settreasurypolicy optional",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("settreasurypolicy"), "key", "policy", "ticket")
			},
			staticCmd: func() interface{} {
				return NewSetTreasuryPolicyCmd("key", "policy", dcrjson.String("ticket"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"settreasurypolicy","params":["key","policy","ticket"],"id":1}`,
			unmarshalled: &SetTreasuryPolicyCmd{
				Key:    "key",
				Policy: "policy",
				Ticket: dcrjson.String("ticket"),
			},
		},
		{
			name: "settxfee",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("settxfee"), 0.0001)
			},
			staticCmd: func() interface{} {
				return NewSetTxFeeCmd(0.0001)
			},
			marshalled: `{"jsonrpc":"1.0","method":"settxfee","params":[0.0001],"id":1}`,
			unmarshalled: &SetTxFeeCmd{
				Amount: 0.0001,
			},
		},
		{
			name: "signmessage",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("signmessage"), "1Address", "message")
			},
			staticCmd: func() interface{} {
				return NewSignMessageCmd("1Address", "message")
			},
			marshalled: `{"jsonrpc":"1.0","method":"signmessage","params":["1Address","message"],"id":1}`,
			unmarshalled: &SignMessageCmd{
				Address: "1Address",
				Message: "message",
			},
		},
		{
			name: "signrawtransaction",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("signrawtransaction"), "001122")
			},
			staticCmd: func() interface{} {
				return NewSignRawTransactionCmd("001122", nil, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"signrawtransaction","params":["001122"],"id":1}`,
			unmarshalled: &SignRawTransactionCmd{
				RawTx:    "001122",
				Inputs:   nil,
				PrivKeys: nil,
				Flags:    dcrjson.String("ALL"),
			},
		},
		{
			name: "signrawtransaction optional1",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("signrawtransaction"), "001122", `[{"txid":"123","vout":1,"tree":0,"scriptPubKey":"00","redeemScript":"01"}]`)
			},
			staticCmd: func() interface{} {
				txInputs := []RawTxInput{
					{
						Txid:         "123",
						Vout:         1,
						ScriptPubKey: "00",
						RedeemScript: "01",
					},
				}

				return NewSignRawTransactionCmd("001122", &txInputs, nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"signrawtransaction","params":["001122",[{"txid":"123","vout":1,"tree":0,"scriptPubKey":"00","redeemScript":"01"}]],"id":1}`,
			unmarshalled: &SignRawTransactionCmd{
				RawTx: "001122",
				Inputs: &[]RawTxInput{
					{
						Txid:         "123",
						Vout:         1,
						ScriptPubKey: "00",
						RedeemScript: "01",
					},
				},
				PrivKeys: nil,
				Flags:    dcrjson.String("ALL"),
			},
		},
		{
			name: "signrawtransaction optional2",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("signrawtransaction"), "001122", `[]`, `["abc"]`)
			},
			staticCmd: func() interface{} {
				txInputs := []RawTxInput{}
				privKeys := []string{"abc"}
				return NewSignRawTransactionCmd("001122", &txInputs, &privKeys, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"signrawtransaction","params":["001122",[],["abc"]],"id":1}`,
			unmarshalled: &SignRawTransactionCmd{
				RawTx:    "001122",
				Inputs:   &[]RawTxInput{},
				PrivKeys: &[]string{"abc"},
				Flags:    dcrjson.String("ALL"),
			},
		},
		{
			name: "signrawtransaction optional3",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("signrawtransaction"), "001122", `[]`, `[]`, "ALL")
			},
			staticCmd: func() interface{} {
				txInputs := []RawTxInput{}
				privKeys := []string{}
				return NewSignRawTransactionCmd("001122", &txInputs, &privKeys,
					dcrjson.String("ALL"))
			},
			marshalled: `{"jsonrpc":"1.0","method":"signrawtransaction","params":["001122",[],[],"ALL"],"id":1}`,
			unmarshalled: &SignRawTransactionCmd{
				RawTx:    "001122",
				Inputs:   &[]RawTxInput{},
				PrivKeys: &[]string{},
				Flags:    dcrjson.String("ALL"),
			},
		},
		{
			name: "sweepaccount - optionals provided",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sweepaccount"), "default", "DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu", 6, 0.05)
			},
			staticCmd: func() interface{} {
				return NewSweepAccountCmd("default", "DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu",
					func(i uint32) *uint32 { return &i }(6),
					func(i float64) *float64 { return &i }(0.05))
			},
			marshalled: `{"jsonrpc":"1.0","method":"sweepaccount","params":["default","DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu",6,0.05],"id":1}`,
			unmarshalled: &SweepAccountCmd{
				SourceAccount:         "default",
				DestinationAddress:    "DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu",
				RequiredConfirmations: func(i uint32) *uint32 { return &i }(6),
				FeePerKb:              func(i float64) *float64 { return &i }(0.05),
			},
		},
		{
			name: "sweepaccount - optionals omitted",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("sweepaccount"), "default", "DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu")
			},
			staticCmd: func() interface{} {
				return NewSweepAccountCmd("default", "DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu", nil, nil)
			},
			marshalled: `{"jsonrpc":"1.0","method":"sweepaccount","params":["default","DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu"],"id":1}`,
			unmarshalled: &SweepAccountCmd{
				SourceAccount:      "default",
				DestinationAddress: "DsUZxxoHJSty8DCfwfartwTYbuhmVct7tJu",
			},
		},
		{
			name: "walletlock",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("walletlock"))
			},
			staticCmd: func() interface{} {
				return NewWalletLockCmd()
			},
			marshalled:   `{"jsonrpc":"1.0","method":"walletlock","params":[],"id":1}`,
			unmarshalled: &WalletLockCmd{},
		},
		{
			name: "walletpassphrase",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("walletpassphrase"), "pass", 60)
			},
			staticCmd: func() interface{} {
				return NewWalletPassphraseCmd("pass", 60)
			},
			marshalled: `{"jsonrpc":"1.0","method":"walletpassphrase","params":["pass",60],"id":1}`,
			unmarshalled: &WalletPassphraseCmd{
				Passphrase: "pass",
				Timeout:    60,
			},
		},
		{
			name: "walletpassphrasechange",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("walletpassphrasechange"), "old", "new")
			},
			staticCmd: func() interface{} {
				return NewWalletPassphraseChangeCmd("old", "new")
			},
			marshalled: `{"jsonrpc":"1.0","method":"walletpassphrasechange","params":["old","new"],"id":1}`,
			unmarshalled: &WalletPassphraseChangeCmd{
				OldPassphrase: "old",
				NewPassphrase: "new",
			},
		},
		{
			name: "walletpubpassphrasechange",
			newCmd: func() (interface{}, error) {
				return dcrjson.NewCmd(Method("walletpubpassphrasechange"), "old", "new")
			},
			staticCmd: func() interface{} {
				return &WalletPubPassphraseChangeCmd{OldPassphrase: "old", NewPassphrase: "new"}
			},
			marshalled: `{"jsonrpc":"1.0","method":"walletpubpassphrasechange","params":["old","new"],"id":1}`,
			unmarshalled: &WalletPubPassphraseChangeCmd{
				OldPassphrase: "old",
				NewPassphrase: "new",
			},
		},
	}

	t.Logf("Running %d tests", len(tests))
	for i, test := range tests {
		// Marshal the command as created by the new static command
		// creation function.
		marshalled, err := dcrjson.MarshalCmd("1.0", testID, test.staticCmd())
		if err != nil {
			t.Errorf("MarshalCmd #%d (%s) unexpected error: %v", i,
				test.name, err)
			continue
		}

		if !bytes.Equal(marshalled, []byte(test.marshalled)) {
			t.Errorf("Test #%d (%s) unexpected marshalled data - "+
				"got %s, want %s", i, test.name, marshalled,
				test.marshalled)
			continue
		}

		// Ensure the command is created without error via the generic
		// new command creation function.
		cmd, err := test.newCmd()
		if err != nil {
			t.Errorf("Test #%d (%s) unexpected dcrjson.NewCmd error: %v ",
				i, test.name, err)
		}

		// Marshal the command as created by the generic new command
		// creation function.
		marshalled, err = dcrjson.MarshalCmd("1.0", testID, cmd)
		if err != nil {
			t.Errorf("MarshalCmd #%d (%s) unexpected error: %v", i,
				test.name, err)
			continue
		}

		if !bytes.Equal(marshalled, []byte(test.marshalled)) {
			t.Errorf("Test #%d (%s) unexpected marshalled data - "+
				"got %s, want %s", i, test.name, marshalled,
				test.marshalled)
			continue
		}

		var request dcrjson.Request
		if err := json.Unmarshal(marshalled, &request); err != nil {
			t.Errorf("Test #%d (%s) unexpected error while "+
				"unmarshalling JSON-RPC request: %v", i,
				test.name, err)
			continue
		}

		cmd, err = dcrjson.ParseParams(Method(request.Method), request.Params)
		if err != nil {
			t.Errorf("UnmarshalCmd #%d (%s) unexpected error: %v", i,
				test.name, err)
			continue
		}

		if !reflect.DeepEqual(cmd, test.unmarshalled) {
			t.Errorf("Test #%d (%s) unexpected unmarshalled command "+
				"- got %s, want %s", i, test.name,
				fmt.Sprintf("(%T) %+[1]v", cmd),
				fmt.Sprintf("(%T) %+[1]v\n", test.unmarshalled))
			continue
		}
	}
}
