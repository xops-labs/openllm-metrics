// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer publishes llm.runtime.normalized events to the
// streaming bus on behalf of the gateway.
//
// The emitter is intentionally fire-and-forget at the call site: a failed
// publish increments a counter but never blocks the inbound response. The
// privacy invariant lives here too — Emit only accepts the
// RuntimeEvent struct, whose fields are all non-payload (tokens, latency,
// status, labels). There is no escape hatch to pipe response bodies onto
// the bus.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

// SourceService identifies this gateway to downstream consumers.
const SourceService = "apps/gateway"

// RuntimeEvent is the wire payload for the llm.runtime.normalized topic.
// Field order and JSON tags match the F008 schema byte-for-byte; see
// packages/contracts/telemetry/go/schemas/llm.runtime.normalized.v1.json.
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

// Emitter is the surface the gateway observer depends on.
type Emitter interface {
	Emit(ctx context.Context, ev RuntimeEvent) error
	Close()
}

// BusEmitter publishes to llm.runtime.normalized via the franz-go producer.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
	logger   *slog.Logger
}

// New constructs a BusEmitter wired to the runtime topic.
func New(producer *busclient.Producer, logger *slog.Logger) *BusEmitter {
	if logger == nil {
		logger = slog.Default()
	}
	return &BusEmitter{
		producer: producer,
		topic:    telemetrycontracts.TopicRuntimeNormalized,
		logger:   logger,
	}
}

// Emit marshals and produces a single runtime event. The error is returned
// to the caller so the observer can count publish failures; callers should
// NOT block the inbound response on this call.
func (e *BusEmitter) Emit(ctx context.Context, ev RuntimeEvent) error {
	if ev.EventID == "" {
		return fmt.Errorf("busproducer: event_id is required")
	}
	if ev.Tenant == "" {
		return fmt.Errorf("busproducer: tenant is required")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("busproducer: marshal event %s: %w", ev.EventID, err)
	}
	if err := e.producer.ProduceEvent(ctx, e.topic, ev.EventID, ev.Tenant, payload); err != nil {
		return fmt.Errorf("busproducer: produce %s: %w", ev.EventID, err)
	}
	return nil
}

// Close releases the underlying producer.
func (e *BusEmitter) Close() {
	if e.producer != nil {
		e.producer.Close()
	}
}

// NoopEmitter is used when the bus is intentionally disabled (no brokers
// configured). Calls succeed silently so the request path is unchanged.
type NoopEmitter struct{}

// Emit on a NoopEmitter is a no-op that returns nil.
func (NoopEmitter) Emit(_ context.Context, _ RuntimeEvent) error { return nil }

// Close on a NoopEmitter is a no-op.
func (NoopEmitter) Close() {}
