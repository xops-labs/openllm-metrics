# OpenLLM Metrics — configuration reference

Operator-facing reference for the Phase 1 services delivered by F009 (OpenAI
poller) and F010 (`/metrics` aggregator). Covers every YAML key, every
environment variable, the recommended scrape and polling intervals, and the
production deployment checklist.

For the broader product context, see the [project README](../../README.md).

> **Scope.** This page covers the two Phase 1 services only. Every other
> service documents its own YAML keys and env vars in its README under
> `apps/` — for example the
> [gateway](../../apps/gateway/README.md#configuration) and
> [reconciler](../../apps/worker/reconciler/README.md#configuration)
> Configuration sections.

## Table of contents

- [Quickstart vs production](#quickstart-vs-production)
- [F009 — OpenAI poller config](#f009--openai-poller-config)
- [F010 — `/metrics` aggregator config](#f010--metrics-aggregator-config)
- [Environment variables](#environment-variables)
- [Recommended intervals](#recommended-intervals)
- [Production checklist](#production-checklist)
- [Image registries](#image-registries)

## Quickstart vs production

| Stack                | Compose file                                 | Audience                                                                                                                                                                                               |
| -------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| All-in-one local dev | `docker-compose.yml` (repo root)             | Contributors editing the codebase. Includes the full bus + DB + TSDB scaffolding for every phase.                                                                                                      |
| Phase 1 quickstart   | `platform/deployment/compose/quickstart.yml` | OSS evaluators. The F009 poller + F010 aggregator data path plus the cost-mapper and reconciler workers (the pull-mode exporter chain is gated behind `--profile exporter`), with the infra they need. |
| Production           | `platform/deployment/helm/openllm-metrics/`  | Operators running on Kubernetes. Externally-managed bus, Postgres, TSDB.                                                                                                                               |

## F009 — OpenAI poller config

File: `apps/worker/usage-poller/openai/config.example.yaml`.
Default container path: `/etc/openllm-poller/openai.yaml`.

```yaml
server:
  port: 8080 # /metrics + /healthz HTTP surface

providers:
  openai:
    enabled: true
    api_key_env: OPENAI_ADMIN_API_KEY
    polling_interval_seconds: 300
    base_url: https://api.openai.com
    circuit_breaker_threshold: 5
    circuit_breaker_cooldown_seconds: 60
    max_retries: 4
    dedup_cache_size: 4096

bus:
  brokers: [redpanda:9092]
  client_id: openllm-openai-poller

labels:
  env: production
  team: ai-platform
  tenant: tenant-001
  app: snapcal
  project: snapcal-prod
  region: us-east-1
```

### Keys

| Key                                                 | Type         | Default                  | Constraint                                                                          |
| --------------------------------------------------- | ------------ | ------------------------ | ----------------------------------------------------------------------------------- |
| `server.port`                                       | int          | `8080`                   | 1..65535                                                                            |
| `providers.openai.enabled`                          | bool         | `true`                   | —                                                                                   |
| `providers.openai.api_key_env`                      | string       | `OPENAI_ADMIN_API_KEY`   | The env var **named** here must be set at startup. The poller fails fast otherwise. |
| `providers.openai.polling_interval_seconds`         | int          | `300`                    | > 0. Lower than 60s is not recommended (OpenAI rate-limit risk).                    |
| `providers.openai.base_url`                         | string       | `https://api.openai.com` | Override for staging proxies or test fixtures.                                      |
| `providers.openai.circuit_breaker_threshold`        | int          | `5`                      | Consecutive failures before the breaker opens.                                      |
| `providers.openai.circuit_breaker_cooldown_seconds` | int          | `60`                     | Cooldown before a probe is allowed.                                                 |
| `providers.openai.max_retries`                      | int          | `4`                      | Per-call retry budget for 429 / 5xx.                                                |
| `providers.openai.dedup_cache_size`                 | int          | `4096`                   | LRU capacity for the idempotency cache.                                             |
| `bus.brokers`                                       | list[string] | —                        | Required. Kafka/Redpanda broker host:port list.                                     |
| `bus.client_id`                                     | string       | `openllm-openai-poller`  | Surfaced in broker logs.                                                            |
| `labels.env`                                        | string       | —                        | Required. One of `development`, `staging`, `production`.                            |
| `labels.team`                                       | string       | —                        | Required. Free-form team identifier.                                                |
| `labels.tenant`                                     | string       | —                        | Required (F005 invariant). Tenant scope every emitted event carries.                |
| `labels.app`                                        | string       | (empty)                  | Optional. Application name.                                                         |
| `labels.project`                                    | string       | (empty)                  | Optional. Project name.                                                             |
| `labels.region`                                     | string       | (empty)                  | Optional. Cloud region.                                                             |

## F010 — `/metrics` aggregator config

File: `apps/api/metrics-endpoint/config.example.yaml`.
Default container path: `/etc/openllm-metrics/metrics-endpoint.yaml`.

```yaml
server:
  port: 9090 # /metrics + /healthz + /readyz HTTP surface

bus:
  brokers: [redpanda:9092]
  client_id: openllm-metrics-endpoint
  group: openllm-metrics-endpoint
  topics:
    - llm.usage.normalized
    - llm.runtime.normalized

replay:
  window_hours: 168 # cold-start replay budget; align with topic retention
```

### Keys

| Key                   | Type         | Default                                          | Constraint                                                                                                  |
| --------------------- | ------------ | ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------- |
| `server.port`         | int          | `9090`                                           | 1..65535                                                                                                    |
| `bus.brokers`         | list[string] | —                                                | Required. Non-empty.                                                                                        |
| `bus.client_id`       | string       | `openllm-metrics-endpoint`                       | Surfaced in broker logs.                                                                                    |
| `bus.group`           | string       | `openllm-metrics-endpoint`                       | Consumer group. Leave empty to replay from earliest on every restart (heavier but maximally deterministic). |
| `bus.topics`          | list[string] | `[llm.usage.normalized, llm.runtime.normalized]` | Required. Non-empty.                                                                                        |
| `replay.window_hours` | int          | `168`                                            | ≥ 0. Aligns with `platform/bus/topics.yaml` default retention (7 days).                                     |

## Environment variables

| Service         | Variable                                                 | Required | Description                                                                                                                |
| --------------- | -------------------------------------------------------- | -------- | -------------------------------------------------------------------------------------------------------------------------- |
| F009 poller     | `OPENAI_ADMIN_API_KEY` (or whatever `api_key_env` names) | yes      | OpenAI Admin API key with Usage scope. Read once at startup. Never logged, traced, or echoed.                              |
| F010 aggregator | —                                                        | —        | No runtime secrets. Bus credentials, when added, will follow the same env-var-name-in-YAML / value-in-env pattern as F009. |

The root compose stack defaults `OPENAI_ADMIN_API_KEY` to a placeholder so
the stack boots keyless (the poller pulls nothing until a real key is set in
`.env`). The Phase 1 quickstart compose
(`platform/deployment/compose/quickstart.yml`) instead hard-requires the
variable via `:?` interpolation and fails fast with a clear error.

## Recommended intervals

| Surface                | Recommendation            | Rationale                                                                                     |
| ---------------------- | ------------------------- | --------------------------------------------------------------------------------------------- |
| F009 poll cycle        | 300s (vision MVP default) | OpenAI billing windows update on a ~5 minute cadence.                                         |
| F009 `/metrics` scrape | 60s                       | Self-health counters only; no need for sub-minute resolution.                                 |
| F010 `/metrics` scrape | 30s                       | F012 dashboards assume 30s. Drop to 15s only if you have a TSDB tuned for higher cardinality. |
| Alertmanager rule eval | 15-30s                    | Match the scrape cadence of the source metric.                                                |

## Production checklist

- [ ] Images pulled from signed tags. Verify with:
      `cosign verify --certificate-identity-regexp 'https://github.com/xops-labs/openllm-metrics/.*' --certificate-oidc-issuer https://token.actions.githubusercontent.com ghcr.io/<owner>/openllm-metrics/<service>:<tag>`
- [ ] SBOM attestation fetched and archived alongside the image digest:
      `cosign download attestation --predicate-type https://cyclonedx.org/bom <image>`
- [ ] Containers run as non-root (`runAsNonRoot: true`, default in the helm chart).
- [ ] `readOnlyRootFilesystem: true` (default in the helm chart; `/tmp` is an `emptyDir`).
- [ ] OpenAI API key stored in a managed secret store (Kubernetes Secret with at-rest encryption, or external-secrets-operator backed by a KMS-protected store). Never bake into the image.
- [ ] `labels.tenant`, `labels.team`, `labels.env` set explicitly per deployment. The poller refuses to start if any are missing.
- [ ] Bus broker list points at a Redpanda/Kafka cluster with `replication >= 3` for the `llm.usage.normalized` and `llm.runtime.normalized` topics in production. (`platform/bus/topics.yaml` documents the defaults — override per environment.)
- [ ] Alert on `(time() - llm_exporter_last_success_timestamp{provider="openai"}) > 2 * polling_interval`. The F009 README ships the recommended PromQL.
- [ ] Alert on `llm_aggregator_series_total > 0.8 * registry_budget_sum` (cardinality safety net per F010 §memory budget).
- [ ] Trivy scan of the pulled image passes CRITICAL-clean. The release workflow guarantees this at publish time; re-scan periodically as new CVEs land.

## Image registries

The release workflow publishes to **GitHub Container Registry (GHCR)** as
the registry of record. Images live under:

- `ghcr.io/<owner>/openllm-metrics/openai-poller:<tag>`
- `ghcr.io/<owner>/openllm-metrics/metrics-endpoint:<tag>`

GHCR was chosen because it inherits the repository's existing OIDC trust
chain (cosign keyless signing works out of the box) and avoids a separate
credential surface. Docker Hub mirroring may be added later if a sufficiently
loud adoption signal warrants it; until then, operators that need a Docker
Hub copy can re-tag and push from GHCR themselves.
