// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package dedup_test

import (
	"strconv"
	"sync"
	"testing"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/dedup"
)

func sampleKey(model string) dedup.Key {
	return dedup.Key{
		Provider:    "openai",
		WindowStart: "2026-05-17T10:00:00Z",
		WindowEnd:   "2026-05-17T10:05:00Z",
		Model:       model,
		App:         "snapcal",
		Team:        "ai-platform",
		Project:     "snapcal-prod",
	}
}

func TestLRU_FirstSeenReturnsTrueOnceOnly(t *testing.T) {
	t.Parallel()
	l := dedup.NewLRU(8)
	k := sampleKey("gpt-4o")
	if !l.Seen(k) {
		t.Fatal("first Seen() must return true (caller should emit)")
	}
	if l.Seen(k) {
		t.Fatal("second Seen() must return false (caller should drop)")
	}
}

func TestLRU_DistinctKeysAreSeparate(t *testing.T) {
	t.Parallel()
	l := dedup.NewLRU(8)
	a := sampleKey("gpt-4o")
	b := sampleKey("gpt-4o-mini")
	if !l.Seen(a) {
		t.Fatal("a first time")
	}
	if !l.Seen(b) {
		t.Fatal("b first time")
	}
	if l.Len() != 2 {
		t.Fatalf("len=%d want 2", l.Len())
	}
}

func TestLRU_EvictsOldestWhenOverCapacity(t *testing.T) {
	t.Parallel()
	l := dedup.NewLRU(2)
	a := sampleKey("a")
	b := sampleKey("b")
	c := sampleKey("c")
	l.Seen(a)
	l.Seen(b)
	l.Seen(c) // evicts a (oldest)
	if l.Len() != 2 {
		t.Fatalf("len=%d want 2", l.Len())
	}
	// a should be re-emittable now (was evicted).
	if !l.Seen(a) {
		t.Fatal("a should be evictable and re-seen as new")
	}
}

func TestLRU_NilReceiverAlwaysEmits(t *testing.T) {
	t.Parallel()
	var l *dedup.LRU
	if !l.Seen(sampleKey("anything")) {
		t.Fatal("nil receiver must always return true")
	}
}

func TestLRU_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	l := dedup.NewLRU(1024)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				k := sampleKey(strconv.Itoa(i) + "-" + strconv.Itoa(j))
				l.Seen(k)
				l.Seen(k) // hit the dedup path concurrently
			}
		}(i)
	}
	wg.Wait()
	if l.Len() == 0 {
		t.Fatal("expected entries after concurrent run")
	}
}
