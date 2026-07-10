// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the reconciler's Prometheus surface.
//
// F023 exposes per-window gauges plus a counter so dashboards (F027) and
// notifications (F033) can render and alert on drift. Every series carries
// the full multi-tenant label set: tenant, team, app, provider, model,
// window (the human-readable window-start ISO timestamp).
//
// The gauges are refreshed by the closer at window-close time and reflect
// the last-closed window's values per label tuple. Counters accumulate.
//
// Series:
//
//	llm_reconciliation_estimated_cost_usd  (gauge, USD)
//	llm_reconciliation_reconciled_cost_usd (gauge, USD)
//	llm_reconciliation_drift_usd           (gauge, signed USD)
//	llm_reconciliation_drift_ratio         (gauge, signed)
//	llm_reconciliation_window_closed_total (counter)
//
// Self-observability counters (consumer/joiner activity) carry the worker
// tenant/env labels only — they are not per-event-tenant.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/store"
)

// labelSet is the full multi-tenant label tuple used on the close-out series.
type labelSet struct {
	Tenant   string
	Team     string
	App      string
	Provider string
	Model    string
	Window   string // RFC3339 window_start
}

func (l labelSet) String() string {
	return fmt.Sprintf(`tenant="%s",team="%s",app="%s",provider="%s",model="%s",window="%s"`,
		escape(l.Tenant),
		escape(l.Team),
		escape(l.App),
		escape(l.Provider),
		escape(l.Model),
		escape(l.Window),
	)
}

type closeoutGauges struct {
	estimatedUSD  float64
	reconciledUSD float64
	driftUSD      float64
	driftRatio    float64
	closedTotal   int64
}

// Registry holds the reconciler's counters and gauges.
type Registry struct {
	tenant string
	env    string

	// Self-observability counters (worker tenant/env label only).
	estimatedConsumed  atomic.Int64
	reconciledConsumed atomic.Int64
	estimatedDropped   atomic.Int64
	reconciledDropped  atomic.Int64
	badPayload         atomic.Int64
	lastCloseUnix      atomic.Int64
	windowsClosed      atomic.Int64

	// Close-out series keyed by full label set.
	mu        sync.RWMutex
	closeouts map[labelSet]*closeoutGauges
}

// New constructs a Registry pinned to the worker's tenant/env labels.
func New(tenant, env string) *Registry {
	return &Registry{
		tenant:    tenant,
		env:       env,
		closeouts: make(map[labelSet]*closeoutGauges, 64),
	}
}

// IncEstimatedConsumed records one consumed cost.estimated event.
func (r *Registry) IncEstimatedConsumed() { r.estimatedConsumed.Add(1) }

// IncReconciledConsumed records one consumed llm.usage.reconciled event.
func (r *Registry) IncReconciledConsumed() { r.reconciledConsumed.Add(1) }

// IncEstimatedDropped records an estimated event that was discarded.
func (r *Registry) IncEstimatedDropped() { r.estimatedDropped.Add(1) }

// IncReconciledDropped records a reconciled event that was discarded.
func (r *Registry) IncReconciledDropped() { r.reconciledDropped.Add(1) }

// IncBadPayload records an unparseable bus record.
func (r *Registry) IncBadPayload() { r.badPayload.Add(1) }

