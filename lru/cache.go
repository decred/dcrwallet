// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package lru

import (
	"container/list"
	"sync"
)

// Cache implements a least-recently-updated (LRU) cache with nearly O(1)
// lookups and inserts.  Items are added to the cache up to a limit, at which
// point further additions will evict the least recently added item.  The zero
// value is not valid and Caches must be created with NewCache.  All Cache
// methods are concurrent safe.
type Cache struct {
	mu    sync.Mutex
	m     map[interface{}]*list.Element
	list  *list.List
	limit int
}

// NewCache creates an initialized and empty LRU cache.
func NewCache(limit int) Cache {
	return Cache{
		m:     make(map[interface{}]*list.Element, limit),
		list:  list.New(),
		limit: limit,
	}
}

// Add adds an item to the LRU cache, removing the oldest item if the new item
// is not already a member, or marking item as the most recently added item if
// it is already present.
func (c *Cache) Add(item interface{}) {
	defer c.mu.Unlock()
	c.mu.Lock()

	// Move this item to front of list if already present
	elem, ok := c.m[item]
	if ok {
		c.list.MoveToFront(elem)
		return
	}

	// If necessary, make room by popping an item off from the back
	if len(c.m) > c.limit {
		elem := c.list.Back()
		if elem != nil {
			v := c.list.Remove(elem)
			delete(c.m, v)
		}
	}

	// Add new item to the LRU
	elem = c.list.PushFront(item)
	c.m[item] = elem
}

// Contains checks whether v is a member of the LRU cache.
func (c *Cache) Contains(v interface{}) bool {
	c.mu.Lock()
	_, ok := c.m[v]
	c.mu.Unlock()
	return ok
}
