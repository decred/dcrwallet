package field

import (
	"fmt"
	"testing"
)

type testdata struct {
	data []Uint128
	res  Uint128
}

var testMulFFData = []testdata{
	{[]Uint128{Prime, Uint128{0x7390f9549e79d27c, 0x208c117eedfc75ea}},
		Uint128{0x0, 0x0}},
	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0x0, 0xFFFFFFFFFFFFFFF0}},
		Uint128{0x7fffffffffffffff, 0xf}},

	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0x0, 0x2}},
		Uint128{0x7fffffffffffffff, 0xFFFFFFFFFFFFFFFD}},

	{[]Uint128{Uint128{0xb1bef14409e0540, 0x80db735e727f4d1d}, Uint128{0x7390f9549e79d27c, 0x208c117eedfc75ea}},
		Uint128{0x556cb10223e60657, 0xf5363a806ba8d108}},
	{[]Uint128{Uint128{0x38ebdafa6f7d8cda, 0x722794440159d78a}, Uint128{0x74301e24ac4cdd17, 0xcb4bf7e0fe6971b5}},
		Uint128{0x521cec713dd29186, 0xd1bc418f2fb8230b}},
}

var testAddFFData = []testdata{
	{[]Uint128{Prime, Uint128{0x0, 0x1}},
		Uint128{0x0, 0x1}},
	{[]Uint128{Prime,
		Uint128{0x0, 0x5}},
		Uint128{0x0, 0x5}},

	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0x0, 0x5}},
		Uint128{0x0, 0x4}},

	{[]Uint128{Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE},
		Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFE}},
		Uint128{0x7FFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFD}},
}

func TestMulFF(t *testing.T) {

	for i, _ := range testMulFFData {
		f1 := NewFF(testMulFFData[i].data[0])
		f2 := NewFF(testMulFFData[i].data[1])

		f3 := f1.Mul(f2)

		fmt.Println("f2.hexstr", f3.HexStr())
		fmt.Println("f2.string", f3.String())

		if f3.N.Compare(testMulFFData[i].res) != 0 {
			t.Errorf("error.\n inputs %v.\n expected: %v.\n res: %v",
				testMulFFData[i].data, testMulFFData[i].res, f3.N)
		}
	}

}

func TestAddFF(t *testing.T) {

	for i, _ := range testAddFFData {
		f1 := NewFF(testAddFFData[i].data[0])
		f2 := NewFF(testAddFFData[i].data[1])

		f3 := f1.Add(f2)

		if f3.N.Compare(testAddFFData[i].res) != 0 {
			t.Errorf("error.\n inputs %v.\n expected: %v.\n res: %v",
				testAddFFData[i].data, testAddFFData[i].res, f3.N)
		}
	}

}
