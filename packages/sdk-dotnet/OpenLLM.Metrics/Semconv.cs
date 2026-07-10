// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

namespace OpenLLMMetrics;

/// <summary>
/// String constants for the OpenTelemetry GenAI semantic-convention attribute
/// keys and instrument names this SDK emits, plus the project-specific
/// <c>llm_*</c> extension names. These mirror
/// <c>packages/telemetry/go/genai.go</c> so the Go gateway and the .NET SDK
/// agree on labels byte-for-byte.
/// </summary>
/// <remarks>
/// The upstream spec lives at https://opentelemetry.io/docs/specs/semconv/gen-ai/.
/// We extend with <c>llm_*</c> names only where the GenAI spec does not yet
/// cover the signal (request counters, USD cost). Per repo guidance, prefer
/// <c>gen_ai.*</c> over inventing parallel names.
/// </remarks>
public static class Semconv
{
    // --- OTel GenAI attribute keys ---------------------------------------

    /// <summary>Provider system identifier, e.g. "openai", "anthropic".</summary>
    public const string GenAiSystem = "gen_ai.system";

    /// <summary>Model requested by the caller, e.g. "gpt-4o-mini".</summary>
    public const string GenAiRequestModel = "gen_ai.request.model";

    /// <summary>Operation name per GenAI spec, e.g. "chat", "embedding".</summary>
    public const string GenAiOperationName = "gen_ai.operation.name";

    /// <summary>Model actually returned by the provider.</summary>
    public const string GenAiResponseModel = "gen_ai.response.model";

    /// <summary>Token-type label distinguishing input from output counts.</summary>
    public const string GenAiTokenType = "gen_ai.token.type";

    /// <summary>Normalized error category. Absent on success.</summary>
    public const string ErrorType = "error.type";

    /// <summary>Provider endpoint region or address.</summary>
    public const string ServerAddress = "server.address";

    // --- OTel GenAI metric instrument names ------------------------------

    /// <summary>Client-side end-to-end LLM call duration, seconds.</summary>
    public const string MetricClientOperationDuration = "gen_ai.client.operation.duration";

    /// <summary>Token usage per request, split by <see cref="GenAiTokenType"/>.</summary>
    public const string MetricClientTokenUsage = "gen_ai.client.token.usage";

    // --- Token-type values -----------------------------------------------

    /// <summary>Value for <see cref="GenAiTokenType"/> on prompt tokens.</summary>
    public const string TokenTypeInput = "input";

    /// <summary>Value for <see cref="GenAiTokenType"/> on completion tokens.</summary>
    public const string TokenTypeOutput = "output";

    // --- Project-specific llm_* extensions -------------------------------
    //
    // These are NOT part of OTel GenAI semconv. They exist only where the
    // upstream spec does not cover the signal we need for FinOps and
    // routing dashboards. Keep this list minimal.

    /// <summary>
    /// Counter of LLM requests. Labeled by provider, model, route, tenant,
    /// team, app, env, project, error_kind.
    /// </summary>
    public const string MetricLlmRequestsTotal = "llm_requests_total";

    /// <summary>
    /// Optional gauge/counter of dollarized usage. Recorded when the caller
    /// supplies <c>SetUsageDollars</c>.
    /// </summary>
    public const string MetricLlmUsageDollars = "llm_usage_dollars";

    // --- Multi-tenant attribute keys -------------------------------------
    //
    // OpenLLM Metrics is multi-tenant from day one. Every metric and span
    // carries this bundle so downstream dashboards can slice without joins.

    /// <summary>Routing label (provider+region or named route).</summary>
    public const string LlmRoute = "route";

    /// <summary>Tenant identifier.</summary>
    public const string LlmTenant = "tenant";

    /// <summary>Owning team.</summary>
    public const string LlmTeam = "team";

    /// <summary>Calling application.</summary>
    public const string LlmApp = "app";

    /// <summary>Deployment environment (development|staging|production).</summary>
    public const string LlmEnv = "env";

    /// <summary>Project identifier.</summary>
    public const string LlmProject = "project";

    /// <summary>Normalized error kind. Empty/absent on success.</summary>
    public const string LlmErrorKind = "error_kind";
}
