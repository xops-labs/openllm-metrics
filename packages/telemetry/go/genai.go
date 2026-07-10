// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// OpenTelemetry GenAI semantic-convention attribute keys. Values match the
// alignment table in platform/observability/otel_alignment.md and the upstream
// spec at https://opentelemetry.io/docs/specs/semconv/gen-ai/.
//
// Use these constants instead of inlining the strings; the alignment doc is
// the source of truth and the constants are the wiring.
const (
	GenAISystem        = attribute.Key("gen_ai.system")
	GenAIRequestModel  = attribute.Key("gen_ai.request.model")
	GenAIOperationName = attribute.Key("gen_ai.operation.name")
	GenAIResponseModel = attribute.Key("gen_ai.response.model")
	GenAITokenType     = attribute.Key("gen_ai.token.type")
	GenAIErrorType     = attribute.Key("error.type")
	GenAIServerAddress = attribute.Key("server.address")
)

// GenAI metric instrument names per the OTel GenAI spec.
const (
	MetricClientOperationDuration = "gen_ai.client.operation.duration"
	MetricClientTokenUsage        = "gen_ai.client.token.usage"
	MetricServerRequestDuration   = "gen_ai.server.request.duration"
	MetricServerTimeToFirstToken  = "gen_ai.server.time_to_first_token"
)

// TokenType values for the gen_ai.token.type attribute.
const (
	TokenTypeInput  = "input"
	TokenTypeOutput = "output"
)

// GenAIInstruments bundles the four first-class OTel GenAI histograms. A
// service obtains one via NewGenAIInstruments at boot and shares it across
// request handlers.
//
// Project-specific llm_* counters (requests, tokens, cost USD) are not in
// this bundle; F008/F010 own those.
type GenAIInstruments struct {
	ClientOperationDuration metric.Float64Histogram
	ClientTokenUsage        metric.Int64Histogram
	ServerRequestDuration   metric.Float64Histogram
	ServerTimeToFirstToken  metric.Float64Histogram
}

// NewGenAIInstruments creates the four OTel GenAI histograms on the global
// meter. Returns an error if any instrument fails to initialize so callers
// can fail-fast at boot rather than discover the gap mid-flight.
func NewGenAIInstruments(meterName string) (*GenAIInstruments, error) {
	m := otel.Meter(meterName)

	clientOp, err := m.Float64Histogram(
		MetricClientOperationDuration,
		metric.WithDescription("LLM client operation duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create %s: %w", MetricClientOperationDuration, err)
	}

	tokenUsage, err := m.Int64Histogram(
		MetricClientTokenUsage,
		metric.WithDescription("LLM token usage per request"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create %s: %w", MetricClientTokenUsage, err)
	}

	serverReq, err := m.Float64Histogram(
		MetricServerRequestDuration,
		metric.WithDescription("Gateway-mode LLM request duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create %s: %w", MetricServerRequestDuration, err)
	}

	ttft, err := m.Float64Histogram(
		MetricServerTimeToFirstToken,
		metric.WithDescription("Gateway-mode time to first token"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: create %s: %w", MetricServerTimeToFirstToken, err)
	}

	return &GenAIInstruments{
		ClientOperationDuration: clientOp,
		ClientTokenUsage:        tokenUsage,
		ServerRequestDuration:   serverReq,
		ServerTimeToFirstToken:  ttft,
	}, nil
}

// GenAIAttributes is the canonical attribute bundle for a single LLM call.
// Only fields the SDK actually populates are encoded; zero-value fields are
// omitted from the resulting attribute set.
type GenAIAttributes struct {
	System        string // e.g. "openai", "anthropic"; lowercased per OTel spec
	RequestModel  string // canonical model name, e.g. "gpt-4o-mini"
	ResponseModel string // model returned by the provider
	Operation     string // "chat", "embedding", etc.
	ServerAddress string // provider endpoint region
	ErrorType     string // normalized error category, empty on success
}

// keyValues returns the non-empty fields as a fresh attribute slice. Capacity
// is 7 so RecordTokenUsage can append gen_ai.token.type without reallocating.
func (a GenAIAttributes) keyValues() []attribute.KeyValue {
	kvs := make([]attribute.KeyValue, 0, 7)
	if a.System != "" {
		kvs = append(kvs, GenAISystem.String(a.System))
	}
	if a.RequestModel != "" {
		kvs = append(kvs, GenAIRequestModel.String(a.RequestModel))
	}
	if a.ResponseModel != "" {
		kvs = append(kvs, GenAIResponseModel.String(a.ResponseModel))
	}
	if a.Operation != "" {
		kvs = append(kvs, GenAIOperationName.String(a.Operation))
	}
	if a.ServerAddress != "" {
		kvs = append(kvs, GenAIServerAddress.String(a.ServerAddress))
	}
	if a.ErrorType != "" {
		kvs = append(kvs, GenAIErrorType.String(a.ErrorType))
	}
	return kvs
}

// AttributeSet returns the attributes as an attribute.Set suitable for
// passing to histogram.Record. Empty fields are skipped so the resulting
// label set never carries a zero-value provider or model.
func (a GenAIAttributes) AttributeSet() attribute.Set {
	return attribute.NewSet(a.keyValues()...)
}

// RecordClientOperation records a client-side operation duration with the
// supplied GenAI attributes. The duration is converted to seconds per the
// OTel spec.
func (g *GenAIInstruments) RecordClientOperation(ctx context.Context, d time.Duration, a GenAIAttributes) {
	g.ClientOperationDuration.Record(ctx, d.Seconds(), metric.WithAttributeSet(a.AttributeSet()))
}

// RecordTokenUsage records a token-count observation with token-type
// distinguishing input from output (per OTel gen_ai.token.type).
func (g *GenAIInstruments) RecordTokenUsage(ctx context.Context, count int64, tokenType string, a GenAIAttributes) {
	kvs := append(a.keyValues(), GenAITokenType.String(tokenType))
	g.ClientTokenUsage.Record(ctx, count, metric.WithAttributes(kvs...))
}
