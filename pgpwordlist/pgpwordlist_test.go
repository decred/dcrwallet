/*
 * Copyright (c) 2015-2016 The decred developers
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
	"strings"
	"testing"
)

var tests = []struct {
	mnemonics string
	data      []byte
}{
	{
		mnemonics: "topmost Istanbul Pluto vagabond treadmill Pacific brackish dictator goldfish Medusa afflict bravado chatter revolver Dupont midsummer stopwatch whimsical cowbell bottomless",
		data: []byte{0xE5, 0x82, 0x94, 0xF2, 0xE9, 0xA2, 0x27, 0x48,
			0x6E, 0x8B, 0x06, 0x1B, 0x31, 0xCC, 0x52, 0x8F, 0xD7,
			0xFA, 0x3F, 0x19},
	},
	{
		mnemonics: "stairway souvenir flytrap recipe adrift upcoming artist positive spearhead Pandora spaniel stupendous tonic concurrent transit Wichita lockup visitor flagpole escapade",
		data: []byte{0xD1, 0xD4, 0x64, 0xC0, 0x04, 0xF0, 0x0F, 0xB5,
			0xC9, 0xA4, 0xC8, 0xD8, 0xE4, 0x33, 0xE7, 0xFB, 0x7F,
			0xF5, 0x62, 0x56},
	},
}

func TestDecode(t *testing.T) {
	for _, test := range tests {
		data, err := DecodeMnemonics(strings.Split(test.mnemonics, " "))
		if err != nil {
			t.Error(err)
			continue
		}
		if !bytes.Equal(data, test.data) {
			t.Errorf("decoded data %x differs from expected %x", data, test.data)
		}
	}
}

func TestEncodeMnemonics(t *testing.T) {
	for _, test := range tests {
		mnemonicsSlice := strings.Split(test.mnemonics, " ")
		for i, b := range test.data {
			mnemonic := ByteToMnemonic(b, i)
			if mnemonic != mnemonicsSlice[i] {
				t.Errorf("returned mnemonic %s differs from expected %s", mnemonic, mnemonicsSlice[i])
			}
		}
	}
}
