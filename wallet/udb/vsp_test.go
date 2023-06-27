// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"testing"

	"github.com/decred/dcrd/chaincfg/chainhash"
)

func TestVSPSerializeVSPHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data *VSPHost
	}{
		{
			name: "all values",
			data: &VSPHost{
				Host: []byte("my host"),
			},
		},
		{
			name: "empty host",
			data: &VSPHost{
				Host: []byte(""),
			},
		},
	}

	for i, test := range tests {
		before := test.data

		// Serialize and deserialize data.
		after := deserializeVSPHost(serializeVSPHost(before))

		// Ensure data before == data after.
		if !bytes.Equal(before.Host, after.Host) {
			t.Fatalf("test %d: host not serialized correctly, expected %q, got %q",
				i, before.Host, after.Host)
		}
	}

}

func TestVSPSerializeVSPPubKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data *VSPPubKey
	}{
		{
			name: "all values",
			data: &VSPPubKey{
				ID:     69,
				PubKey: []byte("my public key"),
			},
		},
		{
			name: "empty ID",
			data: &VSPPubKey{
				PubKey: []byte("my public key"),
			},
		},
		{
			name: "empty pubkey",
			data: &VSPPubKey{
				ID: 69,
			},
		},
	}
	for i, test := range tests {
		before := test.data

		// Serialize and deserialize data.
		after := deserializeVSPPubKey(serializeVSPPubKey(before))

		// Ensure data before == data after.
		if before.ID != after.ID {
			t.Fatalf("test %d: ID not serialized correctly, expected %d, got %d",
				i, before.ID, after.ID)
		}

		if !bytes.Equal(before.PubKey, after.PubKey) {
			t.Fatalf("test %d: pubkey not serialized correctly, expected %q, got %q",
				i, before.PubKey, after.PubKey)
		}
	}
}

func TestVSPSerializeVSPTicket(t *testing.T) {
	t.Parallel()

	feeHash, _ := chainhash.NewHashFromStr("679c508625503e5cd00ba8d35badc40ce040df8986c11321758db831b78f94ec")

	tests := []struct {
		name string
		data *VSPTicket
	}{
		{
			name: "all values",
			data: &VSPTicket{
				FeeHash:     *feeHash,
				FeeTxStatus: 123,
				VSPHostID:   321,
				Host:        "example.com",
				PubKey:      []byte("not-a-real-key"),
			},
		},
		{
			name: "empty fee hash",
			data: &VSPTicket{
				FeeTxStatus: 123,
				VSPHostID:   321,
			},
		},
		{
			name: "empty feetxstatus",
			data: &VSPTicket{
				FeeHash:   *feeHash,
				VSPHostID: 321,
			},
		},
		{
			name: "empty vsphostid",
			data: &VSPTicket{
				FeeHash:     *feeHash,
				FeeTxStatus: 123,
			},
		},
	}

	for i, test := range tests {
		before := test.data

		// Serialize and deserialize data.
		after := deserializeVSPTicket(serializeVSPTicket(before))

		// Ensure data before == data after.
		if !bytes.Equal(before.FeeHash[:], after.FeeHash[:]) {
			t.Fatalf("test %d: fee hash not serialized correctly, expected %q, got %q",
				i, before.FeeHash, after.FeeHash)
		}

		if before.FeeTxStatus != after.FeeTxStatus {
			t.Fatalf("test %d: feetxstatus not serialized correctly, expected %d, got %d",
				i, before.FeeTxStatus, after.FeeTxStatus)
		}

		if before.VSPHostID != after.VSPHostID {
			t.Fatalf("test %d: vsphostid not serialized correctly, expected %d, got %d",
				i, before.VSPHostID, after.VSPHostID)
		}

		// Host and PubKey are not serialized so should always be empty.
		if len(after.Host) != 0 {
			t.Fatalf("test %d: expected empty host but had value %q",
				i, after.Host)
		}

		if len(after.PubKey) != 0 {
			t.Fatalf("test %d: expected empty pubkey but had value %q",
				i, after.PubKey)
		}
	}
}
