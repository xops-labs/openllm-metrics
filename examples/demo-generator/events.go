// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Event wire shapes for the demo generator. Field order and JSON tags match
// the F008 contracts byte-for-byte — every schema in
// packages/contracts/telemetry/go/schemas/ declares
// `additionalProperties: false`, so the structs here must never grow a field
// the schema does not know about. events_test.go enforces that against the
// embedded contract schemas.
package main

// RuntimeEvent is the wire payload for the llm.runtime.normalized topic.
// Mirrors apps/gateway/internal/busproducer.RuntimeEvent (the gateway's
// emitter struct) — the demo generator impersonates proxy-mode traffic.
type RuntimeEvent struct {
	SchemaVersion string `json:"schema_version"`
	EventID       string `json:"event_id"`
	SourceMode    string `json:"source_mode"`
	SourceService string `json:"source_service"`
	RequestIDHash string `json:"request_id_hash"`
	Provider      string `json:"provider"`
	Model         string `json:"model"`
	Operation     string `json:"operation"`
	Tenant        string `json:"tenant"`
	Team          string `json:"team"`
	App           string `json:"app,omitempty"`
	Env           string `json:"env"`
	Project       string `json:"project,omitempty"`
	Region        string `json:"region,omitempty"`
	Status        string `json:"status"`
	StatusCode    int    `json:"status_code,omitempty"`
	ErrorType     string `json:"error_type,omitempty"`
	LatencyUS     int64  `json:"latency_us"`
	TTFBUS        int64  `json:"ttfb_us,omitempty"`
	InputTokens   *int   `json:"input_tokens,omitempty"`
	OutputTokens  *int   `json:"output_tokens,omitempty"`
	TotalTokens   *int   `json:"total_tokens,omitempty"`
	RetryCount    int    `json:"retry_count,omitempty"`
	IsStreaming   bool   `json:"is_streaming,omitempty"`
	RecordedAt    string `json:"recorded_at"`
	TraceID       string `json:"trace_id,omitempty"`
	SpanID        string `json:"span_id,omitempty"`
}

// UsageEvent is the wire payload for the llm.usage.normalized topic — the
// pull-mode billing rollup shape the metrics-endpoint projects
// llm_cost_usd_total from.
type UsageEvent struct {
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

// ReconciledEvent is the wire payload for the llm.usage.reconciled topic —
// the billing-truth leg the reconciler joins against runtime cost estimates.
type ReconciledEvent struct {
	SchemaVersion               string `json:"schema_version"`
	EventID                     string `json:"event_id"`
	SourceEventID               string `json:"source_event_id"`
	Source                      string `json:"source"`
	SourceService               string `json:"source_service"`
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	Tenant                      string `json:"tenant"`
	Team                        string `json:"team"`
	App                         string `json:"app,omitempty"`
	Env                         string `json:"env"`
	Project                     string `json:"project,omitempty"`
	Region                      string `json:"region,omitempty"`
	BillingAccountID            string `json:"billing_account_id"`
	ServiceName                 string `json:"service_name,omitempty"`
	ChargeCategory              string `json:"charge_category,omitempty"`
	ReconciledCostUSDMinorUnits int64  `json:"reconciled_cost_usd_minor_units"`
	ListCostUSDMinorUnits       int64  `json:"list_cost_usd_minor_units,omitempty"`
	PricingCurrency             string `json:"pricing_currency,omitempty"`
	PeriodStart                 string `json:"period_start"`
	PeriodEnd                   string `json:"period_end"`
	ReconciledAt                string `json:"reconciled_at"`
}
