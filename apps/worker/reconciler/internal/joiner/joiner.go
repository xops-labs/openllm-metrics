// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package joiner correlates runtime cost estimates (cost.estimated.v1 from
// cost-mapper, source = gateway | sdk) with vendor-reconciled cost
// (llm.usage.reconciled from focus-ingester, source = exporter) on a
// windowed key (tenant, provider, model, window-start).
//
// Each side contributes to an in-memory bucket. Whenever a contribution
// lands, the joiner upserts the current bucket state to
// control_plane.reconciliation_results so the row is queryable in real
// time and survives a worker restart (downstream of restart, the closer
// finishes the lifecycle from Postgres state).
//
// Drift math (deliberately trivial — anything richer is OSS-deferred):
//
//	drift_usd   = reconciled_cost_usd - estimated_cost_usd
//	drift_ratio = drift_usd / max(estimated_cost_usd, 0.0001)
//
// The denominator floor avoids divide-by-zero for the (relatively common)
// case where reconciled cost arrives before any runtime estimate has been
// recorded for the same bucket.
package joiner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/store"
)

// minDenominator floors the drift_ratio denominator so a 0-estimate
// window does not produce ±Inf. Same constant referenced in the F023
// migration comment and reconciliation.md.
const minDenominator = 0.0001

// WindowKey identifies one correlation bucket. WindowStart is truncated to
// the configured window size before bucketing so both sides agree on the
// bucket regardless of the exact second of the contributing event.
type WindowKey struct {
	TenantID    string
	Provider    string
	Model       string
	WindowStart time.Time
	WindowEnd   time.Time
}

// String renders the key in a stable, log-friendly form.
func (k WindowKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%d-%d",
		k.TenantID,
		canonical(k.Provider),
		canonical(k.Model),
		k.WindowStart.UTC().Unix(),
		k.WindowEnd.UTC().Unix(),
	)
}

// Estimate is the runtime-side contribution from a single cost.estimated
// event (cost-mapper output). Many Estimates roll up into one WindowKey.
type Estimate struct {
	Key              WindowKey
	Team             string
	App              string
	Env              string
	Project          string
	EstimatedCostUSD float64
}

// Reconciled is the vendor-side contribution from a single
// llm.usage.reconciled event (focus-ingester output). A single FOCUS line
// item may already cover the full window in one shot — but we still treat
// the per-window total as a sum so partial vendor breakdowns merge cleanly.
type Reconciled struct {
	Key               WindowKey
	Team              string
	App               string
	Env               string
	Project           string
	ReconciledCostUSD float64
}

// bucket carries the running totals for one WindowKey. Labels are recorded
// from whichever side arrived first; runtime context typically arrives
// before vendor context so the gateway/SDK side usually wins for team/app
// — vendor data rarely carries app-level context at all.
type bucket struct {
	team              string
	app               string
	env               string
	project           string
	estimatedCostUSD  float64
	reconciledCostUSD float64
	estimateSeen      bool
	reconciledSeen    bool
}

// Joiner is safe for concurrent use.
type Joiner struct {
	store      store.Store
	windowSize time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket // key = WindowKey.String()
}

// New constructs a Joiner that writes to the supplied store.
func New(s store.Store, windowSize time.Duration) *Joiner {
	if windowSize <= 0 {
		windowSize = time.Hour
	}
	return &Joiner{
		store:      s,
		windowSize: windowSize,
		buckets:    make(map[string]*bucket, 64),
	}
}

// WindowSize exposes the configured window length so consumers can derive
// (WindowStart, WindowEnd) from an event's recorded_at.
func (j *Joiner) WindowSize() time.Duration { return j.windowSize }

// Bucket returns the (start, end) tuple for the supplied event time. The
// start is truncated to the window size so two events for the same hour
// always land in the same bucket.
func (j *Joiner) Bucket(t time.Time) (time.Time, time.Time) {
	start := t.UTC().Truncate(j.windowSize)
	return start, start.Add(j.windowSize)
}

// RecordEstimate folds a runtime estimate into its bucket and persists the
// running total to Postgres. The persisted row stays in status='open'
// until the closer processes it.
func (j *Joiner) RecordEstimate(ctx context.Context, e Estimate) error {
	j.mu.Lock()
	b := j.bucketLocked(e.Key)
	b.estimateSeen = true
	b.estimatedCostUSD += e.EstimatedCostUSD
	// Runtime-side labels are richer; only adopt them if we haven't yet.
	if b.team == "" {
		b.team = e.Team
	}
	if b.app == "" {
		b.app = e.App
	}
	if b.env == "" {
		b.env = e.Env
	}
	if b.project == "" {
		b.project = e.Project
	}
	snap := j.snapshotLocked(e.Key, b)
	j.mu.Unlock()
	return j.persist(ctx, snap)
}

// RecordReconciled folds a vendor-reconciled cost into its bucket and
// persists the running total to Postgres.
func (j *Joiner) RecordReconciled(ctx context.Context, r Reconciled) error {
	j.mu.Lock()
	b := j.bucketLocked(r.Key)
	b.reconciledSeen = true
	b.reconciledCostUSD += r.ReconciledCostUSD
	// FOCUS rarely carries app-level context; only adopt vendor labels if
	// the bucket has nothing better yet.
	if b.team == "" {
		b.team = r.Team
	}
	if b.app == "" {
		b.app = r.App
	}
	if b.env == "" {
		b.env = r.Env
	}
	if b.project == "" {
		b.project = r.Project
	}
	snap := j.snapshotLocked(r.Key, b)
	j.mu.Unlock()
	return j.persist(ctx, snap)
}

// Forget drops the in-memory bucket for the supplied key. The closer calls
// this once a row has been flipped to a terminal status so we do not leak
// memory across long-running processes.
func (j *Joiner) Forget(key WindowKey) {
	j.mu.Lock()
	delete(j.buckets, key.String())
	j.mu.Unlock()
}

func (j *Joiner) bucketLocked(key WindowKey) *bucket {
	k := key.String()
	b, ok := j.buckets[k]
	if !ok {
		b = &bucket{}
		j.buckets[k] = b
	}
	return b
}

func (j *Joiner) snapshotLocked(key WindowKey, b *bucket) store.Row {
	drift := b.reconciledCostUSD - b.estimatedCostUSD
	denom := b.estimatedCostUSD
	if denom < minDenominator {
		denom = minDenominator
	}
	ratio := drift / denom
	return store.Row{
		TenantID:          key.TenantID,
		Team:              b.team,
		App:               b.app,
		Env:               b.env,
		Project:           b.project,
		Provider:          canonical(key.Provider),
		Model:             canonical(key.Model),
		WindowStart:       key.WindowStart.UTC(),
		WindowEnd:         key.WindowEnd.UTC(),
		EstimatedCostUSD:  b.estimatedCostUSD,
		ReconciledCostUSD: b.reconciledCostUSD,
		DriftUSD:          drift,
		DriftRatio:        ratio,
		Status:            store.StatusOpen,
	}
}

func (j *Joiner) persist(ctx context.Context, row store.Row) error {
	if err := j.store.UpsertResult(ctx, row); err != nil {
		return fmt.Errorf("joiner: persist %s/%s/%s/%s: %w",
			row.TenantID, row.Provider, row.Model, row.WindowStart.Format(time.RFC3339), err)
	}
	return nil
}

func canonical(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
