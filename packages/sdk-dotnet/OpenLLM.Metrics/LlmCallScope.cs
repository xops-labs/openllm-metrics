// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

using System.Diagnostics;
using OpenLLMMetrics.Internal;

namespace OpenLLMMetrics;

/// <summary>
/// Disposable handle returned by <see cref="OpenLLM.StartLlmCall"/>. The caller
/// records prompt/completion token counts, optional dollar cost, and any
/// error kind on the handle, then disposes it — typically via
/// <c>using var op = ...</c>. <c>Dispose</c> emits the duration histogram,
/// token counters, the <c>llm_requests_total</c> counter, and ends the
/// underlying <see cref="Activity"/>.
/// </summary>
/// <remarks>
/// This handle never accepts prompt or completion text. Callers must not
/// stash payloads on it. Token counts and metadata only — that contract is
/// enforced by the absence of any text setter on this class.
/// </remarks>
public sealed class LlmCallScope : IDisposable
{
    // Snapshot of the call identity. Captured at construction so dispose-time
    // metric labels match span attributes byte-for-byte.
    private readonly string _provider;
    private readonly string _model;
    private readonly string _route;
    private readonly string _tenant;
    private readonly string _team;
    private readonly string _app;
    private readonly string _env;
    private readonly string _project;

    private readonly Activity? _activity;
    private readonly long _startTimestamp;

    private int _promptTokens;
    private int _completionTokens;
    private string? _errorKind;
    private decimal? _usageDollars;
    private bool _disposed;

    internal LlmCallScope(
        string provider,
        string model,
        string route,
        string tenant,
        string team,
        string app,
        string env,
        string project,
        Activity? activity)
    {
        _provider = provider;
        _model = model;
        _route = route;
        _tenant = tenant;
        _team = team;
        _app = app;
        _env = env;
        _project = project;
        _activity = activity;
        // Use Stopwatch.GetTimestamp() rather than DateTime.UtcNow so the
        // measurement is monotonic and isn't perturbed by NTP adjustments
        // during a long-running streaming call.
        _startTimestamp = Stopwatch.GetTimestamp();
    }

    /// <summary>Record the prompt/input token count returned by the provider.</summary>
    public void SetPromptTokens(int tokens)
    {
        if (tokens < 0)
        {
            return;
        }
        _promptTokens = tokens;
    }

    /// <summary>Record the completion/output token count returned by the provider.</summary>
    public void SetCompletionTokens(int tokens)
    {
        if (tokens < 0)
        {
            return;
        }
        _completionTokens = tokens;
    }

    /// <summary>
    /// Mark this call as failed with a normalized error kind
    /// (e.g. <c>"rate_limited"</c>, <c>"timeout"</c>, <c>"upstream_5xx"</c>).
    /// Empty/<c>null</c> means "success" and no error label is emitted.
    /// </summary>
    public void SetErrorKind(string? errorKind)
    {
        _errorKind = string.IsNullOrEmpty(errorKind) ? null : errorKind;
    }

    /// <summary>
    /// Record dollarized usage when the caller has a price model handy.
    /// Optional; if unset, the <c>llm_usage_dollars</c> counter is not
    /// incremented for this call.
    /// </summary>
    public void SetUsageDollars(decimal? dollars)
    {
        if (dollars is null || dollars < 0m)
        {
            _usageDollars = null;
            return;
        }
        _usageDollars = dollars;
    }

    /// <summary>
    /// Emit all metrics and end the underlying activity. Safe to call
    /// multiple times — subsequent calls are no-ops.
    /// </summary>
    public void Dispose()
    {
        if (_disposed)
        {
            return;
        }
        _disposed = true;

        var elapsed = Stopwatch.GetElapsedTime(_startTimestamp);
        var durationSeconds = elapsed.TotalSeconds;

        // Build the common tag set once. Histograms/counters in
        // System.Diagnostics.Metrics accept a flat KeyValuePair array, so we
        // stage one array and append the token-type label per token record.
        var baseTags = BuildBaseTags();

        Activities.ClientOperationDuration.Record(durationSeconds, baseTags);

        if (_promptTokens > 0)
        {
            RecordTokenUsage(_promptTokens, Semconv.TokenTypeInput, baseTags);
        }
        if (_completionTokens > 0)
        {
            RecordTokenUsage(_completionTokens, Semconv.TokenTypeOutput, baseTags);
        }

        Activities.LlmRequestsTotal.Add(1, baseTags);

        if (_usageDollars is { } dollars)
        {
            Activities.LlmUsageDollars.Add((double)dollars, baseTags);
        }

        if (_activity is not null)
        {
            // Mirror the metric labels onto the span for trace-to-metric
            // correlation in tools like Grafana exemplars.
            _activity.SetTag(Semconv.GenAiSystem, _provider);
            _activity.SetTag(Semconv.GenAiRequestModel, _model);
            _activity.SetTag(Semconv.GenAiOperationName, "chat");
            _activity.SetTag(Semconv.LlmRoute, _route);
            _activity.SetTag(Semconv.LlmTenant, _tenant);
            _activity.SetTag(Semconv.LlmTeam, _team);
            _activity.SetTag(Semconv.LlmApp, _app);
            _activity.SetTag(Semconv.LlmEnv, _env);
            _activity.SetTag(Semconv.LlmProject, _project);

            if (_promptTokens > 0)
            {
                _activity.SetTag("gen_ai.usage.input_tokens", _promptTokens);
            }
            if (_completionTokens > 0)
            {
                _activity.SetTag("gen_ai.usage.output_tokens", _completionTokens);
            }

            if (_errorKind is not null)
            {
                _activity.SetTag(Semconv.ErrorType, _errorKind);
                _activity.SetStatus(ActivityStatusCode.Error, _errorKind);
            }
            else
            {
                _activity.SetStatus(ActivityStatusCode.Ok);
            }

            _activity.Dispose();
        }
    }

    private KeyValuePair<string, object?>[] BuildBaseTags()
    {
        // Allocate once. The size matches the eight identity labels plus
        // error_kind (always emitted; empty string when the call succeeded
        // so dashboards can use a single label selector).
        return new KeyValuePair<string, object?>[]
        {
            new(Semconv.GenAiSystem, _provider),
            new(Semconv.GenAiRequestModel, _model),
            new(Semconv.LlmRoute, _route),
            new(Semconv.LlmTenant, _tenant),
            new(Semconv.LlmTeam, _team),
            new(Semconv.LlmApp, _app),
            new(Semconv.LlmEnv, _env),
            new(Semconv.LlmProject, _project),
            new(Semconv.LlmErrorKind, _errorKind ?? string.Empty),
        };
    }

    private static void RecordTokenUsage(int count, string tokenType, KeyValuePair<string, object?>[] baseTags)
    {
        // Tag arrays passed to counters must be immutable per-call. We copy
        // and append the token-type discriminator rather than mutating the
        // shared baseTags slice.
        var tags = new KeyValuePair<string, object?>[baseTags.Length + 1];
        Array.Copy(baseTags, tags, baseTags.Length);
        tags[^1] = new KeyValuePair<string, object?>(Semconv.GenAiTokenType, tokenType);
        Activities.ClientTokenUsage.Add(count, tags);
    }
}
