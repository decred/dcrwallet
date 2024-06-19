// Copyright (c) 2019-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wallet

import "github.com/decred/dcrd/crypto/rand"

// Shuffle cryptographically shuffles a total of n items.
func Shuffle(n int, swap func(i, j int)) {
	rand.Shuffle(n, swap)
}
