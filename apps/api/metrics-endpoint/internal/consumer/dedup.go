// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package consumer

import (
	"container/list"
	"sync"
)

// LRUDedup is a goroutine-safe, fixed-capacity LRU dedup set keyed by
// string. First Seen() of a key returns true (the caller should process the
// event); subsequent Seen() calls return false (caller should drop). On
// capacity overflow the oldest entry is evicted.
//
// This is the single-replica dedup. A multi-replica deployment swaps in a
// shared (Redis / Postgres) implementation behind the same Dedup interface.
type LRUDedup struct {
	mu       sync.Mutex
	capacity int
	order    *list.List
	index    map[string]*list.Element
}

// NewLRUDedup constructs a fresh LRU with the given capacity. Capacity <= 0
// is clamped to 1 to guarantee at least one slot.
func NewLRUDedup(capacity int) *LRUDedup {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRUDedup{
		capacity: capacity,
		order:    list.New(),
		index:    make(map[string]*list.Element, capacity),
	}
}

// Seen reports whether key is being observed for the first time. Returns
// true on the first observation (and inserts the key); returns false on any
// subsequent observation until eviction.
func (l *LRUDedup) Seen(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, ok := l.index[key]; ok {
		l.order.MoveToFront(el)
		return false
	}
	el := l.order.PushFront(key)
	l.index[key] = el
	if l.order.Len() > l.capacity {
		back := l.order.Back()
		if back != nil {
			delete(l.index, back.Value.(string))
			l.order.Remove(back)
		}
	}
	return true
}

// Len returns the current number of stored keys. For tests.
func (l *LRUDedup) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}
