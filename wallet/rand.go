// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"crypto/rand"
	"sync"

	"decred.org/dcrwallet/v3/internal/uniformprng"
)

var prng *uniformprng.Source
var prngMu sync.Mutex

func init() {
	var err error
	prng, err = uniformprng.RandSource(rand.Reader)
	if err != nil {
		panic(err)
	}
}

func randInt63n(n int64) int64 {
	defer prngMu.Unlock()
	prngMu.Lock()
	return prng.Int63n(n)
}

func shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("shuffle: negative n")
	}
	if int64(n) >= 1<<32 {
		panic("shuffle: large n")
	}

	defer prngMu.Unlock()
	prngMu.Lock()

	// Fisher-Yates shuffle: https://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle
	for i := uint32(0); i < uint32(n); i++ {
		j := prng.Uint32n(uint32(n)-i) + i
		swap(int(i), int(j))
	}
}
