// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import (
	"crypto/rand"
	"encoding/binary"
	mathrand "math/rand"
	"sync"

	"github.com/decred/dcrwallet/wallet/v3/txauthor"
)

var shuffleRand *mathrand.Rand
var shuffleMu sync.Mutex

func init() {
	buf := make([]byte, 8)
	_, err := rand.Read(buf)
	if err != nil {
		panic(err)
	}
	seed := int64(binary.LittleEndian.Uint64(buf))
	shuffleRand = mathrand.New(mathrand.NewSource(seed))
}

func shuffle(n int, swap func(i, j int)) {
	shuffleMu.Lock()
	shuffleRand.Shuffle(n, swap)
	shuffleMu.Unlock()
}

func shuffleUTXOs(u *txauthor.InputDetail) {
	shuffle(len(u.Inputs), func(i, j int) {
		u.Inputs[i], u.Inputs[j] = u.Inputs[j], u.Inputs[i]
		u.Scripts[i], u.Scripts[j] = u.Scripts[j], u.Scripts[i]
		u.RedeemScriptSizes[i], u.RedeemScriptSizes[j] = u.RedeemScriptSizes[j], u.RedeemScriptSizes[i]
	})
}
