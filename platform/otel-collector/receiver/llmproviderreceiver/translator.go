// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package llmproviderreceiver

import (
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/yasvanth511/openllm-metrics-oss/platform/otel-collector/receiver/llmproviderreceiver/internal/metadata"
)

// RuntimeEvent mirrors the F008 llm.runtime.normalized.v1 contract just
// enough to drive metric translation. JSON tags match the schema byte-for-byte;
// optional fields use `omitempty` semantics but are decoded as-is.
//
// We intentionally keep this struct local to the receiver instead of importing
// the canonical Go shape from packages/contracts/telemetry/go — the receiver
// is a separate Go module so the OTel Collector Builder can pull it without
// cross-module replace directives. Keeping a local mirror is the contrib
// convention; the contract test in packages/telemetry/schema-lint enforces
// alignment.
type RuntimeEvent struct {
	SchemaVersion string `json:"schema_version"`
	EventID       string `json:"event_id"`
	SourceMode    string `json:"source_mode"`
	SourceService string `json:"source_service"`

	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Operation string `json:"operation"`

	Tenant  string `json:"tenant"`
	Team    string `json:"team"`
	App     string `json:"app,omitempty"`
	Env     string `json:"env"`
	Project string `json:"project,omitempty"`
	Region  string `json:"region,omitempty"`

	Status     string `json:"status"`
	StatusCode int    `json:"status_code,omitempty"`
	ErrorType  string `json:"error_type,omitempty"`

	LatencyUs    int64 `json:"latency_us"`
	TTFBUs       int64 `json:"ttfb_us,omitempty"`
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
	TotalTokens  int64 `json:"total_tokens,omitempty"`
	RetryCount   int64 `json:"retry_count,omitempty"`

	RoutingReason  string `json:"routing_reason,omitempty"`
	PolicyName     string `json:"policy_name,omitempty"`
	FallbackReason string `json:"fallback_reason,omitempty"`
	FromModel      string `json:"from_model,omitempty"`
	ToModel        string `json:"to_model,omitempty"`

	// EstimatedCostUSDMinorUnits is filled when the runtime event was joined
	// against the pricing catalog upstream of this receiver. The field is not
	// in the v1 schema yet; emit only when present so older producers do not
	// surface zero-cost spikes.
	EstimatedCostUSDMinorUnits int64 `json:"estimated_cost_usd_minor_units,omitempty"`

	RecordedAt string `json:"recorded_at"`
	TraceID    string `json:"trace_id,omitempty"`
	SpanID     string `json:"span_id,omitempty"`
}

// Metric names emitted by this receiver. The first two follow the OTel GenAI
// semantic conventions verbatim; llm_estimated_cost_usd is a project-specific
// extension because cost is not yet in semconv.
const (
	metricTokenUsage    = "gen_ai.client.token.usage"
	metricOpDuration    = "gen_ai.client.operation.duration"
	metricEstimatedCost = "llm_estimated_cost_usd"
)

// Attribute keys. gen_ai.* keys come from the GenAI semconv spec; the
// multi-tenant set (tenant/team/app/env/project) is project-specific because
// semconv has no equivalent yet.
const (
	attrGenAISystem        = "gen_ai.system"
	attrGenAIRequestModel  = "gen_ai.request.model"
	attrGenAIResponseModel = "gen_ai.response.model"
	attrGenAIOperation     = "gen_ai.operation.name"
	attrGenAITokenType     = "gen_ai.token.type"
	attrGenAIErrorType     = "error.type"
	attrServerAddress      = "server.address"

	attrTenant         = "tenant"
	attrTeam           = "team"
	attrApp            = "app"
	attrEnv            = "env"
	attrProject        = "project"
	attrRoute          = "route"
	attrStatus         = "status"
	attrRoutingReason  = "routing_reason"
	attrPolicyName     = "policy_name"
	attrFallbackReason = "fallback_reason"

	tokenTypeInput  = "input"
	tokenTypeOutput = "output"
)

// Translator converts canonical runtime events into OTLP metrics. It is
// stateless — every Translate call produces a self-contained pmetric.Metrics
// document so the receiver loop can run concurrently if the framework ever
// allows that.
type Translator struct{}

// NewTranslator returns an initialized translator. The constructor exists for
// symmetry with the rest of the codebase; right now there is nothing to wire.
func NewTranslator() *Translator {
	return &Translator{}
}

