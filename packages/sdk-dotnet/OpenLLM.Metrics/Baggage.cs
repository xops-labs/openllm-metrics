// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

using System.Diagnostics;

namespace OpenLLMMetrics;

/// <summary>
/// Helpers to inject and read the OpenLLM Metrics multi-tenant identity bundle
/// (<c>tenant</c>, <c>team</c>, <c>app</c>, <c>env</c>, <c>project</c>) on the
/// W3C Baggage header so downstream services pick it up via the standard
/// <c>baggage</c> / <c>traceparent</c> propagators.
/// </summary>
/// <remarks>
/// .NET's <see cref="Activity"/> exposes baggage via
/// <see cref="Activity.AddBaggage"/> / <see cref="Activity.GetBaggageItem"/>
/// and these are surfaced over the wire by the OTel
/// <c>Baggage</c> propagator that <see cref="OpenLLM.Init"/> wires up. We
/// deliberately do not invent a custom header — the W3C Baggage spec already
/// covers this.
/// </remarks>
public static class Baggage
{
    /// <summary>Inject the identity bundle as baggage on the supplied activity.</summary>
    /// <param name="activity">Target activity. No-op when <c>null</c>.</param>
    /// <param name="tenant">Tenant identifier.</param>
    /// <param name="team">Owning team.</param>
    /// <param name="app">Calling application.</param>
    /// <param name="env">Deployment environment.</param>
    /// <param name="project">Project identifier.</param>
    public static void Inject(Activity? activity, string tenant, string team, string app, string env, string project)
    {
        if (activity is null)
        {
            return;
        }

        // Only set non-empty values so partial bundles don't pollute the
        // baggage header with empty strings. Receivers can then treat
        // "missing" and "empty" the same way.
        AddIfPresent(activity, Semconv.LlmTenant, tenant);
        AddIfPresent(activity, Semconv.LlmTeam, team);
        AddIfPresent(activity, Semconv.LlmApp, app);
        AddIfPresent(activity, Semconv.LlmEnv, env);
        AddIfPresent(activity, Semconv.LlmProject, project);
    }

    /// <summary>
    /// Read the identity bundle from the current <see cref="Activity"/>.
    /// Returns empty strings for missing fields so callers can rely on
    /// non-null results.
    /// </summary>
    public static IdentityBundle Read(Activity? activity)
    {
        if (activity is null)
        {
            return IdentityBundle.Empty;
        }

        return new IdentityBundle(
            Tenant: activity.GetBaggageItem(Semconv.LlmTenant) ?? string.Empty,
            Team: activity.GetBaggageItem(Semconv.LlmTeam) ?? string.Empty,
            App: activity.GetBaggageItem(Semconv.LlmApp) ?? string.Empty,
            Env: activity.GetBaggageItem(Semconv.LlmEnv) ?? string.Empty,
            Project: activity.GetBaggageItem(Semconv.LlmProject) ?? string.Empty);
    }

    private static void AddIfPresent(Activity activity, string key, string value)
    {
        if (!string.IsNullOrEmpty(value))
        {
            activity.AddBaggage(key, value);
        }
    }
}

/// <summary>The multi-tenant identity bundle propagated as W3C Baggage.</summary>
public readonly record struct IdentityBundle(string Tenant, string Team, string App, string Env, string Project)
{
    /// <summary>All-empty bundle returned when no activity is in scope.</summary>
    public static readonly IdentityBundle Empty = new(string.Empty, string.Empty, string.Empty, string.Empty, string.Empty);
}
