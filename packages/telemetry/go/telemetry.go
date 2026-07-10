// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package telemetry provides the shared OpenTelemetry initialization, GenAI
// semantic-convention helpers, and redaction interceptor used by every
// OpenLLM Metrics service.
//
// All services must call Init at boot to obtain a unified TracerProvider and
// MeterProvider configured with:
//
//   - W3C Trace Context propagation across HTTP, gRPC, and the streaming bus.
//   - The mandatory redaction interceptor (see redact.go) that strips LLM
//     payloads, secrets, and provider credentials before any signal leaves
//     the process.
//   - Sampling defaults documented in F006 (100% staging, 10% production,
//     100% for error spans).
//
// Structured-log helpers live in logs.go and use stdlib log/slog rather than
// the experimental OTel logs SDK; logs are emitted as JSON to stdout and
// collected by the OTel Collector filelog receiver (see
// platform/observability/otel-collector/).
//
// Project-specific llm_* metrics are emitted by F008/F010 and downstream
// services. F006 only ships the shared SDK plumbing.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Environment represents the deployment environment a service is running in.
// It is recorded as a resource attribute and drives the sampling default.
type Environment string

const (
	// EnvDev is local development. With OTLPEndpoint unset, exporters
	// connect to localhost:4317 by default.
	EnvDev Environment = "dev"
	// EnvStaging samples 100% of spans by default.
	EnvStaging Environment = "staging"
	// EnvProduction samples 10% of spans by default; error spans are always
	// sampled regardless of ratio.
	EnvProduction Environment = "production"
)

// Resource attribute keys. We keep these as plain attribute.Key values rather
// than depending on a specific semconv version, because OTel semconv module
// paths shift between minor releases and we want F006 to pin only the stable
// API surface.
const (
	AttrServiceName    = attribute.Key("service.name")
	AttrServiceVersion = attribute.Key("service.version")
	AttrDeployEnv      = attribute.Key("deployment.environment")
	AttrTenantID       = attribute.Key("tenant.id")
)

// ServiceConfig is the per-service initialization input.
//
// ServiceName, ServiceVersion, and Environment are required. OTLPEndpoint may
// be empty — the SDK then targets localhost:4317. TenantID, when non-empty,
// is attached as a default resource attribute so every span and metric
// carries it without per-call plumbing.
type ServiceConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    Environment
	OTLPEndpoint   string
	TenantID       string
	// SamplingRatio overrides the environment default. Negative means
	// "use the default for Environment". Must otherwise be in [0, 1].
	SamplingRatio float64
	// RedactionKeys overrides the default redaction key set. Empty means
	// "use DefaultRedactionKeys".
	RedactionKeys []string
}

// Shutdown is returned by Init and must be deferred by the caller. It flushes
// pending signals and closes exporters with the supplied context deadline.
type Shutdown func(context.Context) error

// Init configures the global TracerProvider and MeterProvider for the calling
// service.
//
// On any error, partially-initialized providers are torn down before
// returning. The returned Shutdown must be invoked at process exit.
func Init(ctx context.Context, cfg ServiceConfig) (Shutdown, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	redactor := NewRedactor(cfg.RedactionKeys)

	tp, tpShutdown, err := buildTracerProvider(ctx, cfg, res, redactor)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build tracer provider: %w", err)
	}

	mp, mpShutdown, err := buildMeterProvider(ctx, cfg, res, redactor)
	if err != nil {
		_ = tpShutdown(ctx)
		return nil, fmt.Errorf("telemetry: build meter provider: %w", err)
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		return errors.Join(
			tpShutdown(ctx),
			mpShutdown(ctx),
		)
	}, nil
}

func (c ServiceConfig) validate() error {
	if c.ServiceName == "" {
		return errors.New("telemetry: ServiceName is required")
	}
	if c.ServiceVersion == "" {
		return errors.New("telemetry: ServiceVersion is required")
	}
	switch c.Environment {
	case EnvDev, EnvStaging, EnvProduction:
	default:
		return fmt.Errorf("telemetry: invalid Environment %q", c.Environment)
	}
	if c.SamplingRatio > 1 {
		return fmt.Errorf("telemetry: SamplingRatio %v > 1", c.SamplingRatio)
	}
	return nil
}

func buildResource(ctx context.Context, cfg ServiceConfig) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		AttrServiceName.String(cfg.ServiceName),
		AttrServiceVersion.String(cfg.ServiceVersion),
		AttrDeployEnv.String(string(cfg.Environment)),
	}
	if cfg.TenantID != "" {
		attrs = append(attrs, AttrTenantID.String(cfg.TenantID))
	}
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
}

func buildTracerProvider(ctx context.Context, cfg ServiceConfig, res *resource.Resource, redactor *Redactor) (*sdktrace.TracerProvider, Shutdown, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithInsecure()}
	if cfg.OTLPEndpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
	}
	rawExp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, err
	}

	// Wrap the exporter so every span snapshot is redacted before it leaves
	// the process. SpanProcessor.OnEnd receives a ReadOnlySpan and cannot
	// mutate attributes; the wrapping exporter reconstructs each span with
	// a redacted attribute set and forwards to the OTLP transport.
	exp := NewRedactingSpanExporter(rawExp, redactor)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(buildSampler(cfg)),
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(5*time.Second),
		),
	)
	return tp, func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}, nil
}

func buildMeterProvider(ctx context.Context, cfg ServiceConfig, res *resource.Resource, redactor *Redactor) (*metric.MeterProvider, Shutdown, error) {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithInsecure()}
	if cfg.OTLPEndpoint != "" {
		opts = append(opts, otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint))
	}
	exp, err := otlpmetricgrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, err
	}

	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithView(redactor.MetricView()),
		metric.WithReader(metric.NewPeriodicReader(exp,
			metric.WithInterval(15*time.Second),
		)),
	)
	return mp, func(ctx context.Context) error {
		return mp.Shutdown(ctx)
	}, nil
}
