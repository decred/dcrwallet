package field

import (
	"log"
)

type (
	Field struct {
		N Uint128
	}
)

// Prime of finite field with field size 128 bit (1<<127 - 1).
var Prime = Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF}

// NewFF returns finite field from uint128 type.
// Terminates when value of uint128 is greater than Prime.
func NewFF(n Uint128) Field {
	if Prime.Compare(n) == 0 {
		return Field{Uint128{0, 0}}
	}
	if Prime.Compare(n) != 1 {
		log.Fatalf("N is greater than Prime %s", n.HexStr())
	}
	return Field{n}
}

// Add performs finite field addition
// add return new finite field.
func (ff Field) Add(ff2 Field) Field {
	return Field{(ff.N.Add(ff2.N)).Reduce()}
}

// Neg performs finite field subtraction with Prime number
// and return new finite field.
func (ff Field) Neg() Field {
	return Field{Sub(Prime, ff.N)}
}

// Sub performs finite field subtraction
// and return new finite field.
func (ff Field) Sub(ff2 Field) Field {
	return ff.Add(ff2.Neg())
}

// MulAssign performs finite field multiplication
// and assign result for first receiver.
func (ff *Field) MulAssign(ff2 Field) {

	sh := ff.N.H
	sl := ff.N.L

	oh := ff2.N.H
	ol := ff2.N.L

	// (64 bits * 63 bits) + (64 bits * 63 bits) = 128 bits
	u1 := Uint128{0, sh}
	u2 := Uint128{0, ol}
	u3 := Uint128{0, oh}
	u4 := Uint128{0, sl}
	m := Mul(u1, u2).Add(Mul(u3, u4))

	mh := m.H
	ml := m.L

	// (64 bits * 64 bits) + 128 bits = 129 bits
	// overflowing_add Returns (a + b) mod 2^N, where N is the width of T in bits.
	// (rl, carry) = (sl as u128 * ol as u128).overflowing_add((ml as u128) << 64);
	r1 := Uint128{0, sl}
	r2 := Uint128{0, ol}

	r3 := Mul(r1, r2)
	r31 := Uint128{0, ml}.ShiftL(64)

	rl := Uint128{}
	rl.L = r3.L + r31.L
	// Check if overflow Lo
	var carry uint64 = 0
	if rl.L < r3.L {
		carry = 1
	}

	rl.H = r3.H + r31.H + carry
	if rl.H < r3.H || rl.H < r31.H {
		carry = 1
	}

	// (63 bits * 63 bits) + 64 bits + 1 bit = 127 bits
	// rh u128 = (sh as u128 * oh as u128) + (mh as u128) + (carry as u128);
	rh := (Mul(u1, u3).Add(Uint128{0, mh})).Add(Uint128{0, carry})

	ret := Reduce2(rh, rl).Reduce()
	ff.N = ret
}

// Mul performs finite field multiplation
// and returns result to new finite field.
func (ff Field) Mul(ff2 Field) Field {

	sh := ff.N.H
	sl := ff.N.L

	oh := ff2.N.H
	ol := ff2.N.L

	// (64 bits * 63 bits) + (64 bits * 63 bits) = 128 bits
	u1 := Uint128{0, sh}
	u2 := Uint128{0, ol}
	u3 := Uint128{0, oh}
	u4 := Uint128{0, sl}
	m := Mul(u1, u2).Add(Mul(u3, u4))

	mh := m.H
	ml := m.L

	// (64 bits * 64 bits) + 128 bits = 129 bits
	// overflowing_add Returns (a + b) mod 2^N, where N is the width of T in bits.
	// let (rl, carry) = (sl as u128 * ol as u128).overflowing_add((ml as u128) << 64);
	r1 := Uint128{0, sl}
	r2 := Uint128{0, ol}

	r3 := Mul(r1, r2)
	r31 := Uint128{0, ml}.ShiftL(64)

	rl := Uint128{}
	rl.L = r3.L + r31.L
	// Check if overflow Lo
	var carry uint64 = 0
	if rl.L < r3.L {
		carry = 1
	}

	rl.H = r3.H + r31.H + carry
	if rl.H < r3.H || rl.H < r31.H {
		carry = 1
	}

	// (63 bits * 63 bits) + 64 bits + 1 bit = 127 bits
	rh := (Mul(u1, u3).Add(Uint128{0, mh})).Add(Uint128{0, carry})

	ret := Reduce2(rh, rl).Reduce()
	return NewFF(ret)
}

// Exp calculates exponential of finite field
// and return new finte field.
func (ff Field) Exp(e uint64) Field {
	ret := &Field{ff.N}
	if e == uint64(1) {
		return *ret
	}
	var i uint64
	for i = 1; i < e; i++ {
		ret.MulAssign(ff)
	}

	return *ret
}

// HexStr gets hexa string representation of finite field value.
func (ff Field) HexStr() string {
	n := &ff.N
	return n.HexStr()
}

// String gets string representation of finite field value.
func (ff Field) String() string {
	n := &ff.N
	return n.String()
}