// Translate folds a slice of runtime events into pmetric.Metrics. Events with
// the same resource attribute set (tenant/team/app/env/project/provider/model/
// route) share a ResourceMetrics so the downstream exporter sees the natural
// aggregation grain.
func (t *Translator) Translate(events []RuntimeEvent) pmetric.Metrics {
	out := pmetric.NewMetrics()
	if len(events) == 0 {
		return out
	}

	// Bucket events by the resource attribute fingerprint. Map key is a
	// canonical string of the resource attrs in a fixed order so two events
	// that differ only in label order still share a resource.
	buckets := make(map[string]*resourceBucket, len(events))
	for _, ev := range events {
		key := resourceKey(ev)
		b, ok := buckets[key]
		if !ok {
			b = &resourceBucket{events: make([]RuntimeEvent, 0, 4)}
			buckets[key] = b
		}
		b.events = append(b.events, ev)
	}

	for _, b := range buckets {
		t.appendResource(out, b.events)
	}
	return out
}

type resourceBucket struct {
	events []RuntimeEvent
}

// appendResource adds one ResourceMetrics block to `out` containing all
// metrics for a single resource fingerprint.
func (t *Translator) appendResource(out pmetric.Metrics, events []RuntimeEvent) {
	if len(events) == 0 {
		return
	}
	rm := out.ResourceMetrics().AppendEmpty()
	setResourceAttrs(rm.Resource().Attributes(), events[0])

	sm := rm.ScopeMetrics().AppendEmpty()
	sm.Scope().SetName(metadata.ScopeName)
	sm.Scope().SetVersion(metadata.ScopeVersion)

	tokenHist := newHistogram(sm, metricTokenUsage, "LLM token usage per request.", "{token}")
	durHist := newHistogram(sm, metricOpDuration, "LLM client operation duration.", "s")
	var costSum pmetric.Metric
	costInitialized := false

	for _, ev := range events {
		ts := recordedAtNano(ev.RecordedAt)

		if ev.InputTokens > 0 {
			dp := tokenHist.Histogram().DataPoints().AppendEmpty()
			setTokenPoint(dp, ev, ts, tokenTypeInput, ev.InputTokens)
		}
		if ev.OutputTokens > 0 {
			dp := tokenHist.Histogram().DataPoints().AppendEmpty()
			setTokenPoint(dp, ev, ts, tokenTypeOutput, ev.OutputTokens)
		}

		if ev.LatencyUs > 0 {
			dp := durHist.Histogram().DataPoints().AppendEmpty()
			dp.SetTimestamp(ts)
			dp.SetStartTimestamp(ts)
			seconds := float64(ev.LatencyUs) / 1_000_000.0
			dp.SetSum(seconds)
			dp.SetCount(1)
			setPointAttrs(dp.Attributes(), ev)
		}

		if ev.EstimatedCostUSDMinorUnits > 0 {
			if !costInitialized {
				costSum = sm.Metrics().AppendEmpty()
				costSum.SetName(metricEstimatedCost)
				costSum.SetDescription("Estimated LLM call cost in USD, derived from the pricing catalog. Project extension to gen_ai.* semconv.")
				costSum.SetUnit("USD")
				sumMetric := costSum.SetEmptySum()
				sumMetric.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
				sumMetric.SetIsMonotonic(true)
				costInitialized = true
			}
			dp := costSum.Sum().DataPoints().AppendEmpty()
			dp.SetTimestamp(ts)
			dp.SetStartTimestamp(ts)
			// minor units (cents) -> USD
			dp.SetDoubleValue(float64(ev.EstimatedCostUSDMinorUnits) / 100.0)
			setPointAttrs(dp.Attributes(), ev)
		}
	}

	// Drop placeholder metrics that received no data points so the exporter
	// surface stays clean.
	dropEmptyMetrics(sm)
}

// newHistogram allocates a histogram metric with delta aggregation temporality
// — the receiver emits per-event observations, not cumulative state, so delta
// is the only correct choice. Downstream exporters (Prometheus, OTLP/HTTP) can
// rebucket if cumulative is preferred.
func newHistogram(sm pmetric.ScopeMetrics, name, desc, unit string) pmetric.Metric {
	m := sm.Metrics().AppendEmpty()
	m.SetName(name)
	m.SetDescription(desc)
	m.SetUnit(unit)
	h := m.SetEmptyHistogram()
	h.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
	return m
}

// setTokenPoint fills a histogram data point for a single token observation.
func setTokenPoint(dp pmetric.HistogramDataPoint, ev RuntimeEvent, ts pcommon.Timestamp, tokenType string, count int64) {
	dp.SetTimestamp(ts)
	dp.SetStartTimestamp(ts)
	dp.SetSum(float64(count))
	dp.SetCount(1)
	setPointAttrs(dp.Attributes(), ev)
	dp.Attributes().PutStr(attrGenAITokenType, tokenType)
}

