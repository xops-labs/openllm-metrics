// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// DefaultRedactionKeys is the canonical key list from F006 + the OTel
// alignment doc (platform/observability/otel_alignment.md). Keys are matched
// case-insensitively against attribute, log field, and metric label names.
//
// Bypassing this list is a CODEOWNERS-protected change; see .github/CODEOWNERS.
var DefaultRedactionKeys = []string{
	// Auth and credential keys.
	"authorization",
	"api_key",
	"apikey",
	"x-api-key",
	"secret",
	"password",
	"token",
	"refresh_token",
	"access_token",
	// LLM payload keys (never persisted in telemetry).
	"prompt",
	"completion",
	"messages",
	"input",
	"output",
	"content",
	"response",
	"embedding",
	"request_body",
	"response_body",
}

// redactedValue is the placeholder substituted for any redacted attribute.
const redactedValue = "[REDACTED]"

// keyLikePattern catches values that look like API keys or bearer tokens
// (40+ hex chars, or 30+ url-safe base64 chars). This is a defense-in-depth
// rule for cases where a sensitive value lands under an unexpected key.
var keyLikePattern = regexp.MustCompile(`(?i)^(?:[a-f0-9]{40,}|[A-Za-z0-9_\-]{30,})$`)

// Redactor strips sensitive attribute values from spans, logs, and metric
// labels before they leave the process.
//
// The same Redactor instance is shared across the tracer, meter, and slog
// handler so the redaction policy is consistent regardless of signal type.
// The key set is immutable after construction, so concurrent use requires no
// synchronization.
type Redactor struct {
	keys map[string]struct{}
}

// NewRedactor returns a Redactor configured with the given keys. If keys is
// empty or nil, DefaultRedactionKeys is used. All keys are lowercased.
func NewRedactor(keys []string) *Redactor {
	if len(keys) == 0 {
		keys = DefaultRedactionKeys
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}
	return &Redactor{keys: set}
}

// IsSensitiveKey reports whether the supplied key is in the redaction set.
// Matching is case-insensitive on the lowercase form of the key.
func (r *Redactor) IsSensitiveKey(key string) bool {
	_, ok := r.keys[strings.ToLower(key)]
	return ok
}

// looksKeyLike reports whether the supplied value pattern-matches a credential.
func looksKeyLike(v string) bool {
	if len(v) < 30 {
		return false
	}
	return keyLikePattern.MatchString(v)
}

// RedactAttribute returns the attribute with its value replaced by
// redactedValue if either the key is sensitive or the value pattern-matches
// a credential.
func (r *Redactor) RedactAttribute(kv attribute.KeyValue) attribute.KeyValue {
	if r.IsSensitiveKey(string(kv.Key)) {
		return attribute.String(string(kv.Key), redactedValue)
	}
	if kv.Value.Type() == attribute.STRING && looksKeyLike(kv.Value.AsString()) {
		return attribute.String(string(kv.Key), redactedValue)
	}
	return kv
}

// RedactAttributes applies RedactAttribute element-wise. The returned slice
// is always a fresh allocation so callers may not assume aliasing.
func (r *Redactor) RedactAttributes(in []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, len(in))
	for i, kv := range in {
		out[i] = r.RedactAttribute(kv)
	}
	return out
}

// MetricView returns an OTel metric View that applies an attribute filter
// dropping sensitive labels from every instrument's stream. Labels that
// look key-like are also dropped (rather than redacted) because the label
// value space is the cardinality budget — keeping a high-cardinality
// pseudo-redacted value would still blow the budget.
func (r *Redactor) MetricView() sdkmetric.View {
	return func(i sdkmetric.Instrument) (sdkmetric.Stream, bool) {
		stream := sdkmetric.Stream{
			Name:        i.Name,
			Description: i.Description,
			Unit:        i.Unit,
			AttributeFilter: func(kv attribute.KeyValue) bool {
				if r.IsSensitiveKey(string(kv.Key)) {
					return false
				}
				if kv.Value.Type() == attribute.STRING && looksKeyLike(kv.Value.AsString()) {
					return false
				}
				return true
			},
		}
		return stream, true
	}
}

// RedactingSpanExporter wraps a downstream sdktrace.SpanExporter and redacts
// every span snapshot before it leaves the process.
//
// Span attributes are immutable once a span ends (sdktrace.ReadOnlySpan is
// the type passed to SpanProcessor.OnEnd), so we cannot redact via a
// SpanProcessor. Wrapping the exporter instead lets us reconstruct each span
// with a redacted attribute slice and forward to the real OTLP exporter.
type RedactingSpanExporter struct {
	inner sdktrace.SpanExporter
	r     *Redactor
}

// NewRedactingSpanExporter wraps inner with the supplied Redactor.
func NewRedactingSpanExporter(inner sdktrace.SpanExporter, r *Redactor) *RedactingSpanExporter {
	return &RedactingSpanExporter{inner: inner, r: r}
}

// ExportSpans rebuilds each ReadOnlySpan with redacted attributes and forwards.
func (e *RedactingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	redacted := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		redacted[i] = e.redactSpan(s)
	}
	return e.inner.ExportSpans(ctx, redacted)
}

// Shutdown forwards to the inner exporter.
func (e *RedactingSpanExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}

// redactSpan returns a snapshot of s with attributes and event attributes
// stripped of sensitive values. Span name, status, links, and resource are
// preserved unchanged.
func (e *RedactingSpanExporter) redactSpan(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	stub := tracetest.SpanStub{
		Name:                   s.Name(),
		SpanContext:            s.SpanContext(),
		Parent:                 s.Parent(),
		SpanKind:               s.SpanKind(),
		StartTime:              s.StartTime(),
		EndTime:                s.EndTime(),
		Attributes:             e.r.RedactAttributes(s.Attributes()),
		Events:                 redactEvents(s.Events(), e.r),
		Links:                  s.Links(),
		Status:                 s.Status(),
		DroppedAttributes:      s.DroppedAttributes(),
		DroppedEvents:          s.DroppedEvents(),
		DroppedLinks:           s.DroppedLinks(),
		ChildSpanCount:         s.ChildSpanCount(),
		Resource:               s.Resource(),
		InstrumentationLibrary: s.InstrumentationScope(),
	}
	return stub.Snapshot()
}

func redactEvents(in []sdktrace.Event, r *Redactor) []sdktrace.Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]sdktrace.Event, len(in))
	for i, ev := range in {
		out[i] = sdktrace.Event{
			Name:                  ev.Name,
			Attributes:            r.RedactAttributes(ev.Attributes),
			DroppedAttributeCount: ev.DroppedAttributeCount,
			Time:                  ev.Time,
		}
	}
	return out
}
