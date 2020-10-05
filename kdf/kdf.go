// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package kdf

import (
	"encoding/binary"
	"errors"
	"io"
	"runtime"

	"golang.org/x/crypto/argon2"
)

// Argon2idParams describes the difficulty and parallelism requirements for the
// Argin2id KDF.
type Argon2idParams struct {
	Salt    [16]byte
	Time    uint32
	Memory  uint32
	Threads uint8
}

// NewArgon2idParams returns the minimum recommended parameters for the Argon2id
// KDF with a random salt.  Randomness is read from rand.
//
// The time and memory parameters may be increased by an application when stronger
// security requirements are desired, and additional memory is available.
func NewArgon2idParams(rand io.Reader) (*Argon2idParams, error) {
	ncpu := runtime.NumCPU()
	if ncpu > 256 {
		ncpu = 256
	}
	p := &Argon2idParams{
		Time:    1,
		Memory:  256 * 1024, // 256 MiB
		Threads: uint8(ncpu),
	}
	_, err := rand.Read(p.Salt[:])
	return p, err
}

// MarshaledLen is the length of the marshaled KDF parameters.
const MarshaledLen = 25

// MarshalBinary implements encoding.BinaryMarshaler.
// The returned byte slice has length MarshaledLen.
func (p *Argon2idParams) MarshalBinary() ([]byte, error) {
	b := make([]byte, MarshaledLen)
	copy(b, p.Salt[:])
	binary.LittleEndian.PutUint32(b[16:16+4], p.Time)
	binary.LittleEndian.PutUint32(b[16+4:16+8], p.Memory)
	b[16+8] = p.Threads
	return b, nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (p *Argon2idParams) UnmarshalBinary(data []byte) error {
	if len(data) != MarshaledLen {
		return errors.New("invalid marshaled Argon2id parameters")
	}
	copy(p.Salt[:], data)
	p.Time = binary.LittleEndian.Uint32(data[16:])
	p.Memory = binary.LittleEndian.Uint32(data[16+4:])
	p.Threads = data[16+8]
	return nil
}

// DeriveKey derives a key of len bytes from a passphrase and KDF parameters.
func DeriveKey(password []byte, p *Argon2idParams, len uint32) []byte {
	defer runtime.GC()
	return argon2.IDKey(password, p.Salt[:], p.Time, p.Memory, p.Threads, len)
}
