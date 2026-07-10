# Quick Start — Local Launch

This guide brings up the OpenLLM Metrics stack on your laptop with a single
`docker compose` command. Everything runs locally: infra, all backend services,
and the admin console with its native analytics screens.

> **Verified:** `docker compose up -d` brings up 18 services and every one
> reports healthy. Telemetry is **runtime-first** (the in-repo Go gateway +
> SDKs); the pull-mode `llm-usage-exporter` path is an **optional add-on** (see
> step 3) and is not part of the default stack.

## Prerequisites

| Tool    | Minimum version | Notes                                                                                   |
| ------- | --------------- | --------------------------------------------------------------------------------------- |
| Docker  | 24+             | Docker Desktop or Docker Engine with Compose v2. This is all you need to run the stack. |
| Go      | 1.25+           | Only to build from source outside Docker / run `bootstrap.sh`.                          |
| Node.js | 20+             | Only for admin-console development outside Docker.                                      |
| pnpm    | 9+              | `corepack enable pnpm`. Dev only.                                                       |

> **Port conflicts.** The stack publishes these host ports: `3000` (Grafana),
> `3030` (admin console), `5433` (Postgres), `8095` (Redpanda Console), `8085`/`8086`
> (gateway proxy/metrics), `8083`/`8084` (cost-mapper/reconciler), `8087`–`8093`
> (workers + control-plane APIs), `8096` (analytics-service), `9090`
> (Prometheus), `9092` (metrics endpoint), `9644`/`18081`/`18082`/`19092`
> (Redpanda), `4317`/`4318`/`8889`/`13133` (OTel Collector). Free them first.

## 1. Clone

```bash
git clone https://github.com/xops-labs/openllm-metrics.git
cd openllm-metrics
```

## 2. Create your .env

```bash
cp .env.example .env
```

This step is optional for a first run — every compose variable has a working
default, so the stack boots with no `.env` at all. Set a real
`OPENAI_ADMIN_API_KEY` only when you want live OpenAI billing data; without it
the poller runs against a placeholder key and pulls nothing (the demo profile
below needs no keys at all). Other provider keys are optional.

## 3. Start the stack

```bash
docker compose up -d
```

> **First build takes a while.** The first `up` builds ~16 service images
> from source (Go multi-stage + Next.js builds) — expect 10–20 minutes on a
> typical laptop. Later runs reuse the build cache and start in seconds.

This single command builds and starts the full core stack:

- PostgreSQL 16 (host `5433`) with one-shot `db-migrate` (goose) and `db-seed`
  (demo data) services, Redpanda bus (`19092`), OTel Collector
  (`4317`/`4318`), Prometheus (`9090`), Grafana (`3000`), Redpanda Console (`8095`)
- LLM proxy gateway (proxy `8085`, metrics/health `8086`)
- Metrics-endpoint `/metrics` aggregator (`9092`)
- Control-plane APIs: policy (`8090`), audit (`8091`), decision (`8093`),
  analytics (`8096`)
- Workers: openai-poller, cost-mapper, reconciler, quota-risk, notifier
- Admin console (`3030`)

### Recommended first run: demo traffic (no API keys)

```bash
docker compose --profile demo up -d
```

This adds the [demo traffic generator](../examples/demo-generator/README.md):
synthetic, schema-conformant telemetry across all five providers under the
seeded Acme tenant. Within a minute of the containers starting, the native
`/analytics` screens, the Grafana FinOps dashboards, and the reconciliation
drift panels all show data.
Demo traffic is marked `source_service: examples/demo-generator`; check its
emit counters at `http://localhost:8089/healthz`.

### Optional: pull-mode exporter add-on

Telemetry is runtime-first — the Go gateway + SDKs cover provider usage, cost,
and latency with no exporter. Pull-mode adds billing-API reconciliation via a
separate `llm-usage-exporter` (plus the label-translator and focus-ingester that
consume it). It is bring-your-own and **not** part of the default stack — always
set `LLM_USAGE_EXPORTER_IMAGE`, since the pinned upstream default in compose may
not be publicly pullable:

```bash
# point at your exporter image, then start the optional chain:
LLM_USAGE_EXPORTER_IMAGE=ghcr.io/your-org/llm-usage-exporter:v0.5.0 \
  docker compose --profile exporter up -d
```

## 4. Migrations + demo seed (automatic)

Migrations and the demo seed run automatically: `docker compose up` starts two
one-shot services — `db-migrate` (goose, applies every per-schema migration
tree under `platform/db/<schema>/migrations`) and `db-seed` (psql, loads every
`platform/db/seeds/0*.sql` file). Both are idempotent, so restarts are safe,
and the DB-backed services wait for `db-migrate` to complete before starting.

