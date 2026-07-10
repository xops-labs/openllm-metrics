# Release Notes — v0.1.0

> **Status: released.** Tagged 2026-06-11 from `main` (owner sign-off; see the
> completed pre-tag checklist below).

## What is OpenLLM Metrics v0.1.0?

v0.1.0 is the first public release of the OpenLLM Metrics open-source telemetry
control plane. It ships as a single `docker compose up -d` product: infra, all
backend services, and the admin console (with native analytics) come up together
with one command — DB migrations and demo seed data apply automatically.
Telemetry is runtime-first (the in-repo Go gateway + SDKs); pull-mode billing
reconciliation is an optional add-on and not part of the default stack.

## Highlights

### One-command local launch

```bash
docker compose up -d
```

The core stack — gateway, metrics endpoint, control-plane services, workers,
admin console, PostgreSQL, Redpanda, Prometheus, Grafana — starts together on a
shared local network with no external dependencies. One-shot `db-migrate`
(goose) and `db-seed` services apply every schema migration and the demo seed
automatically, so governance CRUD (policies, audit, decisions, notifications)
is populated on first boot; both are idempotent across restarts.

### Demo mode — no API keys required

```bash
docker compose --profile demo up -d
```

A synthetic traffic generator (`examples/demo-generator`) emits
schema-conformant telemetry across all five providers under the seeded demo
tenant — runtime request events, pull-mode billing rollups, and reconciled
billing windows with realistic drift — so the native analytics screens, the
FinOps Grafana dashboards, and the reconciliation drift panels all show live
data within a minute of first boot. Demo traffic is always identifiable via
`source_service: examples/demo-generator`.

### Telemetry: runtime-first, pull-mode optional

The default telemetry source is **runtime mode** — the in-repo Go gateway and
SDKs capture provider usage, cost, latency, and errors with no external
component. **Pull-mode** (billing-API reconciliation via a separate
`llm-usage-exporter`) is an **optional add-on**: bring your own exporter image
(`LLM_USAGE_EXPORTER_IMAGE`) and run `docker compose --profile exporter up -d`.
This keeps the default product low-dependency and low-complexity. Exports to
external Grafana, Prometheus, or OpenTelemetry Collector instances are likewise
optional, configured in the admin console under **Settings > Exports**.

Default exporter pin (for the optional path): **v0.5.0**
(`platform/adoption/llm-usage-exporter.version`). Bring your own image and set
`LLM_USAGE_EXPORTER_IMAGE` — the pinned upstream default may not be publicly
pullable.

### Native analytics screens

The admin console ships first-party `/analytics/*` views so customers see cost,
latency, error-rate, and quota-risk dashboards without opening Grafana — no
Grafana or Alertmanager required. The console does query a Prometheus-compatible
TSDB for this data (Prometheus is bundled in the compose stack). Grafana remains
available for teams that want it.

### Full GHCR image matrix

All fourteen services are built, signed (cosign keyless OIDC), SBOM-attested
(CycloneDX), and Trivy-scanned on every release tag:

| Image                                               | Dockerfile                                   |
| --------------------------------------------------- | -------------------------------------------- |
| `ghcr.io/<owner>/openllm-metrics/gateway`           | `apps/gateway/Dockerfile`                    |
| `ghcr.io/<owner>/openllm-metrics/metrics-endpoint`  | `apps/api/metrics-endpoint/Dockerfile`       |
| `ghcr.io/<owner>/openllm-metrics/policy-service`    | `apps/api/policy-service/Dockerfile`         |
| `ghcr.io/<owner>/openllm-metrics/audit-service`     | `apps/api/audit-service/Dockerfile`          |
| `ghcr.io/<owner>/openllm-metrics/decision-service`  | `apps/api/decision-service/Dockerfile`       |
| `ghcr.io/<owner>/openllm-metrics/analytics-service` | `apps/api/analytics-service/Dockerfile`      |
| `ghcr.io/<owner>/openllm-metrics/openai-poller`     | `apps/worker/usage-poller/openai/Dockerfile` |
| `ghcr.io/<owner>/openllm-metrics/label-translator`  | `apps/worker/label-translator/Dockerfile`    |
| `ghcr.io/<owner>/openllm-metrics/focus-ingester`    | `apps/worker/focus-ingester/Dockerfile`      |
| `ghcr.io/<owner>/openllm-metrics/cost-mapper`       | `apps/worker/cost-mapper/Dockerfile`         |
| `ghcr.io/<owner>/openllm-metrics/reconciler`        | `apps/worker/reconciler/Dockerfile`          |
| `ghcr.io/<owner>/openllm-metrics/quota-risk`        | `apps/worker/quota-risk/Dockerfile`          |
| `ghcr.io/<owner>/openllm-metrics/notifier`          | `apps/worker/notifier/Dockerfile`            |
| `ghcr.io/<owner>/openllm-metrics/admin-console`     | `apps/web/admin-console/Dockerfile`          |

### Provider support

Phase 1 ships with the OpenAI usage poller. The adapter interface is stable;
Anthropic Claude, Google Gemini / Vertex AI, Azure OpenAI, and AWS Bedrock
pollers follow in Phase 2.

## Platform requirements

- Go 1.25+
- Node.js 20+ / pnpm 9+
- Docker 24+ with Compose v2

## Dependency versions

| Dependency                                          | Version                |
| --------------------------------------------------- | ---------------------- |
| Go (runtime floor)                                  | 1.25.0 (see `go.work`) |
| Node.js (CI)                                        | 20 LTS                 |
| pnpm                                                | 9.15.0                 |
| llm-usage-exporter (optional; bring your own image) | v0.5.0 (default pin)   |
| Redpanda                                            | v24.1.9                |
| PostgreSQL                                          | 16                     |
| Prometheus                                          | v2.52.0                |
| Grafana                                             | 11.0.0                 |

## OSS-deferred features

The following features are not included in this release. Their public Go interfaces are available in `packages/extensions/go/` with safe no-op defaults.

- F024 Reliability scoring
- F025 Cost-efficiency scoring
- F030 Policy and budget evaluator
- F034 Routing decision engine
- F035 Bounded fallback controller

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution scope and review expectations.

## Pre-tag checklist (complete)

- **DB migrations in the stack** — done. One-shot `db-migrate` (goose, all
  per-schema trees) and `db-seed` services run on every `docker compose up`;
  verified live against Postgres (idempotent re-runs, governance data seeded).
- **CI green on `main`** — done. CI runs six parallel jobs: lint
  (golangci-lint, Prettier, ESLint, markdownlint), typecheck (`tsc --noEmit`
  for the admin console and the Node SDK), test (Go `-race` suites plus the
  TypeScript workspace tests), dashboards (Grafana / promtool validation),
  build (every `go.work` module), and secret-scan (gitleaks). The .NET and
  Python SDKs are not yet exercised by CI.
- **Real-key smoke** — waived by the owner for v0.1.0; the no-key path
  (`--profile demo`) is verified end-to-end. Run a real-key smoke before
  promoting v0.1.0 beyond early-adopter use.

Pull-mode (the `llm-usage-exporter`) is intentionally **optional** and was not
a prerequisite for tagging — it stays a bring-your-own add-on.

## How to verify a released image

```bash
cosign verify \
  --certificate-identity-regexp 'https://github.com/.+/openllm-metrics-oss/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/<owner>/openllm-metrics/<service>:0.1.0
```
