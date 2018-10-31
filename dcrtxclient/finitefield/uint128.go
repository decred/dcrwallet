package field

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/pkg/errors"
)

type (
	Uint128 struct {
		H, L uint64
	}
)

// Compare does comparing two uint128 numbers.
// Returns 0: equal, 1: greater, -1: less.
func (op Uint128) Compare(op2 Uint128) int {
	if op.H > op.H {
		return 1
	} else if op.H < op2.H {
		return -1
	}

	if op.L > op2.L {
		return 1
	} else if op.L < op2.L {
		return -1
	}

	return 0
}

// Reduce performs reduce on one uint128.
// Function is used in finite field operation.
func (u Uint128) Reduce() Uint128 {
	return And128(u, Prime).Add(u.ShiftR(127))
}

// Reduce2 performs reduce on two uint128.
// Function is used in finite field operation
func Reduce2(h, l Uint128) Uint128 {
	shift := Or128(h.ShiftL(1), l.ShiftR(127))
	return And128(l, Prime).Add(shift)
}

// Mul performs multiplation on two uint128 numbers
// and return to new uint128
func Mul(n, m Uint128) Uint128 {
	// Split values into four 32-bit parts
	top := []uint64{n.H >> 32, n.H & 0xFFFFFFFF, n.L >> 32, n.L & 0xFFFFFFFF}
	bottom := []uint64{m.H >> 32, m.H & 0xFFFFFFFF, m.L >> 32, m.L & 0xFFFFFFFF}
	products := make([][]uint64, 4)
	for i := range products {
		products[i] = make([]uint64, 4)
	}

	// Multiply each component of the values
	for y := 3; y > -1; y-- {
		for x := 3; x > -1; x-- {
			products[3-x][y] = top[x] * bottom[y]
		}
	}

	// First row
	fourth32 := (products[0][3] & 0xFFFFFFFF)
	third32 := (products[0][2] & 0xFFFFFFFF) + (products[0][3] >> 32)
	second32 := (products[0][1] & 0xFFFFFFFF) + (products[0][2] >> 32)
	first32 := (products[0][0] & 0xFFFFFFFF) + (products[0][1] >> 32)

	// Second row
	third32 += (products[1][3] & 0xFFFFFFFF)
	second32 += (products[1][2] & 0xFFFFFFFF) + (products[1][3] >> 32)
	first32 += (products[1][1] & 0xFFFFFFFF) + (products[1][2] >> 32)

	// Third row
	second32 += (products[2][3] & 0xFFFFFFFF)
	first32 += (products[2][2] & 0xFFFFFFFF) + (products[2][3] >> 32)

	// Fourth row
	first32 += (products[3][3] & 0xFFFFFFFF)

	// Move carry to the next digit
	third32 += fourth32 >> 32
	second32 += third32 >> 32
	first32 += second32 >> 32

	// Remove carry from the current digit
	fourth32 &= 0xFFFFFFFF
	third32 &= 0xFFFFFFFF
	second32 &= 0xFFFFFFFF
	first32 &= 0xFFFFFFFF

	// Combine components
	return Uint128{(first32 << 32) | second32, (third32 << 32) | fourth32}
}

// Divmod performs division on two uint128s.
// Returns remaider and mode
func Divmod(x, y Uint128) (Uint128, Uint128) {

	if y.Compare(Uint128{0, 0}) == 0 {
		log.Fatal("Division by zero")
	} else if y.Compare(Uint128{0, 1}) == 0 {
		return x, Uint128{0, 0}
	} else if y.Compare(y) == 0 {
		return Uint128{1, 0}, Uint128{0, 0}
	} else if x.Compare(Uint128{0, 0}) == 0 || x.Compare(y) == -1 {
		return Uint128{0, 0}, x
	}

	var d, v Uint128
	var i uint64
	for i = 128; i > 0; i-- {
		d.ShiftL(1)
		v.ShiftL(1)

		if And128((x.ShiftR(i-1)), Uint128{0, 1}).Compare(Uint128{0, 0}) == 1 {
			v.Add(Uint128{0, 1})
		}

		if v.Compare(y) != -1 {
			v = Sub(v, y)
			d.Add(Uint128{0, 1})
		}
	}

	return d, v
}

// Add performs addition on two unint128 numbers.
// and returns new uint128.
func (u Uint128) Add(o Uint128) Uint128 {
	carry := u.L

	ret := Uint128{u.H + o.H, u.L + o.L}

	if ret.L < carry {
		ret.H += 1
	}
	return ret
}

