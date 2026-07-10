// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package exposition serializes an aggregator.Snapshot into the Prometheus
// text exposition format (version 0.0.4).
//
// We do not depend on the prometheus/client_golang library. The format is
// small, the serializer is hot but trivial, and avoiding the dependency
// matches the choice made by F009's poller so the two services share a
// uniform footprint and review surface.
//
// References:
//   - https://prometheus.io/docs/instrumenting/exposition_formats/#text-format-details
//   - https://github.com/prometheus/docs/blob/main/content/docs/instrumenting/exposition_formats.md
package exposition

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/aggregator"
)

// ContentType is the Prometheus text-format content type the HTTP handler
// MUST advertise on /metrics.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

// SelfMetrics carries the small set of aggregator-internal series the
// exposition layer surfaces alongside the F008 metrics. Kept narrow so the
// public surface stays auditable.
type SelfMetrics struct {
	// Rejected is the per-reason rejected-event counter snapshot.
	Rejected map[aggregator.RejectReason]int64
	// Processed is the total events successfully applied.
	Processed int64
	// SeriesCount is the live unique-series count (memory-budget signal).
	SeriesCount int
}

// Write emits the Prometheus text exposition of `snap` followed by the
// self-metrics in `self`. Output is deterministic: metric names sorted,
// label-value series within a metric sorted by fingerprint (matches the
// aggregator snapshot order).
func Write(w io.Writer, snap aggregator.Snapshot, self SelfMetrics) error {
	for _, m := range snap.Counters {
		if err := writeMetricHeader(w, m.Name, m.Description, string(m.Type)); err != nil {
			return err
		}
		for _, s := range m.Series {
			if err := writeSeries(w, m.Name, s.Labels, formatFloat(s.Sum)); err != nil {
				return err
			}
		}
	}
	return writeSelfMetrics(w, self)
}

func writeSelfMetrics(w io.Writer, self SelfMetrics) error {
	if err := writeMetricHeader(w,
		"llm_aggregator_rejected_events_total",
		"Events the aggregator rejected before counting them, by reason.",
		"counter"); err != nil {
		return err
	}
	// Emit every known reason, even with value 0, so dashboards have a
	// stable series set to query on.
	for _, reason := range aggregator.AllRejectReasons() {
		labels := map[string]string{"reason": string(reason)}
		if err := writeSeries(w, "llm_aggregator_rejected_events_total", labels, formatInt(self.Rejected[reason])); err != nil {
			return err
		}
	}

	if err := writeMetricHeader(w,
		"llm_aggregator_processed_events_total",
		"Events the aggregator successfully applied to its in-memory state.",
		"counter"); err != nil {
		return err
	}
	if err := writeSeries(w, "llm_aggregator_processed_events_total", nil, formatInt(self.Processed)); err != nil {
		return err
	}

	if err := writeMetricHeader(w,
		"llm_aggregator_series_total",
		"Distinct (metric, labelset) pairs currently held in memory.",
		"gauge"); err != nil {
		return err
	}
	return writeSeries(w, "llm_aggregator_series_total", nil, formatInt(int64(self.SeriesCount)))
}

func writeMetricHeader(w io.Writer, name, help, typ string) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(help)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "# TYPE %s %s\n", name, typ)
	return err
}

func writeSeries(w io.Writer, name string, labels map[string]string, value string) error {
	if len(labels) == 0 {
		_, err := fmt.Fprintf(w, "%s %s\n", name, value)
		return err
	}
	// Stable label ordering.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(labels[k]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())
	return err
}

// formatFloat renders a counter value in a way Prometheus will parse. Whole
// values stay integer-shaped (no trailing ".0") to keep diffs human-readable
// in tests; fractional values use the shortest round-trippable form.
func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return formatInt(int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

// escapeHelp escapes the Prometheus HELP text: backslash and newline must
// be backslash-escaped per the spec.
func escapeHelp(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

// escapeLabelValue escapes a Prometheus label value: backslash, double-quote,
// and newline must be backslash-escaped.
func escapeLabelValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
