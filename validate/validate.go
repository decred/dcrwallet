// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/*
Package validate provides context-free consensus validation.
*/
package validate

import (
	"decred.org/dcrwallet/errors"
	blockchain "github.com/decred/dcrd/blockchain/standalone"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs/v2"
	"github.com/decred/dcrd/wire"
)

const (
	// DCP0005ActiveHeightTestNet3 is the height of activation for DCP0005
	// in TestNet3.
	DCP0005ActiveHeightTestNet3 int32 = 323328

	// DCP0005ActiveHeightMainNet is the height of activation for DCP0005
	// in MainNet.
	DCP0005ActiveHeightMainNet int32 = 431488
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

// CFilterV2HeaderCommitment ensures the given v2 committed filter has actually
// been committed to in the header, assuming dcp0005 was activated.
func CFilterV2HeaderCommitment(net wire.CurrencyNet, header *wire.BlockHeader, filter *gcs.FilterV2, leafIndex uint32, proof []chainhash.Hash) error {
	const opf = "validate.CFilterV2HeaderCommitment(%v)"

	// The commitment for cfilters of blocks before dcp0005 activates are
	// _not_ stored in the stakeroot. They are validated by hashing the
	// full set of cfilters prior to DCP0005 and comparing that hash to a
	// known target hash. This is done by the wallet on a different
	// function, so for headers before DCP0005 we simply consider them as
	// valid.
	switch {
	case net == wire.TestNet3:
		if header.Height < uint32(DCP0005ActiveHeightTestNet3) {
			return nil
		}
	case net == wire.MainNet:
		if header.Height < uint32(DCP0005ActiveHeightMainNet) {
			return nil
		}
	}

	// The inclusion proof should verify that the filter hash is included
	// in the stake root of the header (root for header commitments as
	// defined in DCP0005).
	filterHash := filter.Hash()
	root := header.StakeRoot
	if !blockchain.VerifyInclusionProof(&root, &filterHash, leafIndex, proof) {
		blockHash := header.BlockHash()
		op := errors.Opf(opf, &blockHash)
		err := errors.Errorf("invalid header inclusion proof for cfilterv2")
		return errors.E(op, errors.Consensus, err)
	}
	return nil
}

// PreDCP0005CfilterHash returns nil if the hash for the given set of cf data
// matches the exepected hash for the given network.
func PreDCP0005CFilterHash(net wire.CurrencyNet, cfsethash *chainhash.Hash) error {
	var targetHash string

	switch net {
	case wire.TestNet3:
		targetHash = "619c08f5adda6f834212bbdaee3002fdc4efed731477af6c0fed490bbe2488d0"
	case wire.MainNet:
		targetHash = "f95e09f9ded38f8d6c32e5158a1f286633881393659218c63f5ab0fc86b36c83"
	default:
		return errors.Errorf("unknown network %d", net)
	}

	var target chainhash.Hash
	err := chainhash.Decode(&target, targetHash)
	if err != nil {
		return err
	}

	if !target.IsEqual(cfsethash) {
		return errors.Errorf("hash of provided cfilters %s does not match expected hash %s", cfsethash, target)
	}

	return nil
}
