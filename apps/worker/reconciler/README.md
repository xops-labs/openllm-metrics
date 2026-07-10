# reconciler

The Pull-Mode / Proxy-Mode Reconciliation Framework worker (F023).

Subscribes to two bus topics, joins them in a windowed correlation, computes
drift between the runtime cost estimate and the vendor-billed amount, and
exposes the drift as Prometheus series plus a bus event.

- **`llm.cost.estimated`** — runtime-side cost estimates produced by
  `apps/worker/cost-mapper` from gateway/SDK token counts and the pricing
  catalog (`source = gateway | sdk`).
- **`llm.usage.reconciled`** — vendor-reconciled cost produced by
  `apps/worker/focus-ingester` from the upstream `llm-usage-exporter`
  `/focus.json` endpoint (`source = exporter`).

The reconciler is the join point — and only the join point. It never makes
routing, fallback, scoring, or budget decisions based on the drift. The
drift number is the signal; what to do with it lives in F033 (OSS
notifications) or F034 / F035 decisioning.

## Drift detection methodology

The reconciler buckets every contributing event by
`(tenant, provider, model, window_start)` where `window_start` is the input
event's recorded time truncated to the configured window size (default 1
hour). Each contribution updates the running `(estimated_cost_usd,
reconciled_cost_usd)` totals for the bucket and upserts the row to
`control_plane.reconciliation_results` so the state survives a restart.
A bucket stays `open` while the in-window cycle is live and through the
reconciliation grace period (default 48 hours — providers can lag
substantially on billing finalization). The closer scans Postgres on a slow
cadence; once `window_end + grace_period <= now`, it flips the row to
`reconciled` (both sides contributed non-zero cost) or `unreconciled` (only
one side did), emits a `reconciliation.window.v1` event, refreshes the
per-tuple gauges, and forgets the in-memory bucket. Drift math is
deliberately trivial: `drift_usd = reconciled_cost_usd - estimated_cost_usd`
and `drift_ratio = drift_usd / max(estimated_cost_usd, 0.0001)`. A positive
ratio means the vendor billed more than the runtime estimate predicted
(stale catalog, missing discount, untracked add-on); a negative ratio
typically means a runtime-side token-counting drift.

## Surface

- **Inputs.** Bus topics `llm.cost.estimated`, `llm.usage.reconciled`.
- **Outputs.** Bus topic `llm.reconciliation.window`; Postgres table
  `control_plane.reconciliation_results`.

## Configuration

The compose stack mounts
`platform/deployment/compose/configs/reconciler.yaml` at
`/etc/openllm-reconciler/config.yaml`. The Postgres DSN is read from the env
var named by `database.dsn_env` (compose sets `OPENLLM_CONTROL_PLANE_DSN`) —
never log it.

| YAML path                      | Default                     | Notes                                                                        |
| ------------------------------ | --------------------------- | ---------------------------------------------------------------------------- |
| `server.port`                  | `8084`                      | Metrics + healthz HTTP port.                                                 |
| `window.size_seconds`          | `3600`                      | Correlation window size.                                                     |
| `window.grace_seconds`         | `172800` (48h)              | How long a window stays `open` after `window_end` waiting for the late side. |
| `closer.scan_interval_seconds` | `300`                       | Cadence of the closer's Postgres scan.                                       |
| `bus.estimated_topic`          | `llm.cost.estimated`        | Runtime-side input topic.                                                    |
| `bus.reconciled_topic`         | `llm.usage.reconciled`      | Vendor-side input topic.                                                     |
| `bus.window_topic`             | `llm.reconciliation.window` | Window-close output topic.                                                   |
| `database.dsn_env`             | `OPENLLM_CONTROL_PLANE_DSN` | Env var holding the Postgres DSN.                                            |

## Prometheus series

F023 surface (per-tuple gauges and counter — refreshed at window close):

| Series                                   | Type    | Labels                                       |
| ---------------------------------------- | ------- | -------------------------------------------- |
| `llm_reconciliation_estimated_cost_usd`  | gauge   | `tenant, team, app, provider, model, window` |
| `llm_reconciliation_reconciled_cost_usd` | gauge   | `tenant, team, app, provider, model, window` |
| `llm_reconciliation_drift_usd`           | gauge   | `tenant, team, app, provider, model, window` |
| `llm_reconciliation_drift_ratio`         | gauge   | `tenant, team, app, provider, model, window` |
| `llm_reconciliation_window_closed_total` | counter | `tenant, team, app, provider, model, window` |

Worker-level self-observability (worker `tenant, env` label only):

| Series                                            | Type    | Description                        |
| ------------------------------------------------- | ------- | ---------------------------------- |
| `llm_reconciler_estimated_events_consumed_total`  | counter | `cost.estimated` events consumed.  |
| `llm_reconciler_reconciled_events_consumed_total` | counter | `llm.usage.reconciled` consumed.   |
| `llm_reconciler_estimated_events_dropped_total`   | counter | Estimated events dropped.          |
| `llm_reconciler_reconciled_events_dropped_total`  | counter | Reconciled events dropped.         |
| `llm_reconciler_bad_payload_total`                | counter | Decode failures.                   |
| `llm_reconciler_last_close_timestamp`             | gauge   | Unix seconds of last window close. |

## Hard constraints

- Multi-tenant from day one. Every emitted event and every row carries
  `{tenant, team, app, env, project, provider, model}`.
- Idempotent. Same window + same inputs → same row (upsert on the
  `(tenant_id, provider, model, window_start)` unique key).
- Never logs prompts, completions, or provider API keys.
- Does **not** decide routing, fallback, scoring, or budget enforcement on
  the drift. That is F033 / F034 / F035.

## TODOs left for follow-up wiring

- The new `llm.reconciliation.window` topic name is defined locally in
  `internal/busproducer`. When the F008 contract module adopts it, promote
  the constant to `packages/contracts/telemetry/go/schemas.go` alongside
  `TopicUsageReconciled` and add a JSON Schema under
  `packages/contracts/telemetry/go/schemas/` for the `reconciliation.window.v1`
  envelope.
- The migration lives in the `platform/db/control_plane/migrations/`
  series (`2026051806_f023_reconciliation.sql`) and is applied by
  `tools/scripts/migrate.sh apply control_plane`.
- The F033 notifications worker needs a rule shape for "drift_ratio above
  N for K consecutive windows" — wired when F033 evaluator extensions
  land.
- The dashboards pack (F027) should add a reconciliation-drift panel
  reading the `llm_reconciliation_*` series.
- The worker is containerized ([Dockerfile](./Dockerfile)), wired into
  [docker-compose.yml](../../../docker-compose.yml) and `go.work`, and built
  by CI. Unit tests are still pending.
