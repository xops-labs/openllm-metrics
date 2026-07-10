// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package aggregator owns the in-memory rollup of normalized telemetry
// events into Prometheus-compatible metric families.
//
// Design invariants (F010 §9 / §11 / §12):
//
//   - Aggregator state is purely in-memory and rebuildable from a bus
//     replay. No persistence layer is involved.
//   - Every observation passes through cardinality enforcement before
//     contributing to a series. An event whose labels are outside the F008
//     budget is REJECTED and counted under
//     llm_aggregator_rejected_events_total{reason="..."}.
//   - Forbidden LLM-payload fields are treated as a hard fault: the event is
//     dropped and the same rejected-events counter ticks with reason="forbidden".
//   - Idempotency is enforced one layer up (the consumer dedups by event_id);
//     this package treats every Apply call as authoritative.
//   - Counters and histograms only. Gauges are explicitly out of scope at
//     this phase (F010 §9).
//
// The aggregator deliberately uses no Prometheus client library. The
// exposition format is small enough to write directly, and avoiding the
// dependency keeps the binary footprint comparable to F009's poller (which
// made the same choice for the same reason).
package aggregator

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	metricscontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/metrics/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
	schemalint "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go"
)

// RejectReason enumerates the closed set of reasons the aggregator may
// reject an event. Keeping it closed bounds the cardinality of the
// self-metric llm_aggregator_rejected_events_total{reason="..."}.
type RejectReason string

const (
	// ReasonSchema means the event failed schemalint.LintEvent (missing /
	// malformed required field, bad schema_version, etc.).
	ReasonSchema RejectReason = "schema"
	// ReasonForbidden means the event carried a forbidden LLM-payload field
	// (prompt, completion, etc.). Treated as a security defect; this is
	// already caught upstream by F008 lint but the aggregator defends in
	// depth (F010 §11).
	ReasonForbidden RejectReason = "forbidden"
	// ReasonCardinality means the event's labels included an unknown or
	// unauthorized label per the F008 metric registry.
	ReasonCardinality RejectReason = "cardinality"
	// ReasonUnknownTopic means the event was delivered on a topic the
	// aggregator does not know how to project.
	ReasonUnknownTopic RejectReason = "unknown_topic"
	// ReasonDecode means the bus payload was not valid JSON.
	ReasonDecode RejectReason = "decode"
)

// AllRejectReasons returns every RejectReason in a stable order. Useful for
// deterministic exposition of the self-metric.
func AllRejectReasons() []RejectReason {
	return []RejectReason{
		ReasonDecode,
		ReasonSchema,
		ReasonForbidden,
		ReasonCardinality,
		ReasonUnknownTopic,
	}
}

// Aggregator accumulates counter / histogram families derived from
// normalized telemetry events. Safe for concurrent use.
type Aggregator struct {
	mu sync.RWMutex

	// counters maps (metric name) -> (label fingerprint) -> CounterSeries.
	counters map[string]map[string]*CounterSeries

	// rejected tracks per-reason counts for the self-metric.
	rejected map[RejectReason]int64

	// processed tracks total events successfully applied.
	processed int64

	// metricByName is a pre-built index over the F008 registry so the hot
	// path avoids the linear FindByName walk.
	metricByName map[string]metricscontracts.Metric
}

// CounterSeries is a single observed (metric, labelset) pair. The Sum is a
// float64 so cost (USD) and token counts can share one structure.
type CounterSeries struct {
	Labels map[string]string
	Sum    float64
}

// New returns a fresh Aggregator with empty state.
func New() *Aggregator {
	idx := make(map[string]metricscontracts.Metric, 16)
	for _, m := range metricscontracts.Registry() {
		idx[m.Name] = m
	}
	return &Aggregator{
		counters:     make(map[string]map[string]*CounterSeries, 16),
		rejected:     make(map[RejectReason]int64, len(AllRejectReasons())),
		metricByName: idx,
	}
}

// RejectedEvents returns the per-reason rejected counts. The returned map is
// a snapshot copy safe for the caller to mutate.
func (a *Aggregator) RejectedEvents() map[RejectReason]int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make(map[RejectReason]int64, len(a.rejected))
	for k, v := range a.rejected {
		out[k] = v
	}
	return out
}

// ProcessedEvents returns the total number of events successfully applied.
func (a *Aggregator) ProcessedEvents() int64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.processed
}

// SeriesCount returns the total number of distinct (metric, labelset) pairs
// currently held in memory. Used for the operator memory-budget gauge and
// for tests.
func (a *Aggregator) SeriesCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	n := 0
	for _, by := range a.counters {
		n += len(by)
	}
	return n
}

// Reset clears all accumulated state. Used between cold starts during
// replay-determinism tests, never in production hot paths.
func (a *Aggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.counters = make(map[string]map[string]*CounterSeries, 16)
	a.rejected = make(map[RejectReason]int64, len(AllRejectReasons()))
	a.processed = 0
}

