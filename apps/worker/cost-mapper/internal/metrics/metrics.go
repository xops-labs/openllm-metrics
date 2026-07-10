// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the cost-mapper's self-observability surface. Counters
// follow the project-wide llm_* prefix; OpenTelemetry gen_ai.* conventions
// cover the underlying token/cost shape on the bus side.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Registry holds the cost-mapper's counters and gauges.
type Registry struct {
	tenant string
	env    string

	runtimeConsumed    atomic.Int64
	estimatesEmitted   atomic.Int64
	estimatesSkipped   atomic.Int64
	unpricedEvents     atomic.Int64
	reconciledConsumed atomic.Int64
	driftRowsWritten   atomic.Int64
	driftErrors        atomic.Int64
	lastSuccessUnix    atomic.Int64
}

// New constructs a Registry pinned to the worker's tenant/env labels. The
// labels are informational only — every metric also carries the inherent
// {tenant, env} context from the event-shape downstream, so per-tenant
// dashboards do not depend on these defaults.
func New(tenant, env string) *Registry {
	return &Registry{tenant: tenant, env: env}
}

// IncRuntimeConsumed records one consumed runtime event.
func (r *Registry) IncRuntimeConsumed() { r.runtimeConsumed.Add(1) }

// IncEstimateEmitted records one cost.estimated event published to the bus.
func (r *Registry) IncEstimateEmitted() {
	r.estimatesEmitted.Add(1)
	r.lastSuccessUnix.Store(time.Now().Unix())
}

// IncEstimateSkipped records an estimate skipped (e.g. zero tokens).
func (r *Registry) IncEstimateSkipped() { r.estimatesSkipped.Add(1) }

// IncUnpriced records an event whose (provider, model) is not in the catalog.
func (r *Registry) IncUnpriced() { r.unpricedEvents.Add(1) }

// IncReconciledConsumed records one consumed reconciled (FOCUS) event.
func (r *Registry) IncReconciledConsumed() { r.reconciledConsumed.Add(1) }

// IncDriftRow records one drift row written/upserted to Postgres.
func (r *Registry) IncDriftRow() { r.driftRowsWritten.Add(1) }

// IncDriftError records a failed drift-row write.
func (r *Registry) IncDriftError() { r.driftErrors.Add(1) }

// Handler returns the Prometheus exposition handler.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	base := fmt.Sprintf(`tenant="%s",env="%s"`, r.tenant, r.env)

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_runtime_events_consumed_total Runtime events consumed from llm.runtime.normalized.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_runtime_events_consumed_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_runtime_events_consumed_total{%s} %d\n", base, r.runtimeConsumed.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_estimates_emitted_total cost.estimated events published to the bus.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_estimates_emitted_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_estimates_emitted_total{%s} %d\n", base, r.estimatesEmitted.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_estimates_skipped_total Runtime events skipped (no tokens, validation failure).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_estimates_skipped_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_estimates_skipped_total{%s} %d\n", base, r.estimatesSkipped.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_unpriced_events_total Runtime events whose (provider, model) is not in the pricing catalog.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_unpriced_events_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_unpriced_events_total{%s} %d\n", base, r.unpricedEvents.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_reconciled_events_consumed_total Reconciled FOCUS events consumed from llm.usage.reconciled.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_reconciled_events_consumed_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_reconciled_events_consumed_total{%s} %d\n", base, r.reconciledConsumed.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_drift_rows_total Drift rows upserted into control_plane.cost_reconciliation_drift.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_drift_rows_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_drift_rows_total{%s} %d\n", base, r.driftRowsWritten.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_drift_errors_total Failed drift-row upserts.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_drift_errors_total counter")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_drift_errors_total{%s} %d\n", base, r.driftErrors.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_cost_mapper_last_success_timestamp Unix seconds of the last successful estimate emit.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_cost_mapper_last_success_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_cost_mapper_last_success_timestamp{%s} %d\n", base, r.lastSuccessUnix.Load())
}
