// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package rpctest

import (
	"fmt"
	"sync"
)

// Default harness name
const MainHarnessName = "main"

// HarnessesPool keeps track of reusable harness instances.
// Multiple Harness instances may be run concurrently, to allow for testing
// complex scenarios involving multiple nodes.
type HarnessesPool struct {
	cache                    map[string]*Harness
	registryAccessController sync.RWMutex
	spawner                  HarnessSpawner
}

func NewHarnessesPool(spawner HarnessSpawner) *HarnessesPool {
	return &HarnessesPool{
		cache:   make(map[string]*Harness),
		spawner: spawner,
	}
}

// HarnessSpawner manages new harness instance creation and disposal
type HarnessSpawner interface {
	// NewInstance must return a new freshly created Harness instance
	NewInstance(harnessName string) *Harness

	// NameForTag defines a policy for mapping input tags to harness names
	NameForTag(tag string) string

	// Dispose should take care of harness instance disposal
	Dispose(harnessToDispose *Harness) error
}

// ObtainHarness returns reusable Harness instance upon request,
// creates a new instance when required and stores it in the cache
// for the following calls
func (pool *HarnessesPool) ObtainHarness(tag string) *Harness {

	// Resolve harness name for the tag requested
	// HarnessesPool uses harnessName as a key to cache a harness instance
	harnessName := pool.spawner.NameForTag(tag)

	harness := pool.cache[harnessName]

	// Create and cache a new instance when not present in cache
	if harness == nil {
		harness = pool.spawner.NewInstance(harnessName)
		pool.cache[harnessName] = harness
	}

	return harness
}

// ObtainHarnessConcurrentSafe is safe for concurrent access.
func (pool *HarnessesPool) ObtainHarnessConcurrentSafe(tag string) *Harness {
	pool.registryAccessController.Lock()
	defer pool.registryAccessController.Unlock()
	return pool.ObtainHarness(tag)
}

// TearDownAll disposes all instances in the cache
func (pool *HarnessesPool) TearDownAll() {
	for key, harness := range pool.cache {
		err := pool.spawner.Dispose(harness)
		delete(pool.cache, key)
		if err != nil {
			fmt.Printf("Failed to dispose harness <%v>: %v", key, err)
		}
	}
}

// InitTags ensures the cache will immediately resolve tags from the given list
func (pool *HarnessesPool) InitTags(tags []string) {
	for _, tag := range tags {
		pool.ObtainHarness(tag)
	}
}

func (pool *HarnessesPool) Size() int {
	return len(pool.cache)
}
