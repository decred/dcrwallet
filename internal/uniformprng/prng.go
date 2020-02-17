// Package uniformprng implements a uniform, cryptographically secure
// pseudo-random number generator.
package uniformprng

import (
	"encoding/binary"
	"io"
	"math/bits"

	"golang.org/x/crypto/chacha20"
)

// Source returns cryptographically-secure pseudorandom numbers with uniform
// distribution.
type Source struct {
	buf    [4]byte
	cipher *chacha20.Cipher
}

var nonce = make([]byte, chacha20.NonceSize)

// NewSource seeds a Source from a 32-byte key.
func NewSource(seed *[32]byte) *Source {
	cipher, _ := chacha20.NewUnauthenticatedCipher(seed[:], nonce)
	return &Source{cipher: cipher}
}

// RandSource creates a Source with seed randomness read from rand.
func RandSource(rand io.Reader) (*Source, error) {
	seed := new([32]byte)
	_, err := io.ReadFull(rand, seed[:])
	if err != nil {
		return nil, err
	}
	return NewSource(seed), nil
}

// Uint32 returns a pseudo-random uint32.
func (s *Source) Uint32() uint32 {
	b := s.buf[:]
	for i := range b {
		b[i] = 0
	}
	s.cipher.XORKeyStream(b, b)
	return binary.LittleEndian.Uint32(b)
}

// Uint32n returns a pseudo-random uint32 in range [0,n) without modulo bias.
func (s *Source) Uint32n(n uint32) uint32 {
	if n < 2 {
		return 0
	}
	n--
	mask := ^uint32(0) >> bits.LeadingZeros32(n)
	for {
		u := s.Uint32() & mask
		if u <= n {
			return u
		}
	}
}
