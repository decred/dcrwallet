// Copyright (c) 2018-2024 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package lru

import (
	"container/list"
	"sync"
)

type mapListValue[K comparable, V any] struct {
	k K
	v V
}

// Map implements a least-recently-updated (LRU) map with nearly O(1) lookups
// and inserts.  Items are added to the cache up to a limit, at which point
// further additions will evict the least recently added item.  The zero value
// is not valid and Maps must be created with NewMap.  All Map methods are
// concurrent safe.
type Map[K comparable, V any] struct {
	mu    sync.Mutex
	m     map[K]*list.Element
	list  *list.List
	limit int
}

// NewMap creates an initialized and empty LRU map.
func NewMap[K comparable, V any](limit int) Map[K, V] {
	return Map[K, V]{
		m:     make(map[K]*list.Element, limit),
		list:  list.New(),
		limit: limit,
	}
}

// Add adds an item to the LRU map, removing the oldest item if the new item
// exceeds the total map capacity and is not already a member, or marking the
// already-present item as the most recently added item.
func (m *Map[K, V]) Add(key K, value V) {
	defer m.mu.Unlock()
	m.mu.Lock()

	// Move this item to front of list if already present
	elem, ok := m.m[key]
	if ok {
		m.list.MoveToFront(elem)
		return
	}

	// If necessary, make room by popping an item off from the back
	if len(m.m) > m.limit {
		elem := m.list.Back()
		if elem != nil {
			lv := m.list.Remove(elem)
			delete(m.m, lv.(*mapListValue[K, V]).k)
		}
	}

	// Add new item to the LRU
	lv := &mapListValue[K, V]{k: key, v: value}
	elem = m.list.PushFront(lv)
	m.m[key] = elem
}

// Get fetches the value from the LRU map, prioritizing it to evict it last
// after all other items.  The second return value is true if the value was
// present, and false otherwise.
func (m *Map[K, V]) Get(key K) (V, bool) {
	defer m.mu.Unlock()
	m.mu.Lock()

	elem, ok := m.m[key]
	if ok {
		m.list.MoveToFront(elem)
		return elem.Value.(*mapListValue[K, V]).v, true
	}

	var zero V
	return zero, false
}

// Hit marks the value under a key as recently used and prioritizes it to
// evict it last after all other items.  Returns true if the value was
// present, and false otherwise.
//
// Hit is equivalent to Get but discards the value.
func (m *Map[K, V]) Hit(key K) bool {
	_, ok := m.Get(key)
	return ok
}

// Contains checks whether key is a member of the LRU map.  It does modify the
// priority of any items.
func (m *Map[K, V]) Contains(key K) bool {
	m.mu.Lock()
	_, ok := m.m[key]
	m.mu.Unlock()
	return ok
}
