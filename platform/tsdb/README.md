# platform/tsdb — TSDB Baseline

Prometheus-compatible time-series database baseline for OpenLLM Metrics.
Provides local-dev scrape configuration, retention policy, alert rules,
Grafana datasource provisioning, and remote-write target reference templates.

## Contents

```text
platform/tsdb/
├── docker-compose.yml                          Local-dev Prometheus + Grafana stack
├── prometheus.yml                              Scrape config for all OpenLLM Metrics services
├── alerts/
│   └── scrape_failure.yml                      Alert rules: scrape health + TSDB health
├── grafana/
│   └── provisioning/
│       └── datasources/
│           └── prometheus.yml                  Auto-provisioned Prometheus datasource
├── remote_write_targets.yml                    Remote-write examples (VictoriaMetrics / Mimir / Cortex)
├── CARDINALITY_BUDGET.md                       Canonical label set and cardinality limits
└── RETENTION.md                                Retention tiers and storage sizing guidance
```

## Quick Start (local dev)

```bash
docker compose -f platform/tsdb/docker-compose.yml up -d
```

| Service    | URL                     | Credentials         |
| ---------- | ----------------------- | ------------------- |
| Prometheus | <http://localhost:9090> | none (dev mode)     |
| Grafana    | <http://localhost:3000> | admin / devpassword |

The Prometheus datasource is provisioned automatically.
Open Grafana → Explore → select **Prometheus** to run ad-hoc PromQL queries.

## Running with other stacks

To bring up all local-dev dependencies together:

```bash
# Terminal 1 — streaming bus
docker compose -f platform/bus/docker-compose.yml up -d

# Terminal 2 — database
docker compose -f platform/db/docker-compose.yml up -d

# Terminal 3 — TSDB
docker compose -f platform/tsdb/docker-compose.yml up -d
```

Or use a root-level compose override when one is added in a later feature.

## Scrape Configuration

Service targets are defined in `prometheus.yml` as static configs.
Each `openllm_*` job carries a `service` label injected via `relabel_configs`.

When the Kubernetes infra layer lands, static targets will be replaced with
`kubernetes_sd_configs` using the same label conventions.

## Alert Rules

Alerts are loaded from `alerts/scrape_failure.yml` by Prometheus at startup.
In dev mode, Alertmanager is disabled — alerts appear in the Prometheus UI only.
For staging/production, un-comment the `alertmanagers` block in `prometheus.yml`
and point it at an Alertmanager instance.

## Remote Write

See `remote_write_targets.yml` for reference blocks covering:

- VictoriaMetrics single-node
- Grafana Mimir (multi-tenant)
- Cortex / Thanos receive
- Grafana Cloud managed Prometheus

Copy the relevant block into `prometheus.yml` under `remote_write:`.
Inject credentials via environment variables or a secrets manager — never
commit tokens or passwords to source control.

## Cardinality and Retention

- [CARDINALITY_BUDGET.md](./CARDINALITY_BUDGET.md) — canonical label set, per-dimension limits,
  lint enforcement plan (F008), and the cardinality runbook.
- [RETENTION.md](./RETENTION.md) — retention tiers (14d raw / 90d downsampled / 1y archive),
  recording-rule patterns, and storage sizing guidance.

## Backend Interchangeability

The project never uses TSDB-backend-specific PromQL extensions in core service
code. All queries use standard PromQL so the backend can be swapped from
Prometheus to VictoriaMetrics, Mimir, or Cortex by changing only the
remote-write block and the Grafana datasource URL.
