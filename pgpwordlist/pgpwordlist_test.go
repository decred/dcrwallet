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
		seed:      []byte{25, 155, 199, 172, 124, 21, 4, 135, 159, 36, 175, 175, 176, 145, 83, 233, 94, 9, 25, 100, 145, 98, 194, 163, 120, 128, 22, 20, 14, 230, 190, 44, 67, 224, 151, 197, 249, 19, 38, 97, 138, 236, 241, 120, 253, 122, 2, 18, 9, 37, 235, 197, 154, 72, 220, 222, 161, 246, 85, 207, 217, 0, 120, 103},
		password:  "",
	},
	{
		mnemonics: "only spirit waste control about law high man spin face cross weird",
		seed:      []byte{232, 244, 155, 49, 11, 210, 66, 80, 16, 212, 168, 60, 152, 22, 121, 215, 133, 110, 134, 132, 107, 176, 204, 30, 101, 112, 7, 140, 243, 235, 217, 162, 226, 180, 141, 246, 222, 217, 134, 18, 59, 149, 6, 243, 45, 225, 15, 214, 103, 196, 103, 125, 223, 78, 36, 215, 144, 17, 74, 2, 182, 130, 86, 94},
		password:  "",
	},
	{
		mnemonics: "ecology luxury cave law rose burst swear secret rotate write total antique",
		seed:      []byte{215, 69, 227, 7, 9, 1, 48, 216, 144, 27, 36, 28, 176, 117, 100, 108, 207, 127, 191, 105, 249, 119, 215, 9, 114, 79, 67, 227, 184, 137, 173, 121, 92, 193, 215, 225, 66, 201, 46, 181, 113, 111, 91, 141, 83, 143, 39, 136, 224, 44, 52, 137, 157, 233, 155, 33, 128, 233, 213, 124, 60, 155, 43, 72},
		password:  "",
	},
	{
		mnemonics: "hollow embrace engine nerve embody goose nasty inner crater fix concert text",
		seed:      []byte{161, 165, 113, 211, 234, 203, 193, 80, 43, 209, 8, 11, 75, 83, 243, 230, 244, 201, 174, 37, 79, 58, 121, 62, 215, 186, 71, 134, 170, 9, 47, 56, 107, 182, 157, 141, 213, 178, 203, 176, 246, 6, 219, 92, 213, 68, 175, 160, 55, 64, 79, 216, 130, 137, 146, 225, 181, 6, 217, 63, 43, 77, 82, 67},
		password:  "password",
	},
	{
		mnemonics: "worry tip deal zero early crash skirt agent eager put letter reflect",
		seed:      []byte{3, 7, 57, 158, 59, 135, 195, 53, 213, 7, 186, 127, 32, 132, 163, 232, 177, 90, 156, 7, 84, 173, 133, 162, 142, 113, 20, 223, 61, 169, 150, 62, 197, 103, 73, 233, 155, 128, 32, 222, 114, 98, 241, 23, 215, 44, 225, 7, 74, 196, 142, 76, 186, 205, 31, 136, 179, 216, 244, 249, 219, 30, 116, 180},
		password:  "password password",
	},
	{
		mnemonics: "walk parade material sibling sign second advice nerve resource comfort crash alpha",
		seed:      []byte{245, 44, 186, 123, 81, 112, 191, 254, 118, 94, 81, 244, 165, 161, 148, 0, 4, 181, 153, 43, 78, 165, 17, 46, 168, 231, 32, 63, 78, 75, 142, 146, 11, 30, 110, 251, 160, 214, 194, 163, 104, 6, 129, 230, 103, 244, 195, 20, 57, 115, 1, 219, 134, 221, 212, 167, 242, 63, 102, 156, 3, 238, 225, 132},
		password:  "worry tip deal zero early crash skirt agent eager put letter reflect",
	},
	{
		mnemonics: "idea female awake vehicle review bone potato hand alone main room blue",
		seed:      []byte{140, 27, 15, 119, 56, 16, 166, 203, 155, 116, 5, 128, 141, 212, 105, 121, 94, 113, 215, 57, 155, 233, 85, 82, 185, 42, 100, 155, 29, 103, 148, 115, 119, 34, 187, 97, 220, 106, 22, 119, 166, 57, 164, 165, 34, 21, 150, 236, 75, 216, 226, 69, 155, 27, 183, 28, 231, 97, 205, 183, 3, 128, 90, 155},
		password:  "     ",
	},
	{
		mnemonics: "autumn shine gossip copy number crew cheap truth juice price open spell",
		seed:      []byte{19, 5, 1, 158, 130, 108, 94, 66, 159, 125, 51, 80, 189, 15, 26, 102, 43, 116, 186, 76, 70, 232, 24, 58, 20, 206, 215, 131, 154, 10, 17, 254, 56, 158, 72, 65, 185, 184, 252, 4, 2, 191, 211, 243, 216, 245, 202, 133, 43, 130, 127, 224, 132, 147, 22, 253, 42, 123, 147, 134, 94, 33, 12, 69},
		password:  "ąężó",
	},
	{
		mnemonics: "language bargain age boss elevator spoil install seven shield black drama market",
		seed:      []byte{122, 48, 44, 48, 91, 142, 28, 89, 25, 168, 208, 62, 191, 154, 184, 210, 97, 11, 243, 159, 209, 137, 140, 69, 9, 250, 158, 32, 51, 180, 172, 160, 26, 115, 43, 195, 125, 59, 9, 62, 36, 166, 65, 172, 172, 166, 67, 48, 252, 45, 116, 220, 107, 174, 130, 164, 109, 39, 119, 19, 81, 84, 67, 237},
		password:  "(K\"~)BFT2~*?>Sx\"",
	},
	{
		mnemonics: "license donate phone drama stable cement ramp conduct quit analyst donate struggle",
		seed:      []byte{144, 129, 18, 168, 223, 64, 153, 145, 104, 234, 245, 88, 188, 29, 174, 92, 235, 143, 61, 217, 67, 144, 83, 188, 244, 246, 161, 135, 171, 198, 107, 39, 233, 102, 22, 232, 178, 135, 38, 55, 26, 21, 59, 122, 221, 250, 62, 161, 88, 246, 212, 59, 243, 35, 24, 172, 99, 65, 222, 158, 46, 166, 85, 107},
		password:  "zgm_uQSbH4=STjCEB2YLPywa2D&ga3xSahjBfnbLGYqDT9^rrP",
	},
	{
		mnemonics: "dismiss orbit normal process entire coin excess fragile canyon suffer approve solar",
		seed:      []byte{245, 51, 115, 74, 8, 192, 85, 136, 15, 42, 108, 163, 41, 166, 239, 59, 219, 157, 198, 68, 229, 4, 59, 7, 117, 130, 123, 235, 224, 73, 51, 41, 249, 89, 161, 83, 55, 191, 53, 209, 54, 191, 224, 111, 205, 242, 91, 31, 230, 219, 144, 6, 209, 62, 64, 184, 243, 167, 252, 164, 201, 223, 251, 228},
		password:  "hpL6FPqWYsC=r8SE!EQebT3&Wffq8vzf#phLMFGx+P9H$^zWgAD9#=b?@eAjVjDZS!p6QcgPZeBVmmGmS5nkec-7XD5#-G62@nkP",
	},
	{
		mnemonics: "grief season lonely pulp mammal identify disorder govern garlic strike car surround",
		seed:      []byte{1, 85, 163, 200, 12, 64, 111, 133, 117, 150, 146, 126, 57, 221, 118, 176, 121, 197, 44, 133, 89, 161, 241, 138, 141, 157, 99, 96, 47, 51, 213, 116, 170, 1, 91, 143, 193, 214, 79, 51, 47, 65, 139, 110, 72, 194, 207, 153, 1, 226, 52, 91, 131, 199, 87, 227, 2, 191, 59, 143, 85, 53, 195, 208},
		password:  "Ú¾ÆƂœÂńƄűǇȐƲ7é-ÃÂ&wƭXµ♶ġê♚",
	},
	{
		mnemonics: "lecture giggle twice involve book fog bomb novel myth rabbit increase scrap",
		seed:      []byte{152, 212, 44, 59, 82, 108, 9, 205, 77, 58, 170, 111, 253, 204, 137, 104, 44, 237, 132, 21, 214, 187, 46, 49, 49, 216, 95, 186, 79, 91, 12, 197, 235, 13, 159, 125, 178, 59, 177, 227, 126, 215, 240, 143, 231, 110, 169, 136, 142, 184, 206, 242, 119, 52, 225, 208, 59, 39, 84, 84, 101, 197, 219, 150},
		password:  "Ú¾ÆƂœÂń☄õ⚊☷♊ÜȑǗś☠♋♋*Ó😆🙇ŗè☼cĊ4🙋\"űīƄűǇȐƲ♘âÖŵ😊ǎ4ő🙀šæA☛ǠȰ⚍_😆æ♓☎😠Ĥ☀😟🙌7é-ÃÂ&wƭXµ♶ġê♚",
	},
	{
		mnemonics: "father trip catalog crop van climb pyramid sudden spawn price oven antenna",
		seed:      []byte{158, 46, 58, 35, 36, 151, 52, 78, 116, 128, 85, 76, 193, 193, 206, 155, 151, 71, 23, 151, 122, 97, 102, 156, 170, 138, 199, 246, 250, 237, 200, 22, 138, 222, 86, 117, 24, 24, 106, 214, 201, 135, 74, 165, 251, 187, 1, 11, 133, 151, 59, 177, 156, 126, 160, 27, 252, 86, 198, 79, 20, 175, 124, 255},
		password:  "152,212,44,59,82,108,9,205,77,58,170,111,253,204,137,104,44,237,132,21,214,187,46,49,49,216,95,186,79,91,12,197,235,13,159,125,178,59,177,227,126,215,240,143,231,110,169,136,142,184,206,242,119,52,225,208,59,39,84,84,101,197,219,150",
	},
	{
		mnemonics: "Hollow embRace ENGINE NERVE embody GOose nasTy inner Crater fIx conCert text",
		seed:      []byte{161, 165, 113, 211, 234, 203, 193, 80, 43, 209, 8, 11, 75, 83, 243, 230, 244, 201, 174, 37, 79, 58, 121, 62, 215, 186, 71, 134, 170, 9, 47, 56, 107, 182, 157, 141, 213, 178, 203, 176, 246, 6, 219, 92, 213, 68, 175, 160, 55, 64, 79, 216, 130, 137, 146, 225, 181, 6, 217, 63, 43, 77, 82, 67},
		password:  "password",
	},
	{
		mnemonics: "  hollow   embrace   engine nerve  embody goose nasty   inner  crater fix concert text   ",
		seed:      []byte{161, 165, 113, 211, 234, 203, 193, 80, 43, 209, 8, 11, 75, 83, 243, 230, 244, 201, 174, 37, 79, 58, 121, 62, 215, 186, 71, 134, 170, 9, 47, 56, 107, 182, 157, 141, 213, 178, 203, 176, 246, 6, 219, 92, 213, 68, 175, 160, 55, 64, 79, 216, 130, 137, 146, 225, 181, 6, 217, 63, 43, 77, 82, 67},
		password:  "password",
	},
	{
		mnemonics: "make quote fame expand chimney witness ladder thought dash square ivory wonder stay bridge plunge culture royal luxury mix tomato dust vault innocent moon",
		seed:      []byte{40, 29, 40, 163, 84, 122, 69, 117, 234, 119, 99, 35, 229, 114, 79, 10, 158, 31, 86, 205, 244, 93, 205, 93, 29, 82, 132, 197, 153, 252, 216, 22, 239, 178, 22, 44, 69, 211, 70, 172, 125, 19, 11, 250, 106, 170, 5, 17, 197, 184, 131, 72, 123, 104, 134, 68, 127, 209, 202, 73, 67, 145, 25, 2},
		password:  "test",
	},
}

