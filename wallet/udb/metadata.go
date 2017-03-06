// Copyright (c) 2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"github.com/decred/dcrwallet/apperrors"
	"github.com/decred/dcrwallet/walletdb"
)

type unifiedDBMetadata struct {
}

func (unifiedDBMetadata) rootBucketKey() []byte { return []byte("meta") }

const unifiedDBMetadataVersionKey = "ver"

func (unifiedDBMetadata) putVersion(bucket walletdb.ReadWriteBucket, version uint32) error {
	buf := make([]byte, 4)
	byteOrder.PutUint32(buf, version)
	err := bucket.Put([]byte(unifiedDBMetadataVersionKey), buf)
	if err != nil {
		const str = "failed to put unified database metadata bucket"
		return apperrors.E{ErrorCode: apperrors.ErrDatabase, Description: str, Err: err}
	}
	return nil
}

func (unifiedDBMetadata) getVersion(bucket walletdb.ReadBucket) (uint32, error) {
	v := bucket.Get([]byte(unifiedDBMetadataVersionKey))
	if len(v) != 4 {
		const str = "missing or incorrectly sized unified database version"
		return 0, apperrors.E{ErrorCode: apperrors.ErrData, Description: str, Err: nil}
	}
	return byteOrder.Uint32(v), nil
}
