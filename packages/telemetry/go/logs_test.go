// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	telemetry "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/go"
)

func TestLogger_AlwaysIncludesServiceAndEnv(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.LoggerOptions{
		ServiceName: "gateway",
		Environment: telemetry.EnvStaging,
		Writer:      &buf,
	})
	logger.Info("hello")

	got := decodeLine(t, &buf)
	if got["service"] != "gateway" {
		t.Fatalf("missing service field: %v", got)
	}
	if got["env"] != "staging" {
		t.Fatalf("missing env field: %v", got)
	}
}

func TestLogger_RedactsSensitiveAttributes(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.LoggerOptions{
		ServiceName: "gateway",
		Environment: telemetry.EnvDev,
		Writer:      &buf,
	})
	logger.Info("call", "api_key", "sk-leaked", "model", "gpt-4o-mini")

	got := decodeLine(t, &buf)
	if got["api_key"] == "sk-leaked" {
		t.Fatalf("api_key leaked: %v", got["api_key"])
	}
	if got["model"] != "gpt-4o-mini" {
		t.Fatalf("non-sensitive field mutated: %v", got["model"])
	}
}

func TestLogger_InjectsTraceAndSpanIDs(t *testing.T) {
	t.Parallel()

	tp := sdktrace.NewTracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "op")
	defer span.End()

	var buf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.LoggerOptions{
		ServiceName: "gateway",
		Environment: telemetry.EnvDev,
		Writer:      &buf,
	})
	logger.InfoContext(ctx, "in span")

	got := decodeLine(t, &buf)
	if got["trace_id"] == nil || got["trace_id"] == "" {
		t.Fatalf("trace_id missing: %v", got)
	}
	if got["span_id"] == nil || got["span_id"] == "" {
		t.Fatalf("span_id missing: %v", got)
	}
}

func TestLogger_InjectsTenantID(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := telemetry.NewLogger(telemetry.LoggerOptions{
		ServiceName: "gateway",
		Environment: telemetry.EnvDev,
		Writer:      &buf,
	})
	ctx := telemetry.WithTenant(context.Background(), "tenant-42")
	logger.InfoContext(ctx, "scoped")

	got := decodeLine(t, &buf)
	if got["tenant_id"] != "tenant-42" {
		t.Fatalf("tenant_id missing: %v", got)
	}
}

func TestWithTenant_EmptyIsNoop(t *testing.T) {
	t.Parallel()
	ctx := telemetry.WithTenant(context.Background(), "")
	if got := telemetry.TenantFromContext(ctx); got != "" {
		t.Fatalf("expected empty tenant, got %q", got)
	}
}

func decodeLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("logger produced no output")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("decode log line: %v\nline: %s", err, line)
	}
	return m
}
