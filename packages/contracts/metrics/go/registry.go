// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metricscontracts is the canonical registry of every llm_* metric the
// OpenLLM Metrics platform emits.
//
// F008 (Common Operational Telemetry Schema) owns this registry. Every
// service that emits a Prometheus metric or labels a log/span looks up the
// metric here first; the schema-lint package rejects unknown metric names and
// unauthorized labels at CI time.
//
// Adding a metric: extend Registry() with the new entry, document its
// cardinality budget, and confirm the labels are a subset of the canonical
// label set in Labels().
//
// Removing or renaming a metric is a breaking change — bump a metric set
// version when one is unavoidable.
package metricscontracts

import (
	"fmt"
	"sort"
)

// Type enumerates Prometheus metric types this registry supports.
type Type string

const (
	TypeCounter   Type = "counter"
	TypeGauge     Type = "gauge"
	TypeHistogram Type = "histogram"
)

// Unit names the dimension carried by a metric value. Units match Prometheus /
// OpenMetrics conventions where one exists.
type Unit string

const (
	// UnitOne is a dimensionless count (requests, events, retries).
	UnitOne Unit = "1"
	// UnitTokens is an integer token count.
	UnitTokens Unit = "tokens"
	// UnitUSD is a USD floating-point amount (Prometheus counter or gauge).
	UnitUSD Unit = "USD"
	// UnitMilliseconds is the latency unit for metrics.
	UnitMilliseconds Unit = "ms"
	// UnitSeconds is used by OTel GenAI duration histograms.
	UnitSeconds Unit = "s"
	// UnitRatio is a bounded [0,1] score value.
	UnitRatio Unit = "ratio"
)

// Label identifies a Prometheus label name. The registry pins exactly which
// labels each metric may carry.
type Label string

// Canonical label set (F008 §4, vision §9).
const (
	LabelProvider   Label = "provider"
	LabelModel      Label = "model"
	LabelOperation  Label = "operation"
	LabelApp        Label = "app"
	LabelTeam       Label = "team"
	LabelEnv        Label = "env"
	LabelTenant     Label = "tenant"
	LabelProject    Label = "project"
	LabelStatusCode Label = "status_code"
	LabelErrorType  Label = "error_type"
	LabelRegion     Label = "region"

	// Optional routing-context labels.
	LabelRoutingReason  Label = "routing_reason"
	LabelPolicyName     Label = "policy_name"
	LabelFallbackReason Label = "fallback_reason"
	LabelFromModel      Label = "from_model"
	LabelToModel        Label = "to_model"
)

// Labels returns the canonical mandatory + optional label set. Order is
// stable for deterministic iteration.
func Labels() []Label {
	return []Label{
		LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
		LabelEnv, LabelTenant, LabelProject, LabelStatusCode, LabelErrorType,
		LabelRegion, LabelRoutingReason, LabelPolicyName, LabelFallbackReason,
		LabelFromModel, LabelToModel,
	}
}

// MandatoryLabels are the labels every llm_* core metric MUST carry on every
// observation. Per the multi-tenant invariant, tenant is in this list.
func MandatoryLabels() []Label {
	return []Label{LabelProvider, LabelModel, LabelTenant, LabelEnv}
}

// ForbiddenFields are payload / label / attribute keys that the schema-lint
// package rejects. They protect against accidental LLM-payload exfiltration
// (F008 §11).
func ForbiddenFields() []string {
	return []string{
		"prompt", "completion", "input", "output", "messages", "embedding",
		"content", "request_body", "response_body",
	}
}

// Metric is a single registry entry describing one llm_* metric.
type Metric struct {
	// Name is the Prometheus metric name (must start with "llm_").
	Name string
	// Type is counter / gauge / histogram.
	Type Type
	// Unit is the value dimension.
	Unit Unit
	// Description is a short human-readable summary.
	Description string
	// AllowedLabels is the exact set of label names the metric may carry.
	// MandatoryLabels are implicitly included and re-listed for clarity.
	AllowedLabels []Label
	// CardinalityBudget is the maximum number of time series (label-value
	// combinations) this metric is expected to produce in a single tenant per
	// environment per 24h window. Exceeding this triggers an SRE alert and a
	// linter-side warning when fixture data crosses it.
	CardinalityBudget int
	// Introduced names the feature that introduced this metric (for
	// traceability in the OTel mapping doc).
	Introduced string
}

