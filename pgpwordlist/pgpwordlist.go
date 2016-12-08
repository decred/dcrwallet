/*
 * Copyright (c) 2015-2016 The Decred developers
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
	"fmt"
	"strings"
)

// ByteToMnemonic returns the PGP word list encoding of b when found at index.
func ByteToMnemonic(b byte, index int) string {
	bb := uint16(b) * 2
	if index%2 != 0 {
		bb++
	}
	return wordList[bb]
}

// DecodeMnemonics returns the decoded value that is encoded by words.  Any
// words that are whitespace are empty are skipped.
func DecodeMnemonics(words []string) ([]byte, error) {
	decoded := make([]byte, len(words))
	idx := 0
	for _, w := range words[:len(words)] {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		b, ok := wordIndexes[strings.ToLower(w)]
		if !ok {
			return nil, fmt.Errorf("word %v is not in the PGP word list", w)
		}
		if int(b%2) != idx%2 {
			return nil, fmt.Errorf("word %v is not valid at position %v, "+
				"check for missing words", w, idx)
		}
		decoded[idx] = byte(b / 2)
		idx++
	}
	return decoded[:idx], nil
}
