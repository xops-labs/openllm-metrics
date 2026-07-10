// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package dedup implements an in-memory LRU dedup cache for OpenAI usage
// windows.
//
// Idempotency key per F009 §9 is the tuple:
//
//	(provider, window_start, window_end, model, app, team, project)
//
// Replaying the same window must produce ZERO additional bus events. The
// cache is intentionally small and in-memory: F009 deploys a single replica
// per provider per env, for which an in-memory LRU is sufficient.
package dedup

import (
	"container/list"
	"strings"
	"sync"
)

// Key is the canonical idempotency tuple. Keep the fields exactly aligned
// with F009 §9 so the spec, the code, and the README stay legible together.
type Key struct {
	Provider    string
	WindowStart string // RFC3339
	WindowEnd   string // RFC3339
	Model       string
	App         string
	Team        string
	Project     string
}

// String returns the canonical stable string form used as the LRU key.
// Field order matches the F009 spec to avoid accidental drift.
func (k Key) String() string {
	var b strings.Builder
	b.Grow(len(k.Provider) + len(k.WindowStart) + len(k.WindowEnd) + len(k.Model) + len(k.App) + len(k.Team) + len(k.Project) + 7)
	b.WriteString(k.Provider)
	b.WriteByte('|')
	b.WriteString(k.WindowStart)
	b.WriteByte('|')
	b.WriteString(k.WindowEnd)
	b.WriteByte('|')
	b.WriteString(k.Model)
	b.WriteByte('|')
	b.WriteString(k.App)
	b.WriteByte('|')
	b.WriteString(k.Team)
	b.WriteByte('|')
	b.WriteString(k.Project)
	return b.String()
}

// LRU is a goroutine-safe, fixed-capacity LRU dedup set.
//
// Seen() returns true the FIRST time a key is observed and false thereafter
// (until eviction). This matches "first wins" idempotent emit semantics:
// returning false short-circuits the producer.
type LRU struct {
	mu       sync.Mutex
	capacity int
	order    *list.List
	index    map[string]*list.Element
}

// NewLRU constructs an empty LRU with the given capacity. A capacity <= 0 is
// treated as 1 to guarantee at least one slot.
func NewLRU(capacity int) *LRU {
	if capacity <= 0 {
		capacity = 1
	}
	return &LRU{
		capacity: capacity,
		order:    list.New(),
		index:    make(map[string]*list.Element, capacity),
	}
}

// Seen records the supplied key and returns true if this is the first time
// the key was observed (i.e. the caller SHOULD emit). Returns false if the
// key was already in the cache (i.e. the caller MUST drop the event).
//
// A nil receiver is treated as a permanently-empty cache and always returns
// true. This makes it trivial to bypass dedup in tests when desired.
func (l *LRU) Seen(k Key) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	s := k.String()
	if el, ok := l.index[s]; ok {
		// Already seen — refresh recency, signal duplicate.
		l.order.MoveToFront(el)
		return false
	}
	// New key. Insert at front and evict from back if over capacity.
	el := l.order.PushFront(s)
	l.index[s] = el
	if l.order.Len() > l.capacity {
		back := l.order.Back()
		if back != nil {
			delete(l.index, back.Value.(string))
			l.order.Remove(back)
		}
	}
	return true
}

// Len returns the current number of stored keys. Primarily for tests.
func (l *LRU) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.order.Len()
}
