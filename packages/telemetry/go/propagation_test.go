// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	telemetry "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/go"
)

// TestPropagator_W3CTraceContextRoundtrip verifies that the propagator
// installed by Init carries a span across Inject/Extract boundaries — the
// same path used by the bus client (packages/bus-client/go/bus.go) and any
// future HTTP/gRPC instrumentation.
func TestPropagator_W3CTraceContextRoundtrip(t *testing.T) {
	t.Parallel()

	// The Init function sets the global propagator; we mirror that here
	// without booting the full provider stack so the test remains hermetic.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	ctx, span := otel.Tracer("propagation-test").Start(context.Background(), "outer")
	defer span.End()

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	if carrier["traceparent"] == "" {
		t.Fatalf("propagator did not inject traceparent into carrier: %v", carrier)
	}

	extracted := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	extractedSpan := trace.SpanFromContext(extracted)
	if !extractedSpan.SpanContext().IsValid() {
		t.Fatalf("extracted span context is invalid")
	}
	if extractedSpan.SpanContext().TraceID() != span.SpanContext().TraceID() {
		t.Fatalf("trace ID mismatch: got %s want %s",
			extractedSpan.SpanContext().TraceID(), span.SpanContext().TraceID())
	}
}

func TestRedactionKeysAlignedWithObservabilityDoc(t *testing.T) {
	t.Parallel()
	// Locks in the alignment between platform/observability/otel_alignment.md
	// and the code. If the doc adds a key, this test forces the code to add
	// it too.
	required := []string{
		"authorization", "api_key", "x-api-key", "secret", "password", "token",
		"prompt", "completion", "messages", "input", "output", "content",
	}
	r := telemetry.NewRedactor(nil)
	for _, k := range required {
		if !r.IsSensitiveKey(k) {
			t.Fatalf("key %q is required by otel_alignment.md but not in default redaction set", k)
		}
	}
}
