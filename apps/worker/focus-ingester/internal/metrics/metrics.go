// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the FOCUS ingester's self-observability surface.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Registry collects the ingester's counters and gauges.
type Registry struct {
	tenant string
	env    string

	pollSuccess      atomic.Int64
	pollFailures     atomic.Int64
	lastSuccessUnix  atomic.Int64
	recordsFetched   atomic.Int64
	recordsPersisted atomic.Int64
	recordsEmitted   atomic.Int64
	recordsUnmapped  atomic.Int64
	recordsDropped   atomic.Int64
}

// New constructs a Registry pinned to the worker's tenant/env labels.
func New(tenant, env string) *Registry {
	return &Registry{tenant: tenant, env: env}
}

// IncPollSuccess records a successful poll and stamps the last-success gauge.
func (r *Registry) IncPollSuccess() {
	r.pollSuccess.Add(1)
	r.lastSuccessUnix.Store(time.Now().Unix())
}

// IncPollFailure records a failed poll.
func (r *Registry) IncPollFailure() {
	r.pollFailures.Add(1)
}

// AddFetched bumps the records-fetched counter by n.
func (r *Registry) AddFetched(n int) { r.recordsFetched.Add(int64(n)) }

// AddPersisted bumps the records-persisted counter by n.
func (r *Registry) AddPersisted(n int) { r.recordsPersisted.Add(int64(n)) }

// AddEmitted bumps the records-emitted counter by n.
func (r *Registry) AddEmitted(n int) { r.recordsEmitted.Add(int64(n)) }

// AddUnmapped bumps the unmapped-records counter by n.
func (r *Registry) AddUnmapped(n int) { r.recordsUnmapped.Add(int64(n)) }

// AddDropped bumps the dropped-records counter by n.
func (r *Registry) AddDropped(n int) { r.recordsDropped.Add(int64(n)) }

// Handler returns the Prometheus exposition handler.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	base := fmt.Sprintf(`tenant="%s",env="%s"`, r.tenant, r.env)

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_poll_success_total Successful /focus.json polls.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_poll_success_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_poll_success_total{%s} %d\n", base, r.pollSuccess.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_poll_failure_total Failed /focus.json polls.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_poll_failure_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_poll_failure_total{%s} %d\n", base, r.pollFailures.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_last_success_timestamp Unix timestamp (seconds) of the last successful poll.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_last_success_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_last_success_timestamp{%s} %d\n", base, r.lastSuccessUnix.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_records_fetched_total FOCUS records fetched from upstream.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_records_fetched_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_records_fetched_total{%s} %d\n", base, r.recordsFetched.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_records_persisted_total FOCUS records written to control_plane.focus_records.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_records_persisted_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_records_persisted_total{%s} %d\n", base, r.recordsPersisted.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_records_emitted_total Reconciled events published to the bus.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_records_emitted_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_records_emitted_total{%s} %d\n", base, r.recordsEmitted.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_records_unmapped_total FOCUS records with no matching label_mappings row.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_records_unmapped_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_records_unmapped_total{%s} %d\n", base, r.recordsUnmapped.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_focus_ingester_records_dropped_total FOCUS records dropped (insert error, missing tenant).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_focus_ingester_records_dropped_total counter")
	_, _ = fmt.Fprintf(w, "llm_focus_ingester_records_dropped_total{%s} %d\n", base, r.recordsDropped.Load())
}
