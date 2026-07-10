// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer publishes `quota.risk.v1` snapshot events to the
// streaming bus.
//
// The event is intentionally signal-only: it carries the latest usedRatio,
// secondsToReset, and the transparent linear risk score for a given
// (tenant, provider, model, region, kind) tuple. It carries no decision,
// no recommendation, and no routing hint — those are outside this signal event.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
)

// Event is the wire shape published under topic `llm.quota.risk.v1`.
type Event struct {
	SchemaVersion   string  `json:"schema_version"`
	EventID         string  `json:"event_id"`
	Source          string  `json:"source"`
	SourceService   string  `json:"source_service"`
	Tenant          string  `json:"tenant"`
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	Region          string  `json:"region,omitempty"`
	Kind            string  `json:"kind"`
	Remaining       int64   `json:"remaining"`
	Limit           int64   `json:"limit"`
	UsedRatio       float64 `json:"used_ratio"`
	HasDenominator  bool    `json:"has_denominator"`
	SecondsToReset  float64 `json:"seconds_to_reset"`
	RiskScore       float64 `json:"risk_score"`
	ObservedAt      string  `json:"observed_at"`
	SnapshotEmitted string  `json:"snapshot_emitted_at"`
}

// SchemaVersion is the wire-format version of `quota.risk.v1`.
const SchemaVersion = "1"

// SourceService is recorded on every emitted event so downstream knows
// which worker produced it.
const SourceService = "apps/worker/quota-risk"

// Emitter is the narrow surface the main loop depends on.
type Emitter interface {
	Emit(ctx context.Context, ev Event) error
	Close()
}

// BusEmitter publishes to the configured output topic.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs a BusEmitter wired to `topic`.
func New(producer *busclient.Producer, topic string) *BusEmitter {
	return &BusEmitter{producer: producer, topic: topic}
}

// Emit marshals and produces a single risk event.
func (e *BusEmitter) Emit(ctx context.Context, ev Event) error {
	if ev.EventID == "" {
		return fmt.Errorf("busproducer: event_id is required")
	}
	if ev.Tenant == "" {
		return fmt.Errorf("busproducer: tenant is required")
	}
	if ev.SnapshotEmitted == "" {
		ev.SnapshotEmitted = time.Now().UTC().Format(time.RFC3339)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("busproducer: marshal %s: %w", ev.EventID, err)
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
