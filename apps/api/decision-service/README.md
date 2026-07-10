<!-- Copyright (c) 2026 Yasvanth Udayakumar. -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# decision-service

F036 — Routing-decision ledger and read API.

The decision-service stores routing decisions emitted by a registered
`routing.Decider` implementation and exposes them through a read-only
HTTP API for the admin console (F032). It is a **render contract**:
the OSS layer owns the schema, storage, and HTTP surface; whatever
factor names, values, thresholds, and weight hints the decider chose to
emit are stored verbatim and forwarded to the console unchanged.

## What it does NOT do

This service does **not** decide anything. It does not score candidates,
rank alternatives, pick winners, or implement fallback chains. That logic is outside this service. In OSS the registered decider is the
no-op decider in `packages/extensions/go/routing`, which emits a single
trivial reason step.

## What it does

- Consumes `routing.decision.v1` events from the bus (producer: the
  gateway/SDKs after the registered `routing.Decider` produces a
  decision).
- Persists each event to `routing.routing_decisions` with idempotency on
  `decision_id` (`ON CONFLICT DO NOTHING`).
- Serves a tenant-scoped read API.

The service never stores prompt text, completion text, or raw request
headers. `request.request_id_hash` is SHA-256 of any inbound
`X-Request-ID` (set by the producer), or empty.

## HTTP API

| Path                          | Method | Purpose                              |
| ----------------------------- | ------ | ------------------------------------ |
| `/v1/decisions`               | GET    | Paginated tenant-scoped query        |
| `/v1/decisions/{decision_id}` | GET    | Single decision (full payload)       |
| `/v1/decisions/stats`         | GET    | Bucketed counts by `provider_chosen` |
| `/metrics`                    | GET    | Prometheus self-metrics              |
| `/healthz`, `/readyz`         | GET    | Liveness / readiness                 |

Query parameters for `/v1/decisions`:

- `tenant` (required): tenant UUID
- `app`: app label filter
- `from`, `to`: RFC3339 timestamps
- `limit`: page size (capped by `server.max_page_size`)
- `cursor`: smallest `id` from the previous page (results come back
  newest-first)

The list response is `{decisions: [...], next: <id-cursor>}`. Pass `next`
back in as `?cursor=` to fetch the next, older page.

`/v1/decisions/stats` returns `{tenant_id, total, by_chosen: [...],
window_from, window_to}`. The OSS stats endpoint groups by
`(provider_chosen, model_chosen)` only — it does **not** derive error
rates or override counts from `reason_chain` content because those
fields are decider-defined and OSS treats them as opaque.

## Render contract

`reason_chain` and `alternatives` are persisted as `JSONB` and returned
as raw JSON. The schema at
`packages/contracts/routing/v1/decision-event.schema.json` requires each
`reason_chain` step to carry `step`, `factor`, and `value`, but the
**semantics** of those fields belong to the decider. The OSS layer does
not interpret `factor` names, does not compare `weight_hint` numbers,
and does not rank alternatives by `score_hint`. The admin console
renders whatever the decider emitted; operators upgrading from the OSS
no-op decider to a registered decider will see richer trees without
any change to this service.

## Postgres schema

See `platform/db/control_plane/migrations/2026051807_f036_routing_decisions.sql`. Highlights:

- `routing.routing_decisions` with `id BIGSERIAL`, `decision_id UUID`
  (unique), `tenant_id UUID`, `reason_chain JSONB`, `alternatives JSONB`,
  `decided_at TIMESTAMPTZ`, `ingested_at TIMESTAMPTZ DEFAULT NOW()`.
- Unique index on `decision_id` for idempotent ingest.
- Indexes on `(tenant_id, decided_at DESC)` and
  `(tenant_id, app, decided_at DESC)` for the list endpoints.

Append-only is enforced **at the API level** — the store exposes no
UPDATE or DELETE path, and bus replay is safe because of
`ON CONFLICT DO NOTHING`. (The audit-service, F031, is the
tamper-evident hash-chained surface; this service is the renderable
operational record.)

## Bus contract

The `routing.decision.v1` payload shape is described by
`packages/contracts/routing/v1/decision-event.schema.json`. Producers
MUST:

- Set the `x-tenant-id` and `x-event-id` headers (per
  `packages/bus-client/go`).
- Use a stable `id` (UUIDv7 recommended) — it is the idempotency key.
- Redact secrets and never include prompt/completion text — the schema
  rejects raw headers and the storage column shape has no place to put
  them.

Example payloads:

- `packages/contracts/routing/v1/examples/noop-decision.json` — what the
  OSS no-op decider emits.
- `packages/contracts/routing/v1/examples/fallback-decision.json` — a
  **fabricated** illustration of how rich a payload can be. The factor
  names, values, and counter-factuals are placeholders. Real custom
  output is not shown.

## Configuration

Minimal example:

```yaml
server:
  port: 8094
  max_page_size: 500
  default_page_size: 50
database:
  dsn_env: OPENLLM_DECISION_DSN
bus:
  brokers:
    - localhost:19092
  client_id: openllm-decision-service
  group: openllm-decision-service
  topic: routing.decision.v1
```

The Postgres DSN is read from the env var named in `database.dsn_env`.

The compose stack mounts
`platform/deployment/compose/configs/decision-service.yaml` at
`/etc/openllm-decision-service/config.yaml` (compose sets
`OPENLLM_DECISION_DSN` and publishes the service on host port `8093`).

## Self-metrics

| Series                                  | Type    | Description                                          |
| --------------------------------------- | ------- | ---------------------------------------------------- |
| `llm_decision_appends_total`            | counter | Decision rows appended.                              |
| `llm_decision_append_failures_total`    | counter | Append attempts that errored.                        |
| `llm_decision_rejects_validation_total` | counter | Events dropped for decode or required-field failure. |
| `llm_decision_last_append_timestamp`    | gauge   | Unix seconds of the most recent successful append.   |
| `llm_decision_query_requests_total`     | counter | Calls served by `/v1/decisions`.                     |
| `llm_decision_stats_requests_total`     | counter | Calls served by `/v1/decisions/stats`.               |

## Status & remaining work

Shipped: containerized ([Dockerfile](./Dockerfile)), wired into
[docker-compose.yml](../../../docker-compose.yml) and `go.work`, built by
CI, with an [OpenAPI spec](./openapi.yaml).

Still pending:

- Tests (unit, integration, smoke).
- Wire the decision-service into the OTel Collector pipeline so
  append-failure rate becomes alertable.
- Consider a per-tenant retention sweeper (default 90 days, configurable
  for compliance tenants) — see the feature README §9.
