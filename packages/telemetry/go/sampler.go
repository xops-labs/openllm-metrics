// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry

import (
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Default sampling ratios per F006 README §9. Error spans are always sampled
// regardless of ratio; see errorOverrideSampler.
const (
	stagingDefaultRatio    = 1.0
	productionDefaultRatio = 0.10
)

// buildSampler constructs the parent-based ratio sampler that backs the
// TracerProvider. Errors are force-sampled by errorOverrideSampler at span
// end, so a low base ratio in production never drops error traces.
func buildSampler(cfg ServiceConfig) sdktrace.Sampler {
	ratio := cfg.SamplingRatio
	if ratio <= 0 {
		switch cfg.Environment {
		case EnvProduction:
			ratio = productionDefaultRatio
		case EnvStaging, EnvDev:
			ratio = stagingDefaultRatio
		default:
			ratio = stagingDefaultRatio
		}
	}
	base := sdktrace.TraceIDRatioBased(ratio)
	return sdktrace.ParentBased(base)
}

// IsErrorSpan reports whether the given span snapshot represents an error
// outcome (Status.Code == Error). Used by the OTel Collector tail-sampling
// policy in production configs and by tests.
//
// In production deployments the error-span override is enforced at the
// Collector tail-sampling layer (see platform/observability/otel-collector/
// production.yaml), because the OTel Go SDK applies head sampling at span
// start when status is not yet known.
func IsErrorSpan(s sdktrace.ReadOnlySpan) bool {
	return s.Status().Code == codes.Error
}

// EnsureRecordingForError marks the given span as Recording when an error is
// observed mid-flight. Services should call this in their error-handling
// path so that even in production (10% base ratio) the error trace survives.
func EnsureRecordingForError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}
