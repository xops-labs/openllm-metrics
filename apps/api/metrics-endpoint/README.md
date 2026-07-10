# Metrics-Endpoint Aggregator Service (F010)

A standalone Go service that consumes normalized telemetry from the streaming
bus and exposes it in the Prometheus text exposition format on `/metrics`.

This is the public scrape surface for OpenLLM Metrics. SREs point Prometheus
(or any OpenMetrics-compatible scraper / OTel Collector) at this service and
get a vendor-neutral view of LLM API usage, cost, errors, retries, and
rate-limit events across every configured provider.

## Table of Contents

- [What it does](#what-it-does)
- [What it does NOT do](#what-it-does-not-do)
- [How it relates to F009 (the per-provider poller)](#how-it-relates-to-f009-the-per-provider-poller)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Environment variables](#environment-variables)
- [Metrics exposed](#metrics-exposed)
- [Self-metrics](#self-metrics)
- [Memory budget](#memory-budget)
- [Prometheus scrape config example](#prometheus-scrape-config-example)
- [Security posture](#security-posture)
- [Development](#development)

## What it does

- Subscribes to the `llm.usage.normalized` and `llm.runtime.normalized`
  topics on the streaming bus.
- For each event, validates against F008 schema + cardinality budget.
- Projects valid events onto the canonical counter families from the F008
  metric registry (`packages/contracts/metrics/go`).
- Holds state purely in memory; on restart, rebuilds by replaying the bus.
- Serves `/metrics` in Prometheus text exposition format
  (`Content-Type: text/plain; version=0.0.4; charset=utf-8`).
- Serves `/healthz` (liveness) and `/readyz` (consumer-warmed) for
  orchestrators.

## What it does NOT do

- Does NOT poll any provider — that is F009 (OpenAI) and F013-F016 (the
  other providers).
- Does NOT proxy LLM API traffic — that is the gateway (F018).
- Does NOT compute reliability or cost-efficiency scores (F024 / F025).
- Does NOT enforce policy or routing decisions (F030 / F034).
- Does NOT support OTLP push — ingestion is bus-consume only (F010 §14).
- Does NOT enforce authentication on `/metrics` — that is a deployment
  concern (sidecar, ingress, mTLS). Deferred per F010 §5 / §14.

## How it relates to F009 (the per-provider poller)

These are **two different surfaces**:

| Surface                                       | Owner               | Source of data                                                   | Scope                                       |
| --------------------------------------------- | ------------------- | ---------------------------------------------------------------- | ------------------------------------------- |
| `apps/worker/usage-poller/openai/.../metrics` | F009 poller         | Internal to the poller (scrape success / failure, breaker state) | Operability of the poller itself            |
| `apps/api/metrics-endpoint/.../metrics`       | F010 (this service) | Streaming bus consumer                                           | Cross-provider LLM telemetry for dashboards |

The F009 poller's `/metrics` answers "is the poller healthy?". This
service's `/metrics` answers "what is happening across all providers in this
tenant?". Operators should scrape **both**: F009's port for exporter
health-checking, this port for product telemetry.

## Quick start

```bash
# 1. Bring up the local bus + topics.
docker compose -f platform/bus/docker-compose.yml up -d

# 2. (Optional) run the OpenAI poller so the bus has events to consume.
#    See apps/worker/usage-poller/openai/README.md.

# 3. Copy and edit the example config.
cp apps/api/metrics-endpoint/config.example.yaml /etc/openllm-metrics/metrics-endpoint.yaml

# 4. Build and run.
cd apps/api/metrics-endpoint
go build -o metrics-endpoint ./cmd/metrics-endpoint
./metrics-endpoint --config /etc/openllm-metrics/metrics-endpoint.yaml

# 5. Scrape it.
curl http://localhost:9090/metrics
```

Container build (run from repo root so the workspace deps are in context):

```bash
docker build -f apps/api/metrics-endpoint/Dockerfile -t openllm/metrics-endpoint:dev .
docker run --rm -p 9090:9090 \
  -v "$PWD/apps/api/metrics-endpoint/config.example.yaml:/etc/openllm-metrics/metrics-endpoint.yaml:ro" \
  openllm/metrics-endpoint:dev
```

## Configuration

See [`config.example.yaml`](./config.example.yaml) for an annotated example.
Validated at startup. The binary fails fast on:

- `server.port` outside `1..65535`.
- `bus.brokers` empty.
- `bus.topics` empty.
- `replay.window_hours` negative.

## Environment variables

This service reads no secrets at runtime. Bus credentials, when added, will
follow the same env-var-name-in-YAML / value-in-env pattern used by F009.

## Metrics exposed

Counter families derived from the F008 metric registry:

| Metric                        | Source events                                  | Notes                                                           |
| ----------------------------- | ---------------------------------------------- | --------------------------------------------------------------- |
| `llm_requests_total`          | usage (`request_count`), runtime (every event) | Runtime events also carry `status_code` and `error_type` labels |
| `llm_input_tokens_total`      | usage, runtime                                 |                                                                 |
| `llm_output_tokens_total`     | usage, runtime                                 |                                                                 |
| `llm_total_tokens_total`      | usage, runtime                                 |                                                                 |
| `llm_cost_usd_total`          | usage (`cost_usd_minor_units / 100.0`)         | USD as float per F008 §10                                       |
| `llm_errors_total`            | runtime with `status="error"`                  |                                                                 |
| `llm_timeouts_total`          | runtime with `status="timeout"`                |                                                                 |
| `llm_rate_limit_events_total` | runtime with `status="rate_limited"`           |                                                                 |
| `llm_retries_total`           | runtime with `retry_count > 0`                 | Sum of `retry_count`                                            |

Every series carries the F008 mandatory label set (`provider`, `model`,
`tenant`, `env`) plus the optional contextual labels (`team`, `app`,
`project`, `region`, `operation`, `status_code`, `error_type`).

The registry is the single source of truth; if a new metric is added to
`packages/contracts/metrics/go/registry.go`, project a contribution for it
inside `internal/aggregator/projection.go`.

## Self-metrics

Always emitted, even when the aggregator is empty:

| Metric                                         | Type    | Purpose                                                                                                                                    |
| ---------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| `llm_aggregator_rejected_events_total{reason}` | counter | Events the aggregator dropped before counting. `reason` is a closed enum: `decode`, `schema`, `forbidden`, `cardinality`, `unknown_topic`. |
| `llm_aggregator_processed_events_total`        | counter | Events successfully applied to in-memory state.                                                                                            |
| `llm_aggregator_series_total`                  | gauge   | Distinct (metric, labelset) pairs currently held in memory. Use this for the memory-budget alert.                                          |

## Memory budget

State is purely in-memory. A counter series is ~256 bytes (label map,
sum, map overhead). The aggregator holds at most one series per
(metric × cardinality-bucket-product) combination. With the default F008
cardinality budgets:

```
Σ budget(m) for m in Registry()
  = 50000 + 30000×4 + 60000 + 40000 + 30000×2 + 200
  ≈ 330,200 series cap (per the F008 registry budgets)
```

At ~256 bytes/series that is ~85 MB if every budget is saturated. In
practice, real tenants drive a tiny fraction of the budget; ~10-50 MB
resident is typical. Operators should alert when
`llm_aggregator_series_total > 0.8 * registry_budget_sum` so cardinality
spikes are noticed before they hit the cap.

## Prometheus scrape config example

```yaml
scrape_configs:
  - job_name: openllm-metrics-endpoint
    metrics_path: /metrics
    static_configs:
      - targets: ['metrics-endpoint:9090']
    relabel_configs:
      # Drop synthetic scraper-added labels so the F008 label set stays clean.
      - action: labeldrop
        regex: __tmp_.*
```

For the per-provider poller scrape (different surface), see
`apps/worker/usage-poller/openai/README.md`.

## Security posture

- The service never sees raw LLM payloads. Events are pre-redacted by the
  F008 schema lint at the producer side, and this service defensively
  re-checks every payload for forbidden fields (`prompt`, `completion`,
  etc.). A payload that smuggles a forbidden field is dropped and counted
  under `llm_aggregator_rejected_events_total{reason="forbidden"}`.
- The aggregator is multi-tenant: every series carries the `tenant` label
  from the event. Cross-tenant queries are a downstream PromQL concern; the
  service itself stores tenant data side-by-side.
- `/metrics` is open scrape at this phase. Wrap with a sidecar / ingress
  for production multi-tenant deployments (F010 §5 / §14).
- The bus consumer is read-only. It does NOT publish to any topic — that
  isolates this service from accidentally producing back into the pipeline.

## Development

```bash
# From repo root:
go build ./apps/api/metrics-endpoint/...
go test  ./apps/api/metrics-endpoint/...

# Cross-module contract tests:
go test  ./tests/contract/metrics-endpoint/...
```
