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
	buf    [8]byte
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
	b := s.buf[:4]
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

// Int63 returns a pseudo-random 63-bit positive integer as an int64 without
// modulo bias.
func (s *Source) Int63() int64 {
	b := s.buf[:]
	for i := range b {
		b[i] = 0
	}
	s.cipher.XORKeyStream(b, b)
	return int64(binary.LittleEndian.Uint64(b) &^ (1 << 63))
}

// Int63n returns, as an int64, a pseudo-random 63-bit positive integer in [0,n)
// without modulo bias.
// It panics if n <= 0.
func (s *Source) Int63n(n int64) int64 {
	if n <= 0 {
		panic("invalid argument to Int63n")
	}
	n--
	mask := int64(^uint64(0) >> bits.LeadingZeros64(uint64(n)))
	for {
		i := s.Int63() & mask
		if i <= n {
			return i
		}
	}
}

// Int63 returns a random non-negative int64, with randomness read from rand.
func Int63(rand io.Reader) (int64, error) {
	buf := make([]byte, 8)
	_, err := io.ReadFull(rand, buf)
	if err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf) &^ (1 << 63)), nil

}

// Int63n returns, as an int64, a pseudo-random 63-bit positive integer in [0,n)
// without modulo bias.
// Randomness is read from rand.
// It panics if n <= 0.
func Int63n(rand io.Reader, n int64) (int64, error) {
	if n <= 0 {
		panic("invalid argument to Int63n")
	}
	n--
	mask := int64(^uint64(0) >> bits.LeadingZeros64(uint64(n)))
	for {
		v, err := Int63(rand)
		if err != nil {
			return 0, err
		}
		v &= mask
		if v <= n {
			return v, nil
		}
	}
}
