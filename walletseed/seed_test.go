// Copyright (c) 2016 The Decred developers
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
	data      []byte
}{
	{
		mnemonics: "topmost Istanbul Pluto vagabond treadmill Pacific brackish dictator goldfish Medusa afflict bravado chatter revolver Dupont midsummer stopwatch whimsical cowbell bottomless fracture",
		data: []byte{0xE5, 0x82, 0x94, 0xF2, 0xE9, 0xA2, 0x27, 0x48,
			0x6E, 0x8B, 0x06, 0x1B, 0x31, 0xCC, 0x52, 0x8F, 0xD7,
			0xFA, 0x3F, 0x19},
	},
	{
		mnemonics: "stairway souvenir flytrap recipe adrift upcoming artist positive spearhead Pandora spaniel stupendous tonic concurrent transit Wichita lockup visitor flagpole escapade merit",
		data: []byte{0xD1, 0xD4, 0x64, 0xC0, 0x04, 0xF0, 0x0F, 0xB5,
			0xC9, 0xA4, 0xC8, 0xD8, 0xE4, 0x33, 0xE7, 0xFB, 0x7F,
			0xF5, 0x62, 0x56},
	},
	{
		mnemonics: "tissue disbelief stairway component atlas megaton bedlamp certify tumor monument necklace fascinate tunnel fascinate dreadful armistice upshot Apollo exceed aftermath billiard sardonic vapor microscope brackish suspicious woodlark torpedo hamlet sensation assume recipe",
		data: []byte{0xE3, 0x4C, 0xD1, 0x32, 0x12, 0x8C, 0x19, 0x29,
			0xEC, 0x96, 0x86, 0x5C, 0xED, 0x5C, 0x4D, 0x0B, 0xF4,
			0x0A, 0x5D, 0x02, 0x1F, 0xCE, 0xF5, 0x8D, 0x27, 0xDB,
			0xFE, 0xE3, 0x71, 0xD2, 0x10},
	},
	{
		mnemonics: "aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark adroitness aardvark insurgent",
		data: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00},
	},
}

func TestEncodeMnemonicSlice(t *testing.T) {
	for i, test := range mnemonicTests {
		mnemonics := strings.Join(EncodeMnemonicSlice(test.data), " ")
		if mnemonics != test.mnemonics {
			t.Errorf("test %d: got `%v` want `%v`", i, mnemonics, test.mnemonics)
		}
	}
}

func TestEncodeMnemonic(t *testing.T) {
	for i, test := range mnemonicTests {
		mnemonics := EncodeMnemonic(test.data)
		if mnemonics != test.mnemonics {
			t.Errorf("test %d: got `%v` want `%v`", i, mnemonics, test.mnemonics)
		}
	}
}

func TestDecodeMnemonic(t *testing.T) {
	for i, test := range mnemonicTests {
		data, err := DecodeUserInput(test.mnemonics)
		if err != nil {
			t.Errorf("test %d: error: %v", i, err)
			continue
		}
		if !bytes.Equal(data, test.data) {
			t.Errorf("test %d: got %x want %x", i, data, test.data)
		}
	}
}

func TestDecodeHex(t *testing.T) {
	for i, test := range mnemonicTests {
		data, err := DecodeUserInput(hex.EncodeToString(test.data))
		if err != nil {
			t.Errorf("test %d: error: %v", i, err)
			continue
		}
		if !bytes.Equal(data, test.data) {
			t.Errorf("test %d: got %x want %x", i, data, test.data)
		}
	}
}
