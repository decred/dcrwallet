// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ripemd128

// work buffer indices and roll amounts for one line
var _n = [64]uint{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	7, 4, 13, 1, 10, 6, 15, 3, 12, 0, 9, 5, 2, 14, 11, 8,
	3, 10, 14, 4, 9, 15, 8, 1, 2, 7, 0, 6, 13, 11, 5, 12,
	1, 9, 11, 10, 0, 8, 12, 4, 13, 3, 7, 15, 14, 5, 6, 2,
}

var _r = [64]uint{
	11, 14, 15, 12, 5, 8, 7, 9, 11, 13, 14, 15, 6, 7, 9, 8,
	7, 6, 8, 13, 11, 9, 7, 15, 7, 12, 15, 9, 11, 7, 13, 12,
	11, 13, 6, 7, 14, 9, 13, 15, 14, 8, 13, 6, 5, 12, 7, 5,
	11, 12, 14, 15, 14, 15, 9, 8, 9, 14, 5, 6, 8, 6, 5, 12,
}

// same for the other parallel one
var n_ = [64]uint{
	5, 14, 7, 0, 9, 2, 11, 4, 13, 6, 15, 8, 1, 10, 3, 12,
	6, 11, 3, 7, 0, 13, 5, 10, 14, 15, 8, 12, 4, 9, 1, 2,
	15, 5, 1, 3, 7, 14, 6, 9, 11, 8, 12, 2, 10, 0, 4, 13,
	8, 6, 4, 1, 3, 11, 15, 0, 5, 12, 2, 13, 9, 7, 10, 14,
}

var r_ = [64]uint{
	8, 9, 9, 11, 13, 15, 15, 5, 7, 7, 8, 11, 14, 14, 12, 6,
	9, 13, 15, 7, 12, 8, 9, 11, 7, 7, 12, 7, 6, 15, 13, 11,
	9, 7, 15, 11, 8, 6, 6, 14, 12, 13, 5, 14, 13, 13, 7, 5,
	15, 5, 8, 11, 14, 14, 6, 14, 6, 9, 12, 9, 12, 5, 15, 8,
}

func _Block(md *Digest, p []byte) int {
	n := 0
	var x [16]uint32
	var alpha uint32
	for len(p) >= BlockSize {
		a, b, c, d := md.s[0], md.s[1], md.s[2], md.s[3]
		aa, bb, cc, dd := a, b, c, d
		j := 0
		for i := 0; i < 16; i++ {
			x[i] = uint32(p[j]) | uint32(p[j+1])<<8 | uint32(p[j+2])<<16 | uint32(p[j+3])<<24
			j += 4
		}

		// round 1
		i := 0
		for i < 16 {
			alpha = a + (b ^ c ^ d) + x[_n[i]]
			s := _r[i]
			alpha = (alpha<<s | alpha>>(32-s))
			a, b, c, d = d, alpha, b, c

			// parallel line
			alpha = aa + (bb&dd | cc&^dd) + x[n_[i]] + 0x50a28be6
			s = r_[i]
			alpha = (alpha<<s | alpha>>(32-s))
			aa, bb, cc, dd = dd, alpha, bb, cc

			i++
		}

		// round 2
		for i < 32 {
			alpha = a + (b&c | ^b&d) + x[_n[i]] + 0x5a827999
			s := _r[i]
			alpha = (alpha<<s | alpha>>(32-s))
			a, b, c, d = d, alpha, b, c

			// parallel line
			alpha = aa + (bb | ^cc ^ dd) + x[n_[i]] + 0x5c4dd124
			s = r_[i]
			alpha = (alpha<<s | alpha>>(32-s))
			aa, bb, cc, dd = dd, alpha, bb, cc

			i++
		}

		// round 3
		for i < 48 {
			alpha = a + (b | ^c ^ d) + x[_n[i]] + 0x6ed9eba1
			s := _r[i]
			alpha = (alpha<<s | alpha>>(32-s))
			a, b, c, d = d, alpha, b, c

			// parallel line
			alpha = aa + (bb&cc | ^bb&dd) + x[n_[i]] + 0x6d703ef3
			s = r_[i]
			alpha = (alpha<<s | alpha>>(32-s))
			aa, bb, cc, dd = dd, alpha, bb, cc

			i++
		}

		// round 4
		for i < 64 {
			alpha = a + (b&d | c&^d) + x[_n[i]] + 0x8f1bbcdc
			s := _r[i]
			alpha = (alpha<<s | alpha>>(32-s))
			a, b, c, d = d, alpha, b, c

			// parallel line
			alpha = aa + (bb ^ cc ^ dd) + x[n_[i]]
			s = r_[i]
			alpha = (alpha<<s | alpha>>(32-s))
			aa, bb, cc, dd = dd, alpha, bb, cc

			i++
		}

		// combine results
		c += md.s[1] + dd
		md.s[1] = md.s[2] + d + aa
		md.s[2] = md.s[3] + a + bb
		md.s[3] = md.s[0] + b + cc
		md.s[0] = c

		p = p[BlockSize:]
		n += BlockSize
	}
	return n
}
