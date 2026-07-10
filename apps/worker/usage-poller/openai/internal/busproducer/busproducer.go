// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer wraps packages/bus-client to publish normalized
// usage events to the F008-owned `llm.usage.normalized` topic.
//
// The wrapper exists so the poller has ONE place that knows about the
// topic name, the schema version header, and the EventID/TenantID
// invariants. Test code can swap in a stub Emitter without touching the
// real Kafka client.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/adapter"
)

// Emitter is the narrow interface the poller depends on. Production wires
// it to BusEmitter (Kafka/Redpanda); tests use an in-memory implementation.
type Emitter interface {
	Emit(ctx context.Context, ev adapter.NormalizedEvent) error
	Close()
}

// BusEmitter publishes events to the streaming bus via packages/bus-client.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs a BusEmitter wired to the canonical usage topic.
func New(producer *busclient.Producer) *BusEmitter {
	return &BusEmitter{
		producer: producer,
		topic:    telemetrycontracts.TopicUsageNormalized,
	}
}

// Emit marshals the event and produces it. Returns an error if the
// underlying produce fails; callers should record IncProviderAPIError
// with reason="bus" and IncScrapeFailure on error.
func (e *BusEmitter) Emit(ctx context.Context, ev adapter.NormalizedEvent) error {
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

// Close releases the underlying producer client.
func (e *BusEmitter) Close() {
	if e.producer != nil {
		e.producer.Close()
	}
}