```bash
# watch the one-shots complete (each should exit 0)
docker compose logs db-migrate db-seed
```

To run them by hand instead (e.g. against a non-compose Postgres):

```bash
# requires goose: go install github.com/pressly/goose/v3/cmd/goose@latest
./tools/scripts/migrate.sh apply control_plane
./tools/scripts/migrate.sh apply audit
./tools/scripts/migrate.sh apply gateway
./tools/scripts/migrate.sh apply scoring
./tools/scripts/seed.sh          # applies every platform/db/seeds/0*.sql
```

The demo data loads under the **Acme Corp** tenant, which the admin console uses
as its default tenant (see the seed login note below).

## 5. Verify services are healthy

```bash
docker compose ps
curl -fsS http://localhost:9092/healthz   # metrics-endpoint
curl -fsS http://localhost:8086/healthz   # gateway
curl -fsS http://localhost:3030/analytics # admin console (native analytics)
```

All services should show `running` (postgres, redpanda, prometheus, grafana show
`healthy`). If a service restarts, check `docker compose logs -f <service>`.

## 6. Open the interfaces

| Interface                        | URL                                      | Credentials                |
| -------------------------------- | ---------------------------------------- | -------------------------- |
| Admin console — native analytics | <http://localhost:3030/analytics>        | dev login (no OIDC config) |
| Admin console — policies / audit | <http://localhost:3030>                  | dev login                  |
| Admin console — exports settings | <http://localhost:3030/settings/exports> | dev login                  |
| Grafana dashboards               | <http://localhost:3000>                  | admin / devpassword        |
| Prometheus                       | <http://localhost:9090>                  | n/a                        |
| Metrics endpoint                 | <http://localhost:9092/metrics>          | n/a                        |
| Gateway (proxy target)           | <http://localhost:8085>                  | n/a                        |
| Redpanda Console                 | <http://localhost:8095>                  | n/a                        |

### Seed login

OIDC is not configured, so the console uses a local **dev login** — no password,
enter any email. The session lands on the **Acme Corp** demo tenant
(`OLM_DEFAULT_TENANT`), which carries the seeded policies, notification rules,
and audit entries. Any email works; the dev-login actor comes from
`OLM_DEV_USER`, which is `admin@acme.dev` (the seeded admin) whether or not
you copied `.env.example` in step 2 — the template and the compose fallback
both set it. Seeded users include `admin@acme.dev`, `sre@acme.dev`,
`finops@acme.dev`, and the cross-tenant `platform.admin@openllm-metrics.dev`.
Set the `OIDC_*` env vars to switch to real SSO.

### Native analytics, not Grafana-only

The admin console ships first-party analytics views at `/analytics` (cost over
time, tokens by team, error rate by provider, reconciliation drift). The
console queries the stack's Prometheus-compatible TSDB server-side (the
bundled Prometheus by default; `OLM_METRICS_QUERY_URL` points it elsewhere),
so you never have to open Grafana or Prometheus yourself. Grafana at `:3000`
remains available as an optional add-on.

## 7. Point an LLM SDK at the gateway

```python
# OpenAI Python SDK example — only the base_url changes.
import openai
client = openai.OpenAI(api_key="sk-...", base_url="http://localhost:8085/v1")
```

Usage, cost, latency, and error signals flow to the metrics endpoint and the
admin console. See `examples/proxy-demo/` for Go/Node/Python variants.

## 8. Stop the stack

```bash
docker compose down          # stop containers, keep volumes
docker compose down -v       # stop and wipe all volumes (full reset)
```

---

## Troubleshooting

### A service keeps restarting

```bash
docker compose logs --tail=50 <service-name>
```

Most often a dependency (Postgres / Redpanda) was not yet healthy at first
start; wait ~30s and re-check `docker compose ps`. Each backend service reads a
config file mounted from `platform/deployment/compose/configs/<service>.yaml`.

### Governance CRUD returns errors / empty results

Migrations run automatically (step 4) — check that the `db-migrate` one-shot
exited 0 with `docker compose logs db-migrate`, and re-run it with
`docker compose up db-migrate` if not. Native `/analytics` does not need the
tables; policy/audit/decision CRUD does.

### "port is already allocated"

Another process holds one of the ports listed above. Stop it, or remap the port
in `docker-compose.override.yml`.

### Metrics are missing from Prometheus

1. `curl http://localhost:9092/metrics` — confirm the metrics-endpoint responds.
2. <http://localhost:9090/targets> — all targets should be `UP`.
3. Confirm the gateway/poller is running and receiving traffic. (Pull-mode
   provider data requires the optional `--profile exporter` path.)
