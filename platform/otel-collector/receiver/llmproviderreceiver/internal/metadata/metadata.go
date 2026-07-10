// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metadata exposes the receiver's identity constants so the factory,
// receiver, and translator share a single source of truth for the component
// name, stability level, and OTel instrumentation scope. The OTel Collector
// contrib convention places this in an internal package so it cannot be
// imported by external code.
package metadata

import (
	"go.opentelemetry.io/collector/component"
)

// Type is the OTel Collector component type identifier for this receiver.
// It is the name a user puts under `receivers:` in their collector config.
const TypeStr = "llmprovider"

// Type is the typed component.Type value used by the receiver factory. The
// helper at component.MustNewType panics if the string is empty, but TypeStr
// is a compile-time constant so the panic is unreachable in practice.
var Type = component.MustNewType(TypeStr)

// MetricsStability is the stability level advertised to the Collector for the
// metrics signal. Development means: API may change without notice. Promote
// to Beta once the F037 contract test suite is green against a mock provider.
const MetricsStability = component.StabilityLevelDevelopment

// ScopeName is the OTel instrumentation-scope name attached to every metric
// emitted by this receiver. It identifies the producer for any downstream
// exporter, mirrors the GenAI-semconv recommendation for `gen_ai.*` producers,
// and lets operators filter receiver-sourced metrics from SDK-sourced metrics
// when both flow through the same pipeline.
const ScopeName = "github.com/yasvanth511/openllm-metrics-oss/platform/otel-collector/receiver/llmproviderreceiver"

// ScopeVersion tracks the receiver release. Bump on every tagged release of
// this module so consumers can pin to a known feature set.
const ScopeVersion = "0.1.0"