// Apply ingests one normalized event from `topic` and projects it onto the
// counter families dictated by the F008 metric registry. Returns nil on a
// successful (or intentionally dropped — see RejectReason) ingest; the only
// way Apply returns a non-nil error is an unrecoverable internal-state bug,
// not malformed input.
func (a *Aggregator) Apply(topic string, payload []byte) error {
	// 1) Topic validation. An unknown topic gets its own reject reason so
	//    operators can distinguish topology misconfiguration from
	//    schema-version drift. Done BEFORE LintEvent so the LintEvent
	//    "unknown topic" signal does not collide with payload-shape
	//    rejections.
	if _, err := telemetrycontracts.Schema(topic); err != nil {
		if errors.Is(err, telemetrycontracts.ErrUnknownTopic) {
			a.bumpReject(ReasonUnknownTopic)
			return nil
		}
		a.bumpReject(ReasonSchema)
		return nil
	}

	// 2) Decode.
	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		a.bumpReject(ReasonDecode)
		return nil
	}

	// 3) Defence-in-depth: check forbidden fields directly. F008 lint
	//    catches this too, but we never want to count a payload that smuggled
	//    user content into a tenant's series, even if the upstream lint was
	//    accidentally bypassed.
	if hasForbiddenField(event) {
		a.bumpReject(ReasonForbidden)
		return nil
	}

	// 4) Schema-lint the envelope.
	result := schemalint.LintEvent(topic, payload)
	if !result.OK() {
		a.bumpReject(ReasonSchema)
		return nil
	}

	// 5) Project onto counter families.
	contribs, reason := projectionsFor(topic, event)
	if reason != "" {
		a.bumpReject(reason)
		return nil
	}
	if len(contribs) == 0 {
		// No-op (event valid but carried no quantitative signal we record).
		a.mu.Lock()
		a.processed++
		a.mu.Unlock()
		return nil
	}

	// 6) Cardinality enforcement per contribution.
	for _, c := range contribs {
		if err := a.lintContribution(c); err != nil {
			a.bumpReject(ReasonCardinality)
			return nil
		}
	}

	// 7) Commit.
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range contribs {
		by, ok := a.counters[c.Metric]
		if !ok {
			by = make(map[string]*CounterSeries, 8)
			a.counters[c.Metric] = by
		}
		key := labelFingerprint(c.Labels)
		series, ok := by[key]
		if !ok {
			series = &CounterSeries{Labels: copyLabels(c.Labels)}
			by[key] = series
		}
		series.Sum += c.Value
	}
	a.processed++
	return nil
}

// lintContribution validates a single projection against the F008 registry:
// metric name is known, every label is authorized, mandatory labels are
// present. Returns a non-nil error on the first violation so the caller can
// bump the cardinality-reject counter without committing.
func (a *Aggregator) lintContribution(c Contribution) error {
	_, ok := a.metricByName[c.Metric]
	if !ok {
		return fmt.Errorf("aggregator: unknown metric %q", c.Metric)
	}
	result := schemalint.LintMetric(c.Metric, c.Labels)
	if !result.OK() {
		return fmt.Errorf("aggregator: cardinality reject for %s: %v", c.Metric, result.Error())
	}
	return nil
}

func (a *Aggregator) bumpReject(reason RejectReason) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejected[reason]++
}

// Snapshot returns a deterministic, sorted, copy-on-read view of the
// aggregator state. Used by the exposition layer to render `/metrics` and
// by tests to assert equality between two cold-start runs.
type Snapshot struct {
	Counters  []SnapshotMetric
	Rejected  map[RejectReason]int64
	Processed int64
}

// SnapshotMetric is one metric family with all of its series.
type SnapshotMetric struct {
	Name        string
	Type        metricscontracts.Type
	Description string
	Series      []SnapshotSeries
}

// SnapshotSeries is one labelled series for a metric family.
type SnapshotSeries struct {
	Labels map[string]string
	Sum    float64
}

// Snapshot returns the current aggregator state in a deterministic form.
// Metric names are emitted in alphabetical order; series within a metric are
// sorted by their canonical label fingerprint. Two aggregators that received
// the same set of events in any order produce equal Snapshots.
func (a *Aggregator) Snapshot() Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	names := make([]string, 0, len(a.counters))
	for n := range a.counters {
		names = append(names, n)
	}
	sort.Strings(names)

	out := Snapshot{
		Counters:  make([]SnapshotMetric, 0, len(names)),
		Rejected:  make(map[RejectReason]int64, len(a.rejected)),
		Processed: a.processed,
	}
	for k, v := range a.rejected {
		out.Rejected[k] = v
	}

	for _, name := range names {
		by := a.counters[name]
		meta := a.metricByName[name]
		keys := make([]string, 0, len(by))
		for k := range by {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		series := make([]SnapshotSeries, 0, len(keys))
		for _, k := range keys {
			s := by[k]
			series = append(series, SnapshotSeries{
				Labels: copyLabels(s.Labels),
				Sum:    s.Sum,
			})
		}

		out.Counters = append(out.Counters, SnapshotMetric{
			Name:        name,
			Type:        meta.Type,
			Description: meta.Description,
			Series:      series,
		})
	}
	return out
}

// labelFingerprint produces a stable string from a label map. The format is
// `k1=v1\x00k2=v2\x00...` with keys sorted; the value is opaque to callers
// but guaranteed identical for identical label maps regardless of insert
// order.
func labelFingerprint(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// forbiddenFieldSet is fetched once at package init: ForbiddenFields()
// allocates a fresh slice per call, and hasForbiddenField recurses per JSON
// node on the ingest hot path.
var forbiddenFieldSet = metricscontracts.ForbiddenFields()

// hasForbiddenField walks the event recursively looking for any key in
// metricscontracts.ForbiddenFields(). Matching is case-insensitive on the
// key only (values are not inspected, deliberately — we don't want even a
// substring of the payload retained anywhere in the aggregator path).
func hasForbiddenField(node interface{}) bool {
	switch n := node.(type) {
	case map[string]interface{}:
		for k, v := range n {
			lower := strings.ToLower(k)
			for _, banned := range forbiddenFieldSet {
				if lower == banned {
					return true
				}
			}
			if hasForbiddenField(v) {
				return true
			}
		}
	case []interface{}:
		for _, v := range n {
			if hasForbiddenField(v) {
				return true
			}
		}
	}
	return false
}
