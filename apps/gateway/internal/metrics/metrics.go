// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the gateway's self-observability surface.
//
// All counters/histograms are emitted in the Prometheus text exposition
// format on the side-channel /metrics endpoint (separate listener from the
// proxy port). The label set is intentionally narrow — F008 §10 cardinality
// budgets cap the explosion that "every model × every status" would cause.
//
// Project-specific metrics use the `llm_*` prefix, and we
// extend rather than replace the OTel GenAI semconv names: the histogram
// here is the operational view; the OTel `gen_ai.client.operation.duration`
// histogram is emitted by the SDK-side bundle when wired in.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// LatencyBuckets are the histogram boundaries in seconds. Tuned for
// LLM completion latencies: most chat requests are 100ms..30s. The 5s
// boundary is load-bearing: the SLO pack's latency rules
// (platform/slo/prometheus/) select le="5" for the p99 objective.
var LatencyBuckets = []float64{
	0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60,
}

// Registry collects counters and histograms for the gateway request loop.
//
// Designed for low-contention concurrent access: counters use atomic.Int64
// and histograms use a per-key fixed-size bucket array guarded by a mutex.
// The Prometheus exposition is generated on scrape (cold path).
type Registry struct {
	requestsTotal      *counter
	errorsTotal        *counter
	retriesTotal       *counter
	streamingTotal     *counter
	usageObservedTotal *counter
	usageUnknownTotal  *counter
	busPublishTotal    *counter
	busPublishErrTotal *counter

	mu        sync.Mutex
	histogram map[string]*hist
}

// New constructs a Registry. Counters and the histogram map start empty.
func New() *Registry {
	return &Registry{
		requestsTotal:      newCounter(),
		errorsTotal:        newCounter(),
		retriesTotal:       newCounter(),
		streamingTotal:     newCounter(),
		usageObservedTotal: newCounter(),
		usageUnknownTotal:  newCounter(),
		busPublishTotal:    newCounter(),
		busPublishErrTotal: newCounter(),
		histogram:          make(map[string]*hist),
	}
}

// Labels is the canonical narrow label set for every gateway-side series.
type Labels struct {
	Provider   string
	Model      string
	Tenant     string
	Env        string
	Status     string // success | error | timeout | rate_limited
	StatusCode int    // raw HTTP code; 0 if no response
}

// ObserveRequest records one completed request: increments the total
// counter, adds the latency to the histogram, and bumps error/retry
// counters if applicable.
func (r *Registry) ObserveRequest(lbls Labels, latencySeconds float64, retries int, streaming bool, hasUsage bool) {
	key := lbls.toKey()
	r.requestsTotal.add(key, 1)
	r.histogramFor(key).observe(latencySeconds)
	if lbls.Status != "success" {
		r.errorsTotal.add(key, 1)
	}
	if retries > 0 {
		r.retriesTotal.add(key, int64(retries))
	}
	if streaming {
		r.streamingTotal.add(key, 1)
	}
	if hasUsage {
		r.usageObservedTotal.add(key, 1)
	} else {
		r.usageUnknownTotal.add(key, 1)
	}
}

// ObserveBusPublish records a bus-publish outcome.
func (r *Registry) ObserveBusPublish(ok bool) {
	if ok {
		r.busPublishTotal.add("", 1)
	} else {
		r.busPublishErrTotal.add("", 1)
	}
}

// Handler returns an http.Handler that writes the Prometheus exposition.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	r.writeCounter(w, "llm_gateway_requests_total", "Requests handled by the gateway, by provider/model/status.", r.requestsTotal)
	r.writeCounter(w, "llm_gateway_errors_total", "Requests that returned a non-success status.", r.errorsTotal)
	r.writeCounter(w, "llm_gateway_retries_total", "Sum of retry attempts observed at the proxy boundary.", r.retriesTotal)
	r.writeCounter(w, "llm_gateway_streaming_total", "Requests served with a streaming response body.", r.streamingTotal)
	r.writeCounter(w, "llm_gateway_usage_observed_total", "Requests where provider usage tokens were parsed.", r.usageObservedTotal)
	r.writeCounter(w, "llm_gateway_usage_unknown_total", "Requests where provider usage tokens were not present.", r.usageUnknownTotal)

	_, _ = fmt.Fprintln(w, "# HELP llm_gateway_bus_publish_total Successful runtime event publishes to the bus.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_gateway_bus_publish_total counter")
	_, _ = fmt.Fprintf(w, "llm_gateway_bus_publish_total %d\n", r.busPublishTotal.load(""))

	_, _ = fmt.Fprintln(w, "# HELP llm_gateway_bus_publish_errors_total Failed runtime event publishes to the bus.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_gateway_bus_publish_errors_total counter")
	_, _ = fmt.Fprintf(w, "llm_gateway_bus_publish_errors_total %d\n", r.busPublishErrTotal.load(""))

	r.writeHistogram(w)
}

