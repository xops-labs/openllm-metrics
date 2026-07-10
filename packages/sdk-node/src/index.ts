// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

/**
 * @openllm/metrics — Node.js runtime instrumentation SDK for OpenLLM Metrics.
 *
 * The SDK emits OpenTelemetry GenAI semantic-convention metrics
 * (`gen_ai.client.operation.duration`, `gen_ai.client.token.usage`) plus the
 * project-specific `llm_requests_total` / `llm_usage_dollars_total` counters,
 * tagged with multi-tenant context (`tenant`, `team`, `app`, `env`,
 * `project`, `route`) on every operation.
 *
 * Privacy invariant: the SDK never collects prompt or completion text. Only
 * token counts, durations, error categories, and tenant labels flow through
 * the exporter.
 */

export { init, getSdk, type GenAiInstruments, type InitOptions, type OpenLlmSdk } from './init.js';

export { startLlmCall, withLlmCall, LlmCallScope, type LlmCallOptions } from './llmCall.js';

export { withTenantContext, getTenantContext, type TenantContext } from './baggage.js';

export * as semconv from './semconv.js';
