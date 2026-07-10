// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package openllm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ShutdownFunc flushes pending signals and closes exporters. It must be
// deferred by the caller at process exit.
type ShutdownFunc func(context.Context) error

// runtime holds the SDK's process-wide state — the tracer and the pre-built
// metric instruments — so every StartLlmCall pulls cheap references rather
// than re-creating instruments per call.
type runtime struct {
	tracer      trace.Tracer
	defaultTags map[string]string

	clientOpDuration metric.Float64Histogram
	clientTokenUsage metric.Int64Counter
	requestsTotal    metric.Int64Counter
	usageDollars     metric.Float64Counter
}

var (
	rtMu       sync.RWMutex
	currentRt  *runtime
	errNotInit = errors.New("openllm: Init has not been called")
)

// Init boots OpenTelemetry with the OTLP/HTTP exporters and prepares the SDK
// for use. Repeated calls are not supported — call Init once at process start
// and defer the returned shutdown.
//
// On any setup error the partially-initialized providers are torn down before
// returning so the caller's process state is clean.
func Init(ctx context.Context, opts Options, fnOpts ...Option) (ShutdownFunc, error) {
	for _, fn := range fnOpts {
		fn(&opts)
	}
	if opts.ServiceName == "" {
		return nil, errors.New("openllm: Options.ServiceName is required")
	}
	if opts.ServiceVersion == "" {
		opts.ServiceVersion = "0.0.0"
	}

	res, err := buildResource(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("openllm: build resource: %w", err)
	}

	tp, tpShutdown, err := buildTracerProvider(ctx, opts, res)
	if err != nil {
		return nil, fmt.Errorf("openllm: build tracer provider: %w", err)
	}
	mp, mpShutdown, err := buildMeterProvider(ctx, opts, res)
	if err != nil {
		_ = tpShutdown(ctx)
		return nil, fmt.Errorf("openllm: build meter provider: %w", err)
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	rt, err := newRuntime(opts, tp, mp)
	if err != nil {
		_ = tpShutdown(ctx)
		_ = mpShutdown(ctx)
		return nil, fmt.Errorf("openllm: build instruments: %w", err)
	}

	rtMu.Lock()
	currentRt = rt
	rtMu.Unlock()

	return func(ctx context.Context) error {
		rtMu.Lock()
		currentRt = nil
		rtMu.Unlock()
		return errors.Join(tpShutdown(ctx), mpShutdown(ctx))
	}, nil
}

// newRuntime pre-creates the SDK's instruments so StartLlmCall is allocation-
// light on the hot path. The token-usage instrument is a counter (not a
// histogram) per the GenAI spec because we sum into a "total tokens" series;
// the operation-duration histogram preserves the percentile shape.
func newRuntime(opts Options, tp trace.TracerProvider, mp metric.MeterProvider) (*runtime, error) {
	tracer := tp.Tracer(InstrumentationName, trace.WithInstrumentationVersion(InstrumentationVersion))
	meter := mp.Meter(InstrumentationName, metric.WithInstrumentationVersion(InstrumentationVersion))

	opDur, err := meter.Float64Histogram(
		MetricClientOperationDuration,
		metric.WithDescription("LLM client operation duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", MetricClientOperationDuration, err)
	}
	tokenUsage, err := meter.Int64Counter(
		MetricClientTokenUsage,
		metric.WithDescription("LLM token usage per request"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", MetricClientTokenUsage, err)
	}
	requests, err := meter.Int64Counter(
		MetricLlmRequestsTotal,
		metric.WithDescription("Count of LLM requests by provider/model/tenant/error_kind"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", MetricLlmRequestsTotal, err)
	}
	dollars, err := meter.Float64Counter(
		MetricLlmUsageDollars,
		metric.WithDescription("Dollarized LLM usage"),
		metric.WithUnit("USD"),
	)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", MetricLlmUsageDollars, err)
	}

	return &runtime{
		tracer:           tracer,
		defaultTags:      cloneTags(opts.DefaultTags),
		clientOpDuration: opDur,
		clientTokenUsage: tokenUsage,
		requestsTotal:    requests,
		usageDollars:     dollars,
	}, nil
}

func cloneTags(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func getRuntime() (*runtime, error) {
	rtMu.RLock()
	rt := currentRt
	rtMu.RUnlock()
	if rt == nil {
		return nil, errNotInit
	}
	return rt, nil
}

func buildResource(ctx context.Context, opts Options) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		attribute.String("service.name", opts.ServiceName),
		attribute.String("service.version", opts.ServiceVersion),
	}
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
}

func buildTracerProvider(ctx context.Context, opts Options, res *resource.Resource) (*sdktrace.TracerProvider, ShutdownFunc, error) {
	httpOpts := []otlptracehttp.Option{}
	if opts.ExporterEndpoint != "" {
		httpOpts = append(httpOpts, otlptracehttp.WithEndpoint(stripScheme(opts.ExporterEndpoint)))
	}
	if opts.ExporterInsecure || isInsecureScheme(opts.ExporterEndpoint) {
		httpOpts = append(httpOpts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, httpOpts...)
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
	)
	return tp, func(ctx context.Context) error { return tp.Shutdown(ctx) }, nil
}

func buildMeterProvider(ctx context.Context, opts Options, res *resource.Resource) (*sdkmetric.MeterProvider, ShutdownFunc, error) {
	httpOpts := []otlpmetrichttp.Option{}
	if opts.ExporterEndpoint != "" {
		httpOpts = append(httpOpts, otlpmetrichttp.WithEndpoint(stripScheme(opts.ExporterEndpoint)))
	}
	if opts.ExporterInsecure || isInsecureScheme(opts.ExporterEndpoint) {
		httpOpts = append(httpOpts, otlpmetrichttp.WithInsecure())
	}
	exp, err := otlpmetrichttp.New(ctx, httpOpts...)
	if err != nil {
		return nil, nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(15*time.Second),
		)),
	)
	return mp, func(ctx context.Context) error { return mp.Shutdown(ctx) }, nil
}

// stripScheme converts "http://host:4318" or "https://host:4318" into the
// "host:4318" form the OTLP HTTP exporter expects in WithEndpoint.
func stripScheme(endpoint string) string {
	for _, prefix := range []string{"http://", "https://"} {
		if len(endpoint) > len(prefix) && endpoint[:len(prefix)] == prefix {
			return endpoint[len(prefix):]
		}
	}
	return endpoint
}

// isInsecureScheme returns true when the endpoint explicitly uses http://.
func isInsecureScheme(endpoint string) bool {
	const prefix = "http://"
	return len(endpoint) >= len(prefix) && endpoint[:len(prefix)] == prefix
}
