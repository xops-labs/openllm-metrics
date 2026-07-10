"""String constants for OpenTelemetry GenAI semantic-convention keys and
OpenLLM Metrics ``llm_*`` extension metrics.

These mirror the constants in ``packages/telemetry/go/genai.go`` so every SDK
(.NET, Node.js, Go, Python) records the same attribute names. The OTel spec is
the source of truth: https://opentelemetry.io/docs/specs/semconv/gen-ai/

Only attributes that travel on telemetry signals live here. Do not put
prompt/completion text fields in this module — the SDK never collects them.
"""

from __future__ import annotations

# ---------------------------------------------------------------------------
# OpenTelemetry GenAI attribute keys.
# ---------------------------------------------------------------------------

GEN_AI_SYSTEM = "gen_ai.system"
"""Provider system name, lowercased per OTel spec (``openai``, ``anthropic``)."""

GEN_AI_REQUEST_MODEL = "gen_ai.request.model"
"""Canonical model name requested by the caller (``gpt-4o-mini``)."""

GEN_AI_RESPONSE_MODEL = "gen_ai.response.model"
"""Model name returned by the provider (may differ from request model)."""

GEN_AI_OPERATION_NAME = "gen_ai.operation.name"
"""Operation kind: ``chat``, ``embedding``, ``completion``, etc."""

GEN_AI_TOKEN_TYPE = "gen_ai.token.type"
"""Token-type discriminator for ``gen_ai.client.token.usage``."""

GEN_AI_ERROR_TYPE = "error.type"
"""Normalized error category; empty on success."""

GEN_AI_SERVER_ADDRESS = "server.address"
"""Provider endpoint host or region."""

# ---------------------------------------------------------------------------
# OTel GenAI metric instrument names.
# ---------------------------------------------------------------------------

METRIC_CLIENT_OPERATION_DURATION = "gen_ai.client.operation.duration"
METRIC_CLIENT_TOKEN_USAGE = "gen_ai.client.token.usage"

# ---------------------------------------------------------------------------
# Token type values.
# ---------------------------------------------------------------------------

TOKEN_TYPE_INPUT = "input"
TOKEN_TYPE_OUTPUT = "output"

# ---------------------------------------------------------------------------
# OpenLLM Metrics ``llm_*`` extension attributes and metrics.
#
# These extend OTel rather than replacing it: every llm_* signal is emitted in
# addition to the OTel GenAI signals, never instead of them.
# ---------------------------------------------------------------------------

LLM_PROVIDER = "provider"
LLM_MODEL = "model"
LLM_ROUTE = "route"
LLM_TENANT = "tenant"
LLM_TEAM = "team"
LLM_APP = "app"
LLM_ENV = "env"
LLM_PROJECT = "project"
LLM_ERROR_KIND = "error_kind"

METRIC_LLM_REQUESTS_TOTAL = "llm_requests_total"
METRIC_LLM_USAGE_DOLLARS = "llm_usage_dollars"

# ---------------------------------------------------------------------------
# Baggage keys for multi-tenant context propagation.
# ---------------------------------------------------------------------------

BAGGAGE_TENANT = "openllm.tenant"
BAGGAGE_TEAM = "openllm.team"
BAGGAGE_APP = "openllm.app"
BAGGAGE_ENV = "openllm.env"
BAGGAGE_PROJECT = "openllm.project"

# Default operation name used when callers do not specify one.
DEFAULT_OPERATION = "chat"

# Default instrumentation scope name used by the SDK's tracer and meter.
INSTRUMENTATION_NAME = "openllm-metrics-sdk-python"
INSTRUMENTATION_VERSION = "0.1.0"
