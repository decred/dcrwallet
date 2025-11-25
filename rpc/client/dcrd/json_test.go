// Copyright (c) 2025 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrd

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"decred.org/dcrwallet/v5/errors"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/wire"
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

func TestUnmarshalHeaders(t *testing.T) {
	t.Parallel()
	var h headers

	// null should be decoded to empty set of headers without error.
	input := `null`
	err := h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Headers) != 0 {
		t.Fatalf("Got %d headers, expected %d", len(h.Headers), 0)
	}

	// Invalid json should return an error.
	input = `["`
	err = h.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatalf("Expected error but got none")
	}

	// Invalid headers should return an encoding error.
	input = `["Invalid Header 1","Invalid Header 2"]`
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Valid headers should decode without error.
	header0 := &wire.BlockHeader{
		Version:      6,
		Timestamp:    time.Unix(1533513600, 0),
		Bits:         1,
		SBits:        20000000,
		Nonce:        0x18aea41a,
		StakeVersion: 6,
	}
	header0Bytes, _ := header0.Bytes()

	header1 := &wire.BlockHeader{
		Version:      1,
		Timestamp:    time.Unix(1454954400, 0),
		Bits:         0x1b01ffff,
		SBits:        2 * 1e8,
		Nonce:        0x00000000,
		StakeVersion: 3,
	}
	header1Bytes, _ := header1.Bytes()

	input = fmt.Sprintf(`["%s","%s"]`,
		hex.EncodeToString(header0Bytes),
		hex.EncodeToString(header1Bytes))

	err = h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Headers) != 2 {
		t.Fatalf("Got %d headers, expected %d", len(h.Headers), 2)
	}

	if !reflect.DeepEqual(header0, h.Headers[0]) {
		t.Fatalf("Header 0 decoded incorrectly, expected %+v, got %+v",
			header0, h.Headers[0])
	}

	if !reflect.DeepEqual(header1, h.Headers[1]) {
		t.Fatalf("Header 1 decoded incorrectly, expected %+v, got %+v",
			header1, h.Headers[1])
	}
}

func TestUnmarshalTransactions(t *testing.T) {
	t.Parallel()
	var h transactions

	// null should be decoded to empty set of transactions without error.
	input := `null`
	err := h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Transactions) != 0 {
		t.Fatalf("Got %d transactions, expected %d", len(h.Transactions), 0)
	}

	// Invalid json should return an error.
	input = `["`
	err = h.UnmarshalJSON([]byte(input))
	if err == nil {
		t.Fatalf("Expected error but got none")
	}

	// Non-strings should return an encoding error.
	input = `[1234]`
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Invalid transactions should return an encoding error.
	input = `["Invalid Transaction 1","Invalid Transaction 2"]`
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Valid transactions should decode without error.
	txn0 := &wire.MsgTx{
		SerType:  wire.TxSerializeFull,
		Version:  1,
		TxIn:     []*wire.TxIn{},
		TxOut:    []*wire.TxOut{},
		LockTime: 0,
	}
	txn0Bytes, _ := txn0.Bytes()

	txn1 := &wire.MsgTx{
		SerType:  wire.TxSerializeFull,
		Version:  123,
		TxIn:     []*wire.TxIn{},
		TxOut:    []*wire.TxOut{},
		LockTime: 69420,
	}
	txn1Bytes, _ := txn1.Bytes()

	input = fmt.Sprintf(`["%s","%s"]`,
		hex.EncodeToString(txn0Bytes),
		hex.EncodeToString(txn1Bytes))

	err = h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(h.Transactions) != 2 {
		t.Fatalf("Got %d transactions, expected %d", len(h.Transactions), 2)
	}

	if !reflect.DeepEqual(txn0, h.Transactions[0]) {
		t.Fatalf("Transaction 0 decoded incorrectly, expected %+v, got %+v",
			txn0, h.Transactions[0])
	}

	if !reflect.DeepEqual(txn1, h.Transactions[1]) {
		t.Fatalf("Transaction 1 decoded incorrectly, expected %+v, got %+v",
			txn1, h.Transactions[1])
	}
}

func TestUnmarshalHash(t *testing.T) {
	t.Parallel()
	var h hash

	// Hash too short should return an encoding error.
	const expectedLen = 64
	hash := strings.Repeat("f", expectedLen-1)
	input := fmt.Sprintf(`"%s"`, hash)
	err := h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Hash too long should return an encoding error.
	hash = strings.Repeat("f", expectedLen+1)
	input = fmt.Sprintf(`"%s"`, hash)
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Non-strings should return an error.
	input = fmt.Sprintf(`"%s`, hash) // missing end quote
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	input = fmt.Sprintf(`%s"`, hash) // missing start quote
	err = h.UnmarshalJSON([]byte(input))
	if !errors.Is(err, errors.Encoding) {
		t.Fatalf("Expected errors.Encoding, got %v", err)
	}

	// Valid hash should decode without error.
	hash = strings.Repeat("f", expectedLen)
	input = fmt.Sprintf(`"%s"`, hash)
	err = h.UnmarshalJSON([]byte(input))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	expected := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if h.Hash.String() != expected {
		t.Fatalf("Hash decoded incorrectly, got %q expected %q",
			h.Hash.String(), expected)
	}

}
