// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

/**
 * String constants for OpenTelemetry GenAI semantic-convention attribute keys
 * and metric instrument names, plus project-specific `llm_*` extension keys.
 *
 * Values match the alignment table in
 * `platform/observability/otel_alignment.md` and the upstream spec at
 * https://opentelemetry.io/docs/specs/semconv/gen-ai/ — they are duplicated
 * here as plain strings so the SDK does not have to depend on a specific
 * version of `@opentelemetry/semantic-conventions` (the GenAI keys are still
 * marked experimental upstream and move between minor versions).
 *
 * Keep parity with `packages/telemetry/go/genai.go`.
 */

/** OTel GenAI semantic-convention attribute keys. */
export const GEN_AI_SYSTEM = 'gen_ai.system';
export const GEN_AI_REQUEST_MODEL = 'gen_ai.request.model';
export const GEN_AI_RESPONSE_MODEL = 'gen_ai.response.model';
export const GEN_AI_OPERATION_NAME = 'gen_ai.operation.name';
export const GEN_AI_TOKEN_TYPE = 'gen_ai.token.type';
export const GEN_AI_ERROR_TYPE = 'error.type';
export const GEN_AI_SERVER_ADDRESS = 'server.address';

/** OTel GenAI metric instrument names. */
export const METRIC_CLIENT_OPERATION_DURATION = 'gen_ai.client.operation.duration';
export const METRIC_CLIENT_TOKEN_USAGE = 'gen_ai.client.token.usage';
export const METRIC_SERVER_REQUEST_DURATION = 'gen_ai.server.request.duration';
export const METRIC_SERVER_TIME_TO_FIRST_TOKEN = 'gen_ai.server.time_to_first_token';

/** Token-type values for the `gen_ai.token.type` attribute. */
export const TOKEN_TYPE_INPUT = 'input';
export const TOKEN_TYPE_OUTPUT = 'output';

/**
 * Project-specific `llm_*` extension keys and metric names. These cover the
 * multi-tenant context fields OTel GenAI semconv does not standardize. Names
 * intentionally mirror the F008 normalized telemetry schema discriminators so
 * a single Prometheus label set covers gateway, SDK, exporter, and OTel
 * receiver sources.
 */
export const LLM_TENANT = 'llm.tenant';
export const LLM_TEAM = 'llm.team';
export const LLM_APP = 'llm.app';
export const LLM_ENV = 'llm.env';
export const LLM_PROJECT = 'llm.project';
export const LLM_ROUTE = 'llm.route';

/** Project-specific metric names (extension of OTel GenAI). */
export const METRIC_LLM_REQUESTS_TOTAL = 'llm_requests_total';
export const METRIC_LLM_USAGE_DOLLARS_TOTAL = 'llm_usage_dollars_total';

/** Baggage keys used to propagate multi-tenant context across async work. */
export const BAGGAGE_TENANT = 'llm.tenant';
export const BAGGAGE_TEAM = 'llm.team';
export const BAGGAGE_APP = 'llm.app';
export const BAGGAGE_ENV = 'llm.env';
export const BAGGAGE_PROJECT = 'llm.project';

/** `gen_ai.operation.name` value for chat-completion-style calls. */
export const OPERATION_CHAT = 'chat';
