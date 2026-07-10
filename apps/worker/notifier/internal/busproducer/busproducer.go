// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer publishes audit.event.v1 events for every config
// mutation on notification channels and rules. F031 (the append-only audit
// ledger) consumes from this topic and hash-chains the rows.
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
)

// SourceService identifies the notifier to F031 and to operators.
const SourceService = "apps/worker/notifier"

// AuditEvent is the v1 envelope. The schema is intentionally permissive
// (additional fields allowed) — F031 owns the canonical schema and will
// validate on its side.
type AuditEvent struct {
	SchemaVersion string         `json:"schema_version"`
	EventID       string         `json:"event_id"`
	TenantID      string         `json:"tenant_id"`
	Actor         string         `json:"actor,omitempty"`
	Action        string         `json:"action"`
	Resource      string         `json:"resource"`
	ResourceID    string         `json:"resource_id"`
	SourceService string         `json:"source_service"`
	OccurredAt    string         `json:"occurred_at"`
	Details       map[string]any `json:"details,omitempty"`
}

// AuditEmitter publishes audit events.
type AuditEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs an AuditEmitter wired to the configured topic.
func New(producer *busclient.Producer, topic string) *AuditEmitter {
	return &AuditEmitter{producer: producer, topic: topic}
}

// Emit publishes a single audit event. Failures are returned to the caller;
// the HTTP handler logs them but does not roll back the DB mutation — the
// audit ledger is best-effort downstream of the source of truth.
func (e *AuditEmitter) Emit(ctx context.Context, ev AuditEvent) error {
	if ev.EventID == "" {
		ev.EventID = uuid.NewString()
	}
	if ev.SchemaVersion == "" {
		ev.SchemaVersion = "1"
	}
	if ev.SourceService == "" {
		ev.SourceService = SourceService
	}
	if ev.OccurredAt == "" {
		ev.OccurredAt = time.Now().UTC().Format(time.RFC3339)
	}
	if ev.TenantID == "" {
		return fmt.Errorf("busproducer: tenant_id is required")
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("busproducer: marshal: %w", err)
	}
	if err := e.producer.ProduceEvent(ctx, e.topic, ev.EventID, ev.TenantID, payload); err != nil {
		return fmt.Errorf("busproducer: produce: %w", err)
	}
	return nil
}

// Close releases the underlying producer.
func (e *AuditEmitter) Close() {
	if e.producer != nil {
		e.producer.Close()
	}
}
