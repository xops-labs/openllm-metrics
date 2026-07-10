// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package main

import (
	"encoding/json"
	"math/rand/v2"
	"testing"
	"time"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

// schemaShape is the slice of JSON Schema this test cares about: every
// contract pins additionalProperties:false, so the marshaled demo events must
// stay inside `properties` and cover `required`.
type schemaShape struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

func loadShape(t *testing.T, topic string) schemaShape {
	t.Helper()
	raw, err := telemetrycontracts.Schema(topic)
	if err != nil {
		t.Fatalf("Schema(%q): %v", topic, err)
	}
	var s schemaShape
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal schema for %q: %v", topic, err)
	}
	return s
}

func assertConforms(t *testing.T, topic string, event any) {
	t.Helper()
	shape := loadShape(t, topic)

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	for key := range m {
		if _, ok := shape.Properties[key]; !ok {
			t.Errorf("%s: field %q not in schema properties (additionalProperties is false)", topic, key)
		}
	}
	for _, req := range shape.Required {
		if _, ok := m[req]; !ok {
			t.Errorf("%s: required field %q missing from event", topic, req)
		}
	}
}

func testRNG() *rand.Rand {
	return rand.New(rand.NewPCG(42, 0))
}

func TestRuntimeEventConformsToSchema(t *testing.T) {
	rng := testRNG()
	now := time.Now()
	// Exercise every workload so provider/model/operation variants all pass.
	for i := range demoWorkloads {
		ev := synthesizeRuntime(rng, &demoWorkloads[i], defaultTenant, now)
		assertConforms(t, telemetrycontracts.TopicRuntimeNormalized, ev)

		if ev.SchemaVersion != "1" {
			t.Errorf("schema_version = %q, want \"1\"", ev.SchemaVersion)
		}
		switch ev.Status {
		case "success", "error", "timeout", "rate_limited", "circuit_open":
		default:
			t.Errorf("status %q not in schema enum", ev.Status)
		}
		if ev.Status == "success" && ev.TotalTokens == nil {
			t.Error("success events must carry token usage")
		}
	}
}

func TestUsageRollupConformsToSchema(t *testing.T) {
	a := &rollupAcc{InputTokens: 48_000, OutputTokens: 12_500, Requests: 37}
	start := time.Now().Add(-30 * time.Second)
	for i := range demoWorkloads {
		ev := usageRollup(&demoWorkloads[i], a, defaultTenant, start, time.Now())
		assertConforms(t, telemetrycontracts.TopicUsageNormalized, ev)

		if ev.SourceMode != "pull" || ev.Source != "exporter" {
			t.Errorf("usage rollup source = %q/%q, want pull/exporter", ev.SourceMode, ev.Source)
		}
		if ev.TotalTokens != ev.InputTokens+ev.OutputTokens {
			t.Error("total_tokens must equal input+output")
		}
	}
}

func TestReconciledRollupConformsToSchema(t *testing.T) {
	rng := testRNG()
	a := &rollupAcc{InputTokens: 48_000, OutputTokens: 12_500, Requests: 37}
	start := time.Now().Add(-60 * time.Second)
	for i := range demoWorkloads {
		ev := reconciledRollup(rng, &demoWorkloads[i], a, defaultTenant, start, time.Now())
		assertConforms(t, telemetrycontracts.TopicUsageReconciled, ev)

		if ev.Source != "exporter" {
			t.Errorf("reconciled source = %q, want exporter (schema const)", ev.Source)
		}
		if ev.ReconciledCostUSDMinorUnits < ev.ListCostUSDMinorUnits {
			t.Error("demo drift is modeled as reconciled >= list")
		}
	}
}

func TestWorkloadsUseSchemaEnums(t *testing.T) {
	providers := map[string]bool{
		"openai": true, "anthropic": true, "google": true,
		"azure_openai": true, "bedrock": true,
	}
	operations := map[string]bool{
		"chat": true, "completion": true, "embedding": true,
		"image": true, "audio": true, "moderation": true, "other": true,
	}
	for _, w := range demoWorkloads {
		if !providers[w.Provider] {
			t.Errorf("workload provider %q not in schema enum", w.Provider)
		}
		if !operations[w.Operation] {
			t.Errorf("workload operation %q not in schema enum", w.Operation)
		}
	}
}

func TestUSDToMinorRounds(t *testing.T) {
	cases := map[float64]int64{
		0.0:     0,
		0.004:   0,
		0.005:   1, // rounds half up
		0.01:    1,
		1.0:     100,
		12.3456: 1235,
	}
	for usd, want := range cases {
		if got := usdToMinor(usd); got != want {
			t.Errorf("usdToMinor(%v) = %d, want %d", usd, got, want)
		}
	}
}
