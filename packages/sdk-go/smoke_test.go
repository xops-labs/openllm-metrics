// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Smoke tests for the openllm Go SDK.
//
// All tests use in-memory OTel exporters so no collector is needed.
// The tests operate at the package level (package openllm) to access
// unexported helpers (newRuntime, currentRt, rtMu).

package openllm

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// initInMemory replaces the global OTel providers with in-memory ones,
// wires the SDK runtime, and returns a teardown func that restores state.
func initInMemory(t *testing.T) (*tracetest.SpanRecorder, *sdkmetric.ManualReader, func()) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	rt, err := newRuntime(Options{ServiceName: "smoke-test"}, tp, mp)
	if err != nil {
		t.Fatalf("newRuntime: %v", err)
	}

	rtMu.Lock()
	prev := currentRt
	currentRt = rt
	rtMu.Unlock()

	return recorder, reader, func() {
		rtMu.Lock()
		currentRt = prev
		rtMu.Unlock()
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	}
}

// TestSmokeSpanEmitted verifies that StartLlmCall creates a span with the
// correct name and that it carries the gen_ai.system / gen_ai.request.model
// attributes on end.
func TestSmokeSpanEmitted(t *testing.T) {
	recorder, _, teardown := initInMemory(t)
	defer teardown()

	ctx := context.Background()
	call, callCtx := StartLlmCall(ctx, CallOptions{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Tenant:   "acme",
		Team:     "platform",
		App:      "smoke-test",
		Env:      "test",
		Project:  "openllm-test",
		Route:    "primary",
	})
	call.SetPromptTokens(10)
	call.SetCompletionTokens(25)
	call.End()

	if callCtx == nil {
		t.Fatal("expected non-nil context from StartLlmCall")
	}

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span; got none")
	}

	span := spans[0]
	if span.Name() != SpanNameLlmCall {
		t.Errorf("span name: got %q, want %q", span.Name(), SpanNameLlmCall)
	}

	attrMap := make(map[string]string)
	for _, kv := range span.Attributes() {
		attrMap[string(kv.Key)] = kv.Value.AsString()
	}

	wantAttrs := map[string]string{
		GenAISystem:       "openai",
		GenAIRequestModel: "gpt-4o-mini",
	}
	for k, want := range wantAttrs {
		if got, ok := attrMap[k]; !ok || got != want {
			t.Errorf("span attr %q: got %q, want %q", k, got, want)
		}
	}
}

// TestSmokeBaggagePropagation verifies that the tenant bundle is written onto
// OTel baggage in the context returned by StartLlmCall.
func TestSmokeBaggagePropagation(t *testing.T) {
	_, _, teardown := initInMemory(t)
	defer teardown()

	ctx := context.Background()
	_, callCtx := StartLlmCall(ctx, CallOptions{
		Provider: "anthropic",
		Model:    "claude-3-haiku",
		Tenant:   "tenantA",
		Team:     "teamB",
		App:      "appC",
		Env:      "staging",
		Project:  "projD",
	})

	got := CurrentTenantBaggage(callCtx)

	wantPairs := map[string]string{
		"tenant":  "tenantA",
		"team":    "teamB",
		"app":     "appC",
		"env":     "staging",
		"project": "projD",
	}
	for k, want := range wantPairs {
		if v := got[k]; v != want {
			t.Errorf("baggage[%q] = %q, want %q", k, v, want)
		}
	}
}

// TestSmokeMetricsEmitted verifies that End() records all four expected metric
// instruments into the in-memory reader.
func TestSmokeMetricsEmitted(t *testing.T) {
	_, reader, teardown := initInMemory(t)
	defer teardown()

	ctx := context.Background()
	call, _ := StartLlmCall(ctx, CallOptions{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Tenant:   "acme",
		Team:     "platform",
		App:      "metrics-test",
		Env:      "test",
		Project:  "openllm-test",
	})
	call.SetPromptTokens(42)
	call.SetCompletionTokens(128)
	call.SetUsageDollars(0.001)
	call.End()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("reader.Collect: %v", err)
	}

	foundMetrics := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			foundMetrics[m.Name] = true
		}
	}

	wantMetrics := []string{
		MetricClientOperationDuration,
		MetricClientTokenUsage,
		MetricLlmRequestsTotal,
		MetricLlmUsageDollars,
	}
	for _, name := range wantMetrics {
		if !foundMetrics[name] {
			t.Errorf("metric %q not found in collected data; found: %v", name, foundMetrics)
		}
	}
}

// TestSmokeErrorKindLabel verifies that SetErrorKind sets the error.type
// attribute on the span.
func TestSmokeErrorKindLabel(t *testing.T) {
	recorder, _, teardown := initInMemory(t)
	defer teardown()

	ctx := context.Background()
	call, _ := StartLlmCall(ctx, CallOptions{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Tenant:   "acme",
		Team:     "platform",
		App:      "error-test",
		Env:      "test",
		Project:  "openllm-test",
	})
	call.SetErrorKind("rate_limit")
	call.End()

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected at least one span")
	}

	attrMap := map[attribute.Key]attribute.Value{}
	for _, kv := range spans[0].Attributes() {
		attrMap[kv.Key] = kv.Value
	}
	errType := attrMap[attribute.Key(AttrErrorType)]
	if errType.AsString() != "rate_limit" {
		t.Errorf("error.type = %q, want %q", errType.AsString(), "rate_limit")
	}
}

// TestSmokeNoInitGraceful verifies that StartLlmCall returns a no-op handle
// (and does not panic) when Init has not been called.
func TestSmokeNoInitGraceful(t *testing.T) {
	// Make sure runtime is clear.
	rtMu.Lock()
	prev := currentRt
	currentRt = nil
	rtMu.Unlock()
	defer func() {
		rtMu.Lock()
		currentRt = prev
		rtMu.Unlock()
	}()

	ctx := context.Background()
	call, callCtx := StartLlmCall(ctx, CallOptions{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Tenant:   "acme",
	})

	// Must not panic on End(); no-op handle.
	call.End()
	call.End() // idempotent

	if callCtx == nil {
		t.Error("expected non-nil context even in no-op mode")
	}
}
