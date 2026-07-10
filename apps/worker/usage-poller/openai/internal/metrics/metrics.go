// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics exposes the poller's Prometheus surface.
//
// Per F009 §12 the poller exports:
//
//   - llm_exporter_scrape_success         (counter)
//   - llm_exporter_last_success_timestamp (gauge, unix seconds)
//   - llm_provider_api_errors_total       (counter, labelled by reason)
//   - llm_rate_limit_events_total         (counter, mapped to the F008 metric name)
//
// We intentionally implement the minimal Prometheus exposition format inline
// (one read-mostly mutex, a handful of counters/gauges) so the poller has no
// hard dependency on the Prometheus Go client at this stage. F010
// (Metrics-Endpoint Service) will own a shared client wrapper; until then,
// keeping the surface dependency-free keeps the binary small and reviewable.
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
)

// Registry holds the poller's metric state. Safe for concurrent use.
type Registry struct {
	provider string
	tenant   string
	env      string

	scrapeSuccess         atomic.Int64
	scrapeFailures        atomic.Int64
	lastSuccessUnixSecond atomic.Int64
	rateLimitEvents       atomic.Int64
	apiErrors             *labelCounter
}

// New constructs a Registry pinned to the supplied provider/tenant/env
// labels. Every emitted series carries these three labels so the cardinality
// stays predictable per F008 §12.
func New(provider, tenant, env string) *Registry {
	return &Registry{
		provider:  provider,
		tenant:    tenant,
		env:       env,
		apiErrors: newLabelCounter(),
	}
}

// IncScrapeSuccess records one successful scrape cycle and stamps the
// last-success gauge to now.
func (r *Registry) IncScrapeSuccess() {
	r.scrapeSuccess.Add(1)
	r.lastSuccessUnixSecond.Store(time.Now().Unix())
}

// IncScrapeFailure records a failed cycle. The companion IncProviderAPIError
// adds a per-reason breakdown.
func (r *Registry) IncScrapeFailure() {
	r.scrapeFailures.Add(1)
}

// IncProviderAPIError increments the per-reason error counter. `reason` is a
// stable short string ("network", "rate_limited", "circuit_open", "decode",
// "5xx", "4xx"). Keep the reason set small; cardinality budget is 16.
func (r *Registry) IncProviderAPIError(reason string) {
	r.apiErrors.inc(reason)
}

// IncRateLimitEvent records a 429. Mapped to the F008 metric name
// `llm_rate_limit_events_total` on exposition.
func (r *Registry) IncRateLimitEvent() {
	r.rateLimitEvents.Add(1)
}

// Handler returns an http.Handler that writes the Prometheus exposition
// format on every request. No allocation of a full client library; the
// surface is small and stable.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

// LastSuccessUnix returns the most recent successful scrape timestamp in
// unix seconds, or 0 if no scrape has succeeded yet. Exposed for tests.
func (r *Registry) LastSuccessUnix() int64 {
	return r.lastSuccessUnixSecond.Load()
}

// ScrapeSuccessCount returns the cumulative success count. For tests.
func (r *Registry) ScrapeSuccessCount() int64 { return r.scrapeSuccess.Load() }

// ScrapeFailureCount returns the cumulative failure count. For tests.
func (r *Registry) ScrapeFailureCount() int64 { return r.scrapeFailures.Load() }

// RateLimitEventCount returns the cumulative rate-limit hit count. For tests.
func (r *Registry) RateLimitEventCount() int64 { return r.rateLimitEvents.Load() }

func (r *Registry) write(w io.Writer) {
	base := fmt.Sprintf(`provider="%s",tenant="%s",env="%s"`, r.provider, r.tenant, r.env)

	// Writes to an http.ResponseWriter cannot be usefully recovered from once the
	// client has disconnected; intentionally discard the (n, err) tuple.
	_, _ = fmt.Fprintln(w, "# HELP llm_exporter_scrape_success Cumulative count of successful poll cycles.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_exporter_scrape_success counter")
	_, _ = fmt.Fprintf(w, "llm_exporter_scrape_success{%s} %d\n", base, r.scrapeSuccess.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_exporter_scrape_failure Cumulative count of failed poll cycles.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_exporter_scrape_failure counter")
	_, _ = fmt.Fprintf(w, "llm_exporter_scrape_failure{%s} %d\n", base, r.scrapeFailures.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_exporter_last_success_timestamp Unix timestamp (seconds) of the last successful poll cycle.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_exporter_last_success_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_exporter_last_success_timestamp{%s} %d\n", base, r.lastSuccessUnixSecond.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_rate_limit_events_total Provider rate-limit events (HTTP 429 or quota-exceeded).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_rate_limit_events_total counter")
	_, _ = fmt.Fprintf(w, "llm_rate_limit_events_total{%s} %d\n", base, r.rateLimitEvents.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_provider_api_errors_total Provider API errors observed by the poller, by reason.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_provider_api_errors_total counter")
	for _, v := range r.apiErrors.snapshot() {
		_, _ = fmt.Fprintf(w, "llm_provider_api_errors_total{%s,reason=\"%s\"} %d\n", base, v.label, v.value)
	}
}

// --- labelCounter ---------------------------------------------------------

type labelCounter struct {
	mu     sync.RWMutex
	values map[string]int64
}

func newLabelCounter() *labelCounter {
	return &labelCounter{values: map[string]int64{}}
}

func (c *labelCounter) inc(v string) {
	v = sanitizeLabelValue(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[v]++
}

type labelValue struct {
	label string
	value int64
}

func (c *labelCounter) snapshot() []labelValue {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]labelValue, 0, len(c.values))
	for k, v := range c.values {
		out = append(out, labelValue{label: k, value: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].label < out[j].label })
	return out
}

func sanitizeLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}
