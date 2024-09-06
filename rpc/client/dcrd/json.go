// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcrd

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"decred.org/dcrwallet/v5/errors"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/gcs/v4"
	blockcf "github.com/decred/dcrd/gcs/v4/blockcf2"
	"github.com/decred/dcrd/wire"
)

type deserializer interface {
	Deserialize(r io.Reader) error
}

type unmarshalFunc func(j []byte) error

func (f *unmarshalFunc) UnmarshalJSON(j []byte) error {
	return (*f)(j)
}

// unhex returns a json.Unmarshaler which unmarshals a hex-encoded wire message.
func unhex(msg deserializer) json.Unmarshaler {
	f := func(j []byte) error {
		if len(j) < 2 || j[0] != '"' || j[len(j)-1] != '"' {
			return errors.E(errors.Encoding, "not a string")
		}
		err := msg.Deserialize(hex.NewDecoder(bytes.NewReader(j[1 : len(j)-1])))
		if err != nil {
			return errors.E(errors.Encoding, err)
		}
		return nil
	}
	return (*unmarshalFunc)(&f)
}

// cfilterV2 implements deserializer to read a version 2 committed filter from
// a io.Reader.  Filters are assumed to be serialized as <n filter> with
// consensus-determined B and M values.
//
// Note that this is only the deserializer for the raw filter data and *not*
// for the full response to a cfilterv2 call (see cfilterv2Reply for that).
type cfilterV2 struct {
	Filter *gcs.FilterV2
}

func (f *cfilterV2) Deserialize(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.Filter, err = gcs.FromBytesV2(blockcf.B, blockcf.M, b)
	return err
}

func (f *cfilterV2) UnmarshalJSON(j []byte) error {
	if len(j)&1 == 1 || j[0] != '"' || j[len(j)-1] != '"' {
		return errors.E(errors.Encoding, "cfilterv2 is not an even hex string")
	}

	b := make([]byte, len(j)/2-1)
	_, err := hex.Decode(b, j[1:len(j)-1])
	if err != nil {
		return errors.E(errors.Encoding, err)
	}

	f.Filter, err = gcs.FromBytesV2(blockcf.B, blockcf.M, b)
	return err
}

// cfilterv2Reply implements the reply to a cfilterv2 query to a dcrd rpc
// server.
type cfilterV2Reply struct {
	Filter      *cfilterV2 `json:"data"`
	ProofIndex  uint32     `json:"proofindex"`
	ProofHashes *hashes    `json:"proofhashes"`
}

// hashes converts the internal []*chainhash.Hash to a []chainhash.Hash used by
// inclusion proofs.
func (r cfilterV2Reply) proofHashes() []chainhash.Hash {
	if r.ProofHashes == nil {
		return nil
	}
	h := make([]chainhash.Hash, len(r.ProofHashes.Hashes))
	for i, hp := range r.ProofHashes.Hashes {
		h[i] = *hp
	}
	return h
}

// hash implements json.Unmarshaler to decode 32-byte reversed hex hashes.
type hash struct {
	Hash *chainhash.Hash
}

func (h *hash) UnmarshalJSON(j []byte) error {
	if len(j) != 66 || j[0] != '"' || j[len(j)-1] != '"' {
		return errors.E(errors.Encoding, "not a 32-byte hash hex string")
	}
	h.Hash = new(chainhash.Hash)
	_, err := hex.Decode(h.Hash[:], j[1:len(j)-1])
	if err != nil {
		return errors.E(errors.Encoding, err)
	}
	// Unreverse hash
	for i, j := 0, len(h.Hash)-1; i < j; i, j = i+1, j-1 {
		h.Hash[i], h.Hash[j] = h.Hash[j], h.Hash[i]
	}
	return nil
}

// hashes implements json.Marshaler/Unmarshaler to encode and decode slices of
// hashes as JSON arrays of reversed hex strings.
type hashes struct {
	Hashes []*chainhash.Hash
}

func (h *hashes) UnmarshalJSON(j []byte) error {
	if bytes.Equal(j, []byte("null")) {
		h.Hashes = nil
		return nil
	}
	var array []string
	err := json.Unmarshal(j, &array)
	if err != nil {
		return nil
	}
	h.Hashes = make([]*chainhash.Hash, len(array))
	for i, s := range array {
		hash, err := chainhash.NewHashFromStr(s)
		if err != nil {
			return errors.E(errors.Encoding, err)
		}
		h.Hashes[i] = hash
	}
	return nil
}