// ObserveWindowClose updates the per-tuple close-out gauges + counter when
// the closer transitions a window to a terminal status. The closer calls
// this with the final row state.
func (r *Registry) ObserveWindowClose(row store.Row) {
	r.lastCloseUnix.Store(time.Now().Unix())
	r.windowsClosed.Add(1)

	key := labelSet{
		Tenant:   row.TenantID,
		Team:     row.Team,
		App:      row.App,
		Provider: row.Provider,
		Model:    row.Model,
		Window:   row.WindowStart.UTC().Format(time.RFC3339),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	g, ok := r.closeouts[key]
	if !ok {
		g = &closeoutGauges{}
		r.closeouts[key] = g
	}
	g.estimatedUSD = row.EstimatedCostUSD
	g.reconciledUSD = row.ReconciledCostUSD
	g.driftUSD = row.DriftUSD
	g.driftRatio = row.DriftRatio
	g.closedTotal++
}

// Handler returns the Prometheus exposition handler.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	// Worker-level self-observability counters.
	base := fmt.Sprintf(`tenant="%s",env="%s"`, escape(r.tenant), escape(r.env))

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciler_estimated_events_consumed_total cost.estimated events consumed.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciler_estimated_events_consumed_total counter")
	_, _ = fmt.Fprintf(w, "llm_reconciler_estimated_events_consumed_total{%s} %d\n", base, r.estimatedConsumed.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciler_reconciled_events_consumed_total llm.usage.reconciled events consumed.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciler_reconciled_events_consumed_total counter")
	_, _ = fmt.Fprintf(w, "llm_reconciler_reconciled_events_consumed_total{%s} %d\n", base, r.reconciledConsumed.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciler_estimated_events_dropped_total cost.estimated events dropped (missing tenant, wrong source, bad timestamp).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciler_estimated_events_dropped_total counter")
	_, _ = fmt.Fprintf(w, "llm_reconciler_estimated_events_dropped_total{%s} %d\n", base, r.estimatedDropped.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciler_reconciled_events_dropped_total llm.usage.reconciled events dropped.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciler_reconciled_events_dropped_total counter")
	_, _ = fmt.Fprintf(w, "llm_reconciler_reconciled_events_dropped_total{%s} %d\n", base, r.reconciledDropped.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciler_bad_payload_total Bus records that failed to decode.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciler_bad_payload_total counter")
	_, _ = fmt.Fprintf(w, "llm_reconciler_bad_payload_total{%s} %d\n", base, r.badPayload.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciler_last_close_timestamp Unix seconds of the last window close.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciler_last_close_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_reconciler_last_close_timestamp{%s} %d\n", base, r.lastCloseUnix.Load())

	// F023-required per-tuple close-out series. Snapshot under read lock,
	// then write outside the lock so a slow exposition does not stall the
	// closer.
	r.mu.RLock()
	keys := make([]labelSet, 0, len(r.closeouts))
	snap := make(map[labelSet]closeoutGauges, len(r.closeouts))
	for k, g := range r.closeouts {
		keys = append(keys, k)
		snap[k] = *g
	}
	r.mu.RUnlock()

	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciliation_estimated_cost_usd Runtime estimated cost for the most recently closed window per (tenant, team, app, provider, model, window).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciliation_estimated_cost_usd gauge")
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "llm_reconciliation_estimated_cost_usd{%s} %g\n", k.String(), snap[k].estimatedUSD)
	}

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciliation_reconciled_cost_usd Vendor-reconciled cost (FOCUS) for the most recently closed window.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciliation_reconciled_cost_usd gauge")
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "llm_reconciliation_reconciled_cost_usd{%s} %g\n", k.String(), snap[k].reconciledUSD)
	}

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciliation_drift_usd Signed drift in USD for the most recently closed window. Positive => vendor billed more than the runtime estimate.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciliation_drift_usd gauge")
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "llm_reconciliation_drift_usd{%s} %g\n", k.String(), snap[k].driftUSD)
	}

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciliation_drift_ratio Signed drift ratio = drift_usd / max(estimated_cost_usd, 0.0001).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciliation_drift_ratio gauge")
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "llm_reconciliation_drift_ratio{%s} %g\n", k.String(), snap[k].driftRatio)
	}

	_, _ = fmt.Fprintln(w, "# HELP llm_reconciliation_window_closed_total Number of times a (tenant, team, app, provider, model, window) tuple has been closed.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_reconciliation_window_closed_total counter")
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "llm_reconciliation_window_closed_total{%s} %d\n", k.String(), snap[k].closedTotal)
	}
}

// labelEscaper sanitizes Prometheus label values (RFC: backslashes,
// double-quotes, and newlines). Replacer is safe for concurrent use.
var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// escape minimally sanitizes a Prometheus label value.
func escape(s string) string {
	return labelEscaper.Replace(s)
}
