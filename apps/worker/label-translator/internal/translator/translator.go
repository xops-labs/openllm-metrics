// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package translator converts a scraped batch of upstream exporter samples
// into canonical llm.usage.normalized events with rich {tenant, team, app,
// env, project} labels.
//
// State model: counters are cumulative across scrapes, so the translator
// keeps the previous value per (sample-name, label-set) and emits the
// delta. Gauges are emitted as the instantaneous value (no delta). Cold
// start: the first scrape primes the previous-value table and emits zero
// events — this avoids back-filling a huge synthetic spike on boot.
package translator

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/scraper"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/store"
)

// SourceService is recorded on every emitted event so downstream consumers
// know which package produced it.
const SourceService = "apps/worker/label-translator"

// Names of the upstream metrics this translator decodes. The set is small
// and intentional — adding a new metric to consume requires a code change so
// schema-impacting fields cannot creep in silently.
const (
	metricInputTokens  = "llm_input_tokens_total"
	metricOutputTokens = "llm_output_tokens_total"
	metricCostUSD      = "llm_cost_usd_total"
	metricRequests     = "llm_requests_total"
)

// NormalizedEvent mirrors the F008 llm.usage.normalized payload. JSON tags
// match the schema byte-for-byte so json.Marshal produces a schema-compliant
// document. Kept private to this package so producers cannot construct
// inconsistent shapes.
type NormalizedEvent struct {
	SchemaVersion     string `json:"schema_version"`
	EventID           string `json:"event_id"`
	SourceEventID     string `json:"source_event_id"`
	SourceMode        string `json:"source_mode"`
	Source            string `json:"source"`
	SourceService     string `json:"source_service"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Operation         string `json:"operation"`
	Tenant            string `json:"tenant"`
	Team              string `json:"team"`
	App               string `json:"app,omitempty"`
	Env               string `json:"env"`
	Project           string `json:"project,omitempty"`
	Region            string `json:"region,omitempty"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
	CostUSDMinorUnits int64  `json:"cost_usd_minor_units"`
	RequestCount      int64  `json:"request_count,omitempty"`
	PeriodStart       string `json:"period_start"`
	PeriodEnd         string `json:"period_end"`
	NormalizedAt      string `json:"normalized_at"`
}

// Defaults are the fallback labels applied when an inbound tuple has no
// mapping row. The translator still emits the event so dashboards stay
// continuous; an alert on llm_label_translation_unmapped_total notifies
// operators to populate the mapping.
type Defaults struct {
	Tenant string
	Team   string
	Env    string
}

// Translator is the pure transform from scraper.Sample slices to events.
// It keeps the previous-counter table needed to compute window deltas.
type Translator struct {
	mappings store.Mappings
	defaults Defaults

	mu          sync.Mutex
	prevCounter map[string]float64 // keyed by counter+labels string
	prevTime    time.Time

	now func() time.Time
}

