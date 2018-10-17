package chacharng

import (
	"fmt"
	"io"

	"github.com/tmthrgd/go-rand"
)

//func returns random [rndsize]byte slice with provided seed
//based on chacha20. The seed size must be 32.
func RandBytes(seed []byte, rndsize int) []byte {
	r, err := rand.New(seed[:])
	if err != nil {
		fmt.Println("error new chacharng with seed")
		return nil
	}

	ret := make([]byte, rndsize)

	n, err := r.Read(ret)

	if n != rndsize {
		return nil
	}

	return ret

}

//NewReaderBytes generates random [rndsize]byte slice with provided seed and reader for next random
//based on chacha20. The seed size must be 32.
func NewReaderBytes(seed []byte, rndsize int) (io.Reader, []byte) {
	r := NewRandReader(seed[:])
	if r != nil {
		fmt.Println("error new chacharng with seed")
		return nil, nil
	}

	ret := make([]byte, rndsize)

	n, _ := r.Read(ret)

	if n != rndsize {
		return nil, nil
	}

	fmt.Printf("RngBytes %x\n", ret)

	return r, ret

}

//get new reader from seed.
func NewRandReader(seed []byte) io.Reader {
	r, err := rand.New(seed[:])
	if err != nil {
		fmt.Println("error new chacharng with seed")
		return nil
	}

	return r
}
