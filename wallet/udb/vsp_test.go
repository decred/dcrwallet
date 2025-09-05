// Copyright (c) 2023-2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"decred.org/dcrwallet/v5/errors"
	"decred.org/dcrwallet/v5/wallet/walletdb"
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

func TestSetGetDeleteVSPTicket(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, _, _, teardown, err := cloneDB(ctx, "vsp.kv")
	defer teardown()
	if err != nil {
		t.Fatal(err)
	}

	ticketHash, _ := chainhash.NewHashFromStr("0a472047bfcf1a19580412536570e351d2ac3f30638491c1c1268d6ada514858")
	feeHash, _ := chainhash.NewHashFromStr("679c508625503e5cd00ba8d35badc40ce040df8986c11321758db831b78f94ec")

	ticket := &VSPTicket{
		FeeHash:     *feeHash,
		FeeTxStatus: 123,
		VSPHostID:   321,
		Host:        "example.com",
		PubKey:      []byte("not-a-real-key"),
	}

	// Non-existent tickets should return an error.
	err = walletdb.View(ctx, db, func(dbtx walletdb.ReadTx) error {
		_, err = GetVSPTicket(dbtx, *ticketHash)
		return err
	})
	if !errors.Is(err, errors.NotExist) {
		t.Fatalf("Expected NotExist error, got: %v", err)
	}

	// Insert into DB.
	err = walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
		err := SetVSPTicket(dbtx, ticketHash, ticket)
		return err
	})
	if err != nil {
		t.Fatalf("Inserting VSP ticket failed: %v", err)
	}

	// Retrieve from DB and check retrieved data matches inserted data.
	var got *VSPTicket
	err = walletdb.View(ctx, db, func(dbtx walletdb.ReadTx) error {
		got, err = GetVSPTicket(dbtx, *ticketHash)
		return err
	})
	if err != nil {
		t.Fatalf("Retrieving VSP ticket failed: %v", err)
	}
	if !reflect.DeepEqual(got, ticket) {
		t.Fatalf("Retrieved data does not match inserted data")
	}

	// Delete from DB.
	err = walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
		return DeleteVSPTicket(dbtx, *ticketHash)
	})
	if err != nil {
		t.Fatalf("Deleting VSP ticket failed: %v", err)
	}

	// Deleted data should no longer be retrievable.
	err = walletdb.View(ctx, db, func(dbtx walletdb.ReadTx) error {
		_, err = GetVSPTicket(dbtx, *ticketHash)
		return err
	})
	if !errors.Is(err, errors.NotExist) {
		t.Fatalf("Expected NotExist error, got: %v", err)
	}

	// Deleting non-existent ticket should not return an error.
	err = walletdb.Update(ctx, db, func(dbtx walletdb.ReadWriteTx) error {
		return DeleteVSPTicket(dbtx, *ticketHash)
	})
	if err != nil {
		t.Fatalf("Got unexpected error deleting non-existent ticket: %v", err)
	}
}
