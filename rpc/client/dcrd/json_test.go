// Copyright (c) 2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrd

import (
	"bytes"
	"fmt"
	"testing"

	"decred.org/dcrwallet/v5/errors"
	"github.com/decred/dcrd/chaincfg/chainhash"
)

func TestUnmarshalHashes(t *testing.T) {
	t.Parallel()
	var h hashes

	// null should be decoded to empty set of hashes without error.
	input := `null`
	err := h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Hashes) != 0 {
		t.Fatalf("Got %d hashes, expected %d", len(h.Hashes), 0)
	}

	// Invalid json should return an error.
	input = `["`
	err = h.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatalf("Expected error but got none")
	}

	// Invalid hashes should return an encoding error.
	input = `["Invalid Hash 1","Invalid Hash 2"]`
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Valid hashes should decode without error.
	const hash0 = "b751872e37415e433250d6c8af37441632724ed920b71072b0fc5f9604472633"
	const hash1 = "d41988665c526f7d484c14e1c2a0973f6b0556ed5a0e28c4584977e21ef6d270"
	input = fmt.Sprintf(`["%s","%s"]`, hash0, hash1)
	err = h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Hashes) != 2 {
		t.Fatalf("Got %d hashes, expected %d", len(h.Hashes), 2)
	}
	if h.Hashes[0].String() != hash0 {
		t.Fatalf("Hash 0 decoded incorrectly, expected %q, got %q",
			hash0, h.Hashes[0].String())
	}
	if h.Hashes[1].String() != hash1 {
		t.Fatalf("Hash 1 decoded incorrectly, expected %q, got %q",
			hash1, h.Hashes[1].String())
	}
}

func TestMarshalHashes(t *testing.T) {
	t.Parallel()
	var h hashes

	// Empty set of hashes should encode to null without error.
	output, err := h.MarshalJSON()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expected := `null`
	if !bytes.Equal(output, []byte(expected)) {
		t.Fatalf("Incorrect encoding, expected %q got %q",
			expected, output)
	}

	// Valid hashes should encode without error.
	const h0 = "b751872e37415e433250d6c8af37441632724ed920b71072b0fc5f9604472633"
	const h1 = "d41988665c526f7d484c14e1c2a0973f6b0556ed5a0e28c4584977e21ef6d270"
	hash0, _ := chainhash.NewHashFromStr(h0)
	hash1, _ := chainhash.NewHashFromStr(h1)
	h.Hashes = []*chainhash.Hash{hash0, hash1}
	output, err = h.MarshalJSON()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expected = fmt.Sprintf(`["%s","%s"]`, h0, h1)
	if !bytes.Equal(output, []byte(expected)) {
		t.Fatalf("Incorrect encoding, expected %q got %q",
			expected, output)
	}
}

func TestUnmarshalHashesContiguous(t *testing.T) {
	t.Parallel()
	var h hashesContiguous

	// null should be decoded to empty set of hashes without error.
	input := `null`
	err := h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Hashes) != 0 {
		t.Fatalf("Got %d hashes, expected %d", len(h.Hashes), 0)
	}

	// Invalid json should return an error.
	input = `["`
	err = h.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatalf("Expected error but got none")
	}

	// Invalid hashes should return an encoding error.
	input = `["Invalid Hash 1","Invalid Hash 2"]`
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Valid hashes should decode without error.
	const hash0 = "b751872e37415e433250d6c8af37441632724ed920b71072b0fc5f9604472633"
	const hash1 = "d41988665c526f7d484c14e1c2a0973f6b0556ed5a0e28c4584977e21ef6d270"
	input = fmt.Sprintf(`["%s","%s"]`, hash0, hash1)
	err = h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Hashes) != 2 {
		t.Fatalf("Got %d hashes, expected %d", len(h.Hashes), 2)
	}
	if h.Hashes[0].String() != hash0 {
		t.Fatalf("Hash 0 decoded incorrectly, expected %q, got %q",
			hash0, h.Hashes[0].String())
	}
	if h.Hashes[1].String() != hash1 {
		t.Fatalf("Hash 1 decoded incorrectly, expected %q, got %q",
			hash1, h.Hashes[1].String())
	}
}

func TestMarshalHashesContiguous(t *testing.T) {
	t.Parallel()
	var h hashesContiguous

	// Empty set of hashes should encode to null without error.
	output, err := h.MarshalJSON()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expected := `null`
	if !bytes.Equal(output, []byte(expected)) {
		t.Fatalf("Incorrect encoding, expected %q got %q",
			expected, output)
	}

	// Valid hashes should encode without error.
	const h0 = "b751872e37415e433250d6c8af37441632724ed920b71072b0fc5f9604472633"
	const h1 = "d41988665c526f7d484c14e1c2a0973f6b0556ed5a0e28c4584977e21ef6d270"
	hash0, _ := chainhash.NewHashFromStr(h0)
	hash1, _ := chainhash.NewHashFromStr(h1)
	h.Hashes = []chainhash.Hash{*hash0, *hash1}
	output, err = h.MarshalJSON()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expected = fmt.Sprintf(`["%s","%s"]`, h0, h1)
	if !bytes.Equal(output, []byte(expected)) {
		t.Fatalf("Incorrect encoding, expected %q got %q",
			expected, output)
	}
}
