# audit-service

F031 — Append-only, hash-chained audit ledger.

The audit-service subscribes to `audit.event.v1` on the streaming bus, appends
every event to `audit.audit_entries` as a tamper-evident sha256 chain, and
exposes a read API for filtered queries, JSONL export, and chain verification.
A companion CLI (`olm-audit`, under `cmd/olm-audit/`) re-runs the verification
offline against any Postgres connection.

## What it does

- Consumes `audit.event.v1` events from the bus (producers: policy-service
  F029, gateway F018, admin console F032, label-mapping CRUD F017).
- Redacts forbidden fields (`authorization`, `api_key`, `password`, `token`,
  `bearer`, `prompt`, `completion`) defense-in-depth before insert.
- Computes `entry_hash = sha256(canonical_json({tenant_id, id, actor, action,
resource, payload, prev_hash, created_at}))` per row. The chain is
  per-tenant; `prev_hash` of the first row in a tenant is 32 zero bytes.
- Persists rows into an append-only table — UPDATE and DELETE are rejected
  at the database level by rules + triggers, not just by app code.
- Serves a tenant-scoped read API.

## What it does NOT do

- Does NOT decide what is auditable — that is the producer's responsibility.
- Does NOT enforce auth on the read API at this phase. Deploy behind a
  sidecar / ingress that validates the caller's JWT against `?tenant=`.
- Does NOT archive to object storage. WORM-bucket retention is a future
  enhancement.

## HTTP API

| Path                     | Method | Purpose                        |
| ------------------------ | ------ | ------------------------------ |
| `/v1/audit/entries`      | GET    | Paginated tenant-scoped query  |
| `/v1/audit/entries/{id}` | GET    | Single entry                   |
| `/v1/audit/export`       | GET    | Streaming JSONL bulk export    |
| `/v1/audit/verify`       | GET    | Server-side chain verification |
| `/metrics`               | GET    | Prometheus self-metrics        |
| `/healthz`, `/readyz`    | GET    | Liveness / readiness           |

Query parameters for `/v1/audit/entries`:

- `tenant` (required): tenant UUID
- `action`: exact-match filter (e.g. `policy.update`)
- `actor_id`: actor.id filter
- `from`, `to`: RFC3339 timestamps
- `limit`: page size (capped by `server.max_page_size`)
- `cursor`: largest `id` from the previous page

The list response is `{entries: [...], next: <id-cursor>}`. Pass `next` back
in as `?cursor=` to page forward.

## Schema

See `platform/db/audit/migrations/2026051804_f031_audit_ledger.sql`. Highlights:

- `audit.audit_entries` with `id BIGSERIAL`, `tenant_id UUID`, `prev_hash`
  and `entry_hash` as `BYTEA(32)`.
- Rules `audit_entries_no_update` / `audit_entries_no_delete` rewrite any
  UPDATE / DELETE into a no-op; a paired trigger also raises with
  `ERRCODE insufficient_privilege` so the failure is loud.
- Row-level security with the `app.tenant_id` session key (same convention
  as the F005 control-plane tables).

## Bus contract

The `audit.event.v1` payload shape is described by
`packages/contracts/audit/v1/audit-event.schema.json`. Producers MUST:

- Set the `x-tenant-id` and `x-event-id` headers (per `packages/bus-client/go`).
- Use a stable `event_id` (UUIDv7 recommended).
- Redact secrets before publishing — the audit-service redacts again, but
  defense-in-depth is no substitute for producer-side hygiene.

## CLI verifier

The `olm-audit` CLI (under `cmd/olm-audit/`) re-runs the chain check against
a Postgres DSN:

```bash
olm-audit verify \
    --tenant 11111111-2222-3333-4444-555555555555 \
    --db "postgres://user:pass@localhost:5432/openllm?sslmode=disable"
```

On a clean chain the CLI prints:

```
OK  tenant=...  checked=12345  last_id=...
```

On a break:

```
BREAK at id=42
  expected prev_hash = <base64>
  actual   prev_hash = <base64>
  reason   = prev_hash does not match prior entry_hash
```

The CLI also exports:

```bash
olm-audit export \
    --tenant 11111111-2222-3333-4444-555555555555 \
    --from 2026-05-01T00:00:00Z \
    --to   2026-05-31T23:59:59Z \
    --out  acme-may-audit.jsonl \
    --db   "$OPENLLM_AUDIT_DSN"
```

## Configuration

Minimal example:

```yaml
server:
  port: 8090
  max_page_size: 500
  default_page_size: 50
database:
  dsn_env: OPENLLM_AUDIT_DSN
bus:
  brokers:
    - localhost:19092
  client_id: openllm-audit-service
  group: openllm-audit-service
  topic: audit.event.v1
```

The Postgres DSN is read from the env var named in `database.dsn_env`.

The compose stack mounts
`platform/deployment/compose/configs/audit-service.yaml` at
`/etc/openllm-audit-service/config.yaml` (compose sets `OPENLLM_AUDIT_DSN`
and publishes the service on host port `8091`).

## Self-metrics

| Series                               | Type    | Description                                        |
| ------------------------------------ | ------- | -------------------------------------------------- |
| `llm_audit_appends_total`            | counter | Audit entries appended.                            |
| `llm_audit_append_failures_total`    | counter | Append attempts that errored.                      |
| `llm_audit_rejects_redaction_total`  | counter | Events dropped: forbidden field survived redact.   |
| `llm_audit_rejects_validation_total` | counter | Events dropped: missing required fields.           |
| `llm_audit_last_append_timestamp`    | gauge   | Unix seconds of the most recent successful append. |
| `llm_audit_verify_rows_total`        | counter | Rows scanned by the chain verifier.                |
| `llm_audit_verify_breaks_total`      | counter | Chain breaks detected by the verifier.             |
| `llm_audit_export_rows_total`        | counter | Rows streamed by `/v1/audit/export`.               |
| `llm_audit_query_requests_total`     | counter | Queries served by `/v1/audit/entries`.             |

## Status & remaining work

Shipped: containerized ([Dockerfile](./Dockerfile)), wired into
[docker-compose.yml](../../../docker-compose.yml) and `go.work`, built by
CI, with an [OpenAPI spec](./openapi.yaml).

Still pending:

- Unit / integration / smoke tests.
- Add a `(tenant_id, source_event_id)` UNIQUE index for stricter producer
  idempotency once the audit.event.v1 schema includes `source_event_id`.
- Wire the audit-service into the OTel Collector pipeline so verify failures
  become alertable.
