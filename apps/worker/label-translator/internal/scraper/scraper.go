// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package scraper fetches and parses the upstream llm-usage-exporter /metrics
// endpoint, returning a flat slice of typed samples the translator can iterate
// over without depending on the Prometheus exposition format directly.
//
// The exporter exposes counters and gauges with the canonical label set
// {provider, model, tenant, tenancy_id, ...}. The scraper is intentionally
// permissive about which metric names exist — the translator decides what to
// do with each one. This keeps the scraper a pure I/O + parse layer.
package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// MetricKind is the narrow subset of Prometheus types this scraper exposes
// to the translator. Histograms and summaries are intentionally elided for
// the v0 cut; if upstream introduces them the translator will see the metric
// in Histograms() / Summaries() once we extend this package.
type MetricKind int

const (
	// KindCounter is a monotonically non-decreasing counter sample.
	KindCounter MetricKind = iota
	// KindGauge is an instantaneous gauge sample.
	KindGauge
)

// Sample is one decoded metric sample. Labels are sorted by key on extraction
// so the translator can use the slice directly as part of a deterministic
// cache key.
type Sample struct {
	Name   string
	Kind   MetricKind
	Labels []Label
	Value  float64
}

// Label is one metric label pair.
type Label struct {
	Name  string
	Value string
}

// Scraper performs a single HTTP GET + exposition parse per Scrape() call.
// It is safe to share one Scraper across goroutines (the underlying
// http.Client is). The caller is responsible for cadence.
type Scraper struct {
	URL    string
	Client *http.Client
}

// New constructs a Scraper with sane defaults.
func New(url string, timeout time.Duration) *Scraper {
	return &Scraper{
		URL: url,
		Client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Scrape fetches the upstream /metrics endpoint and returns every decoded
// counter / gauge sample. Histograms and summaries are skipped in v0.
func (s *Scraper) Scrape(ctx context.Context) ([]Sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: build request: %w", err)
	}
	req.Header.Set("Accept", string(expfmt.NewFormat(expfmt.TypeTextPlain)))

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scraper: GET %s: %w", s.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Drain a bounded prefix so the connection can be reused.
		_, _ = io.CopyN(io.Discard, resp.Body, 1<<14)
		return nil, fmt.Errorf("scraper: GET %s: status %d", s.URL, resp.StatusCode)
	}

	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("scraper: parse exposition: %w", err)
	}

	var out []Sample
	for _, fam := range families {
		kind, ok := mapKind(fam.GetType())
		if !ok {
			continue
		}
		for _, m := range fam.GetMetric() {
			labels := extractLabels(m.GetLabel())
			value := metricValue(kind, m)
			out = append(out, Sample{
				Name:   fam.GetName(),
				Kind:   kind,
				Labels: labels,
				Value:  value,
			})
		}
	}
	return out, nil
}

// Get returns the first sample-label value matching name, or "" when absent.
func (s Sample) Get(name string) string {
	for _, l := range s.Labels {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

func mapKind(t dto.MetricType) (MetricKind, bool) {
	switch t {
	case dto.MetricType_COUNTER:
		return KindCounter, true
	case dto.MetricType_GAUGE:
		return KindGauge, true
	case dto.MetricType_SUMMARY, dto.MetricType_UNTYPED,
		dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
		// The exporter only emits counters and gauges; skip the rest.
		return 0, false
	default:
		return 0, false
	}
}

func metricValue(kind MetricKind, m *dto.Metric) float64 {
	switch kind {
	case KindCounter:
		if c := m.GetCounter(); c != nil {
			return c.GetValue()
		}
	case KindGauge:
		if g := m.GetGauge(); g != nil {
			return g.GetValue()
		}
	}
	return 0
}

// extractLabels copies the protobuf label pairs into the package's plain
// struct slice. Labels are sorted by name so two samples with the same set
// produce the same slice ordering regardless of upstream encoding.
func extractLabels(pairs []*dto.LabelPair) []Label {
	if len(pairs) == 0 {
		return nil
	}
	out := make([]Label, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, Label{Name: p.GetName(), Value: p.GetValue()})
	}
	// Sort in place — slice is short, insertion sort is plenty.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