// Registry returns the canonical metric set. The slice is freshly built on
// every call so callers may safely sort or filter it without mutating shared
// state.
func Registry() []Metric {
	core := []Metric{
		{
			Name:        "llm_requests_total",
			Type:        TypeCounter,
			Unit:        UnitOne,
			Description: "Total LLM API requests observed by the platform.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelStatusCode,
				LabelErrorType, LabelRegion,
			},
			CardinalityBudget: 50000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_input_tokens_total",
			Type:        TypeCounter,
			Unit:        UnitTokens,
			Description: "Cumulative input tokens consumed across all providers.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelRegion,
			},
			CardinalityBudget: 30000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_output_tokens_total",
			Type:        TypeCounter,
			Unit:        UnitTokens,
			Description: "Cumulative output tokens generated across all providers.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelRegion,
			},
			CardinalityBudget: 30000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_total_tokens_total",
			Type:        TypeCounter,
			Unit:        UnitTokens,
			Description: "Cumulative total tokens (input + output) across all providers.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelRegion,
			},
			CardinalityBudget: 30000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_cost_usd_total",
			Type:        TypeCounter,
			Unit:        UnitUSD,
			Description: "Cumulative cost in USD (float). Event payloads carry integer minor units.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelRegion,
			},
			CardinalityBudget: 30000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_errors_total",
			Type:        TypeCounter,
			Unit:        UnitOne,
			Description: "Provider API errors normalized into the canonical error_type set.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelStatusCode,
				LabelErrorType, LabelRegion,
			},
			CardinalityBudget: 60000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_retries_total",
			Type:        TypeCounter,
			Unit:        UnitOne,
			Description: "Number of retried provider requests, by reason.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelErrorType, LabelRegion,
			},
			CardinalityBudget: 40000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_timeouts_total",
			Type:        TypeCounter,
			Unit:        UnitOne,
			Description: "Number of provider requests that timed out.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelRegion,
			},
			CardinalityBudget: 30000,
			Introduced:        "F008",
		},
		{
			Name:        "llm_rate_limit_events_total",
			Type:        TypeCounter,
			Unit:        UnitOne,
			Description: "Provider rate-limit events (HTTP 429 / quota exceeded).",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelOperation, LabelApp, LabelTeam,
				LabelEnv, LabelTenant, LabelProject, LabelStatusCode, LabelRegion,
			},
			CardinalityBudget: 30000,
			Introduced:        "F008",
		},
		{
			// F009 introduced this exporter-internal metric for poller-side
			// observability (network / circuit / bus failures broken down by
			// reason). F010 promotes it into the canonical registry so the
			// metrics-endpoint service can also surface aggregator-side error
			// reasons consistently. Reason values are a closed enum (network,
			// rate_limited, circuit_open, decode, 5xx, 4xx, bus, normalize,
			// schema, cardinality, forbidden) reflected through error_type.
			// Model is allowed (mandatory across registry) but exporters that
			// have no model context emit it as "_unknown" to keep cardinality
			// bounded.
			Name:        "llm_provider_api_errors_total",
			Type:        TypeCounter,
			Unit:        UnitOne,
			Description: "Provider API and pipeline errors observed by pollers / aggregators, by reason.",
			AllowedLabels: []Label{
				LabelProvider, LabelModel, LabelTenant, LabelEnv, LabelErrorType,
			},
			CardinalityBudget: 200,
			Introduced:        "F009",
		},
	}
	return core
}

// FindByName returns the registry entry for the given metric name. Returns
// ErrUnknownMetric when no entry exists; callers should treat this as a hard
// failure rather than a warning.
func FindByName(name string) (Metric, error) {
	for _, m := range Registry() {
		if m.Name == name {
			return m, nil
		}
	}
	return Metric{}, fmt.Errorf("%w: %s", ErrUnknownMetric, name)
}

// Names returns every registered metric name in sorted order. Useful for
// generating documentation and dashboards.
func Names() []string {
	reg := Registry()
	out := make([]string, 0, len(reg))
	for _, m := range reg {
		out = append(out, m.Name)
	}
	sort.Strings(out)
	return out
}

// IsAllowedLabel reports whether `label` is part of the metric's allowed set.
func (m Metric) IsAllowedLabel(label Label) bool {
	for _, l := range m.AllowedLabels {
		if l == label {
			return true
		}
	}
	return false
}

// ErrUnknownMetric is returned by FindByName for an unregistered metric name.
var ErrUnknownMetric = fmt.Errorf("metricscontracts: unknown metric")
