# platform

Database, infrastructure, security, deployment, observability, and operations assets.

## Table of Contents

- [Folders](#folders)

## Folders

- `db`: PostgreSQL migrations (goose) and seed data.
- `bus`: streaming bus topology, topic definitions, and schema-evolution rules.
- `tsdb`: Prometheus-compatible scrape config, retention, cardinality budget, alert rules, and remote-write targets.
- `pricing`: per-provider pricing catalogs (YAML) consumed by the cost-mapper.
- `slo`: SLO definition schema, examples, and generated recording / alert rules.
- `deployment`: Docker Compose configs and the Helm chart.
- `observability`: OpenTelemetry Collector configs, Grafana dashboards, and OTel / GenAI alignment notes.
- `otel-collector`: the custom `llmprovider` Collector receiver and its builder manifest.
- `adoption`: pinned upstream component versions (e.g. `llm-usage-exporter`).
- `runbooks`: operational runbooks.

Sensitive-integrity assets (audit ledger, access control, third-party adapters, key management) are change-controlled via [`.github/CODEOWNERS`](../.github/CODEOWNERS). Scoring formulas and routing logic are **not** in this repo — they live behind the extension interfaces in [`packages/extensions/go`](../packages/extensions/go) (see extension interfaces).
