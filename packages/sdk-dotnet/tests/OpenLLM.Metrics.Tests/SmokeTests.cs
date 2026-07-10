// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Smoke tests for the OpenLLM.Metrics .NET SDK.
//
// These tests use the OTel InMemory exporter so no real OTLP collector is
// required. Each test method calls OpenLLM.Shutdown() on entry to reset the
// SDK singleton and reconfigure it against the in-memory exporters.

using System.Diagnostics;
using OpenLLMMetrics;
using OpenLLMMetrics.Internal;
using OpenTelemetry;
using OpenTelemetry.Metrics;
using OpenTelemetry.Trace;
using Xunit;

namespace OpenLLMMetrics.Tests;

public sealed class SmokeTests : IDisposable
{
    private readonly List<Activity> _exportedActivities = [];
    private readonly List<Metric> _exportedMetrics = [];
    private readonly TracerProvider _tracerProvider;
    private readonly MeterProvider _meterProvider;

    public SmokeTests()
    {
        // Reset the SDK singleton so each test class gets a fresh slate.
        OpenLLM.Shutdown();

        _tracerProvider = Sdk.CreateTracerProviderBuilder()
            .AddSource(Activities.SourceName)
            .AddInMemoryExporter(_exportedActivities)
            .Build();

        _meterProvider = Sdk.CreateMeterProviderBuilder()
            .AddMeter(Activities.SourceName)
            .AddInMemoryExporter(_exportedMetrics)
            .Build();
    }

    public void Dispose()
    {
        _tracerProvider.ForceFlush();
        _meterProvider.ForceFlush();
        _tracerProvider.Dispose();
        _meterProvider.Dispose();
        OpenLLM.Shutdown();
    }

    // -----------------------------------------------------------------------
    // Helpers
    // -----------------------------------------------------------------------

    private void CollectMetrics()
    {
        _meterProvider.ForceFlush();
    }

    // -----------------------------------------------------------------------
    // Tests
    // -----------------------------------------------------------------------

    [Fact]
    public void StartLlmCall_EmitsSpanWithCorrectName()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini",
            route: "primary",
            tenant: "acme",
            team: "platform",
            app: "smoke-test",
            env: "test",
            project: "openllm-test"))
        {
            op.SetPromptTokens(10);
            op.SetCompletionTokens(25);
        }

        _tracerProvider.ForceFlush();

        Assert.Single(_exportedActivities);
        var span = _exportedActivities[0];
        Assert.Equal("chat gpt-4o-mini", span.DisplayName);
    }

    [Fact]
    public void StartLlmCall_SpanCarriesGenAiAttributes()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "anthropic",
            model: "claude-3-haiku",
            tenant: "acme",
            team: "research",
            app: "lab",
            env: "staging",
            project: "alpha"))
        {
        }

        _tracerProvider.ForceFlush();

        var span = Assert.Single(_exportedActivities);
        var tags = span.TagObjects.ToDictionary(kv => kv.Key, kv => kv.Value?.ToString() ?? "");

        Assert.Equal("anthropic", tags[Semconv.GenAiSystem]);
        Assert.Equal("claude-3-haiku", tags[Semconv.GenAiRequestModel]);
    }

    [Fact]
    public void StartLlmCall_BaggageCarriesTenantBundle()
    {
        Activity? capturedActivity = null;

        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o",
            tenant: "tenantA",
            team: "teamB",
            app: "appC",
            env: "prod",
            project: "projD"))
        {
            capturedActivity = Activity.Current;
        }

        Assert.NotNull(capturedActivity);
        var bundle = Baggage.Read(capturedActivity);

        Assert.Equal("tenantA", bundle.Tenant);
        Assert.Equal("teamB", bundle.Team);
        Assert.Equal("appC", bundle.App);
        Assert.Equal("prod", bundle.Env);
        Assert.Equal("projD", bundle.Project);
    }

    [Fact]
    public void StartLlmCall_EmitsLlmRequestsTotalMetric()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini",
            tenant: "acme",
            team: "platform",
            app: "metrics-test",
            env: "test",
            project: "openllm-test"))
        {
            op.SetPromptTokens(42);
            op.SetCompletionTokens(128);
        }

        CollectMetrics();

        var metric = _exportedMetrics.FirstOrDefault(m => m.Name == Semconv.MetricLlmRequestsTotal);
        Assert.NotNull(metric);
    }

    [Fact]
    public void StartLlmCall_EmitsClientOperationDurationMetric()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini",
            tenant: "acme",
            team: "platform",
            app: "duration-test",
            env: "test",
            project: "openllm-test"))
        {
        }

        CollectMetrics();

        var metric = _exportedMetrics.FirstOrDefault(m => m.Name == Semconv.MetricClientOperationDuration);
        Assert.NotNull(metric);
    }

    [Fact]
    public void StartLlmCall_EmitsTokenUsageMetrics()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini",
            tenant: "acme",
            team: "platform",
            app: "token-test",
            env: "test",
            project: "openllm-test"))
        {
            op.SetPromptTokens(50);
            op.SetCompletionTokens(200);
        }

        CollectMetrics();

        var metric = _exportedMetrics.FirstOrDefault(m => m.Name == Semconv.MetricClientTokenUsage);
        Assert.NotNull(metric);
    }

    [Fact]
    public void StartLlmCall_EmitsUsageDollarsWhenSet()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini",
            tenant: "acme",
            team: "platform",
            app: "dollar-test",
            env: "test",
            project: "openllm-test"))
        {
            op.SetUsageDollars(0.0012m);
        }

        CollectMetrics();

        var metric = _exportedMetrics.FirstOrDefault(m => m.Name == Semconv.MetricLlmUsageDollars);
        Assert.NotNull(metric);
    }

    [Fact]
    public void StartLlmCall_SetErrorKind_MarksSpanAsError()
    {
        using (var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini",
            tenant: "acme",
            team: "platform",
            app: "error-test",
            env: "test",
            project: "openllm-test"))
        {
            op.SetErrorKind("rate_limited");
        }

        _tracerProvider.ForceFlush();

        var span = Assert.Single(_exportedActivities);
        Assert.Equal(ActivityStatusCode.Error, span.Status);

        var tags = span.TagObjects.ToDictionary(kv => kv.Key, kv => kv.Value?.ToString() ?? "");
        Assert.Equal("rate_limited", tags[Semconv.ErrorType]);
    }

    [Fact]
    public void Dispose_IsIdempotent()
    {
        var op = OpenLLM.StartLlmCall(
            provider: "openai",
            model: "gpt-4o-mini");

        op.Dispose();
        op.Dispose(); // Must not throw.

        _tracerProvider.ForceFlush();
        // Only one span should be recorded.
        Assert.Single(_exportedActivities);
    }
}
