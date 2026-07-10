# admin-console (F032)

OSS admin and governance console for OpenLLM Metrics. Next.js 15 App Router
with React Server Components, TypeScript strict, Tailwind CSS, pnpm.

The console is the **only web surface in this project**. No consumer-facing
UI is built here.

## Privacy invariant

**No prompts or completions are ever rendered anywhere in this UI.** Pages
display token counts, costs, latencies, errors, audit metadata, policy
documents, and routing-decision outcomes only. Provider API keys, request
bodies, and response bodies are not reachable from this app.

## Pages

| Path                                                                  | Backend service               | Notes                                                                                          |
| --------------------------------------------------------------------- | ----------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| `/`                                                                   | policy + audit + decision     | Counts and the 10 most recent decisions.                                                       |
| `/analytics`, `/analytics/{cost,tokens,errors,reconciliation}`        | Prometheus query API (F038)   | First-party native analytics; raw `llm_*` series only — no scoring/routing/anomaly.            |
| `/settings/exports`                                                   | `/api/exports` (F039)         | Optional outbound exports (Grafana, Prometheus remote-write, OTel); typed config contract.     |
| `/tenants`, `/tenants/{id}`                                           | tenant client (F005)          | Reads `OLM_CONTROL_PLANE_URL` `/v1/tenants`; dev seed when unset.                              |
| `/policies`, `/policies/new`, `/policies/{id}`, `/policies/{id}/edit` | `policy-service` (F029)       | RJSF form bound to the policy JSON Schema; mutations append a version and emit an audit event. |
| `/audit`                                                              | `audit-service` (F031)        | Filter by action, actor, tenant, date range; cursor pagination.                                |
| `/decisions`                                                          | decision read API (F036)      | Read-only. Empty state if the OSS no-op decider is in use.                                     |
| `/slo`                                                                | Grafana iframe                | Embeds three F027 dashboards. Configure via `OLM_GRAFANA_URL`.                                 |
| `/notifications/channels`, `/notifications/rules`                     | `notification-service` (F033) | Channel `kind` dropdown is hard-coded to `webhook                                              | smtp` — Slack/PD/Teams are not included in this repo. |

## Native analytics (F038)

First-party screens under `/analytics/*` render the raw normalized telemetry
directly off a Prometheus-compatible query API — customers do **not** have to
assemble Grafana/Prometheus dashboards themselves:

- `/analytics/cost` — USD spend per provider over time (`llm_cost_usd_total`).
- `/analytics/tokens` — tokens consumed per team (`llm_total_tokens_total`).
- `/analytics/errors` — error/timeout/rate-limit share of requests per provider.
- `/analytics/reconciliation` — estimated vs reconciled cost drift (F023 gauges).

The data source is `OLM_METRICS_QUERY_URL`, the base URL of a Prometheus,
VictoriaMetrics, or Mimir instance that scrapes the metrics-endpoint service
(F010). Every query is scoped to the active tenant via the `tenant` label.
These screens render raw data only — no scoring, routing, anomaly, or fallback
inference; those surfaces are outside these raw analytics views.

Charts are dependency-free server-rendered SVG (`components/charts.tsx`) to
keep this internal tool lean.

## Exports (F039)

`/settings/exports` configures optional outbound exports to Grafana,
Prometheus remote-write, and OTel. The typed config contract lives in
`lib/api/exports.ts` and is persisted via `PUT /api/exports` (validated
server-side). Secrets are never stored — targets reference the _name_ of a
secret-store key. The export runtime (the worker that ships series out) is out
of scope for the console.

## Authentication (F005)

Real OIDC scaffold via Auth.js (next-auth). When `OIDC_ISSUER`,
`OIDC_CLIENT_ID`, and `OIDC_CLIENT_SECRET` are set, a generic OIDC provider is
configured (any standards-compliant IdP). When they are absent, the console
falls back to a local dev-login (`/dev-login`) so it boots with zero config —
any email works; `admin@acme.dev` is the seeded admin used by default.
The session carries the actor `id` and `tenantId` (from the OIDC tenant claim);
the tenant switcher writes an `olm_tenant` cookie to override per request. Every
backend call forwards `X-Actor`, `X-OLM-User`, and `X-Tenant-ID`.

## Configuration

[`.env.example`](.env.example) is the canonical, fully annotated env example
(`.env.local.example` is just a pointer to it). Copy it to `.env.local` and
adjust:

```
OLM_POLICY_SERVICE_URL=http://localhost:8090
OLM_AUDIT_SERVICE_URL=http://localhost:8091
OLM_NOTIFICATION_SERVICE_URL=http://localhost:8092
OLM_DECISION_SERVICE_URL=http://localhost:8093
OLM_METRICS_QUERY_URL=http://localhost:9090
OLM_ANALYTICS_SERVICE_URL=http://localhost:8096
OLM_GRAFANA_URL=http://localhost:3000
OLM_DEFAULT_TENANT=00000000-0000-0000-0002-000000000001
OLM_DEV_USER=admin@acme.dev
```

`OLM_ANALYTICS_SERVICE_URL` points at analytics-service, which persists
user-defined saved views for the dashboards screen; the compose stack publishes
it on host port 8096 (host 8095 is Redpanda Console). `OLM_DEFAULT_TENANT`
above is Acme Corp, the demo tenant seeded by the compose stack
(`platform/db/seeds/001_demo_data.sql`), so a host-mode console pointed at the
compose services sees the seeded demo data. In the containerized deployment
the console only receives what `docker-compose.yml` forwards — `.env.local` is
host-mode only.

## Development quickstart

```
pnpm install   # install workspace deps (pnpm-lock.yaml is committed at the repo root)
pnpm dev       # serves on http://localhost:3030
pnpm typecheck
pnpm build
```

## Tests

Vitest + React Testing Library. Run with `pnpm --dir apps/web/admin-console test`.
Coverage: chart components, the exports config validation contract, the
Prometheus query client (mocked fetch), and a smoke test of the cost analytics
screen (empty-state + data render).

## Container image

`docker build -f apps/web/admin-console/Dockerfile -t openllm/admin-console:dev .`
(build context is the **repo root** so the pnpm workspace resolves). The image
runs the Next.js standalone bundle and exposes port 3000.

## Out of scope (do not add here)

- Slack / PagerDuty / Teams notification channels — not included in this repo.
- Scoring / routing / anomaly / fallback screens — not included in this repo.
- Export runtime worker — F039 worker consumes the config persisted here.
- Marketing copy, hero sections, animations — this is an internal tool.

## File layout

```
app/
  layout.tsx, page.tsx, globals.css, dev-login/page.tsx
  analytics/page.tsx + {cost,tokens,errors,reconciliation}/page.tsx, layout.tsx, shared.tsx
  settings/exports/page.tsx
  tenants/page.tsx, tenants/[id]/page.tsx
  policies/page.tsx, policies/new/page.tsx, policies/[id]/page.tsx, policies/[id]/edit/page.tsx
  audit/page.tsx
  decisions/page.tsx
  slo/page.tsx
  notifications/channels/page.tsx, notifications/rules/page.tsx
  api/auth/[...nextauth]/route.ts, api/exports/route.ts
components/
  nav.tsx, tenant-switcher.tsx, table.tsx, form.tsx,
  page-header.tsx, empty-state.tsx, pagination.tsx, charts.tsx,
  policy-editor.tsx, diff-viewer.tsx, exports-editor.tsx
lib/
  auth.ts, auth-config.ts
  api/policy.ts, api/audit.ts, api/notification.ts, api/decision.ts,
  api/tenant.ts, api/metrics.ts, api/exports.ts, api/exports-server.ts
```
