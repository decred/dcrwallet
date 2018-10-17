package util

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"log"
	"time"
)

func Uint64() (v uint64) {
	err := binary.Read(rand.Reader, binary.BigEndian, &v)
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// NewRandUint64 returns a new uint64 or an error
func NewRandUint64() (uint64, error) {
	var b [8]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint64(b[:]), nil
}

// MustRandUint64 returns a new random uint64 or panics
func MustRandUint64() uint64 {
	r, err := NewRandUint64()
	if err != nil {
		panic(err)
	}
	return r
}

func NewRandInt32() (int32, error) {
	var b [4]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return 0, err
	}

	return int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24), nil
}

func MustRandInt32() int32 {
	r, err := NewRandInt32()
	if err != nil {
		panic(err)
	}
	return r
}

func GetTimeString(t time.Time) string {
	ts := t.Format("2006-01-02 15:04:05")
	return ts[11:]
}

// XorBytes takes two byte arrays of the same length, computes the compentwise xor
// of the inputs, and returns the resulting array.  Returns an error if the
// input arrays are not the same length.
func XorBytes(b1 []byte, b2 []byte) ([]byte, error) {
	if len(b1) == 0 {
		return b2, nil
	}
	if len(b2) == 0 {
		return b1, nil
	}
	if len(b1) != len(b2) {
		return nil, errors.New("byte arrays not the same length")
	}

	result := make([]byte, len(b1))

	for i := 0; i < len(b1); i++ {
		result[i] = b1[i] ^ b2[i]
	}

	return result, nil
}

// XorNBytes takes variable byte arrays of the same length, computes the compentwise xor
// of the inputs, and returns the resulting array.  Returns an error if the
// input arrays are not the same length.
func XorNBytes(bs ...[]byte) ([]byte, error) {
	ret := make([]byte, 0)
	var err error = nil
	for _, b := range bs {
		ret, err = XorBytes(ret, b)
		if err != nil {
			return nil, err
		}
	}

	return ret, nil
}
