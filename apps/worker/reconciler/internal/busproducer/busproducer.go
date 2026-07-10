// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package busproducer publishes reconciliation.window.v1 events to the
// streaming bus. The topic ("llm.reconciliation.window" by default) is a
// new bus surface owned by apps/worker/reconciler.
//
// Downstream consumers:
//
//   - F033 notifications worker — fans out an alert when drift_ratio exceeds
//     a tenant-configured tolerance for N consecutive windows.
//   - F027 dashboards — renders per-window drift series.
//
// Decision logic (route, fall back, deny, downgrade, budget action) is
// deliberately NOT here. The reconciler only computes the signal; what to
// do with it lives in F033 (OSS notifications) or F034 / F035 (custom,
// not implemented here).
package busproducer

import (
	"context"
	"encoding/json"
	"fmt"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/store"
)

// SourceService stamps every emitted event.
const SourceService = "apps/worker/reconciler"

// SchemaVersion is the version of the reconciliation.window.v1 envelope.
const SchemaVersion = "1"

// WindowEvent is the canonical reconciliation.window.v1 payload. It carries
// the full tenant/team/app/env/project/provider/model context so downstream
// consumers (notifications, dashboards) can join without re-deriving
// labels.
type WindowEvent struct {
	SchemaVersion     string  `json:"schema_version"`
	EventID           string  `json:"event_id"`
	SourceService     string  `json:"source_service"`
	Tenant            string  `json:"tenant"`
	Team              string  `json:"team,omitempty"`
	App               string  `json:"app,omitempty"`
	Env               string  `json:"env"`
	Project           string  `json:"project,omitempty"`
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	WindowStart       string  `json:"window_start"`
	WindowEnd         string  `json:"window_end"`
	EstimatedCostUSD  float64 `json:"estimated_cost_usd"`
	ReconciledCostUSD float64 `json:"reconciled_cost_usd"`
	DriftUSD          float64 `json:"drift_usd"`
	DriftRatio        float64 `json:"drift_ratio"`
	Status            string  `json:"status"`
}

// Emitter is the narrow surface the closer depends on.
type Emitter interface {
	EmitWindowClosed(ctx context.Context, row store.Row) error
	Close()
}

// BusEmitter publishes to the configured window topic.
type BusEmitter struct {
	producer *busclient.Producer
	topic    string
}

// New constructs a BusEmitter wired to the supplied topic.
func New(producer *busclient.Producer, topic string) *BusEmitter {
	return &BusEmitter{producer: producer, topic: topic}
}

// EmitWindowClosed marshals and produces a single reconciliation.window
// event derived from a closed reconciliation row.
func (e *BusEmitter) EmitWindowClosed(ctx context.Context, row store.Row) error {
	if row.TenantID == "" {
		return fmt.Errorf("busproducer: tenant_id is required")
	}
	if row.ID == 0 {
		return fmt.Errorf("busproducer: row id is required")
	}
	ev := WindowEvent{
		SchemaVersion:     SchemaVersion,
		EventID:           deriveEventID(row),
		SourceService:     SourceService,
		Tenant:            row.TenantID,
		Team:              row.Team,
		App:               row.App,
		Env:               row.Env,
		Project:           row.Project,
		Provider:          row.Provider,
		Model:             row.Model,
		WindowStart:       row.WindowStart.UTC().Format("2006-01-02T15:04:05Z07:00"),
		WindowEnd:         row.WindowEnd.UTC().Format("2006-01-02T15:04:05Z07:00"),
		EstimatedCostUSD:  row.EstimatedCostUSD,
		ReconciledCostUSD: row.ReconciledCostUSD,
		DriftUSD:          row.DriftUSD,
		DriftRatio:        row.DriftRatio,
		Status:            string(row.Status),
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

// deriveEventID is a deterministic identifier derived from the unique key
// of a reconciliation row plus its terminal status. Replaying a window
// close produces a byte-identical event (idempotent consumer guarantee).
func deriveEventID(row store.Row) string {
	return fmt.Sprintf("recon-%d-%s-%s-%s-%d-%s",
		row.ID,
		row.TenantID,
		row.Provider,
		row.Model,
		row.WindowStart.UTC().Unix(),
		row.Status,
	)
}