// setResourceAttrs writes the multi-tenant resource fingerprint. Every metric
// in this ResourceMetrics block inherits these attributes; per-point attrs
// (token type, error, status) go on the data points themselves.
func setResourceAttrs(attrs pcommon.Map, ev RuntimeEvent) {
	putIfNotEmpty(attrs, attrGenAISystem, ev.Provider)
	putIfNotEmpty(attrs, attrGenAIRequestModel, ev.Model)
	putIfNotEmpty(attrs, attrGenAIOperation, ev.Operation)
	putIfNotEmpty(attrs, attrTenant, ev.Tenant)
	putIfNotEmpty(attrs, attrTeam, ev.Team)
	putIfNotEmpty(attrs, attrApp, ev.App)
	putIfNotEmpty(attrs, attrEnv, ev.Env)
	putIfNotEmpty(attrs, attrProject, ev.Project)
	putIfNotEmpty(attrs, attrServerAddress, ev.Region)
	// "route" is the post-routing model; if no routing happened it equals model.
	route := ev.ToModel
	if route == "" {
		route = ev.Model
	}
	putIfNotEmpty(attrs, attrRoute, route)
}

// setPointAttrs writes per-data-point attributes. These are the dimensions
// that vary within one resource fingerprint — status, error category, the
// routing-reason chain. Keep the set small to avoid label explosion.
func setPointAttrs(attrs pcommon.Map, ev RuntimeEvent) {
	putIfNotEmpty(attrs, attrStatus, ev.Status)
	putIfNotEmpty(attrs, attrGenAIErrorType, ev.ErrorType)
	putIfNotEmpty(attrs, attrRoutingReason, ev.RoutingReason)
	putIfNotEmpty(attrs, attrPolicyName, ev.PolicyName)
	putIfNotEmpty(attrs, attrFallbackReason, ev.FallbackReason)
	if ev.FromModel != "" && ev.FromModel != ev.Model {
		attrs.PutStr("gen_ai.request.from_model", ev.FromModel)
	}
	if ev.ToModel != "" && ev.ToModel != ev.Model {
		attrs.PutStr(attrGenAIResponseModel, ev.ToModel)
	}
}

func putIfNotEmpty(attrs pcommon.Map, key, value string) {
	if value == "" {
		return
	}
	attrs.PutStr(key, value)
}

// resourceKey returns a deterministic fingerprint of the resource attributes
// so two events with the same tuple share a ResourceMetrics block.
func resourceKey(ev RuntimeEvent) string {
	route := ev.ToModel
	if route == "" {
		route = ev.Model
	}
	// Fixed-order concatenation — cheaper than building a hash and adequate
	// for the cardinality the receiver sees per poll.
	return ev.Tenant + "|" + ev.Team + "|" + ev.App + "|" + ev.Env + "|" +
		ev.Project + "|" + ev.Provider + "|" + ev.Model + "|" + route + "|" +
		ev.Operation + "|" + ev.Region
}

// recordedAtNano parses the RFC3339 timestamp on the canonical event. If the
// value is missing or malformed we fall back to "now" — losing the exact
// timestamp is preferable to dropping the entire event.
func recordedAtNano(s string) pcommon.Timestamp {
	if s == "" {
		return pcommon.NewTimestampFromTime(time.Now().UTC())
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pcommon.NewTimestampFromTime(time.Now().UTC())
	}
	return pcommon.NewTimestampFromTime(t)
}

// dropEmptyMetrics removes metrics that carry no data points. We allocate the
// histograms up-front because the per-event branches share them, then trim
// here so the wire payload is minimal.
func dropEmptyMetrics(sm pmetric.ScopeMetrics) {
	sm.Metrics().RemoveIf(func(m pmetric.Metric) bool {
		switch m.Type() {
		case pmetric.MetricTypeHistogram:
			return m.Histogram().DataPoints().Len() == 0
		case pmetric.MetricTypeSum:
			return m.Sum().DataPoints().Len() == 0
		case pmetric.MetricTypeGauge:
			return m.Gauge().DataPoints().Len() == 0
		case pmetric.MetricTypeEmpty, pmetric.MetricTypeExponentialHistogram,
			pmetric.MetricTypeSummary:
			// The translator never emits these types; keep them as-is.
			return false
		default:
			return false
		}
	})
}
