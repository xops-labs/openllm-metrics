// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetrycontracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestSchema_KnownTopicsReturnNonEmptyBytes(t *testing.T) {
	for _, topic := range Topics() {
		b, err := Schema(topic)
		if err != nil {
			t.Fatalf("Schema(%q) error: %v", topic, err)
		}
		if len(b) == 0 {
			t.Fatalf("Schema(%q) returned empty bytes", topic)
		}
	}
}

func TestSchema_UnknownTopicReturnsErrUnknownTopic(t *testing.T) {
	_, err := Schema("llm.does.not.exist")
	if !errors.Is(err, ErrUnknownTopic) {
		t.Fatalf("want ErrUnknownTopic, got %v", err)
	}
}

func TestSchemas_ParseAsJSON(t *testing.T) {
	for _, topic := range Topics() {
		b, _ := Schema(topic)
		var parsed map[string]interface{}
		if err := json.Unmarshal(b, &parsed); err != nil {
			t.Fatalf("Schema(%q) not valid JSON: %v", topic, err)
		}
		if parsed["$id"] == nil {
			t.Errorf("Schema(%q) missing $id", topic)
		}
		if parsed["$schema"] == nil {
			t.Errorf("Schema(%q) missing $schema", topic)
		}
	}
}

// Roundtrip test (F008 §13): encode a canonical event, decode, re-encode, and
// confirm byte-for-byte equality (after stable map ordering through Go's
// json.Marshal).
func TestEventRoundTrip_UsageNormalized(t *testing.T) {
	event := map[string]interface{}{
		"schema_version":       SchemaVersion,
		"event_id":             "01890000-0000-7000-8000-000000000001",
		"source_event_id":      "01890000-0000-7000-8000-0000000000aa",
		"source_mode":          "pull",
		"source_service":       "apps/worker/usage-poller/openai",
		"provider":             "openai",
		"model":                "gpt-4o-mini",
		"operation":            "chat",
		"tenant":               "tenant-001",
		"team":                 "platform",
		"app":                  "snapcal",
		"env":                  "production",
		"project":              "snapcal-prod",
		"region":               "us-east-1",
		"input_tokens":         float64(1500),
		"output_tokens":        float64(420),
		"total_tokens":         float64(1920),
		"cost_usd_minor_units": float64(187),
		"request_count":        float64(3),
		"period_start":         "2026-05-17T10:00:00Z",
		"period_end":           "2026-05-17T10:05:00Z",
		"normalized_at":        "2026-05-17T10:05:10Z",
	}

	first, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip mismatch:\n first=%s\n second=%s", first, second)
	}
}
