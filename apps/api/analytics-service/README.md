# analytics-service (F038 — Native Console Analytics)

OSS-safe HTTP service that owns the **storage and CRUD surface for per-tenant
"saved analytics views"** in OpenLLM Metrics. A saved view is a declarative
`llm_*` selector spec (`metric`, `groupBy`, `filters`, `wrap`, `windowSeconds`,
`viz`) that the admin console's dashboards screen renders into a card. This
service is the optional backend the console talks to (`OLM_ANALYTICS_SERVICE_URL`)
to persist user-defined views on top of the four built-in defaults.

> **open-source scope.** This service intentionally does **not** execute
> analytics queries, score series, route requests, or apply anomaly rules. It
> persists and returns the opaque view spec verbatim. The spec carries only the
> normalized `llm_*` selector shape — never prompt or completion text.

## Endpoints

| Method | Path                   | Purpose                         |
| ------ | ---------------------- | ------------------------------- |
| GET    | `/v1/saved-views`      | List saved views for the tenant |
| POST   | `/v1/saved-views`      | Create a saved view             |
| DELETE | `/v1/saved-views/{id}` | Soft-delete a saved view        |
| GET    | `/healthz`             | Liveness                        |
| GET    | `/readyz`              | Readiness                       |

`GET /v1/saved-views` returns `{ "views": [SavedView, ...] }`. `POST` accepts
`{ name, description?, spec, position? }` and returns the persisted `SavedView`
(with its generated `id`). `DELETE` returns `204 No Content`. These paths and
shapes are fixed by the admin console client
(`apps/web/admin-console/lib/api/saved-views.ts`) and must not drift.

All `/v1/*` requests require an `X-Tenant-ID` header (UUID); a missing/invalid
header is rejected with `400`. The console also sends `X-Actor` / `X-OLM-User`,
which this service ignores.

## Storage model

One table in the `control_plane` schema (migration
`platform/db/control_plane/migrations/2026062501_f038_analytics_saved_views.sql`):

- `analytics_saved_views` — one row per saved view (`id`, `tenant_id`, `name`,
  `spec` JSONB, `description`, `position`, `deleted_at` for soft delete).
  `UNIQUE (tenant_id, name)` keeps names stable per tenant (duplicate create →
  `409`). Reads filter `deleted_at IS NULL`.

The table has row-level security **enabled and forced**, keyed on
`current_setting('app.tenant_id', true)`. Every read and write runs inside a
transaction that first executes `SELECT set_config('app.tenant_id', <tenant>,
true)`, so the `analytics_saved_views_tenant_isolation` policy permits exactly
the caller's rows — a tenant can never read or delete another tenant's views.

## Configuration

```yaml
server:
  port: 8095
db:
  dsn_env: OPENLLM_ANALYTICS_DSN
  max_open_conns: 10
```

The DSN is read from the env var named by `db.dsn_env` so the connection string
stays out of the committed config file. A literal `db.dsn` is also supported.

The compose stack mounts
`platform/deployment/compose/configs/analytics-service.yaml` at
`/etc/openllm-analytics-service/config.yaml` (compose sets
`OPENLLM_ANALYTICS_DSN` and publishes the service on host port `8096`).
