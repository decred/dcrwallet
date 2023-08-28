// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrd

import (
	"encoding/json"

	"decred.org/dcrwallet/v4/errors"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
)

func unmarshalArray(j json.RawMessage, params ...any) error {
	err := json.Unmarshal(j, &params)
	if err != nil {
		return errors.E(errors.Encoding, err)
	}
	return nil
}

// WinningTickets extracts the parameters from a winningtickets JSON-RPC
// notification.
func WinningTickets(params json.RawMessage) (block *chainhash.Hash, height int32, winners []*chainhash.Hash, err error) {
	// Parameters (array):
	// 0: block hash (reversed hex string)
	// 1: block height (number)
	// 2: object with ticket hashes in values
	ticketObj := make(map[string]*hash)
	hash := new(hash)
	err = unmarshalArray(params, hash, &height, &ticketObj)
	if err != nil {
		return
	}
	winners = make([]*chainhash.Hash, 0, len(ticketObj))
	for _, h := range ticketObj {
		winners = append(winners, h.Hash)
	}
	return hash.Hash, height, winners, nil
}

// BlockConnected extracts the parameters from a blockconnected JSON-RPC
// notification.
func BlockConnected(params json.RawMessage) (header *wire.BlockHeader, relevant []*wire.MsgTx, err error) {
	// Parameters (array):
	// 0: block header
	// 1: array of relevant hex-encoded transactions
	header = new(wire.BlockHeader)
	txs := new(transactions)
	err = unmarshalArray(params, unhex(header), txs)
	if err != nil {
		return
	}
	return header, txs.Transactions, nil
}

// RelevantTxAccepted extracts the parameters from a relevanttxaccepted JSON-RPC
// notification.
func RelevantTxAccepted(params json.RawMessage) (tx *wire.MsgTx, err error) {
	// Parameters (array):
	// 0: relevant hex-encoded transaction
	tx = new(wire.MsgTx)
	err = unmarshalArray(params, unhex(tx))
	return
}

// TSpend extracts the parameters from a tspend JSON-RPC notification.
func TSpend(params json.RawMessage) (tx *wire.MsgTx, err error) {
	// Parameters (array):
	// 0: relevant hex-encoded transaction
	tx = new(wire.MsgTx)
	err = unmarshalArray(params, unhex(tx))
	return
}
