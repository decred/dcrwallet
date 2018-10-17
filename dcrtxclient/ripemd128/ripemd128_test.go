// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// use to hash 128 bit from address

package ripemd128

// Test vectors are from:
// http://homes.esat.kuleuven.be/~bosselae/ripemd160.html

import (
	"fmt"
	"io"

	"testing"
)

type mdTest struct {
	out string
	in  string
}

var vectors = [...]mdTest{
	{"cdf26213a150dc3ecb610f18f6b38b46", ""},
	{"86be7afa339d0fc7cfc785e72f578d33", "a"},
	{"c14a12199c66e4ba84636b0f69144c77", "abc"},
	{"9e327b3d6e523062afc1132d7df9d1b8", "message digest"},
	{"fd2aa607f71dc8f510714922b371834e", "abcdefghijklmnopqrstuvwxyz"},
	{"a1aa0689d0fafa2ddc22e88b49133a06", "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"},
	{"d1e959eb179c911faea4624c60c5c702", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"},
	{"3f45ef194732c2dbb2c4a2c769795fa3", "12345678901234567890123456789012345678901234567890123456789012345678901234567890"},
}

func TestHash(t *testing.T) {

	sample := []byte("a")
	md := New()

	_, err := md.Write(sample)

	if err != nil {
		fmt.Println("error ", err)
	}

	fmt.Printf("hash of \"%s\": %x\n", sample, md.Sum(nil))

}

func TestVectors(t *testing.T) {

	for i := 0; i < len(vectors); i++ {
		tv := vectors[i]
		md := New()
		for j := 0; j < 3; j++ {
			if j < 2 {
				io.WriteString(md, tv.in)
			} else {
				io.WriteString(md, tv.in[0:len(tv.in)/2])
				md.Sum(nil)
				io.WriteString(md, tv.in[len(tv.in)/2:])
			}
			s := fmt.Sprintf("%x", md.Sum(nil))
			if s != tv.out {
				t.Fatalf("RIPEMD-128[%d](%s) = %s, expected %s", j, tv.in, s, tv.out)
			}
			md.Reset()
		}
	}
}

func millionA() string {
	md := New()
	for i := 0; i < 100000; i++ {
		io.WriteString(md, "aaaaaaaaaa")
	}
	return fmt.Sprintf("%x", md.Sum(nil))
}

func TestMillionA(t *testing.T) {
	const out = "4a7f5723f954eba1216c9d8f6320431f"
	if s := millionA(); s != out {
		t.Fatalf("RIPEMD-128 (1 million 'a') = %s, expected %s", s, out)
	}
}

func BenchmarkMillionA(b *testing.B) {
	for i := 0; i < b.N; i++ {
		millionA()
	}
}
