// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package consumer wires the bus subscription into the joiner. Two topics:
//
//   - llm.cost.estimated   — produced by apps/worker/cost-mapper, this is
//     the runtime-side cost prediction (source = gateway | sdk).
//   - llm.usage.reconciled — produced by apps/worker/focus-ingester, this is
//     the vendor-reconciled cost (source = exporter).
//
// Each record is decoded, normalized to a joiner.Estimate or
// joiner.Reconciled, and folded into the running window.
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/joiner"
)

// MetricsRecorder is the narrow counter surface the consumer touches.
type MetricsRecorder interface {
	IncEstimatedConsumed()
	IncReconciledConsumed()
	IncEstimatedDropped()
	IncReconciledDropped()
	IncBadPayload()
}

// estimatedEvent is the subset of the cost.estimated.v1 payload (emitted by
// apps/worker/cost-mapper/internal/busproducer) the reconciler reads.
//
// The cost-mapper emits cost in integer USD minor units; this worker
// converts to USD float at the boundary so the numeric(20,6) columns and
// the drift math stay in the natural unit operators reason about.
type estimatedEvent struct {
	EventID                    string `json:"event_id"`
	Source                     string `json:"source"`
	Tenant                     string `json:"tenant"`
	Team                       string `json:"team"`
	App                        string `json:"app"`
	Env                        string `json:"env"`
	Project                    string `json:"project"`
	Provider                   string `json:"provider"`
	Model                      string `json:"model"`
	EstimatedCostUSDMinorUnits int64  `json:"estimated_cost_usd_minor_units"`
	RecordedAt                 string `json:"recorded_at"`
	EstimatedAt                string `json:"estimated_at"`
}

// reconciledEvent is the subset of the llm.usage.reconciled.v1 payload
// (emitted by apps/worker/focus-ingester/internal/ingester) the reconciler
// reads.
type reconciledEvent struct {
	EventID                     string `json:"event_id"`
	Source                      string `json:"source"`
	Tenant                      string `json:"tenant"`
	Team                        string `json:"team"`
	App                         string `json:"app"`
	Env                         string `json:"env"`
	Project                     string `json:"project"`
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	ReconciledCostUSDMinorUnits int64  `json:"reconciled_cost_usd_minor_units"`
	PeriodStart                 string `json:"period_start"`
	PeriodEnd                   string `json:"period_end"`
}

// Config bundles the consumer's topic names so the dispatcher knows which
// shape to decode for each record.
type Config struct {
	EstimatedTopic  string
	ReconciledTopic string
}

// Handler implements the bus-client Handler signature.
type Handler struct {
	cfg     Config
	joiner  *joiner.Joiner
	metrics MetricsRecorder
}

// New constructs a Handler.
func New(cfg Config, j *joiner.Joiner, m MetricsRecorder) *Handler {
	return &Handler{cfg: cfg, joiner: j, metrics: m}
}

// Handle is the bus-client callback. The bus-client commits the offset
// regardless of return value (errors route to DLQ at-least-once), so we
// return nil on "malformed but ignorable" payloads and reserve error
// returns for truly retriable failures.
func (h *Handler) Handle(ctx context.Context, record *kgo.Record) error {
	switch record.Topic {
	case h.cfg.EstimatedTopic:
		return h.handleEstimated(ctx, record)
	case h.cfg.ReconciledTopic:
		return h.handleReconciled(ctx, record)
	default:
		return fmt.Errorf("consumer: unexpected topic %q", record.Topic)
	}
}

