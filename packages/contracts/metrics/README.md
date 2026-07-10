# packages/contracts/metrics

Canonical registry of every `llm_*` Prometheus / OpenTelemetry metric the
OpenLLM Metrics platform emits, owned by **F008 - Common Operational
Telemetry Schema**.

Every service that exposes a metric or labels a log / span looks up the
metric here first. The companion `packages/telemetry/schema-lint` package
rejects unknown metrics and unauthorized labels at CI time.

## Metric set (F008 §4 / vision §9)

| Metric                        | Type    | Unit   |
| ----------------------------- | ------- | ------ |
| `llm_requests_total`          | counter | 1      |
| `llm_input_tokens_total`      | counter | tokens |
| `llm_output_tokens_total`     | counter | tokens |
| `llm_total_tokens_total`      | counter | tokens |
| `llm_cost_usd_total`          | counter | USD    |
| `llm_errors_total`            | counter | 1      |
| `llm_retries_total`           | counter | 1      |
| `llm_timeouts_total`          | counter | 1      |
| `llm_rate_limit_events_total` | counter | 1      |

Scoring, routing, fallback, and policy metrics are introduced by their
owning features (F024, F025, F029, F030, F034, F035) and added to this
registry at that time.

## Label set

Mandatory on every observation:

```text
provider, model, tenant, env
```

Allowed (per metric — see Go registry for the exact subset each metric
permits):

```text
operation, app, team, project, status_code, error_type, region
```

Optional routing-context labels (only on the metrics that need them):

```text
routing_reason, policy_name, fallback_reason, from_model, to_model
```

## Cardinality budget

Each metric in the registry carries a `CardinalityBudget` field — the
maximum expected time-series cardinality per tenant per environment per
24h window. The cardinality monitor in F006 raises an SRE alert when a
label combination crosses the budget; the F008 schema-lint regression
test pins the values so accidental loosening blocks CI.

## Unit conventions (F008 §10)

- **Currency** — metrics expose `llm_cost_usd_total` as a float (USD).
  Streaming-bus payloads carry integer minor units. Keep both consistent.
- **Latency** — Prometheus histograms in milliseconds; bus events in
  microseconds.
- **Tokens** — always integer, never null.

## OTel GenAI mapping

Project-specific `llm_*` metrics extend OpenTelemetry GenAI semantic
conventions where OTel is silent. See
[`docs/architecture/otel-genai-mapping.md`](../../../docs/architecture/otel-genai-mapping.md)
for the full table and the GenAI signals owned by the shared SDK in
`packages/telemetry/go`.

## Evolution

- Adding a metric or extending an allowed label set is non-breaking.
- Renaming or removing a metric, or removing an allowed label, is
  breaking and requires a metric-set version bump plus a dual-emission
  window.
