// Copyright (c) 2016 The Decred developers
// Copyright (c) 2018 The ExchangeCoin team
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package walletseed

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

var mnemonicTests = []struct {
	mnemonics string
	seed      []byte
	ent       []byte
	password  string
}{
	{
		mnemonics: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
		seed:      []byte{197, 82, 87, 195, 96, 192, 124, 114, 2, 154, 235, 193, 181, 60, 5, 237, 3, 98, 173, 163, 142, 173, 62, 62, 158, 250, 55, 8, 229, 52, 149, 83, 31, 9, 166, 152, 117, 153, 209, 130, 100, 193, 225, 201, 47, 44, 241, 65, 99, 12, 122, 60, 74, 183, 200, 27, 47, 0, 22, 152, 231, 70, 59, 4},
		ent:       []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		password:  "TREZOR",
	},
	{
		mnemonics: "exile ask congress lamp submit jacket era scheme attend cousin alcohol catch course end lucky hurt sentence oven short ball bird grab wing top",
		seed:      []byte{9, 94, 230, 248, 23, 180, 194, 203, 48, 165, 167, 151, 54, 10, 129, 164, 10, 176, 249, 164, 226, 94, 205, 103, 42, 63, 88, 160, 181, 186, 6, 135, 192, 150, 166, 177, 77, 44, 13, 235, 59, 222, 252, 228, 246, 29, 1, 174, 7, 65, 125, 80, 36, 41, 53, 46, 39, 105, 81, 99, 247, 68, 122, 140},
		ent:       []byte{79, 161, 168, 188, 62, 109, 128, 238, 19, 22, 5, 14, 134, 44, 24, 18, 3, 20, 147, 33, 43, 126, 195, 243, 187, 27, 8, 241, 104, 202, 190, 239},
		password:  "TREZOR",
	},
	{
		mnemonics: "renew stay biology evidence goat welcome casual join adapt armor shuffle fault little machine walk stumble urge swap",
		seed:      []byte{146, 72, 216, 62, 6, 244, 205, 152, 222, 191, 91, 111, 1, 5, 66, 118, 13, 249, 37, 206, 70, 207, 56, 161, 189, 180, 228, 222, 125, 33, 245, 195, 147, 102, 148, 28, 105, 225, 189, 191, 41, 102, 224, 246, 230, 219, 236, 232, 152, 160, 226, 240, 164, 194, 179, 230, 64, 149, 61, 254, 139, 123, 189, 197},
		ent:       []byte{182, 58, 156, 89, 166, 230, 65, 242, 136, 235, 193, 3, 1, 127, 29, 169, 248, 41, 11, 61, 166, 189, 239, 123},
		password:  "TREZOR",
	},
}

func TestEncodeMnemonicSlice(t *testing.T) {
	for i, test := range mnemonicTests {
		result, err := EncodeMnemonicSlice(test.ent)
		if err != nil {
			t.Errorf("test %d: error: %v", i, err)
			continue
		}
		mnemonics := strings.Join(result, " ")
		if mnemonics != test.mnemonics {
			t.Errorf("test %d: got `%v` want `%v`", i, mnemonics, test.mnemonics)
		}
	}
}

func TestEncodeMnemonic(t *testing.T) {
	for i, test := range mnemonicTests {
		mnemonics, err := EncodeMnemonic(test.ent)
		if err != nil {
			t.Errorf("test %d: error: %v", i, err)
			continue
		}
		if mnemonics != test.mnemonics {
			t.Errorf("test %d: got `%v` want `%v`", i, mnemonics, test.mnemonics)
		}
	}
}

func TestDecodeMnemonic(t *testing.T) {
	for i, test := range mnemonicTests {
		data, err := DecodeUserInput(test.mnemonics, test.password)
		if err != nil {
			t.Errorf("test %d: error: %v", i, err)
			continue
		}
		if !bytes.Equal(data, test.seed) {
			t.Errorf("test %d: got %x want %x", i, data, test.seed)
		}
	}
}

func TestDecodeHex(t *testing.T) {
	for i, test := range mnemonicTests {
		data, err := DecodeUserInput(hex.EncodeToString(test.seed), test.password)
		if err != nil {
			t.Errorf("test %d: error: %v", i, err)
			continue
		}
		if !bytes.Equal(data, test.seed) {
			t.Errorf("test %d: got %x want %x", i, data, test.seed)
		}
	}
}
