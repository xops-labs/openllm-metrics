// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Workload profiles for the demo generator. Teams, apps, and envs match the
// Acme Corp rows in platform/db/seeds/001_demo_data.sql so the console's
// default tenant sees the traffic; models match platform/pricing/ so the
// cost-mapper can produce estimates for every synthetic request.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
)

// workload describes one synthetic (provider, model, route) traffic class.
type workload struct {
	Provider string
	Model    string
	Region   string

	Team string
	App  string
	Env  string

	Operation string
	Weight    int // relative pick weight

	// Latency profile (median microseconds; sampled log-normally around it).
	MedianLatencyUS int64

	// Token profile (uniform ranges).
	InputMin, InputMax   int
	OutputMin, OutputMax int

	// Cost per 1K tokens (USD) — approximate list prices, kept in loose
	// lockstep with platform/pricing/. Used for the pull-mode rollup leg only.
	InputPer1K  float64
	OutputPer1K float64

	Streaming bool
}

// demoWorkloads is the fleet the generator simulates: a chat product on three
// providers, a dev/staging variant, and an embedding batch pipeline — all
// under the seeded Acme tenant.
var demoWorkloads = []workload{
	{
		Provider: "openai", Model: "gpt-4o-mini", Region: "us-east-1",
		Team: "platform-eng", App: "chat-assistant", Env: "prod",
		Operation: "chat", Weight: 30, MedianLatencyUS: 620_000,
		InputMin: 400, InputMax: 2400, OutputMin: 120, OutputMax: 700,
		InputPer1K: 0.00015, OutputPer1K: 0.0006, Streaming: true,
	},
	{
		Provider: "anthropic", Model: "claude-sonnet-4", Region: "us-west-2",
		Team: "platform-eng", App: "chat-assistant", Env: "prod",
		Operation: "chat", Weight: 20, MedianLatencyUS: 940_000,
		InputMin: 600, InputMax: 4200, OutputMin: 200, OutputMax: 1100,
		InputPer1K: 0.003, OutputPer1K: 0.015, Streaming: true,
	},
	{
		Provider: "openai", Model: "gpt-4o", Region: "us-east-1",
		Team: "platform-eng", App: "chat-assistant", Env: "prod",
		Operation: "chat", Weight: 10, MedianLatencyUS: 1_400_000,
		InputMin: 900, InputMax: 5200, OutputMin: 300, OutputMax: 1400,
		InputPer1K: 0.0025, OutputPer1K: 0.01, Streaming: true,
	},
	{
		Provider: "google", Model: "gemini-2.5-flash", Region: "us-central1",
		Team: "platform-eng", App: "chat-assistant", Env: "dev",
		Operation: "chat", Weight: 9, MedianLatencyUS: 510_000,
		InputMin: 300, InputMax: 1800, OutputMin: 100, OutputMax: 600,
		InputPer1K: 0.0003, OutputPer1K: 0.0025, Streaming: false,
	},
	{
		Provider: "azure_openai", Model: "gpt-4o", Region: "eastus2",
		Team: "analytics", App: "batch-processor", Env: "dev",
		Operation: "chat", Weight: 7, MedianLatencyUS: 1_250_000,
		InputMin: 800, InputMax: 4800, OutputMin: 250, OutputMax: 1200,
		InputPer1K: 0.0025, OutputPer1K: 0.01, Streaming: false,
	},
	{
		Provider: "bedrock", Model: "anthropic.claude-sonnet-4", Region: "us-west-2",
		Team: "analytics", App: "batch-processor", Env: "dev",
		Operation: "chat", Weight: 9, MedianLatencyUS: 1_100_000,
		InputMin: 700, InputMax: 4400, OutputMin: 220, OutputMax: 1000,
		InputPer1K: 0.003, OutputPer1K: 0.015, Streaming: false,
	},
	{
		Provider: "openai", Model: "text-embedding-3-small", Region: "us-east-1",
		Team: "analytics", App: "batch-processor", Env: "dev",
		Operation: "embedding", Weight: 15, MedianLatencyUS: 130_000,
		InputMin: 200, InputMax: 1600, OutputMin: 0, OutputMax: 0,
		InputPer1K: 0.00002, OutputPer1K: 0, Streaming: false,
	},
}

