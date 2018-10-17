package field

import (
	"testing"
)

var testMulU128Data = []testdata{
	{[]Uint128{Prime, Uint128{0x0, 0x3}},
		Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFD}},
	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0x0, 0xFFFFFFFFFFFFFFF0}},
		Uint128{0Xfffffffffffffffe, 0X0000000000000020}},
}

var testAddU128Data = []testdata{
	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF},
		Uint128{0x0, 0x1}},
		Uint128{0x8000000000000000, 0x0}},

	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF},
		Uint128{0x0, 0x5}},
		Uint128{0x8000000000000000, 0x4}},
	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE}},
		Uint128{0x8FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFC}},

	{[]Uint128{Uint128{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE}},
		Uint128{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFC}},
	{[]Uint128{Uint128{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFD}},
		Uint128{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFB}},
}

func TestMul(t *testing.T) {

	for i, _ := range testMulU128Data {
		f3 := Mul(testMulU128Data[i].data[0], (testMulU128Data[i].data[1]))
		if f3.Compare(testMulU128Data[i].res) != 0 {
			t.Errorf("error.\n inputs %v.\n expected: %v.\n res: %v",
				testMulU128Data[i].data, testMulU128Data[i].res, f3)
		}
	}

}

func TestAdd(t *testing.T) {

	for i, _ := range testAddU128Data {

		f3 := testAddU128Data[i].data[0].Add(testAddU128Data[i].data[1])

		if f3.Compare(testAddU128Data[i].res) != 0 {
			t.Errorf("error.\n inputs %v.\n expected: %v.\n res: %v",
				testAddU128Data[i].data, testAddU128Data[i].res, f3)
		}
	}

}
