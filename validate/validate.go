// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/*
Package validate provides context-free consensus validation.
*/
package validate

import (
	"bytes"

	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/gcs"
	"github.com/decred/dcrd/gcs/blockcf"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
)

// MerkleRoots recreates the merkle roots of regular and stake transactions from
// a block and compares them against the recorded merkle roots in the block
// header.
func MerkleRoots(block *wire.MsgBlock) error {
	const opf = "validate.MerkleRoots(%v)"

	mroot := blockchain.CalcTxTreeMerkleRoot(block.Transactions)
	if block.Header.MerkleRoot != mroot {
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		return errors.E(op, errors.Consensus, "invalid regular merkle root")
	}
	mroot = blockchain.CalcTxTreeMerkleRoot(block.STransactions)
	if block.Header.StakeRoot != mroot {
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		return errors.E(op, errors.Consensus, "invalid stake merkle root")
	}

	return nil
}

// DCP0005MerkleRoot recreates the combined regular and stake transaction merkle
// root and compares it against the merkle root in the block header.
//
// DCP0005 (https://github.com/decred/dcps/blob/master/dcp-0005/dcp-0005.mediawiki)
// describes (among other changes) the hard forking change which combined the
// individual regular and stake merkle roots into a single root.
func DCP0005MerkleRoot(block *wire.MsgBlock) error {
	const opf = "validate.DCP0005MerkleRoot(%v)"

	mroot := blockchain.CalcCombinedTxTreeMerkleRoot(block.Transactions, block.STransactions)
	if block.Header.MerkleRoot != mroot {
		blockHash := block.BlockHash()
		op := errors.Opf(opf, &blockHash)
		return errors.E(op, errors.Consensus, "invalid combined merkle root")
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
