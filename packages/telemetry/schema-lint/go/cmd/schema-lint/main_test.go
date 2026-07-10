// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"strings"
	"testing"
)

const validUsageJSON = `{
  "schema_version": "1",
  "event_id": "01890000-0000-7000-8000-000000000001",
  "source_event_id": "01890000-0000-7000-8000-0000000000aa",
  "source_mode": "pull",
  "source_service": "apps/worker/usage-poller/openai",
  "provider": "openai",
  "model": "gpt-4o-mini",
  "operation": "chat",
  "tenant": "tenant-001",
  "team": "platform",
  "env": "production",
  "input_tokens": 10,
  "output_tokens": 5,
  "total_tokens": 15,
  "cost_usd_minor_units": 1,
  "period_start": "2026-05-17T10:00:00Z",
  "period_end": "2026-05-17T10:05:00Z",
  "normalized_at": "2026-05-17T10:05:10Z"
}`

const eventWithPromptJSON = `{
  "schema_version": "1",
  "event_id": "x",
  "tenant": "t",
  "prompt": "leak me"
}`

func TestRun_ValidEventStdinExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(
		[]string{"--topic", "llm.usage.normalized"},
		strings.NewReader(validUsageJSON),
		&stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK") {
		t.Errorf("stdout missing OK: %q", stdout.String())
	}
}

func TestRun_BadEventStdinExitsOne(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(
		[]string{"--topic", "llm.usage.normalized"},
		strings.NewReader(eventWithPromptJSON),
		&stdout, &stderr,
	)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "OLLM-LINT-004") {
		t.Errorf("stderr missing forbidden-field code: %q", stderr.String())
	}
}

func TestRun_MissingTopicExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, strings.NewReader(""), &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
