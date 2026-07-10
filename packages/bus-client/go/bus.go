// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busclient provides idiomatic Go producer and consumer wrappers
// for the OpenLLM Metrics streaming bus (Redpanda / Kafka).
//
// Trace context is propagated via W3C traceparent/tracestate message headers.
// Every event must include an event_id for idempotency and a tenant_id for
// cross-tenant isolation enforcement.
package busclient

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// HeaderTraceparent is the W3C traceparent header key.
	HeaderTraceparent = "traceparent"
	// HeaderTracestate is the W3C tracestate header key.
	HeaderTracestate = "tracestate"
	// HeaderEventID is the idempotency key header.
	HeaderEventID = "x-event-id"
	// HeaderTenantID ensures every event carries a tenant identifier.
	HeaderTenantID = "x-tenant-id"
)

// Config holds connection configuration for the streaming bus.
type Config struct {
	// Brokers is the list of Kafka/Redpanda broker addresses.
	Brokers []string
	// ClientID is the Kafka client identifier.
	ClientID string
}

// injectTraceContext writes W3C trace context into a kgo.RecordHeaders slice.
func injectTraceContext(ctx context.Context, headers []kgo.RecordHeader) []kgo.RecordHeader {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	for k, v := range carrier {
		headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	return headers
}

// extractTraceContext reads W3C trace context from a kgo.Record and returns a
// child context carrying the extracted span.
func extractTraceContext(ctx context.Context, record *kgo.Record) context.Context {
	carrier := propagation.MapCarrier{}
	for _, h := range record.Headers {
		carrier[h.Key] = string(h.Value)
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// ErrMissingTenantID is returned when a record is produced without a tenant_id.
var ErrMissingTenantID = fmt.Errorf("busclient: tenant_id header is required on every event")

// ErrMissingEventID is returned when a record is produced without an event_id.
var ErrMissingEventID = fmt.Errorf("busclient: event_id header is required for idempotency")