func (h *Handler) handleEstimated(ctx context.Context, record *kgo.Record) error {
	h.metrics.IncEstimatedConsumed()

	var ev estimatedEvent
	if err := json.Unmarshal(record.Value, &ev); err != nil {
		h.metrics.IncBadPayload()
		return err
	}
	if ev.Tenant == "" {
		// No tenant — nothing to correlate; never log payload contents.
		h.metrics.IncEstimatedDropped()
		return nil
	}
	// Defense in depth: cost-mapper already filters non-runtime sources,
	// but the reconciler refuses to double-count exporter events that
	// somehow show up on this topic.
	switch strings.ToLower(strings.TrimSpace(ev.Source)) {
	case "gateway", "sdk":
		// ok
	default:
		h.metrics.IncEstimatedDropped()
		return nil
	}
	when, ok := parseEventTime(ev.RecordedAt, ev.EstimatedAt)
	if !ok {
		h.metrics.IncEstimatedDropped()
		return nil
	}

	start, end := h.joiner.Bucket(when)
	return h.joiner.RecordEstimate(ctx, joiner.Estimate{
		Key: joiner.WindowKey{
			TenantID:    ev.Tenant,
			Provider:    ev.Provider,
			Model:       ev.Model,
			WindowStart: start,
			WindowEnd:   end,
		},
		Team:             ev.Team,
		App:              ev.App,
		Env:              ev.Env,
		Project:          ev.Project,
		EstimatedCostUSD: minorUnitsToUSD(ev.EstimatedCostUSDMinorUnits),
	})
}

func (h *Handler) handleReconciled(ctx context.Context, record *kgo.Record) error {
	h.metrics.IncReconciledConsumed()

	var ev reconciledEvent
	if err := json.Unmarshal(record.Value, &ev); err != nil {
		h.metrics.IncBadPayload()
		return err
	}
	if ev.Tenant == "" {
		h.metrics.IncReconciledDropped()
		return nil
	}
	if strings.ToLower(strings.TrimSpace(ev.Source)) != "exporter" {
		// Only the pull-mode exporter contributes to the reconciled side.
		h.metrics.IncReconciledDropped()
		return nil
	}

	periodStart, ok1 := parseTime(ev.PeriodStart)
	periodEnd, ok2 := parseTime(ev.PeriodEnd)
	if !ok1 || !ok2 {
		h.metrics.IncReconciledDropped()
		return nil
	}

	// A FOCUS line item can cover a billing period that spans many
	// reconciler windows. Distribute the reconciled cost uniformly across
	// every covered window so the drift gauge tracks per-window scale.
	covered := windowsInPeriod(h.joiner, periodStart, periodEnd)
	if len(covered) == 0 {
		h.metrics.IncReconciledDropped()
		return nil
	}
	share := minorUnitsToUSD(ev.ReconciledCostUSDMinorUnits) / float64(len(covered))

	for _, w := range covered {
		if err := h.joiner.RecordReconciled(ctx, joiner.Reconciled{
			Key: joiner.WindowKey{
				TenantID:    ev.Tenant,
				Provider:    ev.Provider,
				Model:       ev.Model,
				WindowStart: w.start,
				WindowEnd:   w.end,
			},
			Team:              ev.Team,
			App:               ev.App,
			Env:               ev.Env,
			Project:           ev.Project,
			ReconciledCostUSD: share,
		}); err != nil {
			return err
		}
	}
	return nil
}

type window struct {
	start, end time.Time
}

func windowsInPeriod(j *joiner.Joiner, periodStart, periodEnd time.Time) []window {
	if !periodEnd.After(periodStart) {
		return nil
	}
	size := j.WindowSize()
	if size <= 0 {
		return nil
	}
	start, _ := j.Bucket(periodStart)
	var out []window
	// Iterate forward; guard against runaway loops by capping at a sane
	// horizon (a billing period larger than 1 year is upstream data
	// corruption, not our problem).
	maxIters := int((365 * 24 * time.Hour) / size)
	if maxIters < 1 {
		maxIters = 1
	}
	for i := 0; i < maxIters && start.Before(periodEnd); i++ {
		end := start.Add(size)
		out = append(out, window{start: start, end: end})
		start = end
	}
	return out
}

// minorUnitsToUSD converts integer USD minor units (1 unit = 0.01 USD) into
// a USD float. Matches the inverse operation in cost-mapper/internal/mapper.
func minorUnitsToUSD(minor int64) float64 {
	if minor == 0 {
		return 0
	}
	return float64(minor) / 100.0
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// parseEventTime tries RecordedAt first, falling back to EstimatedAt.
// Returns ok=false if neither parses.
func parseEventTime(recordedAt, estimatedAt string) (time.Time, bool) {
	if t, ok := parseTime(recordedAt); ok {
		return t, true
	}
	if t, ok := parseTime(estimatedAt); ok {
		return t, true
	}
	return time.Time{}, false
}
