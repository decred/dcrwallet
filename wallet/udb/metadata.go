// Copyright (c) 2017-2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

type unifiedDBMetadata struct {
}

var metadataRootBucketKey = []byte("meta")

func (unifiedDBMetadata) rootBucketKey() []byte { return metadataRootBucketKey }

const unifiedDBMetadataVersionKey = "ver"

func (unifiedDBMetadata) putVersion(bucket walletdb.ReadWriteBucket, version uint32) error {
	buf := make([]byte, 4)
	byteOrder.PutUint32(buf, version)
	err := bucket.Put([]byte(unifiedDBMetadataVersionKey), buf)
	if err != nil {
		return errors.E(errors.IO, err)
	}
	return nil
}

func (unifiedDBMetadata) getVersion(bucket walletdb.ReadBucket) (uint32, error) {
	v := bucket.Get([]byte(unifiedDBMetadataVersionKey))
	if len(v) != 4 {
		return 0, errors.E(errors.IO, errors.Errorf("bad udb version len %d", len(v)))
	}
	return byteOrder.Uint32(v), nil
}
