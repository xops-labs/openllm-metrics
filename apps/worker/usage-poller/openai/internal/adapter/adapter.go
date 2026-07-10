// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package adapter maps OpenAI Usage + Cost API responses into the
// canonical llm.usage.normalized event payload owned by F008.
//
// This is THE provider portability boundary for F009: every
// OpenAI-specific field name, unit, and convention is translated here.
// Downstream code (bus producer, scoring, policy, routing) sees only the
// vendor-neutral schema. Removing this package together with
// internal/openaiclient must NOT break the rest of the platform.
package adapter

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/openaiclient"
)

// SourceService identifies this adapter to downstream consumers.
const SourceService = "apps/worker/usage-poller/openai"

// ProviderName is the canonical provider identifier (matches the schema enum).
const ProviderName = "openai"

// ContextLabels are the static labels that came from the operator's config.
// Every emitted event inherits these; they satisfy the multi-tenant
// invariant (F005) and F008 §9 mandatory-field requirements.
type ContextLabels struct {
	Tenant  string
	Team    string
	App     string
	Env     string
	Project string
	Region  string
}

// NormalizedEvent is the in-memory form of an llm.usage.normalized event.
// JSON tags match the F008 schema exactly so json.Marshal produces a
// byte-for-byte schema-compliant payload.
type NormalizedEvent struct {
	SchemaVersion     string `json:"schema_version"`
	EventID           string `json:"event_id"`
	SourceEventID     string `json:"source_event_id"`
	SourceMode        string `json:"source_mode"`
	Source            string `json:"source"`
	SourceService     string `json:"source_service"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Operation         string `json:"operation"`
	Tenant            string `json:"tenant"`
	Team              string `json:"team"`
	App               string `json:"app,omitempty"`
	Env               string `json:"env"`
	Project           string `json:"project,omitempty"`
	Region            string `json:"region,omitempty"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	TotalTokens       int64  `json:"total_tokens"`
	CostUSDMinorUnits int64  `json:"cost_usd_minor_units"`
	RequestCount      int64  `json:"request_count,omitempty"`
	PeriodStart       string `json:"period_start"`
	PeriodEnd         string `json:"period_end"`
	NormalizedAt      string `json:"normalized_at"`
}

