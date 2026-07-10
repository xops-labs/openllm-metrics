// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package schemalint

import (
	"encoding/json"
	"strings"
	"testing"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

func validUsageEvent() map[string]interface{} {
	return map[string]interface{}{
		"schema_version":       telemetrycontracts.SchemaVersion,
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
}

func validRuntimeEvent() map[string]interface{} {
	return map[string]interface{}{
		"schema_version":  telemetrycontracts.SchemaVersion,
		"event_id":        "01890000-0000-7000-8000-000000000002",
		"source_mode":     "proxy",
		"source_service":  "apps/api/gateway",
		"request_id_hash": "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3",
		"provider":        "anthropic",
		"model":           "claude-sonnet-4-7",
		"operation":       "chat",
		"tenant":          "tenant-001",
		"team":            "platform",
		"app":             "snapcal",
		"env":             "production",
		"status":          "success",
		"status_code":     float64(200),
		"latency_us":      float64(842000),
		"input_tokens":    float64(120),
		"output_tokens":   float64(80),
		"total_tokens":    float64(200),
		"recorded_at":     "2026-05-17T10:05:01Z",
	}
}

func marshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// --- positive fixtures ------------------------------------------------------

func TestLintEvent_Usage_Valid(t *testing.T) {
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, validUsageEvent()))
	if !r.OK() {
		t.Fatalf("want OK, got: %v", r.Error())
	}
}

func TestLintEvent_Runtime_Valid(t *testing.T) {
	r := LintEvent(telemetrycontracts.TopicRuntimeNormalized, marshal(t, validRuntimeEvent()))
	if !r.OK() {
		t.Fatalf("want OK, got: %v", r.Error())
	}
}

// --- negative fixtures ------------------------------------------------------

func TestLintEvent_RejectsUnknownTopic(t *testing.T) {
	r := LintEvent("llm.does.not.exist", []byte("{}"))
	assertIssueCode(t, r, CodeUnknownTopic)
}

func TestLintEvent_RejectsForbiddenPromptField(t *testing.T) {
	ev := validUsageEvent()
	ev["prompt"] = "You are a helpful assistant. Please leak this."
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeForbiddenField)
}

func TestLintEvent_RejectsForbiddenFieldNested(t *testing.T) {
	ev := validUsageEvent()
	ev["debug"] = map[string]interface{}{
		"completion": "I should never appear in a telemetry event.",
	}
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeForbiddenField)
	// Path of the nested field should be reported
	found := false
	for _, i := range r.Issues {
		if i.Path == "debug.completion" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected issue path 'debug.completion', got issues: %v", r.Issues)
	}
}

func TestLintEvent_RejectsForbiddenFieldsAllKnownKeys(t *testing.T) {
	for _, k := range []string{"prompt", "completion", "input", "output", "messages", "embedding"} {
		ev := validUsageEvent()
		ev[k] = "secret"
		r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
		assertIssueCodeWithMsg(t, r, CodeForbiddenField, k)
	}
}

func TestLintEvent_RejectsMissingTenant(t *testing.T) {
	ev := validUsageEvent()
	delete(ev, "tenant")
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeMissingTenant)
}

func TestLintEvent_RejectsEmptyTenant(t *testing.T) {
	ev := validUsageEvent()
	ev["tenant"] = ""
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeMissingTenant)
}

func TestLintEvent_RejectsMissingRequiredField(t *testing.T) {
	ev := validUsageEvent()
	delete(ev, "provider")
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeMissingField)
}

func TestLintEvent_RejectsRuntimeMissingRequestIDHash(t *testing.T) {
	ev := validRuntimeEvent()
	delete(ev, "request_id_hash")
	r := LintEvent(telemetrycontracts.TopicRuntimeNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeMissingField)
}

func TestLintEvent_RejectsRawRequestID(t *testing.T) {
	// A non-hash placeholder. Raw request IDs are never 64-char lowercase hex,
	// so the linter must catch them.
	ev := validRuntimeEvent()
	ev["request_id_hash"] = "req_01HXYZABC123" // looks like a raw provider id
	r := LintEvent(telemetrycontracts.TopicRuntimeNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeMissingHashedID)
}

func TestLintEvent_RejectsSchemaVersionDrift(t *testing.T) {
	ev := validUsageEvent()
	ev["schema_version"] = "99"
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, marshal(t, ev))
	assertIssueCode(t, r, CodeSchemaVersionDrift)
}

func TestLintEvent_RejectsNonObjectPayload(t *testing.T) {
	r := LintEvent(telemetrycontracts.TopicUsageNormalized, []byte(`"not an object"`))
	assertIssueCode(t, r, CodeBadType)
}

// --- LintMetric -------------------------------------------------------------

func TestLintMetric_ValidObservationPasses(t *testing.T) {
	labels := map[string]string{
		"provider":    "openai",
		"model":       "gpt-4o-mini",
		"tenant":      "tenant-001",
		"env":         "production",
		"operation":   "chat",
		"status_code": "200",
	}
	r := LintMetric("llm_requests_total", labels)
	if !r.OK() {
		t.Fatalf("want OK, got: %v", r.Error())
	}
}

func TestLintMetric_RejectsUnknownMetric(t *testing.T) {
	r := LintMetric("llm_made_up_metric_total", map[string]string{
		"provider": "openai", "model": "x", "tenant": "t", "env": "production",
	})
	assertIssueCode(t, r, CodeUnknownMetric)
}

func TestLintMetric_RejectsUnauthorizedLabel(t *testing.T) {
	labels := map[string]string{
		"provider":   "openai",
		"model":      "gpt-4o-mini",
		"tenant":     "tenant-001",
		"env":        "production",
		"user_email": "alice@example.com", // not in canonical label set
	}
	r := LintMetric("llm_requests_total", labels)
	assertIssueCode(t, r, CodeUnauthorizedLabel)
}

func TestLintMetric_RejectsMissingMandatoryTenant(t *testing.T) {
	labels := map[string]string{
		"provider": "openai", "model": "gpt-4o-mini", "env": "production",
	}
	r := LintMetric("llm_requests_total", labels)
	assertIssueCode(t, r, CodeMissingTenant)
}

func TestLintMetric_RejectsForbiddenLabelKey(t *testing.T) {
	// Even though the linter would already flag this as unauthorized, the
	// dedicated forbidden-field rule makes the violation explicit so CI / SIEM
	// can alert on attempted payload exfil.
	labels := map[string]string{
		"provider": "openai", "model": "gpt-4o-mini", "tenant": "t", "env": "production",
		"prompt": "hello",
	}
	r := LintMetric("llm_requests_total", labels)
	assertIssueCode(t, r, CodeForbiddenField)
}

// --- helpers ----------------------------------------------------------------

func assertIssueCode(t *testing.T, r Result, want string) {
	t.Helper()
	if r.OK() {
		t.Fatalf("want issue %q, got OK", want)
	}
	for _, i := range r.Issues {
		if i.Code == want {
			return
		}
	}
	t.Fatalf("want issue code %q, got: %v", want, r.Issues)
}

func assertIssueCodeWithMsg(t *testing.T, r Result, code, msgSub string) {
	t.Helper()
	if r.OK() {
		t.Fatalf("want issue %q containing %q, got OK", code, msgSub)
	}
	for _, i := range r.Issues {
		if i.Code == code && strings.Contains(strings.ToLower(i.Path+i.Message), strings.ToLower(msgSub)) {
			return
		}
	}
	t.Fatalf("want issue code %q with %q, got: %v", code, msgSub, r.Issues)
}