// Sub performs subtraction on uint128 and uint64.
// and returns a new uint128.
func (u Uint128) Sub(n uint64) Uint128 {
	lo := u.L - n
	hi := u.H
	if u.L < lo {
		hi--
	}
	return Uint128{hi, lo}
}

// Sub performs subtraction on two uint128 numbers.
// and returns new uint128.
func Sub(N, M Uint128) Uint128 {
	A := Uint128{0, N.L - M.L}
	var C uint64 = (((A.L & M.L) & 1) + (M.L >> 1) + (A.L >> 1)) >> 63
	A.H = N.H - (M.H + C)
	return A
}

// ShiftL performs shift left operation on uint128 number.
func (N Uint128) ShiftL(shift uint64) Uint128 {

	if shift >= 128 {
		return Uint128{0, 0}
	} else if shift == 64 {
		return Uint128{N.L, 0}
	} else if shift == 0 {
		return N
	} else if shift < 64 {
		return Uint128{(N.H << shift) + (N.L >> (64 - shift)), N.L << shift}
	} else if (128 > shift) && (shift > 64) {
		return Uint128{N.L << (shift - 64), 0}
	} else {
		return Uint128{0, 0}
	}
}

// ShiftR performs shift right operation on uint128 number.
func (N Uint128) ShiftR(shift uint64) Uint128 {

	if shift >= 128 {
		return Uint128{0, 0}
	} else if shift == 64 {
		return Uint128{0, N.H}
	} else if shift == 0 {
		return N
	} else if shift < 64 {
		return Uint128{N.H >> shift, (N.H << (64 - shift)) + (N.L >> shift)}
	} else if (128 > shift) && (shift > 64) {
		return Uint128{0, (N.H >> (shift - 64))}
	} else {
		return Uint128{0, 0}
	}
}

// Or128 performs or operation on two uint128 numbers.
func Or128(N1, N2 Uint128) Uint128 {
	return Uint128{N1.H | N2.H, N1.L | N2.L}
}

// And128 performs and operation on two uint128 numbers.
func And128(N1, N2 Uint128) (A Uint128) {
	A.H = N1.H & N2.H
	A.L = N1.L & N2.L
	return A
}

// Xor128 performs xor operator on two uint128 numbers.
func Xor128(N1, N2 Uint128) Uint128 {
	var A Uint128
	A.H = N1.H ^ N2.H
	A.L = N1.L ^ N2.L
	return A
}

// NewFromString parses uint128 from string and return uint128 value.
func NewFromString(s string) (u *Uint128, err error) {

	if len(s) > 32 {
		return nil, fmt.Errorf("s:%s length greater than 32", s)
	}

	b, err := hex.DecodeString(fmt.Sprintf("%032s", s))
	if err != nil {
		return nil, err
	}
	rdr := bytes.NewReader(b)
	u = new(Uint128)
	err = binary.Read(rdr, binary.BigEndian, u)
	return
}

// HexStr gets hexa representation in string of uint128.
func (u *Uint128) HexStr() string {
	if u.H == 0 {
		return fmt.Sprintf("%x", u.L)
	}
	return fmt.Sprintf("%x%016x", u.H, u.L)
}

// GetBytes gets a big-endian byte representation.
func (u Uint128) GetBytes() []byte {
	buf := make([]byte, 16)
	binary.BigEndian.PutUint64(buf[:8], u.H)
	binary.BigEndian.PutUint64(buf[8:], u.L)
	return buf
}

// String gets a hexadecimal string representation.
func (u Uint128) String() string {
	return hex.EncodeToString(u.GetBytes())
}

// FromBytes parses the byte slice as a 128 bit big-endian unsigned integer.
func FromBytes(b []byte) Uint128 {
	hi := binary.BigEndian.Uint64(b[:8])
	lo := binary.BigEndian.Uint64(b[8:])
	return Uint128{hi, lo}
}

// FromString parses a hexadecimal string as a 128-bit big-endian unsigned integer.
func FromString(s string) (Uint128, error) {
	if len(s) > 32 {
		return Uint128{}, errors.Errorf("input string %s too large for uint128", s)
	}
	bytes, err := hex.DecodeString(s)
	if err != nil {
		return Uint128{}, errors.Wrapf(err, "could not decode %s as hex", s)
	}

	// Grow the byte slice if it's smaller than 16 bytes, by prepending 0s
	if len(bytes) < 16 {
		bytesCopy := make([]byte, 16)
		copy(bytesCopy[(16-len(bytes)):], bytes)
		bytes = bytesCopy
	}

	return FromBytes(bytes), nil
}
