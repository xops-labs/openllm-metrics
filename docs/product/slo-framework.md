# OpenLLM Metrics SLO framework

Operator-facing guide to LLM-native Service Level Objectives in OpenLLM
Metrics. Delivered by F027.

## Table of contents

- [What an SLO is here](#what-an-slo-is-here)
- [The three objective types](#the-three-objective-types)
- [Authoring an SLO](#authoring-an-slo)
- [What's in the pack](#whats-in-the-pack)
- [Loading rules into Prometheus](#loading-rules-into-prometheus)
- [Loading dashboards into Grafana](#loading-dashboards-into-grafana)
- [Burn-rate alerts](#burn-rate-alerts)
- [Wiring alerts into F033 notifications](#wiring-alerts-into-f033-notifications)
- [Validation](#validation)

## What an SLO is here

In OpenLLM Metrics an SLO is a YAML or JSON document that names a
quantitative reliability, availability, or cost target on the LLM
traffic flowing through one team's slice of one tenant. It is the
contract between the team that owns an AI feature and the platform
operators who carry the pager.

The same SLO document drives two artifacts:

1. A Grafana dashboard panel that renders observed performance versus
   target, plus error budget remaining.
2. A set of Prometheus burn-rate alerts that fire when the team is
   spending its error budget too quickly to meet the target by the end
   of the window.

The canonical schema is JSON Schema 2020-12 and lives at
`platform/slo/schemas/slo-definition.v1.json`. Three worked examples
sit in `platform/slo/examples/` -- copy whichever one matches your
objective type and edit the metadata.

## The three objective types

OpenLLM Metrics ships with three first-class objective types in v1:

| `objective_type` | What it measures                                                  | Required extra field    | Source metric                                             |
| ---------------- | ----------------------------------------------------------------- | ----------------------- | --------------------------------------------------------- |
| `latency_p99`    | Fraction of requests under a latency bound at p99.                | `latency_bound_seconds` | `llm_gateway_latency_seconds`                             |
| `availability`   | Fraction of requests that returned a 2xx response.                | _(none)_                | `llm_gateway_requests_total`                              |
| `cost_per_call`  | Fraction of windows where avg cost-per-call is under a USD bound. | `cost_bound_usd`        | `llm_estimated_cost_usd` and `llm_gateway_requests_total` |

The gateway (F018) emits the latency histogram and request counter, so
latency and availability SLOs are live wherever the gateway is scraped.
`llm_estimated_cost_usd` is owned by F017 (Provider-Neutral Cost Mapping
Engine); until the cost pipeline emits it, the cost SLO rules evaluate
against zero series and the burn-rate alerts stay quiet -- no false
positives on a partially deployed stack.

## Authoring an SLO

1. Copy the example that matches your objective type:
   - `platform/slo/examples/latency-p99.yaml`
   - `platform/slo/examples/availability.yaml`
   - `platform/slo/examples/cost-per-call.yaml`
2. Edit the metadata block at the top of the file:
   - `id` -- a short, kebab-case identifier unique within your tenant.
   - `name` -- a human-readable label that will appear in alerts.
   - `tenant`, `team`, `app` -- the three required scope fields.
   - `target` -- a fraction in `(0, 1)`. Typical values: `0.99`,
     `0.995`, `0.999`.
   - `window` -- the accounting window, typically `30d`.
   - `provider` and `model` -- optional narrowing filters.
3. For `latency_p99` set `latency_bound_seconds` (default in the
   example: `5.0`).
4. For `cost_per_call` set `cost_bound_usd` (default in the example:
   `0.02`).
5. Validate the document against the schema (see
   [Validation](#validation)).
6. Drop the file into your SLO catalog location (or, once F032 ships,
   load it through the admin console). The catalog location is
   deployment-specific; mount it as a ConfigMap or a Postgres row.

## What's in the pack

| Artifact                         | Path                                                                                | Purpose                                                                |
| -------------------------------- | ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| JSON Schema (v1)                 | `platform/slo/schemas/slo-definition.v1.json`                                       | Validates SLO documents at author time and at admin-console save time. |
| YAML examples                    | `platform/slo/examples/latency-p99.yaml`, `availability.yaml`, `cost-per-call.yaml` | Copy-and-edit starting points.                                         |
| Recording rules                  | `platform/slo/prometheus/recording-rules.yaml`                                      | Sub-window error ratios and 30-day error-budget-remaining series.      |
| Burn-rate alerts                 | `platform/slo/prometheus/alerts.yaml`                                               | Multi-window multi-burn-rate alerts (Google SRE workbook style).       |
| Grafana dashboard (latency)      | `platform/observability/grafana/dashboards/slo-latency.json`                        | Error budget gauge, burn rate over time, p50/p95/p99.                  |
| Grafana dashboard (availability) | `platform/observability/grafana/dashboards/slo-availability.json`                   | Uptime, error budget gauge, error spikes.                              |
| Grafana dashboard (cost)         | `platform/observability/grafana/dashboards/slo-cost.json`                           | Cost-per-call vs target, monthly trend, error budget gauge.            |

## Loading rules into Prometheus

The two YAML files at `platform/slo/prometheus/` are valid Prometheus
rule groups. Drop them into the directory Prometheus globs as
`rule_files`. A typical layout:

```yaml
# prometheus.yml
rule_files:
  - /etc/prometheus/rules/*.yaml

# /etc/prometheus/rules/slo-recording-rules.yaml      <- copy of recording-rules.yaml
# /etc/prometheus/rules/slo-alerts.yaml               <- copy of alerts.yaml
```

Reload Prometheus:

```bash
curl -sS -XPOST http://<prometheus-host>/-/reload
```

The recording rules evaluate every 30-60s and produce the
`sli:*:error_ratio:*` and `slo:*:budget_remaining:30d` series the
dashboards plot. The alert rules read those series plus the raw
gateway counters to evaluate burn-rate breaches.

## Loading dashboards into Grafana

The three dashboards under
`platform/observability/grafana/dashboards/` are stock Grafana 11
JSON. Two import paths:

**File-based provisioning.** Mount the folder into Grafana's dashboards
provisioning directory; the provider polls every 30s and picks the
files up automatically. Both compose stacks already do this for the
F012 finops dashboard; extend the same provider folder by dropping the
three SLO files alongside.

**Manual import.** In Grafana navigate to **Dashboards -> New ->
Import**, upload the JSON, and pick your Prometheus datasource on the
import screen. The dashboards expect the Prometheus datasource UID
`openllm-prometheus`; if yours differs, swap the UID before import.

Each dashboard exposes three template variables: `tenant`, `team`,
`app`. They are wired to multi-select All-by-default and feed every
panel query, so a single dashboard serves every owner in a multi-tenant
deployment.

## Burn-rate alerts

The alert pack implements the four-pair multi-window multi-burn-rate
ladder from the Google SRE Workbook (Chapter 5, "Alerting on SLOs").
Each severity tier requires two windows to be hot at once -- a long
window for sustained signal and a short window for responsiveness.

| Tier               | Long window | Short window | Burn rate (x normal) | Budget burned in long window | Alert severity |
| ------------------ | ----------- | ------------ | -------------------- | ---------------------------- | -------------- |
| Fast burn (page)   | 1h          | 5m           | 14.4                 | 2%                           | `critical`     |
| Slow burn (page)   | 6h          | 30m          | 6.0                  | 5%                           | `critical`     |
| Slow burn (ticket) | 1d          | 2h           | 3.0                  | 10%                          | `warning`      |
| Slow burn (ticket) | 3d          | 6h           | 1.0                  | 10%                          | `warning`      |

A burn rate of `1.0` means the error budget is being consumed at
exactly the rate that would deplete it precisely at the end of the
window. Anything above `1.0` is overspend.

The alerts are wired so the same `LLMSLO<Objective><Tier>` rule fires
for every `(tenant, team, app)` tuple that breaches; downstream
Alertmanager routing distinguishes audiences by the `team` and
`severity` labels.

## Wiring alerts into F033 notifications

F033 (Notification & Alerting Fan-Out) is the OSS multi-channel
notification surface, shipped as the
[notifier worker](../../apps/worker/notifier/README.md). An SLO document
declares its delivery channels in the `notification_channels` array, e.g.:

```yaml
notification_channels:
  - webhook-platform-search
  - email-finops
```

Each value is a channel identifier defined in the notifier's channel
config, stored in Postgres via its CRUD API (see the
[notifier README](../../apps/worker/notifier/README.md) for the
`/v1/notification/channels` endpoints). OSS ships generic-webhook and
SMTP sinks; vendor-branded sinks (Slack, PagerDuty, Teams) are
not implemented here. The fan-out worker matches each firing alert against
per-tenant routing rules and dispatches to the listed channels.
Operators can also configure Alertmanager routes directly using the
`team` and `severity` labels the alerts already emit -- the F033 wiring
is purely additive.

A starter Alertmanager routing block:

```yaml
route:
  group_by: ['alertname', 'tenant', 'team', 'app', 'slo']
  receiver: default
  routes:
    - matchers: [component = slo, severity = critical]
      receiver: pagerduty
    - matchers: [component = slo, severity = warning]
      receiver: slack
```

## Validation

Validate an SLO YAML against the JSON Schema:

```bash
# Python: jsonschema (handles JSON Schema 2020-12)
python -c "import json,sys,yaml; from jsonschema import validate; \
  schema=json.load(open('platform/slo/schemas/slo-definition.v1.json')); \
  doc=yaml.safe_load(open(sys.argv[1])); validate(doc, schema); print('OK')" \
  platform/slo/examples/latency-p99.yaml

# Node: ajv (JSON Schema 2020-12 via ajv/dist/2020)
npx -y ajv-cli@5 validate \
  --spec=draft2020 \
  -s platform/slo/schemas/slo-definition.v1.json \
  -d platform/slo/examples/latency-p99.yaml
```

Validate the Prometheus rules:

```bash
docker run --rm --entrypoint promtool \
  -v "$PWD/platform/slo/prometheus:/rules" \
  prom/prometheus:v2.52.0 \
  check rules /rules/recording-rules.yaml /rules/alerts.yaml
```

Validate the Grafana dashboards:

```bash
docker run --rm -v "$PWD:/work" grafana/dashboard-linter \
  lint /work/platform/observability/grafana/dashboards/slo-latency.json
docker run --rm -v "$PWD:/work" grafana/dashboard-linter \
  lint /work/platform/observability/grafana/dashboards/slo-availability.json
docker run --rm -v "$PWD:/work" grafana/dashboard-linter \
  lint /work/platform/observability/grafana/dashboards/slo-cost.json
```
