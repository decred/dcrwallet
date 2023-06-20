// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrwallet

import (
	"context"
	"encoding/json"
	"testing"

	"decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/wire"
)

type caller struct {
	response []byte
}

func (c *caller) Call(ctx context.Context, method string, res interface{}, args ...interface{}) error {
	return json.Unmarshal(c.response, res)
}

// empty transaction encoded as a json hex string
const zeroTx = `"010000000000000000000000000000"`

var mainnetParams = chaincfg.MainNetParams()

func TestSignRawTransactionErrors(t *testing.T) {
	expectedErrs := []types.SignRawTransactionError{{
		TxID:      "bfc0e650ad0cc0dd5fa88b6bc84beb5ea4a675b4353671532796171ed319341b",
		Vout:      0,
		ScriptSig: "0123456789abcdef",
		Sequence:  123,
		Error:     "bad things happened",
	}, {
		TxID:      "79dd1f6a5b7fa43407d47ff203259efe9a78453605ffd07b4db1703fed339066",
		Vout:      1,
		ScriptSig: "abcdef0123456789",
		Sequence:  456,
		Error:     "other things happened",
	}}

	caller := new(caller)
	client := NewClient(caller, mainnetParams)
	caller.response = []byte(`{
		"hex":` + zeroTx + `,
		"complete": false,
		"errors": [
			{
				"txid": "bfc0e650ad0cc0dd5fa88b6bc84beb5ea4a675b4353671532796171ed319341b",
				"vout": 0,
				"scriptSig": "0123456789abcdef",
				"sequence": 123,
				"error": "bad things happened"
			},
			{
				"txid": "79dd1f6a5b7fa43407d47ff203259efe9a78453605ffd07b4db1703fed339066",
				"vout": 1,
				"scriptSig": "abcdef0123456789",
				"sequence": 456,
				"error": "other things happened"
			}
		]
	}`)

	ctx := context.Background()
	tx := wire.NewMsgTx()
	calls := []func() (*wire.MsgTx, bool, error){
		func() (*wire.MsgTx, bool, error) {
			return client.SignRawTransaction(ctx, tx)
		},
		func() (*wire.MsgTx, bool, error) {
			return client.SignRawTransaction2(ctx, tx, nil)
		},
		func() (*wire.MsgTx, bool, error) {
			return client.SignRawTransaction3(ctx, tx, nil, nil)
		},
		func() (*wire.MsgTx, bool, error) {
			return client.SignRawTransaction4(ctx, tx, nil, nil, "ALL")
		},
	}
	for i, f := range calls {
		t.Logf("call %d (with errors)", i)
		tx, complete, err := f()
		// Expect error to be SignatureErrors
		sigErrs, ok := err.(SignatureErrors)
		if !ok {
			t.Fatal("failed to return signature errors")
		}
		if tx == nil {
			t.Fatal("did not return partially signed tx with result")
		}
		if complete {
			t.Fatal("complete should be unmarshaled as false")
		}
		if len(sigErrs) != 2 {
			t.Fatal("expected two signature errors")
		}
		// All value fields so can compare with ==
		if sigErrs[0] != expectedErrs[0] {
			t.Fatal("first error did not marshal to expected value")
		}
		if sigErrs[1] != expectedErrs[1] {
			t.Fatal("second error did not marshal to expected value")
		}
	}

	// Test that all calls do not incorrectly return the error type when it
	// is missing from the response.
	caller.response = []byte(`{
		"hex":` + zeroTx + `,
		"complete": true
	}`)
	for i, f := range calls {
		t.Logf("call %d (without errors)", i)
		tx, complete, err := f()
		if err != nil {
			t.Fatalf("call errored: %#v", err)
		}
		if tx == nil {
			t.Fatal("did not return fully signed tx")
		}
		if !complete {
			t.Fatal("complete should be marshaled as true")
		}
	}
}
