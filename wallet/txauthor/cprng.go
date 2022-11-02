// Copyright (c) 2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package txauthor

import (
	"crypto/rand"
	"sync"

	"decred.org/dcrwallet/v3/internal/uniformprng"
)

// cprng is a cryptographically random-seeded prng.  It is seeded during package
// init.  Any initialization errors result in panics.  It is safe for concurrent
// access.
var cprng = cprngType{}

type cprngType struct {
	r  *uniformprng.Source
	mu sync.Mutex
}

func init() {
	r, err := uniformprng.RandSource(rand.Reader)
	if err != nil {
		panic("Failed to seed prng: " + err.Error())
	}
	cprng.r = r
}

func (c *cprngType) Int31n(n int32) int32 {
	defer c.mu.Unlock()
	c.mu.Lock()

	if n <= 0 {
		panic("Int31n: non-positive n")
	}
	return int32(c.r.Uint32n(uint32(n)))
}
