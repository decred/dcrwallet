/*
 * Copyright (c) 2015-2016 The decred developers
 * Copyright (c) 2018 The ExchangeCoin team
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package pgpwordlist

import (
	"bytes"
	"testing"
)

var tests = []struct {
	mnemonics string
	seed      []byte
	password  string
}{
	{
		mnemonics: "stamp spray never march summer scout course bring cave awful radar lake",
		seed: []byte{25, 155, 199, 172, 124, 21, 4, 135, 159, 36, 175, 175, 176, 145, 83, 233, 94, 9, 25, 100, 145,
			98, 194, 163, 120, 128, 22, 20, 14, 230, 190, 44, 67, 224, 151, 197, 249, 19, 38, 97, 138, 236, 241, 120,
			253, 122, 2, 18, 9, 37, 235, 197, 154, 72, 220, 222, 161, 246, 85, 207, 217, 0, 120, 103},
		password: "",
	},
	{
		mnemonics: "only spirit waste control about law high man spin face cross weird",
		seed: []byte{232, 244, 155, 49, 11, 210, 66, 80, 16, 212, 168, 60, 152, 22, 121, 215, 133, 110, 134, 132,
			107, 176, 204, 30, 101, 112, 7, 140, 243, 235, 217, 162, 226, 180, 141, 246, 222, 217, 134, 18, 59, 149,
			6, 243, 45, 225, 15, 214, 103, 196, 103, 125, 223, 78, 36, 215, 144, 17, 74, 2, 182, 130, 86, 94},
		password: "",
	},
	{
		mnemonics: "ecology luxury cave law rose burst swear secret rotate write total antique",
		seed: []byte{215, 69, 227, 7, 9, 1, 48, 216, 144, 27, 36, 28, 176, 117, 100, 108, 207, 127, 191, 105, 249,
			119, 215, 9, 114, 79, 67, 227, 184, 137, 173, 121, 92, 193, 215, 225, 66, 201, 46, 181, 113, 111, 91, 141,
			83, 143, 39, 136, 224, 44, 52, 137, 157, 233, 155, 33, 128, 233, 213, 124, 60, 155, 43, 72},
		password: "",
	},
	{
		mnemonics: "hollow embrace engine nerve embody goose nasty inner crater fix concert text",
		seed: []byte{161, 165, 113, 211, 234, 203, 193, 80, 43, 209, 8, 11, 75, 83, 243, 230, 244, 201, 174, 37,
			79, 58, 121, 62, 215, 186, 71, 134, 170, 9, 47, 56, 107, 182, 157, 141, 213, 178, 203, 176, 246, 6, 219,
			92, 213, 68, 175, 160, 55, 64, 79, 216, 130, 137, 146, 225, 181, 6, 217, 63, 43, 77, 82, 67},
		password: "password",
	},
	{
		mnemonics: "worry tip deal zero early crash skirt agent eager put letter reflect",
		seed: []byte{3, 7, 57, 158, 59, 135, 195, 53, 213, 7, 186, 127, 32, 132, 163, 232, 177, 90, 156, 7, 84,
			173, 133, 162, 142, 113, 20, 223, 61, 169, 150, 62, 197, 103, 73, 233, 155, 128, 32, 222, 114, 98, 241, 23,
			215, 44, 225, 7, 74, 196, 142, 76, 186, 205, 31, 136, 179, 216, 244, 249, 219, 30, 116, 180},
		password: "password password",
	},
	{
		mnemonics: "walk parade material sibling sign second advice nerve resource comfort crash alpha",
		seed: []byte{245, 44, 186, 123, 81, 112, 191, 254, 118, 94, 81, 244, 165, 161, 148, 0, 4, 181, 153, 43,
			78, 165, 17, 46, 168, 231, 32, 63, 78, 75, 142, 146, 11, 30, 110, 251, 160, 214, 194, 163, 104, 6, 129,
			230, 103, 244, 195, 20, 57, 115, 1, 219, 134, 221, 212, 167, 242, 63, 102, 156, 3, 238, 225, 132},
		password: "worry tip deal zero early crash skirt agent eager put letter reflect",
	},
	{
		mnemonics: "idea female awake vehicle review bone potato hand alone main room blue",
		seed: []byte{140, 27, 15, 119, 56, 16, 166, 203, 155, 116, 5, 128, 141, 212, 105, 121, 94, 113, 215, 57,
			155, 233, 85, 82, 185, 42, 100, 155, 29, 103, 148, 115, 119, 34, 187, 97, 220, 106, 22, 119, 166, 57, 164,
			165, 34, 21, 150, 236, 75, 216, 226, 69, 155, 27, 183, 28, 231, 97, 205, 183, 3, 128, 90, 155},
		password: "     ",
	},
	// TODO more tests, national characters, invalid inputs
}

func TestDecode(t *testing.T) {
	for _, test := range tests {
		seed := DecodeMnemonics(test.mnemonics, test.password)
		if !bytes.Equal(seed, test.seed) {
			t.Errorf("decoded seed %x differs from expected %x for mnemonic \"%s\"", seed, test.seed, test.mnemonics)
		}
	}
}

func TestDecodeWithWrongPassword(t *testing.T) {
	for _, test := range tests {
		seed := DecodeMnemonics(test.mnemonics, test.password+"x")
		if bytes.Equal(seed, test.seed) {
			t.Errorf("decoded seed %x equals expected one", seed)
		}
	}
}

func TestDecodeWithoutPassword(t *testing.T) {
	for _, test := range tests {
		if test.password != "" {
			seed := DecodeMnemonics(test.mnemonics, "")
			if bytes.Equal(seed, test.seed) {
				t.Errorf("decoded seed %x equals expected one", seed)
			}
		}
	}
}
