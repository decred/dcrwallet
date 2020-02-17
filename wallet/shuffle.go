// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"crypto/rand"
	"sync"

	"decred.org/dcrwallet/internal/uniformprng"
	"decred.org/dcrwallet/wallet/txauthor"
)

var shuffleRand *uniformprng.Source
var shuffleMu sync.Mutex

func init() {
	var err error
	shuffleRand, err = uniformprng.RandSource(rand.Reader)
	if err != nil {
		panic(err)
	}
}

func shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("shuffle: negative n")
	}
	if int64(n) >= 1<<32 {
		panic("shuffle: large n")
	}

	defer shuffleMu.Unlock()
	shuffleMu.Lock()

	// Fisher-Yates shuffle: https://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle
	for i := uint32(0); i < uint32(n); i++ {
		j := shuffleRand.Uint32n(uint32(n)-i) + i
		swap(int(i), int(j))
	}
}

func shuffleUTXOs(u *txauthor.InputDetail) {
	shuffle(len(u.Inputs), func(i, j int) {
		u.Inputs[i], u.Inputs[j] = u.Inputs[j], u.Inputs[i]
		u.Scripts[i], u.Scripts[j] = u.Scripts[j], u.Scripts[i]
		u.RedeemScriptSizes[i], u.RedeemScriptSizes[j] = u.RedeemScriptSizes[j], u.RedeemScriptSizes[i]
	})
}
