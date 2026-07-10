// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package llmproviderreceiver implements an OpenTelemetry Collector receiver
// that reads canonical OpenLLM Metrics runtime.event.v1 events from a Kafka/
// Redpanda topic and emits them as OTLP metrics following the GenAI semantic
// conventions.
//
// The receiver is built and shipped as a Collector contrib component. Operators
// add it to a custom distribution with `ocb` (the OpenTelemetry Collector
// Builder) — see README.md and the sibling builder-config.yaml.
//
// Design intent. The receiver is the *bridge* between the OpenLLM Metrics bus
// (which carries multi-tenant control-plane events) and any standard OTel
// metrics pipeline (Prometheus, OTLP/HTTP, vendor exporters). It deliberately
// does not poll providers directly: the upstream `llm-usage-exporter`, the
// in-repo gateway (F018), and the SDKs (F019–F022) all converge on the
// canonical bus event, and this receiver is the OTel-shaped tap on that bus.
package llmproviderreceiver

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"

	"github.com/yasvanth511/openllm-metrics-oss/platform/otel-collector/receiver/llmproviderreceiver/internal/metadata"
)

// NewFactory returns the receiver.Factory the Collector uses to discover and
// instantiate llmprovider receivers. The exported symbol name follows the
// contrib convention — `ocb`'s code generator looks for `NewFactory` on every
// component module.
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, metadata.MetricsStability),
	)
}

// createDefaultConfig is invoked by the Collector before the user config is
// merged in. Returning a zero-value Config plus the bare-minimum defaults
// makes the resulting validation errors precise — required fields surface as
// missing-broker errors rather than as nil-pointer panics later.
func createDefaultConfig() component.Config {
	return &Config{}
}

// createMetricsReceiver builds the receiver.Metrics implementation. The
// Collector framework calls this exactly once per pipeline; ctx is the build
// context, not the runtime context.
func createMetricsReceiver(
	_ context.Context,
	set receiver.Settings,
	rawCfg component.Config,
	next consumer.Metrics,
) (receiver.Metrics, error) {
	cfg := rawCfg.(*Config)
	return newReceiver(set, cfg, next)
}
