# OpenLLM Metrics dashboards and alert rules pack

Operator-facing guide to the Phase 1 Grafana dashboards and Prometheus
alert rules shipped with OpenLLM Metrics. Delivered by F012.

## Table of contents

- [What's in the pack](#whats-in-the-pack)
- [How dashboards are auto-provisioned](#how-dashboards-are-auto-provisioned)
- [Manual import into an existing Grafana](#manual-import-into-an-existing-grafana)
- [Required datasource configuration](#required-datasource-configuration)
- [Dashboard variables](#dashboard-variables)
- [Alert rules](#alert-rules)
- [Importing alert rules into an existing Prometheus / Alertmanager](#importing-alert-rules-into-an-existing-prometheus--alertmanager)
- [Runbooks](#runbooks)
- [Local validation](#local-validation)

## What's in the pack

| Artifact                                     | Path                                                                             | Purpose                                                                                    |
| -------------------------------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| FinOps dashboard (canonical source of truth) | `packages/dashboards/grafana/phase1-finops.json`                                 | LLM spend, forecasts, cost per request, error rate.                                        |
| FinOps dashboard (quickstart compose copy)   | `platform/deployment/compose/grafana/provisioning/dashboards/phase1-finops.json` | Auto-loaded by `quickstart.yml`.                                                           |
| FinOps dashboard (root-stack TSDB copy)      | `platform/tsdb/grafana/provisioning/dashboards/phase1-finops.json`               | Auto-loaded by the root `docker-compose.yml` stack and `platform/tsdb/docker-compose.yml`. |
| Alert rules (canonical)                      | `packages/dashboards/prometheus-alerts/*.yml`                                    | LLMCostSpike, LLMHighErrorRate, LLMExporterStale.                                          |
| Alert rules (root-stack TSDB copy)           | `platform/tsdb/alerts/*.yml`                                                     | Auto-loaded by both Prometheus instances.                                                  |
| Runbooks                                     | `platform/runbooks/llm-*.md`                                                     | Per-alert triage notes referenced from `runbook_url`.                                      |
| promtool unit tests                          | `tests/dashboards/*.yml`                                                         | Synthetic fixtures that assert each alert actually fires.                                  |

The `packages/dashboards/` copies are the source of truth. The
`platform/` copies are kept in sync as part of every change so the
running stacks pick up updates without manual import.

## How dashboards are auto-provisioned

All three Docker compose stacks ship Grafana with file-based dashboard
provisioning enabled. Drop a `*.json` into the dashboards folder and
Grafana picks it up on next reload (the provider polls every 30
seconds — see `dashboards.yml`).

| Stack                                                              | Dashboards folder mounted into Grafana                         |
| ------------------------------------------------------------------ | -------------------------------------------------------------- |
| `docker compose up` (root `docker-compose.yml` — the primary path) | `platform/tsdb/grafana/provisioning/dashboards/`               |
| `docker compose -f platform/tsdb/docker-compose.yml up`            | `platform/tsdb/grafana/provisioning/dashboards/`               |
| `docker compose -f platform/deployment/compose/quickstart.yml up`  | `platform/deployment/compose/grafana/provisioning/dashboards/` |

All three stacks already include the Prometheus datasource with UID
`openllm-prometheus`, so the dashboard's `${DS_PROMETHEUS}` template
variable resolves automatically.

## Manual import into an existing Grafana

If you already run Grafana elsewhere:

1. Confirm the Prometheus datasource UID. The dashboard expects
   `openllm-prometheus`. If yours is different, either rename your
   datasource UID or swap it in the JSON before import.
2. In Grafana, navigate to **Dashboards -> New -> Import**.
3. Upload `packages/dashboards/grafana/phase1-finops.json`.
4. On the import screen, map the `DS_PROMETHEUS` variable to your
   Prometheus datasource and click **Import**.

## Required datasource configuration

```yaml
# Grafana provisioning snippet
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    uid: openllm-prometheus # required by the dashboard
    access: proxy
    url: http://prometheus:9090 # point at your Prometheus
    isDefault: true
```

For Prometheus the only requirement is that it is scraping the
OpenLLM Metrics services and emitting series under the metric names
declared in `packages/contracts/metrics/go/registry.go` (the F008 schema).
A reference scrape config lives at `platform/tsdb/prometheus.yml`.

## Dashboard variables

| Variable        | Source                                                               | Behaviour                                   |
| --------------- | -------------------------------------------------------------------- | ------------------------------------------- |
| `DS_PROMETHEUS` | Grafana datasource picker                                            | Required. Defaults to `openllm-prometheus`. |
| `tenant`        | `label_values(llm_requests_total, tenant)`                           | Multi-select, defaults to All.              |
| `env`           | `label_values(llm_requests_total{tenant=~"$tenant"}, env)`           | Multi-select, defaults to All.              |
| `team`          | `label_values(llm_requests_total{...,team)`                          | Multi-select, defaults to All.              |
| `app`           | `label_values(llm_requests_total{...,app)`                           | Multi-select, defaults to All.              |
| `provider`      | `label_values(llm_requests_total{...,provider)`                      | Multi-select, defaults to All.              |
| `model`         | `label_values(llm_requests_total{...,provider=~"$provider"}, model)` | Multi-select, defaults to All.              |

Variables are wired so a deployment can swap the label sources by
editing only the templating block — panel queries themselves are
parameterized.

> Sensitive identifiers (real tenant names, internal app codenames)
> should be masked at the metric source. Because the dashboard uses
> template variables for every dimension, deployments that need
> tenant-scoped views can drop a pre-filtered copy of the dashboard
> (with `tenant=$current_tenant` hard-coded) per audience.

## Alert rules

| Alert              | Severity ladder                          | Window    | Notes                                                                                    |
| ------------------ | ---------------------------------------- | --------- | ---------------------------------------------------------------------------------------- |
| `LLMCostSpike`     | warning at 2x baseline, critical at 3x   | 15m / 10m | Per `(tenant, team, provider)`.                                                          |
| `LLMHighErrorRate` | warning at 5%, critical at 15%           | 10m / 5m  | Per `(tenant, provider, model)`.                                                         |
| `LLMExporterStale` | warning at >2x interval, critical at >4x | 5m        | Per `(tenant, provider)`. Default polling interval is 300s; thresholds are 600s / 1200s. |

Every alert carries a `runbook_url` annotation pointing at the
matching Markdown stub under `platform/runbooks/`.

## Importing alert rules into an existing Prometheus / Alertmanager

1. Copy `packages/dashboards/prometheus-alerts/*.yml` into your
   Prometheus rules directory (whatever path your `rule_files:`
   configuration globs).
2. Reload Prometheus:
   ```bash
   curl -sS -XPOST http://<prometheus-host>/-/reload
   ```
3. Wire the alerts to Alertmanager (the alerts include `severity`
   and `team` labels suitable for routing). A starter routing
   snippet:
   ```yaml
   route:
     group_by: ['alertname', 'tenant', 'team', 'provider']
     receiver: default
     routes:
       - matchers: [severity = critical]
         receiver: pagerduty
       - matchers: [severity = warning]
         receiver: slack
   ```
4. Replace the GitHub URL inside each `runbook_url` annotation if you
   maintain a fork or internal runbook portal.

## Runbooks

| Alert              | Runbook                                                                                  |
| ------------------ | ---------------------------------------------------------------------------------------- |
| `LLMCostSpike`     | [platform/runbooks/llm-cost-spike.md](../../platform/runbooks/llm-cost-spike.md)         |
| `LLMHighErrorRate` | [platform/runbooks/llm-error-rate.md](../../platform/runbooks/llm-error-rate.md)         |
| `LLMExporterStale` | [platform/runbooks/llm-exporter-stale.md](../../platform/runbooks/llm-exporter-stale.md) |

## Local validation

Validate dashboards and alerts before opening a PR.

```bash
# JSON parses
python -m json.tool < packages/dashboards/grafana/phase1-finops.json > /dev/null

# Grafana dashboard linter (runs in CI via Docker)
docker run --rm -v "$PWD:/work" grafana/dashboard-linter \
  lint /work/packages/dashboards/grafana/phase1-finops.json

# Prometheus alert rule lint
docker run --rm --entrypoint promtool \
  -v "$PWD/packages/dashboards/prometheus-alerts:/rules" \
  prom/prometheus:v2.52.0 \
  check rules /rules/llm-cost-spike.yml /rules/llm-error-rate.yml /rules/llm-exporter-stale.yml

# Alert rule unit tests
docker run --rm --entrypoint promtool \
  -v "$PWD:/work" \
  prom/prometheus:v2.52.0 \
  test rules /work/tests/dashboards/cost-spike-fixture.yml \
            /work/tests/dashboards/error-rate-fixture.yml \
            /work/tests/dashboards/exporter-stale-fixture.yml
```

If the Grafana dashboard linter image is unavailable in your
environment, the CI workflow always runs it on the same JSON — the
local JSON-validity check above is a reasonable fallback before
pushing.
