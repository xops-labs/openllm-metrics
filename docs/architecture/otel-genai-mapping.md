# OpenTelemetry GenAI ↔ `llm_*` Mapping

Owned by **F008 - Common Operational Telemetry Schema**.

This document is the per-metric mapping table that complements the broader
[`platform/observability/otel_alignment.md`](../../platform/observability/otel_alignment.md)
alignment contract. The alignment doc explains the OTel-vs-project split at
the signal level. This doc maps every individual `llm_*` metric (and its
labels) to the matching OpenTelemetry GenAI attributes and instruments.

The metric registry in
[`packages/contracts/metrics`](../../packages/contracts/metrics) is the
source of truth for metric names, types, units, and allowed labels. The
mapping below shows, for each entry, the corresponding OTel signal — or
records why the project owns its own metric when OTel is silent.

## Guiding principle

> Project-specific `llm_*` metrics extend OpenTelemetry GenAI semantic
> conventions where OTel is silent. We never replace what OTel already
> covers with a parallel naming scheme.
> — `platform/observability/otel_alignment.md`

OTel GenAI specification:
<https://opentelemetry.io/docs/specs/semconv/gen-ai/>

## Metric mapping

| `llm_*` metric                | OTel equivalent                                          | Notes                                                                               |
| ----------------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `llm_requests_total`          | derived from `gen_ai.client.operation.duration` count    | Prometheus counter; OTel exposes via histogram count.                               |
| `llm_input_tokens_total`      | `gen_ai.client.token.usage` (`gen_ai.token.type=input`)  | OTel histogram; counter form retained for FinOps roll-ups.                          |
| `llm_output_tokens_total`     | `gen_ai.client.token.usage` (`gen_ai.token.type=output`) | OTel histogram; counter form retained for FinOps roll-ups.                          |
| `llm_total_tokens_total`      | derived from input + output token sums                   | Convenience metric; OTel callers may compute it client-side.                        |
| `llm_cost_usd_total`          | **no OTel equivalent**                                   | OpenLLM Metrics extension. Float USD on `/metrics`; integer minor units on the bus. |
| `llm_errors_total`            | `error.type` attribute on operation duration             | Counter form; OTel surfaces errors via attribute on the duration histogram.         |
| `llm_retries_total`           | **no OTel equivalent**                                   | OpenLLM Metrics extension.                                                          |
| `llm_timeouts_total`          | derived from `error.type=timeout`                        | OpenLLM Metrics counter complements the OTel attribute slice.                       |
| `llm_rate_limit_events_total` | **no OTel equivalent**                                   | OpenLLM Metrics extension covering provider rate-limit semantics.                   |

Scoring, routing, fallback, and policy metrics (`llm_reliability_score`,
`llm_cost_efficiency_score`, `llm_routing_decisions_total`,
`llm_fallbacks_total`, `llm_policy_denials_total`) are introduced by their
owning features (F024, F025, F029, F030, F034, F035) and recorded here
when they land. All are project-specific extensions; OTel does not cover
provider-level scoring or governance.

## Label mapping

| `llm_*` label     | OTel attribute              | Notes                                                   |
| ----------------- | --------------------------- | ------------------------------------------------------- |
| `provider`        | `gen_ai.system`             | Lowercased enum (`openai`, `anthropic`, …).             |
| `model`           | `gen_ai.request.model`      | Canonical model name.                                   |
| `operation`       | `gen_ai.operation.name`     | `chat`, `completion`, `embedding`, etc.                 |
| `status_code`     | `http.response.status_code` | HTTP status from the provider response.                 |
| `error_type`      | `error.type`                | Normalized error category; empty on success.            |
| `region`          | `server.address`            | Provider endpoint region or address.                    |
| `tenant`          | **no OTel equivalent**      | Multi-tenant invariant; mandatory on every observation. |
| `team`            | **no OTel equivalent**      | Project-specific organization label.                    |
| `app`             | **no OTel equivalent**      | Project-specific application label.                     |
| `env`             | **no OTel equivalent**      | Mandatory; deployment environment.                      |
| `project`         | **no OTel equivalent**      | Project-specific scoping label.                         |
| `routing_reason`  | **no OTel equivalent**      | Optional routing-context label (F034+).                 |
| `policy_name`     | **no OTel equivalent**      | Optional governance-context label (F029+).              |
| `fallback_reason` | **no OTel equivalent**      | Optional fallback-context label (F035+).                |
| `from_model`      | **no OTel equivalent**      | Original target model before routing/fallback.          |
| `to_model`        | **no OTel equivalent**      | Effective model after routing/fallback.                 |

## Event payload mapping

Streaming-bus events (`llm.usage.normalized`, `llm.runtime.normalized`)
carry the same vocabulary plus provenance fields:

| Event field      | OTel attribute / Note                             |
| ---------------- | ------------------------------------------------- |
| `provider`       | `gen_ai.system`                                   |
| `model`          | `gen_ai.request.model` / `gen_ai.response.model`  |
| `operation`      | `gen_ai.operation.name`                           |
| `input_tokens`   | `gen_ai.token.usage` (`gen_ai.token.type=input`)  |
| `output_tokens`  | `gen_ai.token.usage` (`gen_ai.token.type=output`) |
| `latency_us`     | derived from `gen_ai.client.operation.duration`   |
| `ttfb_us`        | derived from `gen_ai.server.time_to_first_token`  |
| `status_code`    | `http.response.status_code`                       |
| `error_type`     | `error.type`                                      |
| `region`         | `server.address`                                  |
| `trace_id`       | W3C Trace Context trace-id (propagated)           |
| `span_id`        | W3C Trace Context span-id (propagated)            |
| `tenant`         | Project-specific; required on every event.        |
| `source_mode`    | Project-specific provenance (`pull` or `proxy`).  |
| `source_service` | Project-specific provenance.                      |
| `schema_version` | Pinned by the JSON-Schema `$id`.                  |

## Unit reconciliation

| Dimension | Bus payload                                  | Prometheus metric                | OTel signal                                     |
| --------- | -------------------------------------------- | -------------------------------- | ----------------------------------------------- |
| Currency  | integer minor units (`cost_usd_minor_units`) | float USD (`llm_cost_usd_total`) | not covered by OTel GenAI                       |
| Latency   | microseconds (`latency_us`)                  | milliseconds histogram           | seconds (`gen_ai.client.operation.duration`)    |
| Tokens    | integer counts                               | integer counter / histogram      | integer histogram (`gen_ai.client.token.usage`) |

Adapters and the gateway perform the bus → metric unit conversion at the
emission edge. Conversion functions live in `packages/telemetry/go`.

## Out-of-scope mappings

The following OTel GenAI fields exist but are not used in F008 because the
captured normalized event payload already covers the operational need:

- `gen_ai.prompt.*` / `gen_ai.completion.*` — F008 forbids prompt and
  completion content from any topic, span attribute, or log field.
- `gen_ai.usage.input_tokens` / `gen_ai.usage.output_tokens` — covered
  directly by the project event payload and the matching counters above.

## See also

- [`platform/observability/otel_alignment.md`](../../platform/observability/otel_alignment.md)
- [`packages/contracts/metrics/README.md`](../../packages/contracts/metrics/README.md)
- [`packages/contracts/telemetry/README.md`](../../packages/contracts/telemetry/README.md)
- [`packages/telemetry/schema-lint/README.md`](../../packages/telemetry/schema-lint/README.md)
