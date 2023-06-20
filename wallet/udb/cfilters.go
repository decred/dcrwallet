// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"decred.org/dcrwallet/v4/errors"
	"decred.org/dcrwallet/v4/wallet/walletdb"
	"github.com/decred/dcrd/chaincfg/chainhash"
	gcs2 "github.com/decred/dcrd/gcs/v4"
	"github.com/decred/dcrd/gcs/v4/blockcf2"
)

// CFilterV2 returns the saved regular compact filter v2 for a block along with
// the key necessary to query it for matches.
func (s *Store) CFilterV2(dbtx walletdb.ReadTx, blockHash *chainhash.Hash) ([gcs2.KeySize]byte, *gcs2.FilterV2, error) {
	ns := dbtx.ReadBucket(wtxmgrBucketKey)
	k, v, err := fetchRawCFilter2(ns, blockHash[:])
	if err != nil {
		return [16]byte{}, nil, err
	}
	vc := make([]byte, len(v)) // Copy for FromBytesV2 which stores passed slice
	copy(vc, v)
	filter, err := gcs2.FromBytesV2(blockcf2.B, blockcf2.M, vc)
	if err != nil {
		return k, nil, err
	}
	return k, filter, err
}

// ImportCFiltersV2 imports the given list of cfilters into the wallet,
// starting at the provided block height.
func (s *Store) ImportCFiltersV2(dbtx walletdb.ReadWriteTx, startHeight int32, filterData [][]byte) error {
	ns := dbtx.ReadWriteBucket(wtxmgrBucketKey)
	blockIter := makeReadBlockIterator(ns, startHeight)
	for i, fd := range filterData {
		if !blockIter.next() {
			return errors.E(errors.NotExist, errors.Errorf("block height of %d unknown", startHeight+int32(i)))
		}

		// Ensure this is actually a valid slice of cfilter data.
		_, err := gcs2.FromBytesV2(blockcf2.B, blockcf2.M, fd)
		if err != nil {
			return err
		}

		// Find out the key for this filter based on the underlying
		// block header.
		bh := blockIter.elem.Hash
		header, err := fetchRawBlockHeader(ns, keyBlockHeader(&bh))
		if err != nil {
			return err
		}
		merkleRoot := extractBlockHeaderMerkleRoot(header)
		var bcf2Key [gcs2.KeySize]byte
		copy(bcf2Key[:], merkleRoot)

		// Store the cfilter data and query key.
		err = putRawCFilter(ns, bh[:], valueRawCFilter2(bcf2Key, fd))
		if err != nil {
			return err
		}
	}

	return nil
}