func (h *hashes) MarshalJSON() ([]byte, error) {
	if h.Hashes == nil {
		return []byte("null"), nil
	}
	buf := new(bytes.Buffer)
	scratch32 := make([]byte, 32)
	scratch64 := make([]byte, 64)
	buf.Grow(2 + 67*len(h.Hashes)) // 2 for [], 64+2+1 per hash for strings, quotes, and separator
	buf.WriteByte('[')
	for i, h := range h.Hashes {
		if i != 0 {
			buf.WriteByte(',')
		}
		// Reverse hash into scratch space
		for i := 0; i < 32; i++ {
			scratch32[31-i] = h[i]
		}
		// Write hex encoding of reversed hash
		hex.Encode(scratch64, scratch32)
		buf.WriteByte('"')
		buf.Write(scratch64)
		buf.WriteByte('"')
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

type hashesContiguous struct {
	Hashes []chainhash.Hash
}

func (h *hashesContiguous) UnmarshalJSON(j []byte) error {
	if bytes.Equal(j, []byte("null")) {
		h.Hashes = nil
		return nil
	}
	var array []string
	err := json.Unmarshal(j, &array)
	if err != nil {
		return nil
	}
	h.Hashes = make([]chainhash.Hash, len(array))
	for i, s := range array {
		hash, err := chainhash.NewHashFromStr(s)
		if err != nil {
			return errors.E(errors.Encoding, err)
		}
		h.Hashes[i] = *hash
	}
	return nil
}

func (h *hashesContiguous) MarshalJSON() ([]byte, error) {
	if h.Hashes == nil {
		return []byte("null"), nil
	}
	buf := new(bytes.Buffer)
	scratch32 := make([]byte, 32)
	scratch64 := make([]byte, 64)
	buf.Grow(2 + 67*len(h.Hashes)) // 2 for [], 64+2+1 per hash for strings, quotes, and separator
	buf.WriteByte('[')
	for i, h := range h.Hashes {
		if i != 0 {
			buf.WriteByte(',')
		}
		// Reverse hash into scratch space
		for i := 0; i < 32; i++ {
			scratch32[31-i] = h[i]
		}
		// Write hex encoding of reversed hash
		hex.Encode(scratch64, scratch32)
		buf.WriteByte('"')
		buf.Write(scratch64)
		buf.WriteByte('"')
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// headers implements json.Unmarshaler to decode JSON arrays of hex-encoded
// serialized block headers.
type headers struct {
	Headers []*wire.BlockHeader
}

func (h *headers) UnmarshalJSON(j []byte) error {
	if bytes.Equal(j, []byte("null")) {
		h.Headers = nil
		return nil
	}
	var array []string
	err := json.Unmarshal(j, &array)
	if err != nil {
		return err
	}
	h.Headers = make([]*wire.BlockHeader, len(array))
	for i, s := range array {
		h.Headers[i] = new(wire.BlockHeader)
		err = h.Headers[i].Deserialize(hex.NewDecoder(strings.NewReader(s)))
		if err != nil {
			return errors.E(errors.Encoding, err)
		}
	}
	return nil
}

type transactions struct {
	Transactions []*wire.MsgTx
}

func (t *transactions) UnmarshalJSON(j []byte) error {
	if bytes.Equal(j, []byte("null")) {
		t.Transactions = nil
		return nil
	}
	var array []json.RawMessage
	err := json.Unmarshal(j, &array)
	if err != nil {
		return err
	}
	t.Transactions = make([]*wire.MsgTx, len(array))
	for i, j := range array {
		if len(j) < 2 || j[0] != '"' || j[len(j)-1] != '"' {
			return errors.E(errors.Encoding, "not a string")
		}
		t.Transactions[i] = new(wire.MsgTx)
		err = t.Transactions[i].Deserialize(hex.NewDecoder(bytes.NewReader(j[1 : len(j)-1])))
		if err != nil {
			return errors.E(errors.Encoding, err)
		}
	}
	return nil
}

type msgPtr[T any] interface {
	*T
	wire.Message
}

type messageArray[T any, P msgPtr[T]] struct {
	pver     uint32
	Messages []P
}

func makeMessageArray[T any, P msgPtr[T]](pver uint32) messageArray[T, P] {
	var msgArray messageArray[T, P]
	msgArray.pver = pver
	return msgArray
}

func (m *messageArray[T, P]) UnmarshalJSON(j []byte) error {
	if bytes.Equal(j, []byte("null")) {
		m.Messages = nil
		return nil
	}
	var array []json.RawMessage
	err := json.Unmarshal(j, &array)
	if err != nil {
		return err
	}
	m.Messages = make([]P, len(array))
	for i, j := range array {
		if len(j) < 2 || j[0] != '"' || j[len(j)-1] != '"' {
			return errors.E(errors.Encoding, "not a string")
		}
		m.Messages[i] = new(T)
		err = m.Messages[i].BtcDecode(hex.NewDecoder(bytes.NewReader(j[1:len(j)-1])), m.pver)
		if err != nil {
			return errors.E(errors.Encoding, err)
		}
	}
	return nil
}

type buffer struct {
	Buffer bytes.Buffer
}

func (b *buffer) Deserialize(r io.Reader) error {
	_, err := b.Buffer.ReadFrom(r)
	return err
}
