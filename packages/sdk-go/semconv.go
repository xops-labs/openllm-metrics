// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package openllm is the Go runtime instrumentation SDK for OpenLLM Metrics.
//
// The SDK wraps in-process LLM calls so that latency, token counts, error
// classes, and dollarized usage are emitted as OpenTelemetry signals with the
// GenAI semantic conventions and a multi-tenant attribute bundle
// (tenant/team/app/env/project). It never collects prompt or completion text.
//
// See README.md for the 10-line quickstart.
package openllm

// String constants for the OpenTelemetry GenAI semantic-convention attribute
// keys and instrument names this SDK emits, plus the project-specific
// llm_* extension names.
//
// These mirror packages/telemetry/go/genai.go, packages/sdk-dotnet/Semconv.cs,
// and packages/sdk-python/_semconv.py so every SDK and the gateway agree on
// labels byte-for-byte.
//
// The upstream spec lives at https://opentelemetry.io/docs/specs/semconv/gen-ai/.
// We extend with llm_* names only where the GenAI spec does not yet cover the
// signal (request counter, USD cost). Per repo guidance, prefer gen_ai.* over
// inventing parallel names.
const (
	// --- OTel GenAI attribute keys ---------------------------------------

	// GenAISystem is the provider system identifier, e.g. "openai".
	GenAISystem = "gen_ai.system"
	// GenAIRequestModel is the model requested by the caller.
	GenAIRequestModel = "gen_ai.request.model"
	// GenAIResponseModel is the model actually returned by the provider.
	GenAIResponseModel = "gen_ai.response.model"
	// GenAIOperationName is the OTel-spec operation name ("chat", etc.).
	GenAIOperationName = "gen_ai.operation.name"
	// GenAITokenType labels token-usage observations as input or output.
	GenAITokenType = "gen_ai.token.type"
	// AttrErrorType is the normalized error category. Absent on success.
	AttrErrorType = "error.type"
	// AttrServerAddress is the provider endpoint host or region.
	AttrServerAddress = "server.address"

	// --- OTel GenAI metric instrument names ------------------------------

	// MetricClientOperationDuration is the histogram of end-to-end client
	// LLM operation duration, in seconds.
	MetricClientOperationDuration = "gen_ai.client.operation.duration"
	// MetricClientTokenUsage is the counter of tokens consumed per request,
	// split by GenAITokenType.
	MetricClientTokenUsage = "gen_ai.client.token.usage"

	// --- Token-type values -----------------------------------------------

	// TokenTypeInput is the GenAITokenType value for prompt tokens.
	TokenTypeInput = "input"
	// TokenTypeOutput is the GenAITokenType value for completion tokens.
	TokenTypeOutput = "output"

	// --- Project-specific llm_* extension metrics -----------------------
	//
	// Emitted in addition to the OTel GenAI signals, never instead of them.

	// MetricLlmRequestsTotal is the counter of LLM requests, labeled by
	// provider, model, route, tenant, team, app, env, project, error_kind.
	MetricLlmRequestsTotal = "llm_requests_total"
	// MetricLlmUsageDollars is the counter of dollarized usage recorded
	// when the caller supplies SetUsageDollars.
	MetricLlmUsageDollars = "llm_usage_dollars"

	// --- Multi-tenant attribute keys -------------------------------------

	// AttrProvider is the provider name on llm_* metrics.
	AttrProvider = "provider"
	// AttrModel is the model name on llm_* metrics.
	AttrModel = "model"
	// AttrRoute is the routing label (provider+region or named route).
	AttrRoute = "route"
	// AttrTenant is the tenant identifier.
	AttrTenant = "tenant"
	// AttrTeam is the owning team.
	AttrTeam = "team"
	// AttrApp is the calling application.
	AttrApp = "app"
	// AttrEnv is the deployment environment (development|staging|production).
	AttrEnv = "env"
	// AttrProject is the project identifier.
	AttrProject = "project"
	// AttrErrorKind is the normalized error kind on llm_* metrics.
	AttrErrorKind = "error_kind"

	// --- Span name and default operation ---------------------------------

	// SpanNameLlmCall is the span name used by StartLlmCall.
	SpanNameLlmCall = "llm.call"
	// DefaultOperation is the operation name used when CallOptions.Operation
	// is empty.
	DefaultOperation = "chat"

	// --- Instrumentation scope -------------------------------------------

	// InstrumentationName is the OTel instrumentation scope name reported on
	// every span and metric this SDK emits.
	InstrumentationName = "openllm-metrics-sdk-go"
	// InstrumentationVersion is the SDK semver as reported on the OTel scope.
	InstrumentationVersion = "0.1.0"
)
