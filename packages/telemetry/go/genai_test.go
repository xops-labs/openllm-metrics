// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry_test

import (
	"testing"

	telemetry "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/go"
)

func TestGenAIAttributes_AttributeSetSkipsZeroFields(t *testing.T) {
	t.Parallel()

	a := telemetry.GenAIAttributes{
		System:       "openai",
		RequestModel: "gpt-4o-mini",
		Operation:    "chat",
	}
	set := a.AttributeSet()
	if set.Len() != 3 {
		t.Fatalf("expected 3 attributes (system, request.model, operation.name), got %d: %v", set.Len(), set.ToSlice())
	}

	got := map[string]string{}
	for _, kv := range set.ToSlice() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["gen_ai.system"] != "openai" {
		t.Fatalf("system mismatch: %v", got)
	}
	if got["gen_ai.request.model"] != "gpt-4o-mini" {
		t.Fatalf("request.model mismatch: %v", got)
	}
	if got["gen_ai.operation.name"] != "chat" {
		t.Fatalf("operation.name mismatch: %v", got)
	}
	if _, present := got["gen_ai.response.model"]; present {
		t.Fatal("response.model should be omitted when empty")
	}
}

func TestGenAIInstrumentNames_MatchOTelSpec(t *testing.T) {
	t.Parallel()

	// These constants are wired into platform/observability/otel_alignment.md.
	// Drift between the doc and the code is a contract break; the alignment
	// table lists these four signals as first-class.
	cases := map[string]string{
		"client operation duration":  telemetry.MetricClientOperationDuration,
		"client token usage":         telemetry.MetricClientTokenUsage,
		"server request duration":    telemetry.MetricServerRequestDuration,
		"server time to first token": telemetry.MetricServerTimeToFirstToken,
	}
	want := map[string]string{
		"client operation duration":  "gen_ai.client.operation.duration",
		"client token usage":         "gen_ai.client.token.usage",
		"server request duration":    "gen_ai.server.request.duration",
		"server time to first token": "gen_ai.server.time_to_first_token",
	}
	for k, v := range cases {
		if v != want[k] {
			t.Fatalf("%s: got %q, want %q", k, v, want[k])
		}
	}
}
