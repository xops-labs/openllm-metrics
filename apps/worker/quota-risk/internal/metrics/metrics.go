// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the quota-risk worker's Prometheus surface.
//
// Three project-specific gauges (llm_* prefix):
//
//   - llm_quota_used_ratio{tenant,provider,model,region,kind}      gauge
//   - llm_quota_seconds_to_reset{tenant,provider,model,region,kind} gauge
//   - llm_quota_risk_score{tenant,provider,model,region,kind}      gauge
//
// Plus self-observability counters:
//
//   - llm_quota_risk_events_consumed_total
//   - llm_quota_risk_events_emitted_total
//   - llm_quota_risk_events_skipped_total{reason}
//
// We deliberately hand-roll the exposition (same choice as the F010
// aggregator and the label translator) to keep the binary small.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/model"
)

// Registry collects every metric the worker exports.
type Registry struct {
	consumed atomic.Int64
	emitted  atomic.Int64

	skippedMu sync.Mutex
	skipped   map[string]int64 // reason -> count

	gaugesMu  sync.RWMutex
	usedRatio map[string]float64
	secsReset map[string]float64
	riskScore map[string]float64
	gaugeKeys map[string]gaugeLabels
}

type gaugeLabels struct {
	tenant   string
	provider string
	model    string
	region   string
	kind     string
}

// New constructs an empty Registry.
func New() *Registry {
	return &Registry{
		skipped:   make(map[string]int64, 4),
		usedRatio: make(map[string]float64, 64),
		secsReset: make(map[string]float64, 64),
		riskScore: make(map[string]float64, 64),
		gaugeKeys: make(map[string]gaugeLabels, 64),
	}
}

// IncConsumed bumps the events-consumed counter.
func (r *Registry) IncConsumed() { r.consumed.Add(1) }

// IncEmitted bumps the events-emitted counter.
func (r *Registry) IncEmitted() { r.emitted.Add(1) }

// IncSkipped records a skip with a reason label.
func (r *Registry) IncSkipped(reason string) {
	r.skippedMu.Lock()
	defer r.skippedMu.Unlock()
	r.skipped[reason]++
}

// UpsertSnapshot replaces the gauge tables with the latest snapshot from
// the rolling model. Replacing wholesale (rather than incrementally
// updating) means stale keys that aged out of the window stop appearing
// on /metrics immediately, which is what operators expect from a gauge.
func (r *Registry) UpsertSnapshot(rows []model.Row) {
	used := make(map[string]float64, len(rows))
	secs := make(map[string]float64, len(rows))
	risk := make(map[string]float64, len(rows))
	labels := make(map[string]gaugeLabels, len(rows))

	for _, row := range rows {
		key := gaugeKey(row)
		lbl := gaugeLabels{
			tenant:   row.Key.Tenant,
			provider: row.Key.Provider,
			model:    row.Key.Model,
			region:   row.Key.Region,
			kind:     string(row.State.Kind),
		}
		labels[key] = lbl

		if v, ok := row.State.UsedRatio(); ok {
			used[key] = v
		}
		secs[key] = row.State.SecondsToReset()
		if v, ok := row.State.RiskScore(); ok {
			risk[key] = v
		}
	}

	r.gaugesMu.Lock()
	r.usedRatio = used
	r.secsReset = secs
	r.riskScore = risk
	r.gaugeKeys = labels
	r.gaugesMu.Unlock()
}

// Handler returns an http.Handler writing Prometheus exposition.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	// Counters first.
	_, _ = fmt.Fprintln(w, "# HELP llm_quota_risk_events_consumed_total Bus events processed by the quota-risk worker.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_quota_risk_events_consumed_total counter")
	_, _ = fmt.Fprintf(w, "llm_quota_risk_events_consumed_total %d\n", r.consumed.Load())

	_, _ = fmt.Fprintln(w, "# HELP llm_quota_risk_events_emitted_total Risk snapshot events produced to the bus.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_quota_risk_events_emitted_total counter")
	_, _ = fmt.Fprintf(w, "llm_quota_risk_events_emitted_total %d\n", r.emitted.Load())

	r.skippedMu.Lock()
	keys := make([]string, 0, len(r.skipped))
	for k := range r.skipped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	skipSnap := make([]struct {
		reason string
		count  int64
	}, 0, len(keys))
	for _, k := range keys {
		skipSnap = append(skipSnap, struct {
			reason string
			count  int64
		}{k, r.skipped[k]})
	}
	r.skippedMu.Unlock()

	_, _ = fmt.Fprintln(w, "# HELP llm_quota_risk_events_skipped_total Events skipped, labelled by reason.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_quota_risk_events_skipped_total counter")
	for _, s := range skipSnap {
		_, _ = fmt.Fprintf(w, "llm_quota_risk_events_skipped_total{reason=\"%s\"} %d\n",
			sanitize(s.reason), s.count)
	}

	// Gauges.
	r.gaugesMu.RLock()
	defer r.gaugesMu.RUnlock()

	// Deterministic order so two runs against the same state produce
	// byte-identical exposition.
	gaugeKeys := make([]string, 0, len(r.gaugeKeys))
	for k := range r.gaugeKeys {
		gaugeKeys = append(gaugeKeys, k)
	}
	sort.Strings(gaugeKeys)

	_, _ = fmt.Fprintln(w, "# HELP llm_quota_used_ratio Fraction of the provider quota window consumed (0..1). NaN means no denominator was published by the provider.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_quota_used_ratio gauge")
	for _, k := range gaugeKeys {
		v, ok := r.usedRatio[k]
		if !ok {
			continue
		}
		writeLabelled(w, "llm_quota_used_ratio", r.gaugeKeys[k], v)
	}

	_, _ = fmt.Fprintln(w, "# HELP llm_quota_seconds_to_reset Seconds until the provider quota window resets.")
	_, _ = fmt.Fprintln(w, "# TYPE llm_quota_seconds_to_reset gauge")
	for _, k := range gaugeKeys {
		v, ok := r.secsReset[k]
		if !ok {
			continue
		}
		writeLabelled(w, "llm_quota_seconds_to_reset", r.gaugeKeys[k], v)
	}

	_, _ = fmt.Fprintln(w, "# HELP llm_quota_risk_score Linear-shaped risk score in [0,1]. risk = min(1, used_ratio * 1.25).")
	_, _ = fmt.Fprintln(w, "# TYPE llm_quota_risk_score gauge")
	for _, k := range gaugeKeys {
		v, ok := r.riskScore[k]
		if !ok {
			continue
		}
		writeLabelled(w, "llm_quota_risk_score", r.gaugeKeys[k], v)
	}
}

func writeLabelled(w io.Writer, name string, l gaugeLabels, v float64) {
	_, _ = fmt.Fprintf(w,
		"%s{tenant=\"%s\",provider=\"%s\",model=\"%s\",region=\"%s\",kind=\"%s\"} %g\n",
		name,
		sanitize(l.tenant),
		sanitize(l.provider),
		sanitize(l.model),
		sanitize(l.region),
		sanitize(l.kind),
		v,
	)
}

func gaugeKey(row model.Row) string {
	return strings.Join([]string{
		row.Key.Tenant, row.Key.Provider, row.Key.Model, row.Key.Region, string(row.State.Kind),
	}, "\x00")
}

func sanitize(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}
