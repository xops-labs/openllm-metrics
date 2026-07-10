# apps/worker/quota-risk

The quota and rate-limit risk worker (F026). Models the quota and rate-limit risk for every observed `(tenant, provider, model, region)` pair. **Signal-only.** This worker does not enforce routing, throttling, fallback, or any policy decision — routing, throttling, fallback, and policy enforcement are outside this worker.

## What it does

1. Subscribes to `llm.runtime.normalized` (`source=gateway|sdk`) and `llm.usage.normalized` (`source=exporter`) on the streaming bus.
2. Extracts rate-limit signals from provider response headers carried on the event under a `provider_headers` field (forward-compatible producer convention; the worker tolerates absence).
3. Maintains an in-memory rolling per-`(tenant, provider, model, region)` view: `tokens_remaining`, `tokens_limit`, `requests_remaining`, `requests_limit`, `reset_after`.
4. Computes:
   - `usedRatio = 1 - (remaining / limit)` (skipped when limit is unknown)
   - `secondsToReset` (from `Retry-After`, `*-reset-*`, or absolute reset timestamps)
   - `riskScore = min(1.0, usedRatio * 1.25)` — a transparent linear shaping factor that crosses 1.0 at 80% saturation so operators have headroom. **Transparent by design.**
5. On every refresh tick:
   - Re-renders Prometheus gauges (stale keys are dropped immediately).
   - Publishes one `quota.risk.v1` event per `(key, kind)` to the bus output topic so other OSS subscribers can react.

## Prometheus surface

All gauges carry `{tenant, provider, model, region, kind="tokens|requests"}`.

- `llm_quota_used_ratio` — `[0, 1]`, skipped when no denominator.
- `llm_quota_seconds_to_reset` — seconds until the window resets.
- `llm_quota_risk_score` — linear-shaped risk in `[0, 1]`.

Self-observability counters:

- `llm_quota_risk_events_consumed_total`
- `llm_quota_risk_events_emitted_total`
- `llm_quota_risk_events_skipped_total{reason}`

## Provider header parsers

| Provider       | Tokens                                                                                      | Requests                                                                   | Reset                                                   |
| -------------- | ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- | ------------------------------------------------------- |
| `openai`       | `x-ratelimit-{remaining,limit}-tokens`                                                      | `x-ratelimit-{remaining,limit}-requests`                                   | `x-ratelimit-reset-{tokens,requests}`                   |
| `anthropic`    | `anthropic-ratelimit-{tokens,input-tokens,output-tokens}-{remaining,limit}` (min-pool view) | `anthropic-ratelimit-requests-{remaining,limit}`                           | `anthropic-ratelimit-{tokens,requests}-reset` (RFC3339) |
| `google`       | n/a                                                                                         | `x-goog-quota-{remaining,limit}` (when present); fallback to `retry-after` | `x-goog-quota-reset` then `retry-after`                 |
| `bedrock`      | n/a                                                                                         | `x-amzn-RateLimit-Limit` + `retry-after` infers saturation                 | `retry-after`                                           |
| `azure_openai` | `x-ratelimit-{remaining,limit}-tokens`                                                      | `x-ratelimit-{remaining,limit}-requests`                                   | `retry-after-ms` preferred, falls back to `retry-after` |

## Configuration

YAML at `--config` (default `/etc/openllm-quota-risk/config.yaml`):

```yaml
server:
  port: 8084
bus:
  brokers: [redpanda:9092]
  client_id: openllm-quota-risk
  consumer_group: openllm-quota-risk
  output_topic: llm.quota.risk.v1
  # input_topics defaults to llm.runtime.normalized + llm.usage.normalized
risk:
  window_seconds: 300 # observations older than this drop off
  refresh_interval_seconds: 30 # snapshot + emit cadence
defaults:
  tenant: '' # fallback when event has no tenant
```

## Boundary statement

This worker **MODELS** risk. It does **NOT**:

- Route requests to a different provider.
- Throttle, rate-limit, or shed load at the gateway.
- Trigger fallback chains.
- Enforce budgets.
- Apply any decision policy.

Those behaviours are F034 (routing), F035 (bounded fallback), and F030 (policy enforcement). They are intentionally outside this worker.

## Status & remaining work

Wired into `go.work`, containerized ([Dockerfile](./Dockerfile)), and run as the `quota-risk` service in [docker-compose.yml](../../../docker-compose.yml) against the local Redpanda. Unit tests for the per-provider header parsers are still pending.
