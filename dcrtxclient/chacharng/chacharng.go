package chacharng

import (
	"errors"
	"fmt"
	"io"

	"github.com/tmthrgd/go-rand"
)

// RandBytes returns random [rndsize]byte slice with provided seed and size
// based on chacha20. The seed size must be 32.
func RandBytes(seed []byte, rndsize int) ([]byte, error) {
	r, err := rand.New(seed[:])
	if err != nil {
		return nil, err
	}

	ret := make([]byte, rndsize)

	n, err := r.Read(ret)

	if n != rndsize {
		return nil, errors.New(fmt.Sprintf("Wrong number of bytes read. Expected [%d] bytes, got [%d] bytes.", rndsize, n))
	}

	return ret, nil

}

// NewReaderBytes generates random [rndsize]byte slice with provided seed and size.
// Also returns reader for next random based on chacha20. The seed size must be 32.
func NewReaderBytes(seed []byte, rndsize int) (io.Reader, []byte, error) {
	r, err := NewRandReader(seed[:])
	if err != nil {
		return nil, nil, err
	}

	ret := make([]byte, rndsize)

	n, _ := r.Read(ret)

	if n != rndsize {
		return nil, nil, errors.New(fmt.Sprintf("wrong number of bytes read. Expected [%d] bytes, got [%d] bytes.", rndsize, n))
	}

	return r, ret, nil

}

// NewRandReader creates a new rand reader from the provided seed.
func NewRandReader(seed []byte) (io.Reader, error) {
	r, err := rand.New(seed[:])
	if err != nil {
		return nil, err
	}

	return r, nil
}
