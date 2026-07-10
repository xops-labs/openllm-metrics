// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the label translator's self-observability surface.
//
// Cardinality budget mirrors F009: a small fixed-cardinality label set on
// each series. The unmapped counter carries (provider) so operators can see
// which provider's mapping table needs attention without exploding the
// label space.
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

// Registry collects all the counters/gauges the translator exports.
type Registry struct {
	tenant string
	env    string

	scrapeSuccess         atomic.Int64
	scrapeFailures        atomic.Int64
	lastSuccessUnixSecond atomic.Int64
	emittedTotal          atomic.Int64
	skippedTotal          atomic.Int64
	droppedTotal          atomic.Int64
	unmappedTotal         *labelCounter
}

// New constructs a Registry pinned to the worker's tenant/env labels.
func New(tenant, env string) *Registry {
	return &Registry{
		tenant:        tenant,
		env:           env,
		unmappedTotal: newLabelCounter(),
	}
}

// IncScrapeSuccess marks the most recent upstream scrape as successful.
func (r *Registry) IncScrapeSuccess() {
	r.scrapeSuccess.Add(1)
	r.lastSuccessUnixSecond.Store(time.Now().Unix())
}

// IncScrapeFailure records a failed scrape cycle.
func (r *Registry) IncScrapeFailure() {
	r.scrapeFailures.Add(1)
}

// AddEmitted bumps the emitted counter by n.
func (r *Registry) AddEmitted(n int) { r.emittedTotal.Add(int64(n)) }

// AddSkipped bumps the skipped counter by n.
func (r *Registry) AddSkipped(n int) { r.skippedTotal.Add(int64(n)) }

// AddDropped bumps the dropped counter by n.
func (r *Registry) AddDropped(n int) { r.droppedTotal.Add(int64(n)) }

// AddUnmapped bumps the unmapped counter for a provider by n.
func (r *Registry) AddUnmapped(provider string, n int) {
	r.unmappedTotal.add(provider, int64(n))
}

// Handler returns an http.Handler that writes Prometheus exposition.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	base := fmt.Sprintf(`tenant="%s",env="%s"`, r.tenant, r.env)

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translator_scrape_success_total Successful upstream scrape cycles.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translator_scrape_success_total counter")
	_, _ = fmt.Fprintf(w, "llm_label_translator_scrape_success_total{%s} %d\n", base, r.scrapeSuccess.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translator_scrape_failure_total Failed upstream scrape cycles.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translator_scrape_failure_total counter")
	_, _ = fmt.Fprintf(w, "llm_label_translator_scrape_failure_total{%s} %d\n", base, r.scrapeFailures.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translator_last_success_timestamp Unix timestamp (seconds) of the last successful scrape.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translator_last_success_timestamp gauge")
	_, _ = fmt.Fprintf(w, "llm_label_translator_last_success_timestamp{%s} %d\n", base, r.lastSuccessUnixSecond.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translator_emitted_total Translated events published to the bus.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translator_emitted_total counter")
	_, _ = fmt.Fprintf(w, "llm_label_translator_emitted_total{%s} %d\n", base, r.emittedTotal.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translator_skipped_total Samples skipped (priming scrape or zero-delta window).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translator_skipped_total counter")
	_, _ = fmt.Fprintf(w, "llm_label_translator_skipped_total{%s} %d\n", base, r.skippedTotal.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translator_dropped_total Events dropped because no fallback tenant could be resolved.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translator_dropped_total counter")
	_, _ = fmt.Fprintf(w, "llm_label_translator_dropped_total{%s} %d\n", base, r.droppedTotal.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_label_translation_unmapped_total Inbound samples with no row in control_plane.label_mappings, by provider.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_label_translation_unmapped_total counter")
	for _, v := range r.unmappedTotal.snapshot() {
		_, _ = fmt.Fprintf(w, "llm_label_translation_unmapped_total{%s,provider=\"%s\"} %d\n", base, v.label, v.value)
	}
}

// --- labelCounter (mirrors the helper in the OpenAI poller) ---------------

type labelCounter struct {
	mu     sync.RWMutex
	values map[string]int64
}

func newLabelCounter() *labelCounter {
	return &labelCounter{values: map[string]int64{}}
}

func (c *labelCounter) add(v string, n int64) {
	v = sanitizeLabelValue(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[v] += n
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
