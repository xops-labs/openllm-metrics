// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

using System.Diagnostics;
using OpenLLMMetrics.Internal;
using OpenTelemetry;
using OpenTelemetry.Metrics;
using OpenTelemetry.Resources;
using OpenTelemetry.Trace;

namespace OpenLLMMetrics;

/// <summary>
/// Static entry point for the OpenLLM Metrics .NET SDK. Mirrors the contract
/// implemented by the parallel Go, Python, and Node.js SDKs:
/// <list type="number">
///   <item><description><see cref="Init"/> boots OTel TracerProvider / MeterProvider with GenAI semconv attribute keys.</description></item>
///   <item><description><see cref="StartLlmCall"/> opens a disposable scope per LLM call.</description></item>
///   <item><description>The scope's <c>Dispose</c> emits the duration histogram, token counter, and llm_requests_total counter.</description></item>
/// </list>
/// </summary>
/// <remarks>
/// This SDK never collects prompt or completion text. The surface deliberately
/// exposes only count-shaped setters on <see cref="LlmCallScope"/>; there is
/// no <c>SetPrompt</c> or <c>SetCompletion</c> method, and one will never be
/// added.
/// </remarks>
public static class OpenLLM
{
    private static readonly object InitLock = new();
    private static TracerProvider? _tracerProvider;
    private static MeterProvider? _meterProvider;
    private static IReadOnlyDictionary<string, string> _defaultTags = new Dictionary<string, string>();

    /// <summary>
    /// Boot the OTel SDK with the supplied OTLP exporter endpoint and
    /// default tags. Safe to call multiple times — subsequent calls are
    /// no-ops so apps can call <c>Init</c> from <c>Program.cs</c> and from
    /// integration test fixtures without coordination.
    /// </summary>
    /// <param name="serviceName">Logical service name; surfaced as the OTel resource <c>service.name</c>.</param>
    /// <param name="exporterEndpoint">
    /// OTLP/gRPC endpoint URL, e.g. <c>http://localhost:4317</c>. When
    /// <c>null</c> or empty the OTel SDK falls back to the standard
    /// <c>OTEL_EXPORTER_OTLP_ENDPOINT</c> environment variable.
    /// </param>
    /// <param name="defaultTags">
    /// Process-wide labels merged onto every metric and span (e.g.
    /// <c>{ "deployment.environment", "production" }</c>). Tag values set
    /// on the per-call scope take precedence.
    /// </param>
    public static void Init(
        string serviceName,
        string? exporterEndpoint = null,
        IReadOnlyDictionary<string, string>? defaultTags = null)
    {
        if (string.IsNullOrWhiteSpace(serviceName))
        {
            throw new ArgumentException("serviceName is required", nameof(serviceName));
        }

        lock (InitLock)
        {
            if (_tracerProvider is not null && _meterProvider is not null)
            {
                // Already initialized. We don't tear down and re-init because
                // OTel providers are process-singletons and re-initializing
                // mid-process drops in-flight spans.
                return;
            }

            _defaultTags = defaultTags ?? new Dictionary<string, string>();

            var resourceBuilder = ResourceBuilder.CreateDefault()
                .AddService(serviceName, serviceVersion: Activities.SourceVersion);

            // Default tags are attached to the OTel resource so they appear
            // on every metric and span emitted from this process.
            foreach (var kv in _defaultTags)
            {
                resourceBuilder.AddAttributes(new[] { new KeyValuePair<string, object>(kv.Key, kv.Value) });
            }

            _tracerProvider = Sdk.CreateTracerProviderBuilder()
                .SetResourceBuilder(resourceBuilder)
                .AddSource(Activities.SourceName)
                .AddOtlpExporter(opt => ConfigureEndpoint(opt, exporterEndpoint))
                .Build();

            _meterProvider = Sdk.CreateMeterProviderBuilder()
                .SetResourceBuilder(resourceBuilder)
                .AddMeter(Activities.SourceName)
                .AddOtlpExporter((opt, _) => ConfigureEndpoint(opt, exporterEndpoint))
                .Build();
        }
    }

    /// <summary>
    /// Open an LLM call scope. The returned handle is disposable — wrap it
    /// in <c>using var op = OpenLLM.StartLlmCall(...)</c> so duration,
    /// tokens, and request count are emitted regardless of the call's
    /// success path.
    /// </summary>
    /// <param name="provider">GenAI system, e.g. <c>"openai"</c>.</param>
    /// <param name="model">Requested model, e.g. <c>"gpt-4o-mini"</c>.</param>
    /// <param name="route">Routing label (provider+region or named route).</param>
    /// <param name="tenant">Tenant identifier.</param>
    /// <param name="team">Owning team.</param>
    /// <param name="app">Calling application.</param>
    /// <param name="env">Deployment environment.</param>
    /// <param name="project">Project identifier.</param>
    public static LlmCallScope StartLlmCall(
        string provider,
        string model,
        string route = "",
        string tenant = "",
        string team = "",
        string app = "",
        string env = "",
        string project = "")
    {
        // Span name follows GenAI semconv: "chat <model>".
        var spanName = string.IsNullOrEmpty(model) ? "chat" : $"chat {model}";

        var activity = Activities.Source.StartActivity(spanName, ActivityKind.Client);

        if (activity is not null)
        {
            // Propagate the identity bundle on W3C baggage so HTTP/gRPC
            // calls made inside the using-block automatically carry it.
            Baggage.Inject(activity, tenant, team, app, env, project);
        }

        return new LlmCallScope(provider, model, route, tenant, team, app, env, project, activity);
    }

    /// <summary>
    /// Tear down the OTel providers. Optional — most apps let process exit
    /// flush the providers — but useful in tests and short-lived CLI tools
    /// that need a deterministic flush.
    /// </summary>
    public static void Shutdown()
    {
        lock (InitLock)
        {
            _tracerProvider?.ForceFlush();
            _meterProvider?.ForceFlush();
            _tracerProvider?.Dispose();
            _meterProvider?.Dispose();
            _tracerProvider = null;
            _meterProvider = null;
        }
    }

    private static void ConfigureEndpoint(OpenTelemetry.Exporter.OtlpExporterOptions opt, string? endpoint)
    {
        if (!string.IsNullOrWhiteSpace(endpoint))
        {
            opt.Endpoint = new Uri(endpoint);
        }
    }
}