// Normalize converts a CombinedWindow into zero or more NormalizedEvents.
//
// One event is emitted per (bucket, model, project) usage row. Cost is
// attributed best-effort by joining cost rows in the same bucket, weighted
// by total tokens. The cost-join heuristic is documented in the README so
// operators understand the trade-off (OpenAI's cost API is line-item per
// bucket, not per model — F017 will own per-model attribution).
//
// `now` is injected so tests can pin normalized_at; nil falls back to
// time.Now().UTC().
func Normalize(window openaiclient.CombinedWindow, ctxLabels ContextLabels, now func() time.Time) ([]NormalizedEvent, error) {
	if ctxLabels.Tenant == "" {
		// Hard-fail: an event without tenant violates F005 and would be
		// rejected by schemalint.LintEvent anyway. Better to error early.
		return nil, fmt.Errorf("adapter: tenant label is required")
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	events := make([]NormalizedEvent, 0, 8)
	for _, bucket := range window.Usage.Data {
		costMinorByBucket := costMinorUnitsForBucket(window.Cost, bucket.StartTime, bucket.EndTime)
		totalTokensInBucket := int64(0)
		for _, r := range bucket.Results {
			totalTokensInBucket += r.InputTokens + r.OutputTokens
		}

		for _, r := range bucket.Results {
			rowTokens := r.InputTokens + r.OutputTokens
			rowCostMinor := int64(0)
			if totalTokensInBucket > 0 {
				// Weighted attribution. Round nearest to avoid systematic underflow.
				share := float64(rowTokens) / float64(totalTokensInBucket)
				rowCostMinor = int64(math.Round(float64(costMinorByBucket) * share))
			}

			ev := NormalizedEvent{
				SchemaVersion:     telemetrycontracts.SchemaVersion,
				EventID:           uuid.NewString(),
				SourceEventID:     deriveSourceEventID(bucket, r),
				SourceMode:        "pull",
				Source:            telemetrycontracts.SourceExporter,
				SourceService:     SourceService,
				Provider:          ProviderName,
				Model:             canonicalModelName(r.Model),
				Operation:         operationFor(r.Model),
				Tenant:            ctxLabels.Tenant,
				Team:              ctxLabels.Team,
				App:               ctxLabels.App,
				Env:               ctxLabels.Env,
				Project:           pickProject(ctxLabels.Project, r.ProjectID),
				Region:            ctxLabels.Region,
				InputTokens:       r.InputTokens,
				OutputTokens:      r.OutputTokens,
				TotalTokens:       r.InputTokens + r.OutputTokens,
				CostUSDMinorUnits: rowCostMinor,
				RequestCount:      r.NumModelRequests,
				PeriodStart:       time.Unix(bucket.StartTime, 0).UTC().Format(time.RFC3339),
				PeriodEnd:         time.Unix(bucket.EndTime, 0).UTC().Format(time.RFC3339),
				NormalizedAt:      now().UTC().Format(time.RFC3339),
			}
			events = append(events, ev)
		}
	}
	return events, nil
}

// costMinorUnitsForBucket sums every cost line item whose bucket times match
// the supplied (start,end). Convert OpenAI's USD float to integer minor units
// (1 unit = 0.01 USD) per F008 §10.
func costMinorUnitsForBucket(cost openaiclient.CostResponse, start, end int64) int64 {
	var totalMinor int64
	for _, b := range cost.Data {
		if b.StartTime != start || b.EndTime != end {
			continue
		}
		for _, r := range b.Results {
			if r.Amount.Currency != "" && !strings.EqualFold(r.Amount.Currency, "usd") {
				// Skip non-USD lines; we cannot safely normalize without an FX
				// rate (out of scope for F009; F017 owns cross-currency).
				continue
			}
			totalMinor += int64(math.Round(r.Amount.Value * 100.0))
		}
	}
	return totalMinor
}

// deriveSourceEventID gives consumers a stable handle back to the raw upstream
// bucket. Format: openai:<start>:<end>:<model>:<project>. This is also the
// per-row tag used by the dedup cache (see dedup.Key).
func deriveSourceEventID(bucket openaiclient.UsageBucket, r openaiclient.UsageResult) string {
	return fmt.Sprintf("openai:%d:%d:%s:%s", bucket.StartTime, bucket.EndTime, r.Model, r.ProjectID)
}

// canonicalModelName trims provider prefixes / aliases so dashboards see one
// model name regardless of API surface variant. F017 will own a richer model
// registry; for F009 we strip whitespace and lower-case.
func canonicalModelName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// operationFor maps the model family onto the canonical operation enum.
// Conservative mapping; unknown models fall back to "other" so the schema
// enum is always satisfied.
func operationFor(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "text-embedding"), strings.Contains(m, "embedding"):
		return "embedding"
	case strings.HasPrefix(m, "dall-e"), strings.Contains(m, "image"):
		return "image"
	case strings.HasPrefix(m, "whisper"), strings.HasPrefix(m, "tts"):
		return "audio"
	case strings.Contains(m, "moderation"):
		return "moderation"
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "o1-"), strings.HasPrefix(m, "o3-"),
		strings.HasPrefix(m, "claude-"), strings.HasPrefix(m, "gemini-"), strings.Contains(m, "chat"):
		return "chat"
	case strings.Contains(m, "instruct"), strings.HasPrefix(m, "davinci"), strings.HasPrefix(m, "babbage"):
		return "completion"
	default:
		return "other"
	}
}

// pickProject prefers the project label declared in operator config when set;
// falls back to the OpenAI-side project_id when not. Either way the value
// remains in the canonical schema's `project` slot — never leaks OpenAI
// terminology into downstream consumers.
func pickProject(configProject, openAIProjectID string) string {
	if configProject != "" {
		return configProject
	}
	return openAIProjectID
}
