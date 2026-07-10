// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer publishes llm.usage.reconciled events to the bus.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/ingester"
)

// BusEmitter publishes to the canonical llm.usage.reconciled topic.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs a BusEmitter wired to the reconciled topic.
func New(producer *busclient.Producer) *BusEmitter {
	return &BusEmitter{
		producer: producer,
		topic:    telemetrycontracts.TopicUsageReconciled,
	}
}

// Emit marshals and produces a single reconciled event.
func (e *BusEmitter) Emit(ctx context.Context, ev ingester.ReconciledEvent) error {
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
