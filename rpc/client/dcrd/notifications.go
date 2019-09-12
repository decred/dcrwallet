// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrd

import (
	"encoding/json"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
)

func unmarshalArray(j json.RawMessage, params ...interface{}) error {
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

// MissedTickets extracts the missed ticket hashes from the parameters of a
// spentandmissedtickets JSON-RPC notification.
func MissedTickets(params json.RawMessage) (missed []*chainhash.Hash, err error) {
	// Parameters (array):
	// 0: Block hash (reversed hex)
	// 1: Block height (numeric)
	// 2: Stake difficulty (numeric)
	// 3: Object of tickets (keyed by ticket hash, value is "missed" or "spent")
	//
	// Of these, we only need the hashes of missed tickets.
	var paramArray []json.RawMessage
	err = json.Unmarshal(params, &paramArray)
	if err != nil {
		return nil, errors.E(errors.Encoding, err)
	}
	if len(paramArray) < 4 {
		return nil, errors.E(errors.Protocol, "missing notification parameters")
	}
	ticketObj := make(map[string]string)
	err = json.Unmarshal(paramArray[3], &ticketObj)
	if err != nil {
		return nil, errors.E(errors.Encoding, err)
	}
	for k, v := range ticketObj {
		if v != "missed" {
			continue
		}
		hash, err := chainhash.NewHashFromStr(k)
		if err != nil {
			return nil, errors.E(errors.Encoding, err)
		}
		missed = append(missed, hash)
	}
	return missed, nil
}
