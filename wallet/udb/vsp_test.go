// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package udb

import (
	"bytes"
	"testing"
)

func Test_VSP_SerializeVSPHost(t *testing.T) {
	before := &VSPHost{
		Host:   []byte("my host"),
		PubKey: []byte("my pubkey"),
	}

	// Serialize and then deserialize data.

	after := deserializeVSPHost(serializeVSPHost(before))

	// Ensure data before == data after.

	if !bytes.Equal(before.Host, after.Host) {
		t.Fatalf("vsp host not serialized correctly, expected %q, got %q",
			before.Host, after.Host)
	}

	if !bytes.Equal(before.PubKey, after.PubKey) {
		t.Fatalf("vsp pubkey not serialized correctly, expected %q, got %q",
			before.PubKey, after.PubKey)
	}
}
