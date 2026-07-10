// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer publishes cost.estimated.v1 events to the streaming
// bus. The topic defaults to "llm.cost.estimated" — a new bus surface owned
// by the cost-mapper. Downstream consumers (FinOps dashboards, the
// reconciliation reader) subscribe to this topic alongside
// llm.usage.reconciled to render drift.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/mapper"
)

// TopicCostEstimated is the canonical bus topic for runtime-side cost
// estimates produced by the cost-mapper. It deliberately mirrors the naming
// convention of the F008 topics in packages/contracts/telemetry/go so the
// schema-lint contract module can adopt it on the next bump.
const TopicCostEstimated = "llm.cost.estimated"

// Emitter is the narrow surface the cost-mapper depends on.
type Emitter interface {
	Emit(ctx context.Context, ev mapper.EstimatedEvent) error
	Close()
}

// BusEmitter publishes to the configured estimated topic.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs a BusEmitter. An empty topic falls back to
// TopicCostEstimated.
func New(producer *busclient.Producer, topic string) *BusEmitter {
	if topic == "" {
		topic = TopicCostEstimated
	}
	return &BusEmitter{producer: producer, topic: topic}
}

// Emit marshals and produces a single cost.estimated event.
func (e *BusEmitter) Emit(ctx context.Context, ev mapper.EstimatedEvent) error {
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
