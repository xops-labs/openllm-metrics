# Adopted Components

OpenLLM Metrics is built as a **composition** of upstream open-source
components plus original platform code. This document catalogs the upstream
components this repository adopts, explains why each was adopted rather than
built from scratch, describes the integration points, and states the
**upstream-PR-only modification rule** that governs how we work with each
component.

## The upstream-PR-only modification rule

For every component listed in this document, the following rule applies:

> **We do not modify upstream component source code in this repository.**
> If a behaviour change is needed, the change is proposed via a pull request
> to the upstream project. We do not maintain a fork, a vendored copy, or a
> runtime patch of any upstream component. Bundling is composition, not
> customization.

This rule has one narrow exception: configuration files (environment variables,
YAML configs, scrape targets) that are owned by this repository and passed
into the upstream component at runtime. We own the configuration; we do not
own the binary.

## `llm-usage-exporter` (xops-labs)

| Property            | Value                                                   |
| ------------------- | ------------------------------------------------------- |
| Upstream repository | <https://github.com/xops-labs/llm-usage-exporter>       |
| License             | Apache-2.0                                              |
| Pinned version      | See `platform/adoption/llm-usage-exporter.version`      |
| Integration mode    | Bundled as a Docker image inside the compose/Helm stack |

> **Current status (v0.1.0).** The exporter is an **optional pull-mode add-on**,
> not part of the default stack. The default product uses runtime mode (the
> in-repo Go gateway + SDKs) for provider telemetry. Enable pull-mode with
> `docker compose --profile exporter up -d` and a bring-your-own image
> (`LLM_USAGE_EXPORTER_IMAGE`); the upstream repository/registry path above is
> still to be confirmed when you publish that image.

### Why adopted

The `llm-usage-exporter` is a production-grade Prometheus exporter and FOCUS
billing API client for LLM providers. Building equivalent pull-mode coverage
from scratch across five providers would take months and introduce ongoing
maintenance burden for each provider's billing API evolution. The upstream
project has established that surface; composing with it lets this repo focus
on the control-plane layer (normalization, policy, routing, audit) that the
exporter does not provide.

See `docs/architecture/bundled-vs-external.md` for the full bundling
rationale.

### Integration points

1. **Pull-mode telemetry.** The bundled exporter polls each provider's usage
   API and exposes Prometheus metrics at `http://llm-usage-exporter:9090/metrics`.
   The label-translator worker (`apps/worker/label-translator/`) scrapes this
   endpoint and re-publishes canonical events to the bus.

