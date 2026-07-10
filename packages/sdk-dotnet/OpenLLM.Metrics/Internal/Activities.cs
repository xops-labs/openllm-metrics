// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

using System.Diagnostics;
using System.Diagnostics.Metrics;

namespace OpenLLMMetrics.Internal;

/// <summary>
/// Process-wide <see cref="ActivitySource"/> and <see cref="Meter"/> singletons
/// used by the OpenLLM Metrics SDK. Held in one place so the OTel
/// <c>TracerProvider</c> / <c>MeterProvider</c> can subscribe to a stable name
/// regardless of which assembly creates the first span.
/// </summary>
/// <remarks>
/// Callers should not new up their own <see cref="ActivitySource"/> with the
/// same name — that fragments span attribution. <see cref="OpenLLM.Init"/>
/// registers both the source and the meter with the global providers and is
/// the only supported entry point.
/// </remarks>
public static class Activities
{
    /// <summary>Stable source/meter name advertised to OTel.</summary>
    public const string SourceName = "OpenLLM.Metrics";

    /// <summary>Stable source/meter version. Bump in lockstep with the NuGet package version.</summary>
    public const string SourceVersion = "0.1.0";

    /// <summary>The shared ActivitySource for LLM call spans.</summary>
    public static readonly ActivitySource Source = new(SourceName, SourceVersion);

    /// <summary>The shared Meter for GenAI + llm_* instruments.</summary>
    public static readonly Meter Meter = new(SourceName, SourceVersion);

    // --- Instrument singletons -------------------------------------------
    //
    // Histograms and counters are created lazily on first access so the
    // OTel MeterProvider has a chance to register views before any
    // measurement is recorded. .NET caches the instrument inside the Meter,
    // so repeated lookups are cheap.

    /// <summary>Histogram: client-side LLM call duration in seconds.</summary>
    public static readonly Histogram<double> ClientOperationDuration = Meter.CreateHistogram<double>(
        name: Semconv.MetricClientOperationDuration,
        unit: "s",
        description: "LLM client operation duration");

    /// <summary>Counter: token usage per request, labeled by <see cref="Semconv.GenAiTokenType"/>.</summary>
    public static readonly Counter<long> ClientTokenUsage = Meter.CreateCounter<long>(
        name: Semconv.MetricClientTokenUsage,
        unit: "{token}",
        description: "LLM token usage per request");

    /// <summary>Counter: total LLM requests, labeled by provider/model/route/tenant/team/app/env/project/error_kind.</summary>
    public static readonly Counter<long> LlmRequestsTotal = Meter.CreateCounter<long>(
        name: Semconv.MetricLlmRequestsTotal,
        unit: "{request}",
        description: "Total LLM requests handled by this process");

    /// <summary>Counter: dollarized usage when supplied by the caller.</summary>
    public static readonly Counter<double> LlmUsageDollars = Meter.CreateCounter<double>(
        name: Semconv.MetricLlmUsageDollars,
        unit: "USD",
        description: "Dollarized LLM usage; populated only when SetUsageDollars is called");
}