// New constructs a Translator with the supplied mapping store and defaults.
func New(mappings store.Mappings, defaults Defaults) *Translator {
	return &Translator{
		mappings:    mappings,
		defaults:    defaults,
		prevCounter: make(map[string]float64, 64),
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// Result reports per-cycle outcomes the metrics package uses to drive
// counters/gauges. Counts are over the BATCH of events emitted in this cycle.
type Result struct {
	Emitted  int
	Skipped  int // first-scrape priming, zero-delta windows, etc.
	Unmapped int // events emitted with default labels because no mapping existed
	Dropped  int // events dropped (e.g. missing tenant fallback)
}

// Translate consumes one full batch of samples (a single scrape) and returns
// the set of canonical events to publish. The caller is responsible for
// publishing — this keeps the package free of I/O.
func (t *Translator) Translate(ctx context.Context, samples []scraper.Sample) ([]NormalizedEvent, Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	periodEnd := now
	periodStart := t.prevTime
	if periodStart.IsZero() {
		periodStart = periodEnd
	}
	primingScrape := t.prevTime.IsZero()
	t.prevTime = periodEnd

	// Group samples by (tenant_external_id, tenancy_id, provider, model) so
	// one canonical event captures input + output + cost + requests for the
	// same upstream tuple. This is the natural granularity for the bus
	// topic and the FinOps dashboards.
	type aggKey struct {
		provider         string
		tenantExternalID string
		tenancyID        string
		model            string
	}
	type agg struct {
		inputTokens  int64
		outputTokens int64
		costMinor    int64
		requests     int64
	}
	buckets := make(map[aggKey]*agg, len(samples))

	for _, s := range samples {
		provider := s.Get("provider")
		if provider == "" {
			continue
		}
		k := aggKey{
			provider:         provider,
			tenantExternalID: s.Get("tenant"),
			tenancyID:        s.Get("tenancy_id"),
			model:            s.Get("model"),
		}
		b, ok := buckets[k]
		if !ok {
			b = &agg{}
			buckets[k] = b
		}
		delta := t.delta(s)
		if primingScrape {
			// On the priming scrape we still want to seed prevCounter so the
			// next scrape produces a real delta, but we DO NOT roll the
			// upstream cumulative value into a synthetic window.
			continue
		}
		switch s.Name {
		case metricInputTokens:
			b.inputTokens += int64(delta)
		case metricOutputTokens:
			b.outputTokens += int64(delta)
		case metricCostUSD:
			b.costMinor += int64(delta * 100.0)
		case metricRequests:
			b.requests += int64(delta)
		}
	}

	if primingScrape {
		return nil, Result{Skipped: len(samples)}, nil
	}

	var (
		out    []NormalizedEvent
		res    Result
		nowStr = periodEnd.Format(time.RFC3339)
		startS = periodStart.Format(time.RFC3339)
		endS   = periodEnd.Format(time.RFC3339)
	)

	for k, b := range buckets {
		if b.inputTokens == 0 && b.outputTokens == 0 && b.costMinor == 0 && b.requests == 0 {
			res.Skipped++
			continue
		}

		mapping, err := t.mappings.Lookup(ctx, store.Key{
			Provider:         k.provider,
			TenantExternalID: k.tenantExternalID,
			TenancyID:        k.tenancyID,
		})
		if err != nil {
			return nil, res, fmt.Errorf("translator: mapping lookup: %w", err)
		}

		tenant, team, app, env, project, region := t.resolveLabels(mapping)
		if !mapping.Found {
			res.Unmapped++
		}
		if tenant == "" {
			// Hard-fail: F005 requires tenant. Drop and bump dropped counter.
			res.Dropped++
			continue
		}

		ev := NormalizedEvent{
			SchemaVersion:     telemetrycontracts.SchemaVersion,
			EventID:           uuid.NewString(),
			SourceEventID:     buildSourceEventID(k.provider, k.tenantExternalID, k.tenancyID, k.model, startS, endS),
			SourceMode:        "pull",
			Source:            telemetrycontracts.SourceExporter,
			SourceService:     SourceService,
			Provider:          k.provider,
			Model:             canonicalModel(k.model),
			Operation:         operationFor(k.model),
			Tenant:            tenant,
			Team:              team,
			App:               app,
			Env:               env,
			Project:           project,
			Region:            region,
			InputTokens:       b.inputTokens,
			OutputTokens:      b.outputTokens,
			TotalTokens:       b.inputTokens + b.outputTokens,
			CostUSDMinorUnits: b.costMinor,
			RequestCount:      b.requests,
			PeriodStart:       startS,
			PeriodEnd:         endS,
			NormalizedAt:      nowStr,
		}
		out = append(out, ev)
		res.Emitted++
	}
	return out, res, nil
}

// delta computes the change in a counter since the previous scrape. For
// gauges, we return the instantaneous value. A counter reset (current <
// previous) collapses to current — this matches Prometheus rate() semantics
// for monotonic counters that wrapped or were re-initialized upstream.
func (t *Translator) delta(s scraper.Sample) float64 {
	if s.Kind != scraper.KindCounter {
		return s.Value
	}
	key := counterKey(s)
	prev, ok := t.prevCounter[key]
	t.prevCounter[key] = s.Value
	if !ok || s.Value < prev {
		return s.Value
	}
	return s.Value - prev
}

func (t *Translator) resolveLabels(m store.Mapping) (tenant, team, app, env, project, region string) {
	if !m.Found {
		return t.defaults.Tenant, t.defaults.Team, "", t.defaults.Env, "", ""
	}
	return m.TenantSlug, m.TeamSlug, m.AppSlug, m.CanonicalEnv, m.CanonicalProject, m.CanonicalRegion
}

func counterKey(s scraper.Sample) string {
	var b strings.Builder
	b.WriteString(s.Name)
	for _, l := range s.Labels {
		b.WriteByte('|')
		b.WriteString(l.Name)
		b.WriteByte('=')
		b.WriteString(l.Value)
	}
	return b.String()
}

func buildSourceEventID(provider, tenantExternal, tenancy, model, start, end string) string {
	return fmt.Sprintf("exporter:%s:%s:%s:%s:%s:%s", provider, tenantExternal, tenancy, model, start, end)
}

func canonicalModel(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// operationFor mirrors the F009 mapping so the same model name always
// produces the same operation regardless of which producer path it took.
func operationFor(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "text-embedding"), strings.Contains(m, "embedding"):
		return "embedding"
	case strings.HasPrefix(m, "dall-e"), strings.Contains(m, "image"):
		return "image"
	case strings.HasPrefix(m, "whisper"), strings.HasPrefix(m, "tts"):
		return "audio"
	case strings.Contains(m, "moderation"):
		return "moderation"
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "o1-"), strings.HasPrefix(m, "o3-"),
		strings.HasPrefix(m, "claude-"), strings.HasPrefix(m, "gemini-"), strings.Contains(m, "chat"):
		return "chat"
	case strings.Contains(m, "instruct"), strings.HasPrefix(m, "davinci"), strings.HasPrefix(m, "babbage"):
		return "completion"
	default:
		return "other"
	}
}
