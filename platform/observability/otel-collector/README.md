# platform/observability/otel-collector — OpenTelemetry Collector reference configs

OpenTelemetry Collector configurations shared across every OpenLLM Metrics
service. The shared Go SDK at [packages/telemetry/go](../../../packages/telemetry/go)
emits OTLP traces/metrics/logs into a Collector that ships to the
environment-specific backends declared here.

This is reference configuration — secrets and per-environment endpoints are
never committed. They are injected at deploy time via environment variables.

## Contents

```text
platform/observability/otel-collector/
├── dev.yaml             Local dev: stdout traces + Prometheus scrape exporter
├── staging.yaml         Staging: OTLP traces, remote-write metrics, OTLP logs
├── production.yaml      Production: tail sampling + memory limiter + retry
├── docker-compose.yml   Local dev Collector wired to dev.yaml
└── README.md            This file
```

## Quick start (local dev)

```bash
docker compose -f platform/observability/otel-collector/docker-compose.yml up -d
```

Pair with the TSDB stack so Prometheus scrapes the Collector's exporter:

```bash
docker compose -f platform/tsdb/docker-compose.yml up -d
```

The Collector exposes OTLP on `localhost:4317` (gRPC) and `:4318` (HTTP),
and the Prometheus scrape endpoint on `:8889`. Add this scrape target to
your local `platform/tsdb/prometheus.yml` if it is not already present.

## Deployment topology

| Environment | Topology                                         | Rationale                                                                                                      |
| ----------- | ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| dev         | single Collector, central                        | Lowest friction.                                                                                               |
| staging     | single Collector, central                        | Consolidates traces/metrics/logs to one config; simulates prod backends.                                       |
| production  | central deployment, optional sidecar per service | Default to central until per-service overhead becomes a measurable concern. Sidecars are an opt-in escalation. |

The F006 README open question on sidecar-vs-daemonset is answered as: **central
at staging; reconsider at production after measuring overhead under load.**

## Defense-in-depth redaction

Every config layers an `attributes/redact` processor that mirrors the redaction
key list in [packages/telemetry/go/redact.go](../../../packages/telemetry/go/redact.go)
and the alignment doc at [otel_alignment.md](../otel_alignment.md). The shared
SDK is the primary line of defense; the Collector processor is the safety net
in case a service ships with a buggy SDK release.

If you add a new sensitive key to one place, **add it to all three** (SDK
default list, alignment doc, every Collector config). The contract test in
[packages/telemetry/go/propagation_test.go](../../../packages/telemetry/go/propagation_test.go)
asserts the SDK list aligns with the doc; reviewers must verify the Collector
configs by hand.

## Sampling

| Environment | Base ratio | Error spans                               |
| ----------- | ---------- | ----------------------------------------- |
| dev         | 100%       | always sampled                            |
| staging     | 100%       | always sampled                            |
| production  | 10%        | always sampled (via tail_sampling policy) |

The Go SDK applies head sampling at span start. Tail sampling in
`production.yaml` enforces "error spans are always kept" because the SDK
cannot know an outcome at span start.

## Backend interchangeability

Endpoints and credentials are environment variables only. Swapping the trace
backend (Tempo to Jaeger to commercial APM), the metric backend (Prometheus to
VictoriaMetrics to Mimir to Grafana Cloud), or the log backend (Loki to
OpenSearch) is a config change, not a code change.

Variables consumed:

- `OTLP_TRACES_ENDPOINT`
- `OTLP_LOGS_ENDPOINT`
- `PROM_REMOTE_WRITE_ENDPOINT`
- `PROM_REMOTE_WRITE_TOKEN`

## Image pinning

`docker-compose.yml` pins `otel/opentelemetry-collector-contrib:0.104.0`. Bump
the tag in lockstep with the SDK version pinned in
[packages/telemetry/go/go.mod](../../../packages/telemetry/go/go.mod). Mismatched
SDK and Collector versions still interoperate over OTLP, but the redaction
processor's action names occasionally change between minor Collector releases.

## Owned by

F006 — Observability Baseline.
