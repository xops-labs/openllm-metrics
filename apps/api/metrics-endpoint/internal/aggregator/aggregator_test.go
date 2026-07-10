// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package aggregator

import (
	"encoding/json"
	"testing"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

func usagePayload(t *testing.T, overrides map[string]interface{}) []byte {
	t.Helper()
	ev := map[string]interface{}{
		"schema_version":       telemetrycontracts.SchemaVersion,
		"event_id":             "01900000-0000-7000-8000-000000000001",
		"source_event_id":      "openai:1747476000:1747476300:gpt-4o:proj",
		"source_mode":          "pull",
		"source_service":       "apps/worker/usage-poller/openai",
		"provider":             "openai",
		"model":                "gpt-4o",
		"operation":            "chat",
		"tenant":               "tenant-001",
		"team":                 "ai-platform",
		"app":                  "snapcal",
		"env":                  "production",
		"project":              "snapcal-prod",
		"region":               "us-east-1",
		"input_tokens":         float64(100),
		"output_tokens":        float64(50),
		"total_tokens":         float64(150),
		"cost_usd_minor_units": float64(300),
		"request_count":        float64(2),
		"period_start":         "2026-05-17T10:00:00Z",
		"period_end":           "2026-05-17T10:05:00Z",
		"normalized_at":        "2026-05-17T10:05:10Z",
	}
	for k, v := range overrides {
		ev[k] = v
	}
	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func TestApply_ValidUsageEvent_PopulatesFiveCounters(t *testing.T) {
	a := New()
	if err := a.Apply(telemetrycontracts.TopicUsageNormalized, usagePayload(t, nil)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := a.ProcessedEvents(); got != 1 {
		t.Errorf("processed=%d, want 1", got)
	}
	// 5 counter families × 1 series each = 5 series.
	if got := a.SeriesCount(); got != 5 {
		t.Errorf("series=%d, want 5", got)
	}
}

func TestApply_DecodeFailure_BumpsDecodeReason(t *testing.T) {
	a := New()
	_ = a.Apply(telemetrycontracts.TopicUsageNormalized, []byte("{not json"))
	if got := a.RejectedEvents()[ReasonDecode]; got != 1 {
		t.Errorf("decode rejected=%d, want 1", got)
	}
	if got := a.SeriesCount(); got != 0 {
		t.Errorf("series leaked from bad-decode event: %d", got)
	}
}

func TestApply_ForbiddenField_RejectsBeforeSeries(t *testing.T) {
	a := New()
	_ = a.Apply(telemetrycontracts.TopicUsageNormalized, usagePayload(t, map[string]interface{}{
		"prompt": "this must never reach metrics",
	}))
	if got := a.RejectedEvents()[ReasonForbidden]; got != 1 {
		t.Errorf("forbidden rejected=%d, want 1", got)
	}
	if got := a.SeriesCount(); got != 0 {
		t.Errorf("series leaked from forbidden-field event: %d", got)
	}
}

func TestApply_UnknownTopic_BumpsUnknownTopicReason(t *testing.T) {
	a := New()
	_ = a.Apply("llm.does.not.exist", usagePayload(t, nil))
	if got := a.RejectedEvents()[ReasonUnknownTopic]; got != 1 {
		t.Errorf("unknown-topic rejected=%d, want 1", got)
	}
}

func TestApply_SchemaDrift_BumpsSchemaReason(t *testing.T) {
	a := New()
	_ = a.Apply(telemetrycontracts.TopicUsageNormalized, usagePayload(t, map[string]interface{}{
		"schema_version": "999",
	}))
	if got := a.RejectedEvents()[ReasonSchema]; got != 1 {
		t.Errorf("schema rejected=%d, want 1", got)
	}
}

func TestSnapshot_IsDeterministic(t *testing.T) {
	a := New()
	for i := 0; i < 5; i++ {
		_ = a.Apply(telemetrycontracts.TopicUsageNormalized, usagePayload(t, map[string]interface{}{
			"event_id": "evt-" + string(rune('a'+i)),
		}))
	}
	snap1 := a.Snapshot()
	snap2 := a.Snapshot()
	if snap1.Processed != snap2.Processed {
		t.Fatalf("processed drift: %d vs %d", snap1.Processed, snap2.Processed)
	}
	if len(snap1.Counters) != len(snap2.Counters) {
		t.Fatalf("metric count drift: %d vs %d", len(snap1.Counters), len(snap2.Counters))
	}
	for i := range snap1.Counters {
		if snap1.Counters[i].Name != snap2.Counters[i].Name {
			t.Fatalf("metric order drift at %d: %s vs %s", i, snap1.Counters[i].Name, snap2.Counters[i].Name)
		}
	}
}

func TestReset_ClearsAllState(t *testing.T) {
	a := New()
	_ = a.Apply(telemetrycontracts.TopicUsageNormalized, usagePayload(t, nil))
	a.Reset()
	if a.ProcessedEvents() != 0 || a.SeriesCount() != 0 {
		t.Fatalf("reset did not clear state: processed=%d series=%d", a.ProcessedEvents(), a.SeriesCount())
	}
}
