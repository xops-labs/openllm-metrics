// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package consumer wires the streaming bus to the audit store.
//
// Each consumed message is:
//
//  1. Decoded as an audit.event.v1 envelope.
//  2. Validated for required fields (tenant_id, action, actor.type).
//  3. Redacted: a deny-list of sensitive keys (authorization, api_key,
//     password, token, bearer, prompt, completion) is stripped from the
//     `payload` and `actor` objects recursively. The redact step is
//     defense-in-depth — producers are expected to redact before sending.
//  4. Appended via the Store. The store wraps the append in a SERIALIZABLE
//     transaction so the per-tenant chain stays linear.
//
// Idempotency: producers MUST set a stable event_id header (UUIDv7). The
// service does NOT dedup on event_id at this phase — the audit ledger's
// uniqueness comes from the per-tenant monotonically increasing id and the
// chain hash. Duplicates produce duplicate rows; the chain is still valid.
// A future enhancement is a (tenant_id, source_event_id) UNIQUE index for
// stricter idempotency.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/store"
)

// Sink is the narrow Store surface the consumer depends on.
type Sink interface {
	Append(ctx context.Context, in store.AppendInput) (store.Entry, error)
}

// Counter is the narrow metrics surface the consumer depends on. The audit
// service's metrics.Registry satisfies it; tests fake it.
type Counter interface {
	IncAppend()
	IncAppendFailure()
	IncRedactionReject()
	IncValidationReject()
}

// Config wires the bus connection.
type Config struct {
	Brokers  []string
	ClientID string
	Group    string
	Topic    string
}

// Consumer drains the audit topic into the Sink.
type Consumer struct {
	cfg     Config
	client  *kgo.Client
	sink    Sink
	metrics Counter
}

// New constructs a Consumer. The Kafka client uses ConsumeResetOffset =
// AtStart so a brand-new group fully replays the topic — events that
// failed to land during downtime are caught up.
func New(cfg Config, sink Sink, metrics Counter) (*Consumer, error) {
	if cfg.Topic == "" {
		return nil, fmt.Errorf("consumer: topic is required")
	}
	if sink == nil {
		return nil, fmt.Errorf("consumer: sink is required")
	}
	if metrics == nil {
		return nil, fmt.Errorf("consumer: metrics is required")
	}
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	}
	if cfg.Group != "" {
		opts = append(opts, kgo.ConsumerGroup(cfg.Group))
	}
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("consumer: new client: %w", err)
	}
	return &Consumer{cfg: cfg, client: client, sink: sink, metrics: metrics}, nil
}

// Close releases the underlying client.
func (c *Consumer) Close() {
	if c.client != nil {
		c.client.Close()
	}
}

// Run drains the topic until ctx cancels. Each record is decoded, redacted,
// and appended. A decode or append error increments the appropriate counter
// but does NOT halt the loop — an audit-service that goes deaf because one
// producer sent a malformed event is worse than one that drops the bad event.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		fetches.EachRecord(func(rec *kgo.Record) {
			c.handle(ctx, rec)
		})
		if err := c.client.CommitUncommittedOffsets(ctx); err != nil {
			// Best effort: the next restart will reprocess uncommitted
			// records. Duplicates in the ledger are acceptable — the
			// chain stays valid either way.
			_ = err
		}
	}
}

// handle decodes and appends one record.
func (c *Consumer) handle(ctx context.Context, rec *kgo.Record) {
	tenantHeader := headerValue(rec, busclient.HeaderTenantID)
	in, err := decodeRecord(rec, tenantHeader)
	if err != nil {
		c.metrics.IncValidationReject()
		return
	}
	in.Actor = redactMap(in.Actor)
	in.Payload = redactMap(in.Payload)
	if containsForbidden(in.Payload) || containsForbidden(in.Actor) {
		c.metrics.IncRedactionReject()
		return
	}
	if _, err := c.sink.Append(ctx, in); err != nil {
		c.metrics.IncAppendFailure()
		return
	}
	c.metrics.IncAppend()
}

// auditEvent matches packages/contracts/audit/v1/audit-event.schema.json.
type auditEvent struct {
	SchemaVersion string         `json:"schema_version"`
	EventID       string         `json:"event_id"`
	TenantID      string         `json:"tenant_id"`
	Actor         map[string]any `json:"actor"`
	Action        string         `json:"action"`
	Resource      map[string]any `json:"resource"`
	Payload       map[string]any `json:"payload"`
	CreatedAt     string         `json:"created_at"`
	TraceID       string         `json:"trace_id,omitempty"`
}

// decodeRecord converts a kgo.Record into a store.AppendInput. The tenant
// header is treated as authoritative if present; the JSON tenant_id is the
// fallback.
func decodeRecord(rec *kgo.Record, tenantHeader string) (store.AppendInput, error) {
	var ev auditEvent
	if err := json.Unmarshal(rec.Value, &ev); err != nil {
		return store.AppendInput{}, fmt.Errorf("consumer: decode: %w", err)
	}
	tenant := tenantHeader
	if tenant == "" {
		tenant = ev.TenantID
	}
	if tenant == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: tenant_id is required")
	}
	if ev.Action == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: action is required")
	}
	if ev.Actor == nil {
		ev.Actor = map[string]any{"type": "system"}
	}
	if _, ok := ev.Actor["type"]; !ok {
		ev.Actor["type"] = "system"
	}
	// CreatedAt is recorded by the database via NOW(); the producer-side
	// timestamp is preserved inside the payload for skew analysis.
	if ev.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, ev.CreatedAt); err == nil {
			if ev.Payload == nil {
				ev.Payload = map[string]any{}
			}
			if _, exists := ev.Payload["_producer_ts"]; !exists {
				ev.Payload["_producer_ts"] = ev.CreatedAt
			}
		}
	}
	return store.AppendInput{
		TenantID: tenant,
		Actor:    ev.Actor,
		Action:   ev.Action,
		Resource: ev.Resource,
		Payload:  ev.Payload,
	}, nil
}

func headerValue(rec *kgo.Record, key string) string {
	for _, h := range rec.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// forbiddenKeys are stripped from any nested object before persistence.
// The list intentionally errs on the side of "redact too much" — producers
// that need a specific key can rename it (e.g. `password_hash_method`).
var forbiddenKeys = []string{
	"authorization",
	"api_key",
	"apikey",
	"password",
	"passwd",
	"secret",
	"token",
	"bearer",
	"client_secret",
	"prompt",
	"completion",
}

// redactMap walks m recursively and strips any key whose lowercase form
// matches forbiddenKeys. Nested maps and slices of maps are descended into.
func redactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isForbiddenKey(k) {
			continue
		}
		out[k] = redactValue(v)
	}
	return out
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return redactMap(x)
	case []any:
		for i, el := range x {
			x[i] = redactValue(el)
		}
		return x
	default:
		return v
	}
}

// containsForbidden walks m recursively and returns true if any forbidden
// key survived the redact step. Used as a final defense-in-depth check.
func containsForbidden(m map[string]any) bool {
	for k, v := range m {
		if isForbiddenKey(k) {
			return true
		}
		switch x := v.(type) {
		case map[string]any:
			if containsForbidden(x) {
				return true
			}
		case []any:
			for _, el := range x {
				if mm, ok := el.(map[string]any); ok {
					if containsForbidden(mm) {
						return true
					}
				}
			}
		}
	}
	return false
}

func isForbiddenKey(k string) bool {
	lk := strings.ToLower(k)
	for _, f := range forbiddenKeys {
		if lk == f {
			return true
		}
	}
	return false
}