var totalWeight = func() int {
	sum := 0
	for _, w := range demoWorkloads {
		sum += w.Weight
	}
	return sum
}()

// outcome is one synthesized request result.
type outcome struct {
	Status     string // success | error | timeout | rate_limited
	StatusCode int
	ErrorType  string
	RetryCount int
}

// sampleOutcome draws a request outcome: ~96% success, ~2% server error,
// ~1.5% throttle, ~0.5% timeout — enough red to make the error and SLO
// panels interesting without looking like an outage.
func sampleOutcome(rng *rand.Rand) outcome {
	r := rng.Float64()
	switch {
	case r < 0.005:
		return outcome{Status: "timeout", StatusCode: 504, ErrorType: "timeout", RetryCount: rng.IntN(3)}
	case r < 0.020:
		return outcome{Status: "rate_limited", StatusCode: 429, ErrorType: "rate_limited", RetryCount: 1 + rng.IntN(2)}
	case r < 0.040:
		return outcome{Status: "error", StatusCode: 500, ErrorType: "server_error", RetryCount: rng.IntN(2)}
	default:
		return outcome{Status: "success", StatusCode: 200}
	}
}

// sampleLatencyUS draws a latency around the workload median with a
// log-normal-ish right tail (sigma ~0.45).
func sampleLatencyUS(rng *rand.Rand, medianUS int64) int64 {
	factor := math.Exp(rng.NormFloat64() * 0.45)
	v := int64(float64(medianUS) * factor)
	if v < 20_000 {
		v = 20_000
	}
	return v
}

// synthesizeRuntime builds one schema-conformant runtime event for the
// workload at time now.
func synthesizeRuntime(rng *rand.Rand, w *workload, tenant string, now time.Time) RuntimeEvent {
	oc := sampleOutcome(rng)
	latency := sampleLatencyUS(rng, w.MedianLatencyUS)

	in := w.InputMin
	if w.InputMax > w.InputMin {
		in += rng.IntN(w.InputMax - w.InputMin)
	}
	out := w.OutputMin
	if w.OutputMax > w.OutputMin {
		out += rng.IntN(w.OutputMax - w.OutputMin)
	}
	total := in + out

	ev := RuntimeEvent{
		SchemaVersion: "1",
		EventID:       uuid.NewString(),
		SourceMode:    "proxy",
		SourceService: sourceService,
		RequestIDHash: hashID(uuid.NewString()),
		Provider:      w.Provider,
		Model:         w.Model,
		Operation:     w.Operation,
		Tenant:        tenant,
		Team:          w.Team,
		App:           w.App,
		Env:           w.Env,
		Region:        w.Region,
		Status:        oc.Status,
		StatusCode:    oc.StatusCode,
		ErrorType:     oc.ErrorType,
		LatencyUS:     latency,
		RetryCount:    oc.RetryCount,
		IsStreaming:   w.Streaming,
		RecordedAt:    now.UTC().Format(time.RFC3339Nano),
	}
	if w.Streaming {
		ev.TTFBUS = latency / (3 + int64(rng.IntN(4)))
	}
	// Failed calls may not have token usage — mirror the gateway, which only
	// reports tokens when the provider returned a usage block.
	if oc.Status == "success" {
		ev.InputTokens = &in
		ev.OutputTokens = &out
		ev.TotalTokens = &total
	}
	return ev
}

// costUSD computes the demo list cost for a token tuple.
func (w *workload) costUSD(inputTokens, outputTokens int64) float64 {
	return float64(inputTokens)/1000*w.InputPer1K + float64(outputTokens)/1000*w.OutputPer1K
}

func hashID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
