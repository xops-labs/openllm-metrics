// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package model holds the rolling per-(tenant, provider, model, region)
// quota state.
//
// # Design notes
//
//  1. The model is signal-only. It does NOT decide anything. Routing,
//     fallback, throttling, and budget enforcement are explicitly out of
//     scope and live in this repository (F034).
//  2. State is purely in-memory and rebuildable from a bus replay. The
//     worker can crash and resume; the next inbound event repopulates the
//     relevant key. There is no persistence layer.
//  3. The risk score is intentionally simple and transparent:
//     risk = min(1.0, usedRatio * 1.25)
//     The 1.25 multiplier is a LINEAR shaping factor that makes the gauge
//     cross 1.0 a bit before the pool actually hits zero, so dashboards
//     and alerts have a small headroom. This is intentionally NOT a
//     opaque score; the formula is published in code and README.
//  4. Multi-tenant from day one: every key carries tenant.
package model

import (
	"math"
	"sort"
	"sync"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/parser"
)

// Kind enumerates the two quota pools modelled per key.
type Kind string

const (
	// KindTokens is the per-window tokens quota.
	KindTokens Kind = "tokens"
	// KindRequests is the per-window request-count quota.
	KindRequests Kind = "requests"
)

// Key uniquely identifies one rolling-state row.
type Key struct {
	Tenant   string
	Provider string
	Model    string
	Region   string
}

// State is the latest observation for one (key, kind) pair plus the wall
// time it was last refreshed.
type State struct {
	Kind       Kind
	Remaining  int64
	Limit      int64
	ResetAfter time.Duration
	UpdatedAt  time.Time
}

// UsedRatio returns 1 - (remaining/limit), clipped to [0,1]. When Limit is
// zero (provider did not publish it) we return 0 and the caller should
// skip the risk gauge for this kind. The bool tells the caller whether the
// value is meaningful.
func (s State) UsedRatio() (float64, bool) {
	if s.Limit <= 0 {
		return 0, false
	}
	r := 1.0 - float64(s.Remaining)/float64(s.Limit)
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}
	return r, true
}

// SecondsToReset returns ResetAfter as a non-negative float of seconds.
// Returns 0 when ResetAfter is zero (unknown).
func (s State) SecondsToReset() float64 {
	if s.ResetAfter <= 0 {
		return 0
	}
	return s.ResetAfter.Seconds()
}

// RiskScore returns the linear-shaped risk gauge in [0,1]. The score is
//
//	min(1.0, usedRatio * 1.25)
//
// The 1.25 multiplier crosses 1.0 at usedRatio=0.8, giving the operator
// 20% headroom before "fully saturated". Returns false in the second
// return when Limit is unknown (no denominator).
func (s State) RiskScore() (float64, bool) {
	used, ok := s.UsedRatio()
	if !ok {
		return 0, false
	}
	r := used * 1.25
	if r > 1.0 {
		r = 1.0
	}
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 0, false
	}
	return r, true
}

// Model is the concurrent-safe rolling-state container.
type Model struct {
	mu     sync.RWMutex
	window time.Duration
	rows   map[Key]map[Kind]State

	now func() time.Time
}

// New constructs a Model with the supplied retention window. Observations
// older than `window` are dropped from snapshots so dashboards don't show
// stale saturation forever after a quiet period.
func New(window time.Duration) *Model {
	return &Model{
		window: window,
		rows:   make(map[Key]map[Kind]State, 32),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Observe records a Signal under (key). Missing pools are ignored: the
// previous observation for that pool, if any, is retained until it ages
// out of the window.
func (m *Model) Observe(key Key, sig parser.Signal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	bucket, ok := m.rows[key]
	if !ok {
		bucket = make(map[Kind]State, 2)
		m.rows[key] = bucket
	}
	if sig.HasTokens {
		bucket[KindTokens] = State{
			Kind:       KindTokens,
			Remaining:  sig.TokensRemaining,
			Limit:      sig.TokensLimit,
			ResetAfter: sig.ResetAfter,
			UpdatedAt:  now,
		}
	}
	if sig.HasRequests {
		bucket[KindRequests] = State{
			Kind:       KindRequests,
			Remaining:  sig.RequestsRemaining,
			Limit:      sig.RequestsLimit,
			ResetAfter: sig.ResetAfter,
			UpdatedAt:  now,
		}
	}
}

// Row is one (key, kind, state) tuple as returned by Snapshot.
type Row struct {
	Key   Key
	State State
}

// Snapshot returns every non-stale (key, kind) pair in a stable order:
// tenant, then provider, then model, then region, then kind. Stale entries
// (UpdatedAt older than the retention window) are excluded.
func (m *Model) Snapshot() []Row {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cutoff := m.now().Add(-m.window)
	out := make([]Row, 0, len(m.rows)*2)
	for k, bucket := range m.rows {
		for _, s := range bucket {
			if s.UpdatedAt.Before(cutoff) {
				continue
			}
			out = append(out, Row{Key: k, State: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Key.Tenant != b.Key.Tenant {
			return a.Key.Tenant < b.Key.Tenant
		}
		if a.Key.Provider != b.Key.Provider {
			return a.Key.Provider < b.Key.Provider
		}
		if a.Key.Model != b.Key.Model {
			return a.Key.Model < b.Key.Model
		}
		if a.Key.Region != b.Key.Region {
			return a.Key.Region < b.Key.Region
		}
		return a.State.Kind < b.State.Kind
	})
	return out
}
