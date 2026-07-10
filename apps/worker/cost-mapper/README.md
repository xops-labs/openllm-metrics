# cost-mapper

The provider-neutral cost mapping worker (F017) — prices runtime token
counts into estimated cost. Runtime-side companion to the FOCUS ingester.

Subscribes to two bus topics:

- **`llm.runtime.normalized`** — runtime events from the gateway / SDK
  (`source = gateway | sdk`). For each event the worker joins
  `input_tokens` and `output_tokens` against a per-provider pricing
  catalog (`platform/pricing/<provider>.yaml`) and publishes a
  `cost.estimated.v1` event to **`llm.cost.estimated`** with the full
  canonical tenant/team/app/env/project context preserved.

- **`llm.usage.reconciled`** — FOCUS-derived billing events from the
  focus-ingester. For each event the worker joins the accumulated runtime
  estimate for the same `(tenant, provider, model, period_start, period_end)`
  bucket and upserts a row into
  `control_plane.cost_reconciliation_drift`.

Idempotent by construction: the emitted `event_id` is a SHA-256 over the
input `event_id` plus the catalog version, so a replay of the same input
against the same catalog produces a byte-identical output event. The
drift table's `UNIQUE (tenant_id, provider, model, period_start, period_end)`
plus `ON CONFLICT DO UPDATE` keeps drift writes idempotent on replay as
well.

## Surface

- **Inputs.** Bus topics `llm.runtime.normalized`, `llm.usage.reconciled`.
- **Outputs.** Bus topic `llm.cost.estimated`; Postgres table
  `control_plane.cost_reconciliation_drift`.
- **Catalog.** Reads `platform/pricing/*.yaml`. Reload cadence is
  configurable (default 5 min) so price-update PRs land without a restart.

## Configuration

The compose stack mounts
`platform/deployment/compose/configs/cost-mapper.yaml` at
`/etc/openllm-cost-mapper/config.yaml` and the pricing catalog at
`/etc/openllm-cost-mapper/pricing/`. The Postgres DSN is read from the env
var named by `database.dsn_env` (compose sets `OPENLLM_CONTROL_PLANE_DSN`) —
never log it.

| YAML path                          | Value (compose)                     | Notes                                 |
| ---------------------------------- | ----------------------------------- | ------------------------------------- |
| `server.port`                      | `8083`                              | Metrics + healthz HTTP port.          |
| `catalog.dir`                      | `/etc/openllm-cost-mapper/pricing`  | Pricing catalog directory.            |
| `catalog.reload_interval_seconds`  | `300`                               | Catalog reload cadence.               |
| `bus.brokers`                      | `[redpanda:9092]`                   | Kafka/Redpanda broker list.           |
| `bus.runtime_topic`                | `llm.runtime.normalized`            | Runtime-side input topic.             |
| `bus.reconciled_topic`             | `llm.usage.reconciled`              | Vendor-side input topic.              |
| `bus.estimated_topic`              | `llm.cost.estimated`                | Estimate output topic.                |
| `database.dsn_env`                 | `OPENLLM_CONTROL_PLANE_DSN`         | Env var holding the Postgres DSN.     |
| `defaults.tenant` / `defaults.env` | `quickstart-tenant` / `development` | Fallback labels for unlabeled events. |

## Pricing catalog

Each `platform/pricing/<provider>.yaml` file lists publicly-quoted list
prices in USD per 1,000 tokens (input / output split). All catalog files
in this repo are marked `approximate: true` — they do **not** reflect
committed-use discounts, batch tiers, cached-input pricing, PTU /
reserved capacity, or region surcharges. Operators reconcile the truth
via `control_plane.cost_reconciliation_drift`; the catalog is the
expected-cost prior, not ground truth.

Price changes land via PR (CODEOWNERS-protected).
Backward-compatible additions only; price _changes_ should add a new
entry with a new `effective_from` rather than mutating an existing rate
in place once the time-bounded lookup lands.

## Drift math

OSS scope only. Anything more elaborate than the math below lives behind
the F025 cost-efficiency scoring boundary in this repository.

```
estimated_cost_usd = (input_tokens / 1000)  × input_rate_per_1k
                   + (output_tokens / 1000) × output_rate_per_1k

drift_minor_units  = reconciled_cost_minor - estimated_cost_minor
drift_ratio        = drift_minor_units / max(reconciled_cost_minor, 1)
```

`drift_ratio > 0` means the vendor billed more than the runtime estimate
predicted (catalog stale, missing discount, or untracked add-on).
`drift_ratio < 0` means the runtime estimate over-predicted (rare —
usually a token-counting drift on the runtime side).

## Self-observability

| Series                                             | Type    | Description                         |
| -------------------------------------------------- | ------- | ----------------------------------- |
| `llm_cost_mapper_runtime_events_consumed_total`    | counter | Runtime events consumed.            |
| `llm_cost_mapper_estimates_emitted_total`          | counter | `cost.estimated` events published.  |
| `llm_cost_mapper_estimates_skipped_total`          | counter | Skipped (no tokens, wrong source).  |
| `llm_cost_mapper_unpriced_events_total`            | counter | `(provider, model)` not in catalog. |
| `llm_cost_mapper_reconciled_events_consumed_total` | counter | Reconciled events consumed.         |
| `llm_cost_mapper_drift_rows_total`                 | counter | Drift rows upserted.                |
| `llm_cost_mapper_drift_errors_total`               | counter | Failed drift upserts.               |
| `llm_cost_mapper_last_success_timestamp`           | gauge   | Unix seconds of last emit.          |

## Hard constraints

- Cost mapping is `tokens × rate` only — no scoring weights, no routing
  ranks, no policy thresholds. Anything richer is not implemented here.
- Multi-tenant from day one. Every emitted event and every drift row
  carries `{tenant, team, app, env, project, provider, model}`.
- Never log prompts, completions, or provider API keys.
- Idempotent: same input event + same catalog version → same output
  event_id.

## TODOs left for follow-up wiring

- The new `llm.cost.estimated` topic name is defined locally in
  `internal/busproducer`. When the F008 contract module adopts it,
  promote the constant to `packages/contracts/telemetry/go/schemas.go`
  alongside `TopicUsageReconciled` and add a JSON Schema under
  `packages/contracts/telemetry/go/schemas/`.
- The migration lives in the `platform/db/control_plane/migrations/`
  series (`2026051801_f017_cost_reconciliation_drift.sql`) and is applied
  by `tools/scripts/migrate.sh apply control_plane`.
- The dashboards pack should add a runtime-drift panel reading
  `control_plane.cost_reconciliation_drift`.
- The worker is containerized ([Dockerfile](./Dockerfile)), wired into
  [docker-compose.yml](../../../docker-compose.yml) and `go.work`, and built
  by CI. Unit tests are still pending.