var invalidMnemonicsTests = []struct {
	mnemonics string
	error     string
}{
	{
		mnemonics: "stamp spray never march summer scout course bring cave awful radar",
		error:     "invalid number of words, expected 12, 15, 18, 21 or 24, instead got 11",
	},
	{
		mnemonics: "idea female awake vehicle review bone potato hand alone main room blue text awake",
		error:     "invalid number of words, expected 12, 15, 18, 21 or 24, instead got 14",
	},
	{
		mnemonics: "idea female awake vehicle review bone potato hand alone main room blue idea female awake vehicle review bone potato hand alone main room blue main",
		error:     "invalid number of words, expected 12, 15, 18, 21 or 24, instead got 25",
	},
	{
		mnemonics: "idea female awake vehicle review bone potato notaword alone main room blue",
		error:     "word \"notaword\" is outside of dictionary",
	},
}

func TestDecode(t *testing.T) {
	for _, test := range tests {
		seed, err := DecodeMnemonics(test.mnemonics, test.password)
		if err != nil {
			t.Errorf("failed to decode mnemonics: %v", err)
		}
		if !bytes.Equal(seed, test.seed) {
			t.Errorf("decoded seed %x differs from expected %x for mnemonic \"%s\"", seed, test.seed, test.mnemonics)
		}
	}
}

func TestDecodeWithWrongPassword(t *testing.T) {
	for _, test := range tests {
		seed, err := DecodeMnemonics(test.mnemonics, test.password+"x")
		if err != nil {
			t.Errorf("failed to decode mnemonics: %v", err)
		}
		if bytes.Equal(seed, test.seed) {
			t.Errorf("decoded seed equals expected one when password is wrong for mnemonic \"%s\"", test.mnemonics)
		}
	}
}

func TestDecodeWithoutPassword(t *testing.T) {
	for _, test := range tests {
		if test.password != "" {
			seed, err := DecodeMnemonics(test.mnemonics, "")
			if err != nil {
				t.Errorf("failed to decode mnemonics: %v", err)
			}
			if bytes.Equal(seed, test.seed) {
				t.Errorf("decoded seed equals expected one when no password given for mnemonic \"%s\"", test.mnemonics)
			}
		}
	}
}

func TestDecodeInvalidMnemonics(t *testing.T) {
	for _, test := range invalidMnemonicsTests {
		_, err := DecodeMnemonics(test.mnemonics, "")
		if err.Error() != test.error {
			t.Errorf("returned error \n\"%v\"\nis different than expected\n\"%s\"", err, test.error)
		}
	}
}
