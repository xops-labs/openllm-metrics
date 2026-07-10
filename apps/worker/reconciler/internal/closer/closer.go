// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package closer scans control_plane.reconciliation_results on a slow
// cadence for windows whose grace period has elapsed, flips their status
// out of 'open' → ('reconciled' | 'unreconciled'), emits a
// reconciliation.window.v1 event for each closed window, and updates the
// Prometheus close-out gauges.
//
// The grace period is a separate knob from the window size because
// provider billing lags — FOCUS data for hour H may not show up in the
// upstream exporter until many hours (or days) later. 48 hours is a safe
// default; tighten it only after measuring the upstream's specific lag.
//
// What the closer does NOT do (deliberate):
//   - decide to fall back, downgrade, or block traffic on drift
//   - feed scoring weights
//   - mutate budget enforcement
//
// Those live in F034 / F035 decisioning or in F033
// (notifications, which subscribe to the reconciliation.window topic).
package closer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/joiner"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/store"
)

// MetricsRecorder is the narrow gauge/counter surface the closer touches.
type MetricsRecorder interface {
	ObserveWindowClose(row store.Row)
}

// Emitter is the bus surface the closer publishes window-close events to.
type Emitter interface {
	EmitWindowClosed(ctx context.Context, row store.Row) error
}

// BucketForgetter is implemented by the joiner so the closer can drop the
// in-memory bucket for a window once it has reached a terminal status.
type BucketForgetter interface {
	Forget(key joiner.WindowKey)
}

// Closer scans Postgres for windows past their grace horizon and closes them.
type Closer struct {
	store     store.Store
	emitter   Emitter
	metrics   MetricsRecorder
	forgetter BucketForgetter
	grace     time.Duration
	batch     int
	logger    *slog.Logger
	now       func() time.Time
}

// Config bundles the closer's tunables.
type Config struct {
	Grace     time.Duration
	BatchSize int
}

// New constructs a Closer.
func New(s store.Store, e Emitter, m MetricsRecorder, f BucketForgetter, cfg Config, logger *slog.Logger) *Closer {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 256
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Closer{
		store:     s,
		emitter:   e,
		metrics:   m,
		forgetter: f,
		grace:     cfg.Grace,
		batch:     cfg.BatchSize,
		logger:    logger,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

// Result reports per-cycle counts.
type Result struct {
	Scanned      int
	Reconciled   int
	Unreconciled int
	Errors       int
}

// RunOnce executes one close-out scan: find every 'open' row whose
// window_end + grace ≤ now, flip its status, emit a window-close event,
// and update self-observability gauges.
func (c *Closer) RunOnce(ctx context.Context) (Result, error) {
	cutoff := c.now().Add(-c.grace)
	rows, err := c.store.ScanClosable(ctx, cutoff, c.batch)
	if err != nil {
		return Result{}, fmt.Errorf("closer: scan: %w", err)
	}

	res := Result{Scanned: len(rows)}
	for _, r := range rows {
		newStatus := terminalStatus(r)
		if err := c.store.MarkStatus(ctx, r.ID, newStatus); err != nil {
			c.logger.Warn("closer: mark status failed",
				"err", err,
				"id", r.ID,
				"tenant", r.TenantID,
				"provider", r.Provider,
				"model", r.Model,
				"window_start", r.WindowStart.Format(time.RFC3339))
			res.Errors++
			continue
		}
		r.Status = newStatus

		// Emit the close event before forgetting the in-memory bucket so
		// downstream consumers see the final cost columns.
		if err := c.emitter.EmitWindowClosed(ctx, r); err != nil {
			c.logger.Warn("closer: emit window-close failed",
				"err", err,
				"id", r.ID,
				"tenant", r.TenantID,
				"status", string(newStatus))
			res.Errors++
			// Continue — Postgres is the source of truth; the bus emit
			// can be replayed by a follow-up scan if needed.
		}

		// Update Prometheus gauges + the close counter.
		c.metrics.ObserveWindowClose(r)

		// Release the in-memory bucket. Any late event after this point
		// would re-create the bucket; the upsert below would be guarded
		// by the WHERE status='open' clause and silently no-op on the
		// already-closed row.
		if c.forgetter != nil {
			c.forgetter.Forget(joiner.WindowKey{
				TenantID:    r.TenantID,
				Provider:    r.Provider,
				Model:       r.Model,
				WindowStart: r.WindowStart,
				WindowEnd:   r.WindowEnd,
			})
		}

		switch newStatus {
		case store.StatusReconciled:
			res.Reconciled++
		case store.StatusUnreconciled:
			res.Unreconciled++
		case store.StatusOpen, store.StatusClosed:
			// CloseDue only ever reports reconciled/unreconciled outcomes.
		}
	}
	return res, nil
}

// Run executes RunOnce on the supplied cadence until ctx is cancelled.
func (c *Closer) Run(ctx context.Context, interval time.Duration) error {
	cycle := func() {
		res, err := c.RunOnce(ctx)
		if err != nil {
			c.logger.Warn("closer cycle failed", "err", err)
			return
		}
		if res.Scanned > 0 {
			c.logger.Info("closer cycle complete",
				"scanned", res.Scanned,
				"reconciled", res.Reconciled,
				"unreconciled", res.Unreconciled,
				"errors", res.Errors)
		}
	}

	cycle()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			cycle()
		}
	}
}

// terminalStatus decides reconciled vs unreconciled based on whether both
// sides contributed any non-zero cost. A zero-USD bucket with no
// contributions on either side never reaches the closer because nothing
// would have inserted the row in the first place.
func terminalStatus(r store.Row) store.Status {
	estPresent := r.EstimatedCostUSD > 0
	recPresent := r.ReconciledCostUSD > 0
	if estPresent && recPresent {
		return store.StatusReconciled
	}
	return store.StatusUnreconciled
}
