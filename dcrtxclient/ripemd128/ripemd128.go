// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Golang only has 160 bits hash of ripemd.
// We have modified to create 128 bits hash
package ripemd128

// The size of the checksum in bytes.
const Size = 16

// The block size of the hash algorithm in bytes.
const BlockSize = 64

const (
	_s0 = 0x67452301
	_s1 = 0xefcdab89
	_s2 = 0x98badcfe
	_s3 = 0x10325476
)

// Digest represents the partial evaluation of a checksum.
type Digest struct {
	s  [4]uint32       // running context
	x  [BlockSize]byte // temporary buffer
	nx int             // index into x
	tc uint64          // total count of bytes processed
}

// Reset resets all sample data of rimpe128.
func (d *Digest) Reset() {
	d.s[0], d.s[1], d.s[2], d.s[3] = _s0, _s1, _s2, _s3
	d.nx = 0
	d.tc = 0
}

// New returns a new Digest for computing the checksum.
func New() *Digest {
	result := new(Digest)
	result.Reset()
	return result
}

// Size returns hash size in bytes.
func (d *Digest) Size() int { return Size }

// BlockSize returns size of block.
func (d *Digest) BlockSize() int { return BlockSize }

// Write writes data to fields of rimpemd128.
func (d *Digest) Write(p []byte) (nn int, err error) {
	nn = len(p)
	d.tc += uint64(nn)
	if d.nx > 0 {
		n := len(p)
		if n > BlockSize-d.nx {
			n = BlockSize - d.nx
		}
		for i := 0; i < n; i++ {
			d.x[d.nx+i] = p[i]
		}
		d.nx += n
		if d.nx == BlockSize {
			_Block(d, d.x[0:])
			d.nx = 0
		}
		p = p[n:]
	}
	n := _Block(d, p)
	p = p[n:]
	if len(p) > 0 {
		d.nx = copy(d.x[:], p)
	}
	return
}

// Sum calculates checksum of rimpe128.
func (d0 *Digest) Sum(in []byte) []byte {
	// Make a copy of d0 so that caller can keep writing and summing.
	d := *d0

	// Padding.  Add a 1 bit and 0 bits until 56 bytes mod 64.
	tc := d.tc
	var tmp [64]byte
	tmp[0] = 0x80
	if tc%64 < 56 {
		d.Write(tmp[0 : 56-tc%64])
	} else {
		d.Write(tmp[0 : 64+56-tc%64])
	}

	// Length in bits.
	tc <<= 3
	for i := uint(0); i < 8; i++ {
		tmp[i] = byte(tc >> (8 * i))
	}
	d.Write(tmp[0:8])

	if d.nx != 0 {
		panic("d.nx != 0")
	}

	var digest [Size]byte
	for i, s := range d.s {
		digest[i*4] = byte(s)
		digest[i*4+1] = byte(s >> 8)
		digest[i*4+2] = byte(s >> 16)
		digest[i*4+3] = byte(s >> 24)
	}

	return append(in, digest[:]...)
}

// Sum127 calculates 128 bits hash and reset first 4 bits.
func (d0 *Digest) Sum127(in []byte) []byte {
	// Make a copy of d0 so that caller can keep writing and summing.
	d := *d0

	// Padding.  Add a 1 bit and 0 bits until 56 bytes mod 64.
	tc := d.tc
	var tmp [64]byte
	tmp[0] = 0x80
	if tc%64 < 56 {
		d.Write(tmp[0 : 56-tc%64])
	} else {
		d.Write(tmp[0 : 64+56-tc%64])
	}

	// Length in bits.
	tc <<= 3
	for i := uint(0); i < 8; i++ {
		tmp[i] = byte(tc >> (8 * i))
	}
	d.Write(tmp[0:8])

	if d.nx != 0 {
		panic("d.nx != 0")
	}

	var digest [Size]byte
	for i, s := range d.s {
		digest[i*4] = byte(s)
		digest[i*4+1] = byte(s >> 8)
		digest[i*4+2] = byte(s >> 16)
		digest[i*4+3] = byte(s >> 24)
	}
	ret := append(in, digest[:]...)

	ret[0] = ret[0] & 0x0f

	return ret
}
