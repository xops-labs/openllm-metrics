// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry_test

import (
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"

	telemetry "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/go"
)

func TestDefaultRedactionKeys_RedactsEveryListedKey(t *testing.T) {
	t.Parallel()
	r := telemetry.NewRedactor(nil)

	for _, key := range telemetry.DefaultRedactionKeys {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			kv := attribute.String(key, "any-secret-value")
			got := r.RedactAttribute(kv)
			if got.Value.AsString() == "any-secret-value" {
				t.Fatalf("redactor leaked value for key %q: %v", key, got.Value.AsString())
			}
			if !strings.HasPrefix(got.Value.AsString(), "[REDACTED") {
				t.Fatalf("expected REDACTED placeholder for key %q, got %q", key, got.Value.AsString())
			}
		})
	}
}

func TestRedactor_CaseInsensitiveKeyMatch(t *testing.T) {
	t.Parallel()
	r := telemetry.NewRedactor(nil)
	cases := []string{"Authorization", "API_KEY", "X-Api-Key", "Password", "Prompt"}
	for _, k := range cases {
		got := r.RedactAttribute(attribute.String(k, "leak-me"))
		if got.Value.AsString() == "leak-me" {
			t.Fatalf("redactor failed case-insensitive match for %q", k)
		}
	}
}

func TestRedactor_KeyLikeValuePattern(t *testing.T) {
	t.Parallel()
	r := telemetry.NewRedactor(nil)

	cases := []struct {
		name   string
		key    string
		value  string
		redact bool
	}{
		{"hex 40 chars", "request_id", strings.Repeat("a", 40), true},
		{"hex 64 chars (sha256-like)", "request_id", strings.Repeat("0", 64), true},
		{"base64-like 36 chars", "anonymous", "AKIAIOSFODNN7EXAMPLEthisispadding123", true},
		{"short hex 8 chars", "request_id", "deadbeef", false},
		{"plain ascii", "model", "gpt-4o-mini", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := r.RedactAttribute(attribute.String(tc.key, tc.value))
			gotRedacted := got.Value.AsString() != tc.value
			if gotRedacted != tc.redact {
				t.Fatalf("key=%q value=%q redacted=%v want=%v", tc.key, tc.value, gotRedacted, tc.redact)
			}
		})
	}
}

func TestRedactor_NonStringValuesPassThrough(t *testing.T) {
	t.Parallel()
	r := telemetry.NewRedactor(nil)
	got := r.RedactAttribute(attribute.Int64("token_count", 4096))
	if got.Value.AsInt64() != 4096 {
		t.Fatalf("non-sensitive int attribute mutated: %v", got.Value.AsInt64())
	}
}

func TestRedactor_CustomKeysOverrideDefault(t *testing.T) {
	t.Parallel()
	r := telemetry.NewRedactor([]string{"custom_secret"})

	got := r.RedactAttribute(attribute.String("custom_secret", "value"))
	if got.Value.AsString() == "value" {
		t.Fatal("custom key not redacted")
	}

	// "password" is in the default list but NOT in the custom override.
	got = r.RedactAttribute(attribute.String("password", "value"))
	if got.Value.AsString() != "value" {
		t.Fatal("custom key set should fully replace default; password should pass through")
	}
}

func TestRedactor_RedactAttributesReturnsFreshSlice(t *testing.T) {
	t.Parallel()
	r := telemetry.NewRedactor(nil)
	in := []attribute.KeyValue{
		attribute.String("api_key", "sk-test"),
		attribute.String("model", "gpt-4o-mini"),
	}
	out := r.RedactAttributes(in)
	if &in[0] == &out[0] {
		t.Fatal("RedactAttributes must not alias the input slice")
	}
	if out[0].Value.AsString() == "sk-test" {
		t.Fatal("api_key not redacted")
	}
	if out[1].Value.AsString() != "gpt-4o-mini" {
		t.Fatal("non-sensitive attribute mutated")
	}
}
