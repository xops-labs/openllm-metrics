// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer wraps packages/bus-client to publish translated
// llm.usage.normalized events. Mirrors the pattern from the OpenAI poller
// so the schema-version header and tenant-required invariants live in one
// place per worker rather than scattered across the call sites.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/translator"
)

// Emitter is the narrow surface the translator depends on; tests substitute
// an in-memory implementation that captures emitted events.
type Emitter interface {
	Emit(ctx context.Context, ev translator.NormalizedEvent) error
	Close()
}

// BusEmitter publishes to the canonical llm.usage.normalized topic.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs a BusEmitter wired to the canonical normalized topic.
func New(producer *busclient.Producer) *BusEmitter {
	return &BusEmitter{
		producer: producer,
		topic:    telemetrycontracts.TopicUsageNormalized,
	}
}

// Emit marshals and produces a single event.
func (e *BusEmitter) Emit(ctx context.Context, ev translator.NormalizedEvent) error {
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
