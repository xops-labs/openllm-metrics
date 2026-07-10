// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the audit-service's self-observability surface.
//
// Prefix: llm_audit_*. Series are deliberately tenant-free at the metric
// level so an operator can chart aggregate health without cardinality
// explosion; tenant-scoped totals come from PromQL over the chain itself.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Registry collects the audit-service's counters and gauges.
type Registry struct {
	appendsTotal      atomic.Int64
	appendFailures    atomic.Int64
	rejectsRedaction  atomic.Int64
	rejectsValidation atomic.Int64
	lastAppendUnix    atomic.Int64
	verifyChecks      atomic.Int64
	verifyBreaks      atomic.Int64
	exportRows        atomic.Int64
	queryServed       atomic.Int64
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

// IncRedactionReject records an event dropped because it carried a
// forbidden field that the redact step alone cannot fix safely.
func (r *Registry) IncRedactionReject() { r.rejectsRedaction.Add(1) }

// IncValidationReject records an event dropped for missing required fields.
func (r *Registry) IncValidationReject() { r.rejectsValidation.Add(1) }

// IncVerifyCheck records one row scanned by the verify endpoint or CLI.
func (r *Registry) IncVerifyCheck() { r.verifyChecks.Add(1) }

// IncVerifyBreak records a chain break observed by verify.
func (r *Registry) IncVerifyBreak() { r.verifyBreaks.Add(1) }

// AddExportRows bumps the export-rows counter.
func (r *Registry) AddExportRows(n int) { r.exportRows.Add(int64(n)) }

// IncQuery records one /v1/audit/entries call.
func (r *Registry) IncQuery() { r.queryServed.Add(1) }

// Handler returns the Prometheus exposition handler.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	_, _ = fmt.Fprintln(w, "# HELP llm_audit_appends_total Audit entries appended.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_appends_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_appends_total %d\n", r.appendsTotal.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_append_failures_total Audit append attempts that errored.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_append_failures_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_append_failures_total %d\n", r.appendFailures.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_rejects_redaction_total Audit events dropped because a forbidden field survived redaction.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_rejects_redaction_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_rejects_redaction_total %d\n", r.rejectsRedaction.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_rejects_validation_total Audit events dropped for failing schema validation.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_rejects_validation_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_rejects_validation_total %d\n", r.rejectsValidation.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_last_append_timestamp Unix seconds of the most recent successful append.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_last_append_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_audit_last_append_timestamp %d\n", r.lastAppendUnix.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_verify_rows_total Rows scanned by the chain verifier.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_verify_rows_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_verify_rows_total %d\n", r.verifyChecks.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_verify_breaks_total Chain breaks detected by the verifier.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_verify_breaks_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_verify_breaks_total %d\n", r.verifyBreaks.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_export_rows_total Rows streamed by /v1/audit/export.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_export_rows_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_export_rows_total %d\n", r.exportRows.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_audit_query_requests_total Queries served by /v1/audit/entries.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_audit_query_requests_total counter")
	_, _ = fmt.Fprintf(w, "llm_audit_query_requests_total %d\n", r.queryServed.Load())
}
