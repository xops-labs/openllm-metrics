// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package reconciler joins runtime cost estimates against reconciled
// (FOCUS) billing events on the correlation key
// (tenant, provider, model, period_start, period_end) and computes drift.
//
// The reconciler is the runtime-side companion to the FOCUS ingester. The
// FOCUS ingester emits one llm.usage.reconciled event per (period, account)
// tuple; the cost-mapper accumulates runtime estimates into the same
// time-bucket and writes a drift row to control_plane.cost_reconciliation_drift
// whenever a matching pair exists.
//
// Drift math is intentionally trivial — this is OSS, not a scoring engine:
//
//	drift_minor = reconciled - estimated
//	drift_ratio = drift_minor / max(reconciled, 1)
//
// Anything richer than this lives behind the F025 cost-efficiency boundary
// and is OSS-deferred.
package reconciler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/store"
)

// CorrelationKey identifies a single drift row. period_start / period_end
// are truncated to whole RFC3339 seconds before being formatted so the
// estimate side and the reconciled side bucket to the same string.
type CorrelationKey struct {
	TenantID    string
	Provider    string
	Model       string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// String renders the key in the deterministic format also written to the
// control_plane.cost_reconciliation_drift.correlation_key column.
func (k CorrelationKey) String() string {
	return fmt.Sprintf("%s:%s:%s:%d:%d",
		k.TenantID,
		canonical(k.Provider),
		canonical(k.Model),
		k.PeriodStart.UTC().Unix(),
		k.PeriodEnd.UTC().Unix(),
	)
}

// Estimate carries the runtime-side cost contribution for a single event.
// Many Estimates roll up into one CorrelationKey bucket.
type Estimate struct {
	Key                CorrelationKey
	Team               string
	App                string
	Env                string
	Project            string
	EstimatedCostMinor int64
	CatalogVersion     string
}

// Reconciled carries the billing-side cost from a single FOCUS-derived
// llm.usage.reconciled event.
type Reconciled struct {
	Key                 CorrelationKey
	ReconciledCostMinor int64
}

// Reconciler accumulates Estimates per bucket and writes drift rows when a
// matching Reconciled event arrives. It is safe for concurrent use.
type Reconciler struct {
	store store.DriftStore

	mu      sync.Mutex
	buckets map[string]*bucket // key = CorrelationKey.String()
}

type bucket struct {
	team           string
	app            string
	env            string
	project        string
	estimatedMinor int64
	catalogVersion string
}

// New constructs a Reconciler that writes drift rows to the supplied store.
func New(s store.DriftStore) *Reconciler {
	return &Reconciler{
		store:   s,
		buckets: make(map[string]*bucket, 64),
	}
}

// RecordEstimate accumulates a single runtime estimate into its bucket. The
// bucket is created on first sight and updated with each subsequent event
// for the same correlation key. No I/O occurs here.
func (r *Reconciler) RecordEstimate(e Estimate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := e.Key.String()
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{
			team:           e.Team,
			app:            e.App,
			env:            e.Env,
			project:        e.Project,
			catalogVersion: e.CatalogVersion,
		}
		r.buckets[key] = b
	}
	b.estimatedMinor += e.EstimatedCostMinor
	// Keep the latest catalog version stamp on the bucket.
	if e.CatalogVersion != "" {
		b.catalogVersion = e.CatalogVersion
	}
}

// ApplyReconciled joins the supplied Reconciled event with the accumulated
// estimate for the same correlation key and upserts one drift row. The
// estimate side is NOT zeroed out — late-arriving runtime events for the
// same bucket can produce an updated drift row on the next ApplyReconciled
// for that bucket (idempotent upsert via the UNIQUE constraint).
func (r *Reconciler) ApplyReconciled(ctx context.Context, rec Reconciled) error {
	r.mu.Lock()
	key := rec.Key.String()
	b := r.buckets[key]
	r.mu.Unlock()

	var (
		team, app, env, project, catalogVersion string
		estimated                               int64
	)
	if b != nil {
		team, app, env, project = b.team, b.app, b.env, b.project
		catalogVersion = b.catalogVersion
		estimated = b.estimatedMinor
	}

	driftMinor := rec.ReconciledCostMinor - estimated
	ratioDen := rec.ReconciledCostMinor
	if ratioDen < 1 {
		ratioDen = 1
	}
	driftRatio := float64(driftMinor) / float64(ratioDen)

	row := store.DriftRow{
		TenantID:                 rec.Key.TenantID,
		Team:                     team,
		App:                      app,
		Env:                      env,
		Project:                  project,
		Provider:                 canonical(rec.Key.Provider),
		Model:                    canonical(rec.Key.Model),
		PeriodStart:              rec.Key.PeriodStart.UTC(),
		PeriodEnd:                rec.Key.PeriodEnd.UTC(),
		EstimatedCostMinorUnits:  nonNegative(estimated),
		ReconciledCostMinorUnits: nonNegative(rec.ReconciledCostMinor),
		DriftMinorUnits:          driftMinor,
		DriftRatio:               driftRatio,
		CatalogVersion:           catalogVersion,
		CorrelationKey:           key,
	}
	if err := r.store.UpsertDrift(ctx, row); err != nil {
		return fmt.Errorf("reconciler: upsert drift %s: %w", key, err)
	}
	return nil
}

func canonical(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// nonNegative is a NOT NULL CHECK guard — Postgres rejects negatives on the
// minor-units columns; we floor to zero on the way in.
func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
