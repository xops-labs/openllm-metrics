# OpenTelemetry and GenAI Semantic Convention Alignment

This document is the shared alignment contract for every OpenLLM Metrics service.
It is a living reference — update it when a convention is adopted, deferred, or
extended.

**F006 - Observability Baseline** owns the OTel Collector reference configs and
the shared OTel SDK initialization library. This document records the alignment
decisions so that each service knows which OTel signals to emit and how to name them.

---

## Guiding Principle

OpenTelemetry GenAI semantic conventions are the alignment target. Project-specific
`llm_*` metrics extend OTel where OTel is silent. We never replace what OTel
already covers with a parallel naming scheme.

Current OTel GenAI specification: <https://opentelemetry.io/docs/specs/semconv/gen-ai/>

---

## First-Class OTel GenAI Signals

| OTel Signal                | Attribute / Metric Name                         | Notes                                     |
| -------------------------- | ----------------------------------------------- | ----------------------------------------- |
| Client operation duration  | `gen_ai.client.operation.duration` (histogram)  | p50/p95/p99 by provider + model           |
| Token usage                | `gen_ai.client.token.usage` (histogram)         | `gen_ai.token.type` = `input` or `output` |
| Server request duration    | `gen_ai.server.request.duration` (histogram)    | Gateway-mode only (Phase 3+)              |
| Server time to first token | `gen_ai.server.time_to_first_token` (histogram) | Gateway-mode only (Phase 3+)              |

These four signals are emitted by the shared OTel SDK (F006) and must not be
re-implemented as custom `llm_*` metrics.

---

## Project-Specific `llm_*` Extensions

The following signals are not covered by the current OTel GenAI spec and are
added as project-specific Prometheus metrics with the `llm_` prefix:

| Metric Name                           | Type    | Description                                       | Introduced          |
| ------------------------------------- | ------- | ------------------------------------------------- | ------------------- |
| `llm_requests_total`                  | Counter | Total LLM API requests by provider/model/status   | F010                |
| `llm_input_tokens_total`              | Counter | Cumulative input tokens consumed                  | F010                |
| `llm_output_tokens_total`             | Counter | Cumulative output tokens generated                | F010                |
| `llm_cost_usd_total`                  | Counter | Cumulative cost in USD                            | F010                |
| `llm_provider_errors_total`           | Counter | Provider API errors by error_type                 | F010                |
| `llm_exporter_scrape_success`         | Gauge   | 1 if last usage-API scrape succeeded, 0 otherwise | F009                |
| `llm_exporter_last_success_timestamp` | Gauge   | Unix timestamp of last successful scrape          | F009                |
| `llm_reliability_score`               | Gauge   | Per-provider/model reliability score (0–1)        | F024 (OSS-deferred) |
| `llm_cost_efficiency_score`           | Gauge   | Per-provider/model cost-efficiency score (0–1)    | F025 (OSS-deferred) |
| `llm_routing_decisions_total`         | Counter | Routing decisions by routing_reason               | F034 (OSS-deferred) |
| `llm_fallbacks_total`                 | Counter | Fallback activations by fallback_reason           | F035 (OSS-deferred) |

---

## Label Alignment

OTel GenAI attribute names map to Prometheus label names as follows:

| OTel Attribute          | Prometheus Label | Notes                        |
| ----------------------- | ---------------- | ---------------------------- |
| `gen_ai.system`         | `provider`       | Normalized to lowercase enum |
| `gen_ai.request.model`  | `model`          | Canonical model name         |
| `gen_ai.operation.name` | `operation`      | `chat`, `embedding`, etc.    |
| `server.address`        | `region`         | Provider endpoint region     |
| `error.type`            | `error_type`     | Normalized error category    |

Project-specific labels with no OTel equivalent: `tenant`, `team`, `app`,
`env`, `project`, `routing_reason`, `policy_name`, `fallback_reason`.

---

## Redaction Requirements

These keys must be stripped by the redaction interceptor (F006) before any
span attribute, log field, or metric label is emitted:

- `authorization`, `api_key`, `x-api-key`, `secret`, `password`, `token`
- `prompt`, `completion`, `messages`, `input`, `output`, `content`
- `embedding`, `request_body`, `response_body`
- Any attribute whose value matches a key-like pattern (base64, 40+ hex chars)

---

## Trace Context Propagation

| Transport                      | Propagation Format                               |
| ------------------------------ | ------------------------------------------------ |
| HTTP                           | W3C Trace Context (`traceparent` / `tracestate`) |
| gRPC                           | OpenTelemetry gRPC metadata                      |
| Streaming bus (Redpanda/Kafka) | Header injection into Kafka record headers       |

---

## Collector Reference Configs

OTel Collector reference configs live at
[`platform/observability/otel-collector/`](./otel-collector/) and are owned by
**F006 - Observability Baseline**. See that directory's [README](./otel-collector/README.md)
for deployment topology, image pinning, and the defense-in-depth redaction layer.

| Config            | Use        | Highlights                                                        |
| ----------------- | ---------- | ----------------------------------------------------------------- |
| `dev.yaml`        | Local dev  | Stdout traces; Prometheus scrape exporter on `:8889`.             |
| `staging.yaml`    | Staging    | OTLP → Tempo, remote-write metrics, OTLP logs.                    |
| `production.yaml` | Production | Tail sampling (errors always kept), memory limiter, retry queues. |

## Shared SDK

The shared OTel initialization library is [`packages/telemetry/go`](../../packages/telemetry/go).
Services call `telemetry.Init(ctx, ServiceConfig)` at boot and receive a
unified TracerProvider, MeterProvider, propagator, redactor, and structured
logger. The redaction key list above is the single source of truth — the
SDK and every Collector config mirror it.
