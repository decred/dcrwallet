// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/*
Package validate provides context-free consensus validation.
*/
package validate

import (
	"bytes"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors"
)

// MerkleRoots recreates the merkle roots of regular and stake transactions from
// a block and compares them against the recorded merkle roots in the block
// header.
func MerkleRoots(block *wire.MsgBlock) error {
	const opf = "validate.MerkleRoots(%v)"

	merkles := blockchain.BuildMsgTxMerkleTreeStore(block.Transactions)
	if block.Header.MerkleRoot != *merkles[len(merkles)-1] {
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		return errors.E(op, errors.Consensus, "invalid regular merkle root")
	}
	merkles = blockchain.BuildMsgTxMerkleTreeStore(block.STransactions)
	if block.Header.StakeRoot != *merkles[len(merkles)-1] {
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		return errors.E(op, errors.Consensus, "invalid stake merkle root")
	}

	return nil
}

// RegularCFilter verifies a regular committed filter received over wire
// protocol matches the block.  Currently (without cfilter header commitments)
// this is performed by reconstructing the filter from the block and comparing
// equality.
func RegularCFilter(block *wire.MsgBlock, filter *gcs.Filter) error {
	const opf = "validate.RegularCFilter(%v)"

	f, err := blockcf.Regular(block)
	if err != nil {
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		return errors.E(op, err)
	}
	eq := f.N() == filter.N()
	eq = eq && f.P() == filter.P()
	eq = eq && bytes.Equal(f.Bytes(), filter.Bytes())
	if !eq {
		// Committed filters are not technically consensus yet, but are planned
		// to be.
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		err := errors.Errorf("invalid regular cfilter: want=%x got=%x",
			f.NBytes(), filter.NBytes())
		return errors.E(op, errors.Consensus, err)
	}
	return nil
}