2. **FOCUS billing data.** The exporter exposes a `/focus.json` endpoint with
   [FOCUS-compatible](https://focus.finops.org/) cost records. The
   focus-ingester worker (`apps/worker/focus-ingester/`) polls this endpoint
   and stores records in Postgres for the F023 reconciliation layer.

3. **Credential pass-through.** Provider API keys are passed to the exporter
   container as environment variables. The platform forwards them without
   reading or persisting them. See the credential configuration sections in the
   per-provider docs under `docs/architecture/providers/`.

4. **Version pin.** The pinned upstream image tag lives in
   `platform/adoption/llm-usage-exporter.version`. All deployment files
   (compose, Helm) read from this single source. Bumping the pin is a
   one-PR change to this repository.

5. **Cosign verification.** The release workflow verifies the upstream image's
   cosign signature against the xops-labs Sigstore identity before publishing
   any release of this product that bundles the exporter.

### What we do NOT adopt

- The upstream C# source code. It is never copied into this repository.
- Build tooling, NuGet packages, or CI pipelines from the upstream repo.
- Any forked or patched image. We pull the published upstream tag by digest.

### Upgrade path

See `platform/adoption/README.md` for the step-by-step pin bump procedure.

### Upstream contribution path

If OpenLLM Metrics needs a behaviour change in the exporter (new provider
adapter, new FOCUS field, bug fix), the change is submitted to
`xops-labs/llm-usage-exporter` as a GitHub pull request. This repository's
issue tracker may track the upstream PR as a dependency, but no downstream
workaround is acceptable as a substitute for an upstream fix.

---

## OpenTelemetry Collector (open-telemetry)

| Property            | Value                                                                  |
| ------------------- | ---------------------------------------------------------------------- |
| Upstream repository | <https://github.com/open-telemetry/opentelemetry-collector>            |
| Contrib repository  | <https://github.com/open-telemetry/opentelemetry-collector-contrib>    |
| License             | Apache-2.0                                                             |
| Integration mode    | Custom distribution built with `ocb` (OpenTelemetry Collector Builder) |

### Why adopted

The OpenTelemetry Collector is the industry standard for vendor-neutral
telemetry pipeline construction. Using it as the host for the
`llmproviderreceiver` means operators can slot the receiver into their existing
OTel pipelines — Datadog, Grafana Cloud, Honeycomb, Splunk, and any other
OTLP-compatible backend Just Work without any changes to the receiver.

### Integration points

1. **`llmproviderreceiver`** (`platform/otel-collector/receiver/llmproviderreceiver/`):
   A custom OTel Collector receiver that bridges the OpenLLM Metrics streaming
   bus (Kafka / Redpanda) into any OTel metrics pipeline. Built as a standalone
   Go module following the OTel Collector contrib pattern.

2. **Builder manifest** (`platform/otel-collector/builder-config.yaml`):
   An `ocb` manifest that combines the `llmproviderreceiver` with upstream
   Collector contrib processors and exporters (Prometheus, OTLP/HTTP,
   `attributesprocessor`, `batchprocessor`) into a single distributable binary.

3. **Example config** (`platform/otel-collector/example-config.yaml`):
   A reference Collector configuration showing the receiver wired to both a
   Prometheus scrape endpoint and an OTLP/HTTP exporter.

4. **Collector contrib processors.** `attributesprocessor` and
   `resourceprocessor` from `opentelemetry-collector-contrib` are used for
   redaction and resource enrichment; they are referenced by the builder
   manifest and pulled at build time, not vendored.

### What we do NOT adopt

- The upstream Collector source code. We build a custom distribution using
  `ocb`; we do not fork or patch the upstream Collector binary.
- Internal Collector packages. The `llmproviderreceiver` imports only the
  stable public interfaces: `component`, `consumer`, `receiver`, and `pdata`.

### Upstream contribution path

If the `llmproviderreceiver` reaches sufficient maturity, it may be proposed
for inclusion in `opentelemetry-collector-contrib`. Until then it ships as a
custom distribution. Any issue in the upstream Collector framework (not the
receiver itself) is filed at
<https://github.com/open-telemetry/opentelemetry-collector/issues>.

---

## Prometheus / VictoriaMetrics (TSDB)

| Property              | Value                                                                                            |
| --------------------- | ------------------------------------------------------------------------------------------------ |
| Upstream repositories | <https://github.com/prometheus/prometheus>, <https://github.com/VictoriaMetrics/VictoriaMetrics> |
| License               | Apache-2.0                                                                                       |
| Integration mode      | Docker image (stock, unmodified); scrape config owned by this repo                               |

### Why adopted

Prometheus is the de facto standard for metrics storage in cloud-native
infrastructure. The `llm_*` metric series this platform emits follow the
Prometheus data model; any Prometheus-compatible TSDB (Prometheus, Mimir,
VictoriaMetrics, Thanos) can store them. We do not build a custom TSDB.

### Integration points

1. **Scrape configs** (`platform/tsdb/prometheus.yml`,
   `platform/deployment/compose/prometheus/prometheus.yml`): owned by this
   repo; define which endpoints Prometheus scrapes and at what interval.

2. **Recording rules** (`platform/slo/prometheus/recording-rules.yaml`): owned
   by this repo; define the SLI/SLO sub-window aggregations the dashboards and
   alerts consume.

3. **Alert rules** (`platform/slo/prometheus/alerts.yaml`,
   `platform/tsdb/alerts/`): owned by this repo; multi-window multi-burn-rate
   SLO alerts plus cost, error-rate, and stale-exporter alerts.

4. **Remote-write targets** (`platform/tsdb/remote_write_targets.yml`): example
   config for forwarding series to Grafana Cloud, Mimir, or other remote-write
   endpoints.

### Upstream contribution path

Any required change to Prometheus query language semantics or remote-write
protocol belongs in the upstream Prometheus project. Recording rule and alert
rule improvements go in this repo's `platform/slo/prometheus/` directory.

---

## Grafana (dashboards)

| Property            | Value                                                               |
| ------------------- | ------------------------------------------------------------------- |
| Upstream repository | <https://github.com/grafana/grafana>                                |
| License             | AGPL-3.0 (Grafana OSS), Apache-2.0 (Grafana Alloy, Grafana plugins) |
| Integration mode    | Docker image (stock); dashboard JSON owned by this repo             |

### Why adopted

Grafana is the most widely deployed open-source dashboard and alerting UI.
Shipping Grafana-compatible dashboard JSON means operators who already run
Grafana can import the dashboards without any additional tooling.

### Integration points

1. **Dashboard JSON files** (`platform/observability/grafana/dashboards/`,
   `platform/tsdb/grafana/provisioning/dashboards/`,
   `platform/deployment/compose/grafana/provisioning/dashboards/`): all
   dashboard JSON is owned by this repository and follows the Grafana dashboard
   schema. We do not fork Grafana's source.

2. **Provisioning configs** (`platform/deployment/compose/grafana/provisioning/`):
   datasource and dashboard provider configurations owned by this repo and
   mounted into the Grafana container at runtime.

3. **SLO dashboards** (`platform/observability/grafana/dashboards/slo-*.json`):
   latency, availability, and cost-per-call SLO views backed by the recording
   rules in `platform/slo/prometheus/recording-rules.yaml`.

### Upstream contribution path

Dashboard layout and UI issues belong to the upstream Grafana project. Panel
query changes and new dashboards belong in this repository.

---

## Redpanda / Apache Kafka (streaming bus)

| Property         | Value                                                                          |
| ---------------- | ------------------------------------------------------------------------------ |
| Upstream         | <https://github.com/redpanda-data/redpanda>, <https://github.com/apache/kafka> |
| License          | BSL 1.1 (Redpanda), Apache-2.0 (Kafka)                                         |
| Integration mode | Docker image (stock); topic config and schema owned by this repo               |

### Why adopted

The streaming bus decouples producers (gateway, SDKs, exporter adapter) from
consumers (aggregator, label-translator, focus-ingester, OTel receiver,
scoring worker) at scale. Redpanda is Kafka-compatible and operationally
simpler for self-hosted deployments.

### Integration points

1. **Topic definitions** (`platform/bus/topics.yaml`): topic names, partition
   counts, and retention policies owned by this repo.
2. **Schema evolution rules** (`platform/bus/SCHEMA_EVOLUTION.md`): owned by
   this repo; govern how event schemas evolve without breaking consumers.
3. **Consumer libraries**: producers and consumers in this repo use
   `franz-go` (Go) for Kafka-protocol compatibility with both Kafka and
   Redpanda. No fork.

### Upstream contribution path

Broker behaviour issues belong to the upstream Redpanda or Kafka projects.
Topic schema and consumer logic changes belong in this repository.
