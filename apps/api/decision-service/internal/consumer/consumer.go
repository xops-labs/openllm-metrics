// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package consumer wires the streaming bus to the decision store.
//
// Each consumed message is:
//
//  1. Decoded as a routing.decision.v1 envelope.
//  2. Validated for required fields (id, tenant_id, request.provider/model,
//     decision.provider/model, decided_at, decider_version, reason_chain).
//  3. Appended via the Store. Idempotency on decision_id is enforced by
//     the store's ON CONFLICT DO NOTHING.
//
// The consumer does NOT interpret reason_chain or alternatives content —
// those JSON blobs are forwarded to the store untouched. The OSS layer
// owns shape and storage; the registered routing.Decider owns semantics.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/store"
)

// Sink is the narrow Store surface the consumer depends on.
type Sink interface {
	Append(ctx context.Context, in store.AppendInput) error
}

// Counter is the narrow metrics surface the consumer depends on.
type Counter interface {
	IncAppend()
	IncAppendFailure()
	IncValidationReject()
}

// Config wires the bus connection.
type Config struct {
	Brokers  []string
	ClientID string
	Group    string
	Topic    string
}

// Consumer drains the routing-decision topic into the Sink.
type Consumer struct {
	cfg     Config
	client  *kgo.Client
	sink    Sink
	metrics Counter
}

// New constructs a Consumer.
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

// Run drains the topic until ctx cancels.
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
			// Best effort: ON CONFLICT (decision_id) DO NOTHING on the
			// store side makes re-delivery safe.
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
	if err := c.sink.Append(ctx, in); err != nil {
		c.metrics.IncAppendFailure()
		return
	}
	c.metrics.IncAppend()
}

// decisionEvent matches packages/contracts/routing/v1/decision-event.schema.json.
type decisionEvent struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	Team          string `json:"team,omitempty"`
	App           string `json:"app,omitempty"`
	Env           string `json:"env,omitempty"`
	Project       string `json:"project,omitempty"`
	Request       struct {
		ProviderRequested string `json:"provider_requested"`
		ModelRequested    string `json:"model_requested"`
		Route             string `json:"route,omitempty"`
		RequestIDHash     string `json:"request_id_hash,omitempty"`
	} `json:"request"`
	Decision struct {
		ProviderChosen string `json:"provider_chosen"`
		ModelChosen    string `json:"model_chosen"`
		RouteChosen    string `json:"route_chosen,omitempty"`
	} `json:"decision"`
	ReasonChain    json.RawMessage `json:"reason_chain"`
	Alternatives   json.RawMessage `json:"alternatives,omitempty"`
	DecidedAt      string          `json:"decided_at"`
	DeciderVersion string          `json:"decider_version"`
	TraceID        string          `json:"trace_id,omitempty"`
}

// decodeRecord converts a kgo.Record into a store.AppendInput. The tenant
// header is treated as authoritative if present; the JSON tenant_id is
// the fallback. reason_chain and alternatives are carried through as
// opaque json.RawMessage — the consumer never inspects their content.
func decodeRecord(rec *kgo.Record, tenantHeader string) (store.AppendInput, error) {
	var ev decisionEvent
	if err := json.Unmarshal(rec.Value, &ev); err != nil {
		return store.AppendInput{}, fmt.Errorf("consumer: decode: %w", err)
	}
	if ev.ID == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: id is required")
	}
	tenant := tenantHeader
	if tenant == "" {
		tenant = ev.TenantID
	}
	if tenant == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: tenant_id is required")
	}
	if ev.Request.ProviderRequested == "" || ev.Request.ModelRequested == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: request.provider/model required")
	}
	if ev.Decision.ProviderChosen == "" || ev.Decision.ModelChosen == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: decision.provider/model required")
	}
	if ev.DeciderVersion == "" {
		return store.AppendInput{}, fmt.Errorf("consumer: decider_version is required")
	}
	if len(ev.ReasonChain) == 0 {
		return store.AppendInput{}, fmt.Errorf("consumer: reason_chain is required")
	}
	decidedAt, err := time.Parse(time.RFC3339Nano, ev.DecidedAt)
	if err != nil {
		// Accept RFC3339 (second precision) as a fallback.
		decidedAt, err = time.Parse(time.RFC3339, ev.DecidedAt)
		if err != nil {
			return store.AppendInput{}, fmt.Errorf("consumer: decided_at parse: %w", err)
		}
	}
	return store.AppendInput{
		DecisionID:        ev.ID,
		TenantID:          tenant,
		Team:              ev.Team,
		App:               ev.App,
		Env:               ev.Env,
		Project:           ev.Project,
		ProviderRequested: ev.Request.ProviderRequested,
		ModelRequested:    ev.Request.ModelRequested,
		RouteRequested:    ev.Request.Route,
		RequestIDHash:     ev.Request.RequestIDHash,
		ProviderChosen:    ev.Decision.ProviderChosen,
		ModelChosen:       ev.Decision.ModelChosen,
		RouteChosen:       ev.Decision.RouteChosen,
		ReasonChain:       ev.ReasonChain,
		Alternatives:      ev.Alternatives,
		DeciderVersion:    ev.DeciderVersion,
		DecidedAt:         decidedAt,
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
