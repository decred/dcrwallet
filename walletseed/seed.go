// Copyright (c) 2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package walletseed

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/decred/dcrd/hdkeychain/v2"
	"github.com/decred/dcrwallet/errors"
	"github.com/decred/dcrwallet/pgpwordlist"
)

// GenerateRandomSeed returns a new seed created from a cryptographically-secure
// random source.  If the seed size is unacceptable,
// hdkeychain.ErrInvalidSeedLen is returned.
func GenerateRandomSeed(size uint) ([]byte, error) {
	const op errors.Op = "walletseed.GenerateRandomSeed"
	if size >= uint(^uint8(0)) {
		return nil, errors.E(op, errors.Invalid, hdkeychain.ErrInvalidSeedLen)
	}
	if size < hdkeychain.MinSeedBytes || size > hdkeychain.MaxSeedBytes {
		return nil, errors.E(op, errors.Invalid, hdkeychain.ErrInvalidSeedLen)
	}
	seed, err := hdkeychain.GenerateSeed(uint8(size))
	if err != nil {
		return nil, errors.E(op, err)
	}
	return seed, nil
}

// checksumByte returns the checksum byte used at the end of the seed mnemonic
// encoding.  The "checksum" is the first byte of the double SHA256.
func checksumByte(data []byte) byte {
	intermediateHash := sha256.Sum256(data)
	return sha256.Sum256(intermediateHash[:])[0]
}

// EncodeMnemonicSlice encodes a seed as a mnemonic word list.
func EncodeMnemonicSlice(seed []byte) []string {
	words := make([]string, len(seed)+1) // Extra word for checksumByte
	for i, b := range seed {
		words[i] = pgpwordlist.ByteToMnemonic(b, i)
	}
	checksum := checksumByte(seed)
	words[len(words)-1] = pgpwordlist.ByteToMnemonic(checksum, len(seed))
	return words
}

// EncodeMnemonic encodes a seed as a mnemonic word list separated by spaces.
func EncodeMnemonic(seed []byte) string {
	var buf bytes.Buffer
	for i, b := range seed {
		if i != 0 {
			buf.WriteRune(' ')
		}
		buf.WriteString(pgpwordlist.ByteToMnemonic(b, i))
	}
	checksum := checksumByte(seed)
	buf.WriteRune(' ')
	buf.WriteString(pgpwordlist.ByteToMnemonic(checksum, len(seed)))
	return buf.String()
}

// DecodeUserInput decodes a seed in either hexadecimal or mnemonic word list
// encoding back into its binary form.
func DecodeUserInput(input string) ([]byte, error) {
	const op errors.Op = "walletseed.DecodeUserInput"
	words := strings.Split(strings.TrimSpace(input), " ")
	var seed []byte
	switch {
	case len(words) == 1:
		// Assume hex
		var err error
		seed, err = hex.DecodeString(words[0])
		if err != nil {
			return nil, errors.E(op, errors.Encoding, err)
		}
	case len(words) > 1:
		// Assume mnemonic with encoded checksum byte
		decoded, err := pgpwordlist.DecodeMnemonics(words)
		if err != nil {
			return nil, errors.E(op, errors.Encoding, err)
		}
		if len(decoded) < 2 { // need data (0) and checksum (1) to check checksum
			break
		}
		if checksumByte(decoded[:len(decoded)-1]) != decoded[len(decoded)-1] {
			return nil, errors.E(op, errors.Encoding, "checksum mismatch")
		}
		seed = decoded[:len(decoded)-1]
	}

	if len(seed) < hdkeychain.MinSeedBytes || len(seed) > hdkeychain.MaxSeedBytes {
		return nil, errors.E(op, errors.Encoding, hdkeychain.ErrInvalidSeedLen)
	}
	return seed, nil
}