func (r *Registry) writeCounter(w io.Writer, name, help string, c *counter) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	_, _ = fmt.Fprintf(w, "# TYPE %s counter\n", name)
	c.each(func(key string, v int64) {
		_, _ = fmt.Fprintf(w, "%s{%s} %d\n", name, key, v)
	})
}

func (r *Registry) writeHistogram(w io.Writer) {
	const name = "llm_gateway_latency_seconds"
	_, _ = fmt.Fprintf(w, "# HELP %s Request latency at the proxy boundary in seconds.\n", name)
	_, _ = fmt.Fprintf(w, "# TYPE %s histogram\n", name)

	r.mu.Lock()
	keys := make([]string, 0, len(r.histogram))
	for k := range r.histogram {
		keys = append(keys, k)
	}
	r.mu.Unlock()
	sort.Strings(keys)

	for _, key := range keys {
		r.mu.Lock()
		h := r.histogram[key]
		r.mu.Unlock()
		h.mu.Lock()
		for i, b := range LatencyBuckets {
			_, _ = fmt.Fprintf(w, "%s_bucket{%s,le=\"%g\"} %d\n", name, key, b, h.counts[i])
		}
		_, _ = fmt.Fprintf(w, "%s_bucket{%s,le=\"+Inf\"} %d\n", name, key, h.counts[len(h.counts)-1])
		_, _ = fmt.Fprintf(w, "%s_sum{%s} %g\n", name, key, h.sum)
		_, _ = fmt.Fprintf(w, "%s_count{%s} %d\n", name, key, h.count)
		h.mu.Unlock()
	}
}

func (r *Registry) histogramFor(key string) *hist {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.histogram[key]
	if !ok {
		h = newHist(len(LatencyBuckets) + 1)
		r.histogram[key] = h
	}
	return h
}

func (l Labels) toKey() string {
	var b strings.Builder
	b.WriteString(`provider="`)
	b.WriteString(escape(l.Provider))
	b.WriteString(`",model="`)
	b.WriteString(escape(l.Model))
	b.WriteString(`",tenant="`)
	b.WriteString(escape(l.Tenant))
	b.WriteString(`",env="`)
	b.WriteString(escape(l.Env))
	b.WriteString(`",status="`)
	b.WriteString(escape(l.Status))
	if l.StatusCode > 0 {
		b.WriteString(`",status_code="`)
		b.WriteString(strconv.Itoa(l.StatusCode))
	}
	b.WriteString(`"`)
	return b.String()
}

func escape(s string) string {
	if s == "" {
		return ""
	}
	// Prometheus label-value escaping: \ → \\, " → \", \n → \\n.
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// counter holds keyed atomic counters. Keys are the pre-formatted label
// strings produced by Labels.toKey so we never allocate a map[Labels]Int64.
type counter struct {
	mu     sync.RWMutex
	values map[string]*atomic.Int64
}

func newCounter() *counter {
	return &counter{values: make(map[string]*atomic.Int64)}
}

func (c *counter) add(key string, delta int64) {
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		v.Add(delta)
		return
	}
	c.mu.Lock()
	v, ok = c.values[key]
	if !ok {
		v = &atomic.Int64{}
		c.values[key] = v
	}
	c.mu.Unlock()
	v.Add(delta)
}

func (c *counter) load(key string) int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.values[key]
	if !ok {
		return 0
	}
	return v.Load()
}

func (c *counter) each(fn func(key string, v int64)) {
	c.mu.RLock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	c.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		fn(k, c.load(k))
	}
}

type hist struct {
	mu     sync.Mutex
	counts []int64
	sum    float64
	count  int64
}

func newHist(n int) *hist {
	return &hist{counts: make([]int64, n)}
}

func (h *hist) observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.count++
	for i, b := range LatencyBuckets {
		if v <= b {
			h.counts[i]++
		}
	}
	// +Inf bucket: cumulative count.
	h.counts[len(h.counts)-1]++
}
