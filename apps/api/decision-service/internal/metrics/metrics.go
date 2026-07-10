// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the decision-service's self-observability surface.
//
// Prefix: llm_decision_*. Series are deliberately tenant-free at the
// metric level so an operator can chart aggregate health without
// cardinality explosion.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Registry collects the decision-service's counters and gauges.
type Registry struct {
	appendsTotal      atomic.Int64
	appendFailures    atomic.Int64
	rejectsValidation atomic.Int64
	lastAppendUnix    atomic.Int64
	queryServed       atomic.Int64
	statsServed       atomic.Int64
}

// New constructs a fresh Registry.
func New() *Registry { return &Registry{} }

// IncAppend records a successful append.
func (r *Registry) IncAppend() {
	r.appendsTotal.Add(1)
	r.lastAppendUnix.Store(time.Now().Unix())
}

// IncAppendFailure records a failed append.
func (r *Registry) IncAppendFailure() { r.appendFailures.Add(1) }

// IncValidationReject records an event dropped for missing required fields
// or for failing schema-shape checks at decode time.
func (r *Registry) IncValidationReject() { r.rejectsValidation.Add(1) }

// IncQuery records one /v1/decisions or /v1/decisions/{id} call.
func (r *Registry) IncQuery() { r.queryServed.Add(1) }

// IncStats records one /v1/decisions/stats call.
func (r *Registry) IncStats() { r.statsServed.Add(1) }

// Handler returns the Prometheus exposition handler.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	_, _ = fmt.Fprintln(w, "# HELP llm_decision_appends_total Decision rows appended.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_decision_appends_total counter")
	_, _ = fmt.Fprintf(w, "llm_decision_appends_total %d\n", r.appendsTotal.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_decision_append_failures_total Decision append attempts that errored.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_decision_append_failures_total counter")
	_, _ = fmt.Fprintf(w, "llm_decision_append_failures_total %d\n", r.appendFailures.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_decision_rejects_validation_total Decision events dropped for failing decode or required-field checks.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_decision_rejects_validation_total counter")
	_, _ = fmt.Fprintf(w, "llm_decision_rejects_validation_total %d\n", r.rejectsValidation.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_decision_last_append_timestamp Unix seconds of the most recent successful append.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_decision_last_append_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_decision_last_append_timestamp %d\n", r.lastAppendUnix.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_decision_query_requests_total Queries served by /v1/decisions.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_decision_query_requests_total counter")
	_, _ = fmt.Fprintf(w, "llm_decision_query_requests_total %d\n", r.queryServed.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_decision_stats_requests_total Queries served by /v1/decisions/stats.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_decision_stats_requests_total counter")
	_, _ = fmt.Fprintf(w, "llm_decision_stats_requests_total %d\n", r.statsServed.Load())
}
