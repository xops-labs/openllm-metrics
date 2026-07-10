// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package consumer

import "testing"

func TestLRUDedup_FirstSeenReturnsTrue(t *testing.T) {
	d := NewLRUDedup(4)
	if !d.Seen("a") {
		t.Errorf("Seen(a) first call = false, want true")
	}
	if d.Seen("a") {
		t.Errorf("Seen(a) second call = true, want false")
	}
}

func TestLRUDedup_EvictsOldestOnOverflow(t *testing.T) {
	d := NewLRUDedup(2)
	d.Seen("a")
	d.Seen("b")
	d.Seen("c") // evicts "a"
	if !d.Seen("a") {
		t.Errorf("after eviction, Seen(a) should return true again")
	}
}

func TestLRUDedup_ZeroCapacityIsClampedToOne(t *testing.T) {
	d := NewLRUDedup(0)
	d.Seen("a")
	if d.Len() != 1 {
		t.Errorf("Len=%d, want 1", d.Len())
	}
}
