# focus-ingester

Pull-mode billing ingestion worker (part of the optional `--profile exporter`
chain). Polls the upstream
[`llm-usage-exporter`](https://github.com/xops-labs/llm-usage-exporter)
`/focus.json` endpoint, persists every FOCUS line item to
`control_plane.focus_records` (append-only), and publishes a canonical
`llm.usage.reconciled` event for each row to the streaming bus.

The reconciled events join against the runtime cost estimates produced by
the gateway/SDK pipeline (see the F023 reconciler) on
`(tenant, provider, model, period_start, period_end)`. The drift dashboards
surface the gap between the platform's runtime estimate and the vendor's
billed amount.

## Surface

- **Input.** HTTP GET against the upstream `/focus.json` endpoint on a
  configurable cadence (default 1 hour). Accepts both `{"records": [...]}`
  envelope and bare-array shapes. Body cap 32 MiB.
- **Lookup.** `control_plane.label_mappings` resolved by
  `(provider, tenant_external_id, tenancy_id)` with TTL cache.
- **Persistence.** Append-only insert into `control_plane.focus_records`.
  `source_event_id` (derived from
  `billing_account_id + period_start + period_end + service_name`) is the
  read-side last-write-wins key.
- **Output.** One `llm.usage.reconciled` event per persisted record on the
  `llm.usage.reconciled` bus topic.

## Configuration

The compose stack mounts
`platform/deployment/compose/configs/focus-ingester.yaml` at
`/etc/openllm-focus-ingester/config.yaml`. The Postgres DSN is read from the
env var named by `database.dsn_env` (compose sets
`OPENLLM_CONTROL_PLANE_DSN`) — never log it.

| YAML path                            | Value (compose)                             | Notes                                      |
| ------------------------------------ | ------------------------------------------- | ------------------------------------------ |
| `server.port`                        | `8082`                                      | Metrics + healthz HTTP port.               |
| `focus.url`                          | `http://llm-usage-exporter:9090/focus.json` | Upstream FOCUS snapshot endpoint.          |
| `focus.poll_interval_seconds`        | `300`                                       | Poll cadence (upstream refreshes ~hourly). |
| `focus.poll_timeout_seconds`         | `30`                                        | Per-poll HTTP timeout.                     |
| `database.dsn_env`                   | `OPENLLM_CONTROL_PLANE_DSN`                 | Env var holding the Postgres DSN.          |
| `database.mapping_cache_ttl_seconds` | `300`                                       | `label_mappings` lookup cache TTL.         |
| `bus.brokers`                        | `[redpanda:9092]`                           | Kafka/Redpanda broker list.                |

## Self-observability

| Series                                       | Type    | Description                                 |
| -------------------------------------------- | ------- | ------------------------------------------- |
| `llm_focus_ingester_poll_success_total`      | counter | Successful upstream polls.                  |
| `llm_focus_ingester_poll_failure_total`      | counter | Failed upstream polls.                      |
| `llm_focus_ingester_last_success_timestamp`  | gauge   | Unix seconds of last successful poll.       |
| `llm_focus_ingester_records_fetched_total`   | counter | Records fetched from upstream.              |
| `llm_focus_ingester_records_persisted_total` | counter | Records written to `focus_records`.         |
| `llm_focus_ingester_records_emitted_total`   | counter | Reconciled events on the bus.               |
| `llm_focus_ingester_records_unmapped_total`  | counter | Records with no `label_mappings` row.       |
| `llm_focus_ingester_records_dropped_total`   | counter | Records dropped (DB error, missing tenant). |

## Hard constraints

- Never modifies, vendors, or forks the upstream `llm-usage-exporter`;
  integration is composition-only (container image + scrape/FOCUS consumption).
- `control_plane.focus_records` is **append-only**: never UPDATE or DELETE.
- Records without a resolvable tenant are dropped (the NOT NULL on
  `tenant_id` would reject them anyway).
