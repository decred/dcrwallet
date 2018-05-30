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
		mnemonics: "legal winner thank year wave sausage worth useful legal winner thank yellow",
		seed:      []byte{46, 137, 5, 129, 155, 135, 35, 254, 44, 29, 22, 24, 96, 229, 238, 24, 48, 49, 141, 191, 73, 168, 59, 212, 81, 207, 184, 68, 12, 40, 189, 111, 164, 87, 254, 18, 150, 16, 101, 89, 163, 200, 9, 55, 161, 193, 6, 155, 227, 163, 165, 189, 56, 30, 230, 38, 14, 141, 151, 57, 252, 225, 246, 7},
		ent:       []byte{127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127},
		password:  "TREZOR",
	},

	{
		mnemonics: "letter advice cage absurd amount doctor acoustic avoid letter advice cage above",
		seed:      []byte{215, 29, 232, 86, 248, 26, 138, 204, 101, 230, 252, 133, 26, 56, 212, 215, 236, 33, 111, 208, 121, 109, 10, 104, 39, 163, 173, 110, 213, 81, 26, 48, 250, 40, 15, 18, 235, 46, 71, 237, 42, 192, 59, 92, 70, 42, 3, 88, 209, 141, 105, 254, 79, 152, 94, 200, 23, 120, 193, 179, 112, 182, 82, 168},
		ent:       []byte{128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128},
		password:  "TREZOR",
	},

	{
		mnemonics: "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo wrong",
		seed:      []byte{172, 39, 73, 84, 128, 34, 82, 34, 7, 157, 123, 225, 129, 88, 55, 81, 232, 111, 87, 16, 39, 176, 73, 123, 91, 93, 17, 33, 142, 10, 138, 19, 51, 37, 114, 145, 127, 15, 142, 90, 88, 150, 32, 198, 241, 91, 17, 198, 29, 238, 50, 118, 81, 161, 76, 52, 225, 130, 49, 5, 46, 72, 192, 105},
		ent:       []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
		password:  "TREZOR",
	},

	{
		mnemonics: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon agent",
		seed:      []byte{3, 88, 149, 242, 244, 129, 177, 176, 240, 31, 207, 140, 40, 156, 121, 70, 96, 178, 137, 152, 26, 120, 248, 16, 100, 71, 112, 127, 221, 150, 102, 202, 6, 218, 90, 154, 86, 81, 129, 89, 155, 121, 245, 59, 132, 77, 138, 113, 221, 159, 67, 156, 82, 163, 215, 179, 232, 167, 156, 144, 106, 200, 69, 250},
		ent:       []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		password:  "TREZOR",
	},

	{
		mnemonics: "legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal will",
		seed:      []byte{242, 185, 69, 8, 115, 43, 203, 172, 188, 192, 32, 250, 239, 236, 252, 137, 254, 175, 166, 100, 154, 84, 145, 184, 201, 82, 206, 222, 73, 108, 33, 74, 12, 123, 60, 57, 45, 22, 135, 72, 242, 212, 166, 18, 186, 218, 7, 83, 181, 42, 28, 122, 197, 60, 30, 147, 171, 213, 198, 50, 11, 158, 149, 221},
		ent:       []byte{127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127},
		password:  "TREZOR",
	},

	{
		mnemonics: "letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic avoid letter always",
		seed:      []byte{16, 125, 124, 2, 165, 170, 111, 56, 197, 128, 131, 255, 116, 240, 76, 96, 124, 45, 44, 14, 204, 85, 80, 29, 173, 215, 45, 2, 91, 117, 27, 194, 127, 233, 19, 255, 183, 150, 248, 65, 196, 155, 29, 51, 182, 16, 207, 14, 145, 211, 170, 35, 144, 39, 245, 233, 159, 228, 206, 158, 80, 136, 205, 101},
		ent:       []byte{128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128},
		password:  "TREZOR",
	},

	{
		mnemonics: "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo when",
		seed:      []byte{12, 214, 229, 216, 39, 187, 98, 235, 143, 193, 226, 98, 37, 66, 35, 129, 127, 208, 104, 167, 75, 91, 68, 156, 194, 246, 103, 195, 241, 249, 133, 167, 99, 121, 180, 51, 72, 217, 82, 226, 38, 91, 76, 209, 41, 9, 7, 88, 179, 227, 194, 196, 145, 3, 181, 5, 26, 172, 46, 174, 184, 144, 165, 40},
		ent:       []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
		password:  "TREZOR",
	},

	{
		mnemonics: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art",
		seed:      []byte{189, 168, 84, 70, 198, 132, 19, 112, 112, 144, 165, 32, 34, 237, 210, 106, 28, 148, 98, 41, 80, 41, 242, 230, 12, 215, 196, 242, 187, 211, 9, 113, 112, 175, 122, 77, 115, 36, 92, 175, 169, 195, 204, 168, 213, 97, 167, 195, 222, 111, 93, 74, 16, 190, 142, 210, 165, 230, 8, 214, 143, 146, 252, 200},
		ent:       []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		password:  "TREZOR",
	},

	{
		mnemonics: "legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth title",
		seed:      []byte{188, 9, 252, 161, 128, 79, 126, 105, 218, 147, 194, 242, 2, 142, 178, 56, 194, 39, 242, 233, 221, 163, 12, 214, 54, 153, 35, 37, 120, 72, 10, 64, 33, 177, 70, 173, 113, 127, 187, 126, 69, 28, 233, 235, 131, 95, 67, 98, 11, 245, 197, 20, 219, 15, 138, 221, 73, 245, 209, 33, 68, 157, 62, 135},
		ent:       []byte{127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127, 127},
		password:  "TREZOR",
	},

	{
		mnemonics: "letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic bless",
		seed:      []byte{192, 197, 25, 189, 14, 145, 162, 237, 84, 53, 125, 157, 30, 190, 246, 245, 175, 33, 138, 21, 54, 36, 207, 79, 45, 169, 17, 160, 237, 143, 122, 9, 226, 239, 97, 175, 10, 202, 0, 112, 150, 223, 67, 0, 34, 247, 162, 182, 251, 145, 102, 26, 149, 137, 9, 112, 105, 114, 13, 1, 94, 78, 152, 47},
		ent:       []byte{128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128, 128},
		password:  "TREZOR",
	},

	{
		mnemonics: "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo vote",
		seed:      []byte{221, 72, 193, 4, 105, 140, 48, 207, 226, 182, 20, 33, 3, 36, 134, 34, 251, 123, 176, 255, 105, 46, 235, 176, 0, 137, 179, 45, 34, 72, 78, 22, 19, 145, 47, 10, 91, 105, 68, 7, 190, 137, 159, 253, 49, 237, 57, 146, 196, 86, 205, 246, 15, 93, 69, 100, 184, 186, 63, 5, 166, 152, 144, 173},
		ent:       []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255},
		password:  "TREZOR",
	},

	{
		mnemonics: "jelly better achieve collect unaware mountain thought cargo oxygen act hood bridge",
		seed:      []byte{181, 182, 208, 18, 125, 177, 169, 210, 34, 106, 240, 195, 52, 96, 49, 215, 122, 243, 30, 145, 141, 186, 100, 40, 122, 27, 68, 184, 235, 246, 60, 221, 82, 103, 111, 103, 42, 41, 10, 174, 80, 36, 114, 207, 45, 96, 44, 5, 31, 62, 111, 24, 5, 94, 132, 228, 196, 56, 151, 252, 78, 81, 166, 255},
		ent:       []byte{119, 194, 176, 7, 22, 206, 199, 33, 56, 57, 21, 158, 64, 77, 181, 13},
		password:  "TREZOR",
	},

	{
		mnemonics: "renew stay biology evidence goat welcome casual join adapt armor shuffle fault little machine walk stumble urge swap",
		seed:      []byte{146, 72, 216, 62, 6, 244, 205, 152, 222, 191, 91, 111, 1, 5, 66, 118, 13, 249, 37, 206, 70, 207, 56, 161, 189, 180, 228, 222, 125, 33, 245, 195, 147, 102, 148, 28, 105, 225, 189, 191, 41, 102, 224, 246, 230, 219, 236, 232, 152, 160, 226, 240, 164, 194, 179, 230, 64, 149, 61, 254, 139, 123, 189, 197},
		ent:       []byte{182, 58, 156, 89, 166, 230, 65, 242, 136, 235, 193, 3, 1, 127, 29, 169, 248, 41, 11, 61, 166, 189, 239, 123},
		password:  "TREZOR",
	},

	{
		mnemonics: "dignity pass list indicate nasty swamp pool script soccer toe leaf photo multiply desk host tomato cradle drill spread actor shine dismiss champion exotic",
		seed:      []byte{255, 127, 49, 132, 223, 134, 150, 216, 190, 249, 75, 108, 3, 17, 77, 190, 224, 239, 137, 255, 147, 135, 18, 48, 29, 39, 237, 131, 54, 202, 137, 239, 150, 53, 218, 32, 175, 7, 212, 23, 95, 43, 245, 243, 222, 19, 15, 57, 201, 217, 232, 221, 4, 114, 72, 156, 25, 177, 160, 32, 169, 64, 218, 103},
		ent:       []byte{62, 20, 22, 9, 185, 121, 51, 182, 106, 6, 13, 205, 220, 113, 250, 209, 217, 22, 119, 219, 135, 32, 49, 232, 95, 76, 1, 92, 94, 126, 137, 130},
		password:  "TREZOR",
	},

	{
		mnemonics: "afford alter spike radar gate glance object seek swamp infant panel yellow",
		seed:      []byte{101, 249, 58, 159, 54, 182, 200, 92, 190, 99, 79, 252, 31, 153, 242, 184, 44, 187, 16, 179, 30, 220, 127, 8, 123, 79, 108, 185, 233, 118, 233, 250, 247, 111, 244, 31, 143, 39, 201, 154, 253, 243, 143, 122, 48, 59, 161, 19, 110, 228, 138, 76, 30, 127, 205, 61, 186, 122, 168, 118, 17, 58, 54, 228},
		ent:       []byte{4, 96, 239, 71, 88, 86, 4, 197, 102, 6, 24, 219, 46, 106, 126, 127},
		password:  "TREZOR",
	},

	{
		mnemonics: "indicate race push merry suffer human cruise dwarf pole review arch keep canvas theme poem divorce alter left",
		seed:      []byte{59, 191, 157, 170, 13, 250, 216, 34, 151, 134, 172, 229, 221, 180, 224, 15, 169, 138, 4, 74, 228, 196, 151, 95, 253, 94, 9, 77, 186, 158, 11, 178, 137, 52, 157, 190, 32, 145, 118, 31, 48, 243, 130, 212, 227, 92, 74, 103, 14, 232, 171, 80, 117, 141, 44, 85, 136, 27, 230, 158, 50, 113, 23, 186},
		ent:       []byte{114, 246, 14, 186, 197, 221, 138, 221, 141, 42, 37, 167, 151, 16, 44, 60, 226, 27, 192, 41, 194, 0, 7, 111},
		password:  "TREZOR",
	},

	{
		mnemonics: "clutch control vehicle tonight unusual clog visa ice plunge glimpse recipe series open hour vintage deposit universe tip job dress radar refuse motion taste",
		seed:      []byte{254, 144, 143, 150, 244, 102, 104, 178, 213, 179, 125, 130, 245, 88, 199, 126, 208, 214, 157, 208, 231, 224, 67, 165, 176, 81, 28, 72, 194, 241, 6, 70, 148, 169, 86, 248, 99, 96, 201, 61, 208, 64, 82, 168, 137, 148, 151, 206, 158, 152, 94, 190, 12, 140, 82, 185, 85, 230, 174, 134, 212, 255, 68, 73},
		ent:       []byte{44, 133, 239, 199, 242, 78, 228, 87, 61, 43, 129, 166, 236, 102, 206, 226, 9, 178, 220, 189, 9, 216, 237, 220, 81, 224, 33, 91, 11, 104, 228, 22},
		password:  "TREZOR",
	},

	{
		mnemonics: "turtle front uncle idea crush write shrug there lottery flower risk shell",
		seed:      []byte{189, 251, 118, 160, 117, 159, 48, 27, 11, 137, 154, 30, 57, 133, 34, 126, 83, 179, 245, 30, 103, 227, 242, 166, 83, 99, 202, 237, 243, 227, 47, 222, 66, 166, 108, 64, 79, 24, 215, 176, 88, 24, 201, 94, 243, 202, 30, 81, 70, 100, 104, 86, 196, 97, 192, 115, 22, 148, 103, 81, 22, 128, 135, 108},
		ent:       []byte{234, 235, 171, 178, 56, 51, 81, 253, 49, 215, 3, 132, 11, 50, 233, 226},
		password:  "TREZOR",
	},

	{
		mnemonics: "kiss carry display unusual confirm curtain upgrade antique rotate hello void custom frequent obey nut hole price segment",
		seed:      []byte{237, 86, 255, 108, 131, 60, 7, 152, 46, 183, 17, 154, 143, 72, 253, 54, 60, 74, 155, 22, 1, 205, 45, 231, 54, 176, 16, 69, 197, 235, 138, 180, 245, 123, 7, 148, 3, 72, 93, 28, 73, 36, 240, 121, 13, 193, 10, 151, 23, 99, 51, 124, 185, 249, 198, 34, 38, 246, 79, 255, 38, 57, 124, 121},
		ent:       []byte{122, 196, 92, 254, 119, 34, 238, 108, 123, 168, 79, 188, 45, 91, 214, 27, 69, 203, 47, 229, 235, 101, 170, 120},
		password:  "TREZOR",
	},

	{
		mnemonics: "exile ask congress lamp submit jacket era scheme attend cousin alcohol catch course end lucky hurt sentence oven short ball bird grab wing top",
		seed:      []byte{9, 94, 230, 248, 23, 180, 194, 203, 48, 165, 167, 151, 54, 10, 129, 164, 10, 176, 249, 164, 226, 94, 205, 103, 42, 63, 88, 160, 181, 186, 6, 135, 192, 150, 166, 177, 77, 44, 13, 235, 59, 222, 252, 228, 246, 29, 1, 174, 7, 65, 125, 80, 36, 41, 53, 46, 39, 105, 81, 99, 247, 68, 122, 140},
		ent:       []byte{79, 161, 168, 188, 62, 109, 128, 238, 19, 22, 5, 14, 134, 44, 24, 18, 3, 20, 147, 33, 43, 126, 195, 243, 187, 27, 8, 241, 104, 202, 190, 239},
		password:  "TREZOR",
	},

	{
		mnemonics: "board flee heavy tunnel powder denial science ski answer betray cargo cat",
		seed:      []byte{110, 255, 27, 178, 21, 98, 145, 133, 9, 199, 60, 185, 144, 38, 13, 176, 124, 12, 227, 79, 240, 227, 204, 74, 140, 179, 39, 97, 41, 251, 203, 48, 11, 221, 254, 0, 88, 49, 53, 14, 253, 99, 57, 9, 244, 118, 196, 92, 136, 37, 50, 118, 217, 253, 13, 246, 239, 72, 96, 158, 139, 183, 220, 168},
		ent:       []byte{24, 171, 25, 169, 245, 74, 146, 116, 240, 62, 82, 9, 162, 172, 138, 145},
		password:  "TREZOR",
	},

	{
		mnemonics: "board blade invite damage undo sun mimic interest slam gaze truly inherit resist great inject rocket museum chief",
		seed:      []byte{248, 69, 33, 199, 119, 161, 59, 97, 86, 66, 52, 191, 143, 139, 98, 179, 175, 206, 39, 252, 64, 98, 181, 27, 181, 230, 43, 223, 236, 178, 56, 100, 238, 110, 207, 7, 193, 213, 169, 124, 8, 52, 48, 124, 92, 133, 45, 140, 235, 136, 231, 201, 121, 35, 192, 163, 180, 150, 190, 221, 78, 95, 136, 169},
		ent:       []byte{24, 162, 225, 216, 27, 142, 207, 178, 163, 51, 173, 203, 12, 23, 165, 185, 235, 118, 204, 93, 5, 219, 145, 164},
		password:  "TREZOR",
	},

	{
		mnemonics: "beyond stage sleep clip because twist token leaf atom beauty genius food business side grid unable middle armed observe pair crouch tonight away coconut",
		seed:      []byte{177, 85, 9, 234, 162, 208, 157, 62, 253, 62, 0, 110, 244, 33, 81, 179, 3, 103, 220, 110, 58, 165, 228, 76, 171, 163, 254, 77, 62, 53, 46, 101, 16, 31, 189, 184, 106, 150, 119, 107, 145, 148, 111, 240, 111, 142, 172, 89, 77, 198, 238, 29, 62, 130, 164, 45, 254, 27, 64, 254, 246, 188, 195, 253},
		ent:       []byte{21, 218, 135, 44, 149, 161, 61, 215, 56, 251, 245, 14, 66, 117, 131, 173, 97, 241, 143, 217, 159, 98, 140, 65, 122, 97, 207, 131, 67, 201, 4, 25},
		password:  "TREZOR",
	},

	{
		mnemonics: "exile ask congress lamp submit jacket era scheme attend cousin alcohol catch course end lucky hurt sentence oven short ball bird grab wing top",
		seed:      []byte{138, 145, 168, 67, 173, 79, 237, 233, 95, 35, 147, 112, 153, 169, 79, 17, 113, 21, 163, 105, 144, 54, 3, 118, 30, 202, 186, 231, 52, 181, 213, 1, 221, 186, 4, 177, 163, 201, 242, 37, 100, 55, 239, 45, 35, 15, 41, 93, 143, 8, 103, 110, 93, 233, 58, 213, 25, 13, 166, 100, 93, 237, 129, 96},
		ent:       []byte{79, 161, 168, 188, 62, 109, 128, 238, 19, 22, 5, 14, 134, 44, 24, 18, 3, 20, 147, 33, 43, 126, 195, 243, 187, 27, 8, 241, 104, 202, 190, 239},
		password:  "",
	},

	{
		mnemonics: "sadness ocean country fun flat soul athlete slogan extra option chief exchange strike muscle helmet absent true need east fly heavy fabric strike oval",
		seed:      []byte{48, 87, 25, 173, 216, 113, 182, 45, 46, 43, 95, 238, 25, 16, 111, 71, 85, 186, 16, 139, 8, 248, 43, 250, 64, 134, 141, 37, 63, 148, 114, 34, 77, 196, 146, 107, 187, 148, 161, 254, 130, 83, 145, 213, 64, 254, 114, 253, 171, 102, 75, 36, 229, 71, 148, 1, 3, 117, 151, 51, 191, 41, 1, 26},
		ent:       []byte{189, 243, 28, 196, 47, 5, 137, 159, 67, 142, 95, 81, 19, 116, 159, 39, 93, 113, 35, 26, 184, 5, 233, 82, 121, 22, 172, 246, 166, 162, 245, 196},
		password:  "",
	},

	{
		mnemonics: "delay purity grid close north render quick phone unit chicken trophy echo oil perfect foot gossip agent tide humor vanish trade original pledge manual",
		seed:      []byte{33, 226, 221, 2, 88, 174, 234, 44, 88, 122, 123, 216, 77, 116, 132, 50, 157, 177, 171, 106, 131, 29, 25, 134, 252, 245, 232, 100, 76, 179, 227, 105, 23, 182, 71, 184, 201, 76, 147, 55, 221, 246, 98, 94, 43, 72, 185, 134, 128, 252, 118, 142, 37, 33, 55, 221, 77, 222, 237, 68, 201, 106, 242, 69},
		ent:       []byte{57, 245, 201, 153, 149, 217, 101, 108, 43, 237, 28, 237, 164, 247, 163, 162, 249, 157, 70, 22, 187, 39, 4, 252, 57, 188, 120, 174, 107, 57, 105, 164},
		password:  "     ",
	},

	{
		mnemonics: "pill improve grain game pepper birth legend have proud copper input tenant announce case crack minor cage venture spin debris hen face million nurse",
		seed:      []byte{45, 38, 42, 60, 71, 103, 27, 194, 105, 126, 184, 157, 209, 211, 70, 32, 35, 103, 57, 57, 191, 203, 114, 76, 60, 168, 52, 120, 153, 123, 191, 167, 172, 180, 118, 101, 7, 75, 90, 82, 241, 120, 7, 144, 63, 202, 21, 127, 12, 123, 150, 226, 157, 11, 60, 207, 140, 64, 122, 90, 103, 87, 88, 189},
		ent:       []byte{164, 206, 65, 150, 47, 154, 46, 45, 95, 227, 77, 172, 197, 253, 210, 239, 128, 148, 70, 76, 116, 105, 32, 62, 75, 71, 156, 54, 178, 163, 35, 44},
		password:  "Ú¾ÆƂœÂń☄õ⚊☷♊ÜȑǗś☠♋♋*Ó😆🙇ŗè☼cĊ4🙋\"űīƄűǇȐƲ♘âÖŵ😊ǎ4ő🙀šæA☛ǠȰ⚍_😆æ♓☎😠Ĥ☀😟🙌7é-ÃÂ&wƭXµ♶ġê♚",
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
