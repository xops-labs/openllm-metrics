# policy-service (F029)

OSS-safe HTTP service that owns the **policy document schema, storage, and
versioning** for OpenLLM Metrics. It exposes CRUD over policy documents,
records an append-only version history, and emits an `audit.event.v1` to the
streaming bus on every mutation.

> **open-source scope.** This service intentionally does **not** evaluate
> policies, compute budget burn, order rules by precedence, or make
> enforcement decisions. Those behaviors are owned by **F030** in the
> this repository.

## Endpoints

| Method | Path                             | Purpose                                |
| ------ | -------------------------------- | -------------------------------------- |
| POST   | `/v1/policies`                   | Create a policy (version 1)            |
| GET    | `/v1/policies/{id}`              | Get current version of a policy        |
| PUT    | `/v1/policies/{id}`              | Append a new version                   |
| DELETE | `/v1/policies/{id}`              | Soft-delete the policy                 |
| GET    | `/v1/policies/{id}/versions`     | List version history (newest first)    |
| GET    | `/v1/policies/{id}/versions/{n}` | Get a specific version                 |
| POST   | `/v1/policies/{id}/validate`     | Schema validation only (no evaluation) |
| GET    | `/healthz`                       | Liveness                               |
| GET    | `/readyz`                        | Readiness                              |

All `/v1/*` requests require an `X-Tenant-ID` header (UUID). Mutations also
read `X-Actor` and record it on the audit event.

## Storage model

Three tables in the `control_plane` schema (migration
`platform/db/control_plane/migrations/2026051803_f029_policy_schema.sql`):

- `policies` — one row per logical policy (`id`, `tenant_id`, `name`,
  `current_version`, `deleted_at` for soft delete).
- `policy_versions` — append-only history. One row per write; the full policy
  document is stored as `JSONB`. Unique on `(policy_id, version)`.
- `policy_validation_errors` — append-only structural validation findings tied
  to a specific `policy_version` row. Schema-level findings only.

All three tables are protected by row-level security keyed on
`current_setting('app.tenant_id')`, following the F005 baseline pattern.

## JSON Schema

The canonical policy contract lives at
`packages/contracts/policy/v1/policy.schema.json`. Example documents:

- `packages/contracts/policy/v1/examples/budget.json`
- `packages/contracts/policy/v1/examples/allow_model.json`
- `packages/contracts/policy/v1/examples/rate_limit.json`

The schema specifies the **shape** of a policy (identity, scope, rules,
parameters). It does **not** specify rule ordering, precedence, or
enforcement semantics — those live with the F030 evaluator.

## Audit events

Every mutation produces an `audit.event.v1` message on the `audit.event.v1`
topic. The envelope carries `event_id`, `tenant_id`, `actor`, `action`
(`policy.created`, `policy.version_appended`, `policy.soft_deleted`), and
`resource` (`policy:<uuid>`). F031 owns durable storage of the audit ledger.

## Configuration

```yaml
server:
  port: 8090
db:
  dsn: postgres://policy:policy@localhost:5432/openllm
  max_open_conns: 10
bus:
  enabled: true
  brokers: ['redpanda:9092']
  client_id: openllm-policy-service
  audit_topic: audit.event.v1
schema:
  path: packages/contracts/policy/v1/policy.schema.json
```

The compose stack mounts
`platform/deployment/compose/configs/policy-service.yaml` at
`/etc/openllm-policy-service/config.yaml` and the policy JSON Schema at
`/etc/openllm-policy-service/schema/`. There the DSN is read from the env
var named by `db.dsn_env` (compose sets `OPENLLM_POLICY_DSN`) — never log it.

## Status & remaining work

Shipped: the service is containerized ([Dockerfile](./Dockerfile)), wired
into [docker-compose.yml](../../../docker-compose.yml) and `go.work`, built
by CI, has an [OpenAPI spec](./openapi.yaml), and applies its goose
migration via `tools/scripts/migrate.sh apply control_plane`
(`platform/db/control_plane/migrations/2026051803_f029_policy_schema.sql`).

Still pending:

- Unit / integration / smoke tests.
- OTel metrics export (the service currently exposes counters via
  `/debug/counters` only).
- Replace the `X-Tenant-ID` / `X-Actor` header shim with full JWT-based
  identity (F005).
