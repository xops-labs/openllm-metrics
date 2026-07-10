// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package mapper converts a normalized runtime event (gateway/SDK) into a
// canonical cost.estimated.v1 event by joining the event's input/output
// token counts against the pricing catalog.
//
// The transform is pure: tokens × rate = USD. No scoring, no ranking, no
// routing. The output event_id is a deterministic hash of the input
// event_id and the catalog version, so a replay of the same upstream event
// against the same catalog produces byte-identical output (idempotent
// consumer guarantee).
package mapper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/catalog"
)

// SchemaVersion is the version of the cost.estimated.v1 envelope.
const SchemaVersion = "1"

// SourceService stamps every emitted event so downstream consumers know
// which worker produced it.
const SourceService = "apps/worker/cost-mapper"

// RuntimeEvent is the subset of the F008 llm.runtime.normalized payload
// the mapper consumes. We deliberately keep it narrow — the mapper has no
// business inspecting latency, retry, or trace context fields.
type RuntimeEvent struct {
	EventID      string `json:"event_id"`
	Source       string `json:"source"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	Tenant       string `json:"tenant"`
	Team         string `json:"team"`
	App          string `json:"app,omitempty"`
	Env          string `json:"env"`
	Project      string `json:"project,omitempty"`
	Region       string `json:"region,omitempty"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	RecordedAt   string `json:"recorded_at"`
}

// EstimatedEvent is the canonical cost.estimated.v1 payload. It carries the
// full tenant/team/app/env/project/provider/model context so downstream
// consumers can join cost without re-deriving labels.
type EstimatedEvent struct {
	SchemaVersion              string `json:"schema_version"`
	EventID                    string `json:"event_id"`
	SourceEventID              string `json:"source_event_id"`
	Source                     string `json:"source"`
	SourceService              string `json:"source_service"`
	Provider                   string `json:"provider"`
	Model                      string `json:"model"`
	Tenant                     string `json:"tenant"`
	Team                       string `json:"team"`
	App                        string `json:"app,omitempty"`
	Env                        string `json:"env"`
	Project                    string `json:"project,omitempty"`
	Region                     string `json:"region,omitempty"`
	InputTokens                int64  `json:"input_tokens"`
	OutputTokens               int64  `json:"output_tokens"`
	EstimatedCostUSDMinorUnits int64  `json:"estimated_cost_usd_minor_units"`
	CatalogVersion             string `json:"catalog_version"`
	CatalogApproximate         bool   `json:"catalog_approximate"`
	PricingCurrency            string `json:"pricing_currency"`
	EstimatedAt                string `json:"estimated_at"`
	RecordedAt                 string `json:"recorded_at,omitempty"`
}

// Mapper joins runtime events against the pricing catalog.
type Mapper struct {
	cat *catalog.Catalog
	now func() time.Time
}

// New constructs a Mapper bound to the supplied Catalog.
func New(cat *catalog.Catalog) *Mapper {
	return &Mapper{
		cat: cat,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Estimate produces a cost.estimated.v1 payload for ev, or an error if the
// (provider, model) pair is not in the catalog. The function is pure: same
// (ev, catalog version) → same EstimatedEvent.
func (m *Mapper) Estimate(ev RuntimeEvent) (EstimatedEvent, error) {
	if ev.EventID == "" {
		return EstimatedEvent{}, fmt.Errorf("mapper: event_id is required")
	}
	if ev.Tenant == "" {
		return EstimatedEvent{}, fmt.Errorf("mapper: tenant is required")
	}
	rate, err := m.cat.Lookup(ev.Provider, ev.Model)
	if err != nil {
		return EstimatedEvent{}, err
	}

	costUSD := perToken(ev.InputTokens)*rate.InputPer1K + perToken(ev.OutputTokens)*rate.OutputPer1K
	minor := toMinorUnits(costUSD)

	version := m.cat.Version()
	out := EstimatedEvent{
		SchemaVersion:              SchemaVersion,
		EventID:                    deriveEventID(ev.EventID, version),
		SourceEventID:              ev.EventID,
		Source:                     normalizeSource(ev.Source),
		SourceService:              SourceService,
		Provider:                   canonical(ev.Provider),
		Model:                      canonical(ev.Model),
		Tenant:                     ev.Tenant,
		Team:                       ev.Team,
		App:                        ev.App,
		Env:                        ev.Env,
		Project:                    ev.Project,
		Region:                     ev.Region,
		InputTokens:                ev.InputTokens,
		OutputTokens:               ev.OutputTokens,
		EstimatedCostUSDMinorUnits: minor,
		CatalogVersion:             version,
		CatalogApproximate:         rate.Approximate,
		PricingCurrency:            rate.Currency,
		EstimatedAt:                m.now().Format(time.RFC3339),
		RecordedAt:                 ev.RecordedAt,
	}
	return out, nil
}

// perToken converts integer-token counts to "per-1k" multiplier (e.g. 1500
// tokens × $0.001 per 1k = $0.0015).
func perToken(tokens int64) float64 {
	if tokens <= 0 {
		return 0
	}
	return float64(tokens) / 1000.0
}

// toMinorUnits converts a USD float into integer USD minor units
// (1 unit = 0.01 USD). Rounded half-away-from-zero so $0.0049 → 0 minor
// units but $0.0050 → 1 minor unit, matching the FOCUS reconciler.
//
// Implementation detail: floats hold the math, but the bus payload is the
// integer-minor-units form so the reconciliation diff against
// reconciledCostUsd never carries float drift downstream.
func toMinorUnits(usd float64) int64 {
	if usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0
	}
	return int64(math.Round(usd * 100.0))
}

// deriveEventID is a stable SHA-256 derivation: same (source_event_id,
// catalog version) → same event_id. Keeps consumers idempotent across
// replays without requiring an external dedup store.
func deriveEventID(sourceEventID, catalogVersion string) string {
	h := sha256.New()
	_, _ = h.Write([]byte("cost.estimated.v1|"))
	_, _ = h.Write([]byte(sourceEventID))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(catalogVersion))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// normalizeSource ensures emitted events carry one of the F008 source values.
// We only accept gateway|sdk on the input side; anything else falls back to
// the literal value (the schema-lint contract will catch unknown values).
func normalizeSource(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "gateway":
		return "gateway"
	case "sdk":
		return "sdk"
	default:
		return s
	}
}

func canonical(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
