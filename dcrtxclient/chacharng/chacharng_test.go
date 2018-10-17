package chacharng

import (
	"bytes"
	"fmt"

	//"fmt"
	"testing"
)

func TestRandomGenerate(t *testing.T) {
	seed := []byte("01234567890123456789012345678901")

	rnd := RandBytes(seed, 20)

	if rnd == nil {
		t.Fail()
	}

	seed1 := []byte("01234567890123456789012345678911")

	rnd1 := RandBytes(seed1, 20)

	if rnd1 == nil {
		t.Fail()
	}

	if bytes.Compare(rnd, rnd1) == 0 {
		t.Fail()
	}
}

func TestRandBytesAfterReseed(t *testing.T) {
	seed := []byte("01234567890123456789012345678901")

	r := NewRandReader(seed)

	if r == nil {
		t.Fail()
	}

	b := make([]byte, 10)
	b1 := make([]byte, 10)
	c := make([]byte, 10)
	c1 := make([]byte, 10)

	r.Read(b)
	r.Read(b1)

	fmt.Printf("b %x\n", b)
	fmt.Printf("b1 %x\n", b1)

	r1 := NewRandReader(seed)

	if r1 == nil {
		t.Fail()
	}

	r1.Read(c)
	r1.Read(c1)

	fmt.Printf("c %x\n", c)
	fmt.Printf("c1 %x\n", c1)
	if bytes.Compare(b, c) != 0 || bytes.Compare(b1, c1) != 0 {
		t.Errorf("two first random byte slices after reseed must be the same")
	}
}

func TestInvalidSeedSize(t *testing.T) {
	seed := []byte("abcdefd")

	rnd := RandBytes(seed, 20)

	if rnd != nil {
		t.Fail()
	}

}
