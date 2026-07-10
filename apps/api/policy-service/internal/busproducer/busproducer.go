// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer emits audit.event.v1 messages to the streaming bus
// on every policy mutation. F029 only PUBLISHES audit events — durable
// storage of the audit ledger is owned by F031 (audit-event-storage),
// which consumes from this same topic.
//
// This package never observes or interprets policy evaluation outcomes.
// It records WHO did WHAT to WHICH policy, not WHY the platform later
// allowed or denied a request.
package busproducer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
)

// AuditEvent is the v1 audit envelope this service publishes. The shape is
// kept narrow on purpose: F031 owns the canonical AuditEvent contract and
// will widen it. This struct contains only the fields F029 mutations need.
type AuditEvent struct {
	// EventID is a UUID v4 unique to this event.
	EventID string `json:"event_id"`
	// SchemaVersion is the audit envelope version. F029 emits v1.
	SchemaVersion string `json:"schema_version"`
	// EmittedAt is the producer-side timestamp (RFC 3339).
	EmittedAt string `json:"emitted_at"`
	// TenantID is the multi-tenant boundary. Required for routing on the
	// consumer side.
	TenantID string `json:"tenant_id"`
	// Actor identifies the human or service principal performing the action.
	Actor string `json:"actor"`
	// Action is the verb (e.g. "policy.created", "policy.version_appended",
	// "policy.soft_deleted").
	Action string `json:"action"`
	// Resource identifies the target (e.g. "policy:<uuid>").
	Resource string `json:"resource"`
	// Metadata is a free-form opaque map (e.g. {"version": 3, "name": "..."}).
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Action constants used by handlers. Keeping them here makes search-by-action
// auditable.
const (
	ActionCreated         = "policy.created"
	ActionVersionAppended = "policy.version_appended"
	ActionSoftDeleted     = "policy.soft_deleted"
)

// Producer is the audit-event publisher. The zero value is non-functional;
// callers should construct it via New or NewDisabled.
type Producer struct {
	inner   *busclient.Producer
	topic   string
	enabled bool
}

// Config is the constructor input.
type Config struct {
	Brokers  []string
	ClientID string
	Topic    string
}

// New constructs a live Producer backed by the streaming bus.
func New(cfg Config) (*Producer, error) {
	if cfg.Topic == "" {
		return nil, errors.New("busproducer: Config.Topic is required")
	}
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("busproducer: Config.Brokers is required")
	}
	inner, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Brokers,
		ClientID: cfg.ClientID,
	})
	if err != nil {
		return nil, fmt.Errorf("busproducer: %w", err)
	}
	return &Producer{inner: inner, topic: cfg.Topic, enabled: true}, nil
}

// NewDisabled returns a no-op Producer. Useful in local dev where the bus is
// not available; mutations still proceed but no audit event is emitted.
func NewDisabled() *Producer {
	return &Producer{enabled: false}
}

// Close releases the underlying client. Safe to call on a disabled producer.
func (p *Producer) Close() {
	if p == nil || !p.enabled || p.inner == nil {
		return
	}
	p.inner.Close()
}

// Emit publishes an audit event for the given action and resource. It is
// best-effort: a publish failure is returned to the caller but should NOT
// be treated as a request failure. The handler logs the error and moves on.
func (p *Producer) Emit(ctx context.Context, tenantID uuid.UUID, actor, action, resource string, metadata map[string]any) error {
	if p == nil || !p.enabled {
		return nil
	}
	event := AuditEvent{
		EventID:       uuid.New().String(),
		SchemaVersion: "v1",
		EmittedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		TenantID:      tenantID.String(),
		Actor:         actor,
		Action:        action,
		Resource:      resource,
		Metadata:      metadata,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("busproducer: marshal: %w", err)
	}
	if err := p.inner.ProduceEvent(ctx, p.topic, event.EventID, event.TenantID, payload); err != nil {
		return fmt.Errorf("busproducer: produce: %w", err)
	}
	return nil
}
