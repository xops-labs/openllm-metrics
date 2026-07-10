# label-translator

Pull-mode label-mapping worker (part of the optional `--profile exporter`
chain). Scrapes the upstream
[`llm-usage-exporter`](https://github.com/xops-labs/llm-usage-exporter)
`/metrics` endpoint, enriches each sample with the canonical
`{tenant, team, app, env, project}` tuple from
`control_plane.label_mappings`, and publishes the result to the streaming
bus as `llm.usage.normalized` events with `source=exporter`.

## Surface

- **Input.** HTTP GET against the upstream exporter's `/metrics` endpoint
  on a configurable interval (default 60s). Parses the Prometheus
  exposition format and consumes `llm_input_tokens_total`,
  `llm_output_tokens_total`, `llm_cost_usd_total`, `llm_requests_total`.
- **Lookup.** `control_plane.label_mappings` resolved by
  `(provider, tenant_external_id, tenancy_id)` with a 5-minute in-process
  TTL cache. Cache misses also cache negatively so a sustained unmapped
  stream does not hammer Postgres.
- **Output.** One canonical `llm.usage.normalized` event per
  `(provider, tenant_external_id, tenancy_id, model)` per scrape window,
  published to the streaming bus.
- **Fallback.** When no mapping row exists, the event is still emitted
  using the configured default `{tenant, team, env}` labels and the
  `llm_label_translation_unmapped_total` counter is bumped. Events
  without a resolvable tenant are dropped.

## Configuration

The compose stack mounts
`platform/deployment/compose/configs/label-translator.yaml` at
`/etc/openllm-label-translator/config.yaml`. The Postgres DSN is read from
the env var named by `database.dsn_env` (compose sets
`OPENLLM_CONTROL_PLANE_DSN`) — never log it.

| YAML path                                            | Value (compose)                                    | Notes                                 |
| ---------------------------------------------------- | -------------------------------------------------- | ------------------------------------- |
| `server.port`                                        | `8081`                                             | Metrics + healthz HTTP port.          |
| `exporter.url`                                       | `http://llm-usage-exporter:9090/metrics`           | Upstream exporter scrape endpoint.    |
| `exporter.scrape_interval_seconds`                   | `60`                                               | Scrape cadence.                       |
| `exporter.scrape_timeout_seconds`                    | `15`                                               | Per-scrape HTTP timeout.              |
| `database.dsn_env`                                   | `OPENLLM_CONTROL_PLANE_DSN`                        | Env var holding the Postgres DSN.     |
| `database.mapping_cache_ttl_seconds`                 | `300`                                              | `label_mappings` lookup cache TTL.    |
| `bus.brokers`                                        | `[redpanda:9092]`                                  | Kafka/Redpanda broker list.           |
| `defaults.tenant` / `defaults.team` / `defaults.env` | `quickstart-tenant` / `quickstart` / `development` | Fallback labels for unmapped samples. |

## Self-observability

| Series                                           | Type    | Description                                               |
| ------------------------------------------------ | ------- | --------------------------------------------------------- |
| `llm_label_translator_scrape_success_total`      | counter | Successful upstream scrape cycles.                        |
| `llm_label_translator_scrape_failure_total`      | counter | Failed upstream scrape cycles.                            |
| `llm_label_translator_last_success_timestamp`    | gauge   | Unix seconds of last successful scrape.                   |
| `llm_label_translator_emitted_total`             | counter | Translated events published to the bus.                   |
| `llm_label_translator_skipped_total`             | counter | Samples skipped (priming scrape, zero-delta window).      |
| `llm_label_translator_dropped_total`             | counter | Events dropped because no tenant fallback was resolvable. |
| `llm_label_translation_unmapped_total{provider}` | counter | Inbound samples with no row in `label_mappings`.          |

## Hard constraints

- Never modifies, vendors, or forks the upstream `llm-usage-exporter`;
  integration is composition-only (container image + scrape/FOCUS consumption).
- Never logs upstream credentials or DSN values.
- Every emitted event carries `tenant`, `team`, `env`; missing those
  drops the event.
