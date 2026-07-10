# OpenAI Usage and Cost Poller (F009)

A standalone Go service that polls the OpenAI Admin Usage and Cost APIs,
normalizes results into the F008 `llm.usage.normalized` event schema, and
publishes them to the streaming bus.

This is the reference implementation that later provider pollers
(F013 Anthropic, F014 Gemini, F015 Azure OpenAI, F016 Bedrock) copy.

## Table of Contents

- [What it does](#what-it-does)
- [What it does not do](#what-it-does-not-do)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Environment variables](#environment-variables)
- [Metrics exposed](#metrics-exposed)
- [Recommended alerts](#recommended-alerts)
- [Cost attribution caveat](#cost-attribution-caveat)
- [Security posture](#security-posture)
- [Development](#development)

## What it does

- Wakes every `polling_interval_seconds` (default 300s per vision MVP).
- Calls the OpenAI Admin `/v1/organization/usage/completions` and
  `/v1/organization/costs` endpoints for the most recent window.
- Maps each `(bucket, model, project)` row into a vendor-neutral
  `llm.usage.normalized` event (F008 schema v1).
- Deduplicates by `(provider, window_start, window_end, model, app, team,
project)` so a replay does not double-emit.
- Produces events to the `llm.usage.normalized` topic on the streaming bus.
- Exposes scrape-health metrics on `/metrics` (Prometheus exposition).

## What it does not do

- Does NOT touch prompts, completions, messages, embeddings, or any other
  request/response payload. The Usage API exposes operational counters
  only; the binary refuses to look at anything that could carry user data.
- Does NOT own the cost mapping model (F017 will).
- Does NOT compute reliability or cost-efficiency scores (F024/F025).
- Does NOT enforce policy or routing decisions (F030/F034).
- Does NOT run the proxy / runtime telemetry path (F018).

## Quick start

```bash
# 1. Set the OpenAI Admin key (read-only on Usage scope is sufficient).
export OPENAI_ADMIN_API_KEY="sk-admin-..."

# 2. Copy and edit the example config.
cp apps/worker/usage-poller/openai/config.example.yaml /etc/openllm-poller/openai.yaml

# 3. Build and run.
cd apps/worker/usage-poller/openai
go build -o openai-poller ./cmd/openai-poller
./openai-poller --config /etc/openllm-poller/openai.yaml
```

Container build (run from repo root so the workspace deps are in context):

```bash
docker build -f apps/worker/usage-poller/openai/Dockerfile -t openllm/openai-poller:dev .
docker run --rm -p 8080:8080 \
  -e OPENAI_ADMIN_API_KEY="$OPENAI_ADMIN_API_KEY" \
  -v "$PWD/apps/worker/usage-poller/openai/config.example.yaml:/etc/openllm-poller/openai.yaml:ro" \
  openllm/openai-poller:dev
```

## Configuration

See [`config.example.yaml`](./config.example.yaml) for an annotated example.
Validated at startup. The binary fails fast on:

- Missing or empty `labels.tenant` (F005 invariant: every event carries a tenant).
- Missing `labels.env` or `labels.team`, or `labels.env` not in `{development, staging, production}`.
- `polling_interval_seconds <= 0`.
- The env var named by `providers.openai.api_key_env` being unset or empty.

## Environment variables

| Variable                                                 | Required | Description                                              |
| -------------------------------------------------------- | -------- | -------------------------------------------------------- |
| `OPENAI_ADMIN_API_KEY` (or whatever `api_key_env` names) | yes      | OpenAI Admin API key with Usage scope. **Never** logged. |

The binary reads NO other secrets from the environment. Bus credentials,
when added, will follow the same pattern: env-var name in YAML, value in env.

## Metrics exposed

Served on `:{server.port}/metrics` in Prometheus text format. Every series
carries `provider="openai"`, `tenant="<configured>"`, `env="<configured>"`.

| Metric                                | Type    | Purpose                                                                                              |
| ------------------------------------- | ------- | ---------------------------------------------------------------------------------------------------- |
| `llm_exporter_scrape_success`         | counter | Cumulative successful poll cycles.                                                                   |
| `llm_exporter_scrape_failure`         | counter | Cumulative failed poll cycles.                                                                       |
| `llm_exporter_last_success_timestamp` | gauge   | Unix seconds of the most recent successful cycle.                                                    |
| `llm_provider_api_errors_total`       | counter | Per-reason error counter (`reason="network\|rate_limited\|circuit_open\|5xx\|4xx\|bus\|normalize"`). |
| `llm_rate_limit_events_total`         | counter | F008-canonical rate-limit hit counter.                                                               |

`/healthz` returns 200 once the binary is up and the metrics surface is bound.
It does NOT reflect the breaker state — health is "process alive". Wire the
recommended alert below for "are scrapes actually succeeding".

## Recommended alerts

```yaml
# Alert when the last successful scrape is older than 2 * polling_interval.
- alert: OpenAIPollerStale
  expr: (time() - llm_exporter_last_success_timestamp{provider="openai"}) > 600
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: 'OpenAI poller has not succeeded for more than 2 * polling_interval.'

# Alert when the breaker is tripping repeatedly.
- alert: OpenAIPollerCircuitFlap
  expr: increase(llm_provider_api_errors_total{provider="openai",reason="circuit_open"}[15m]) > 3
  for: 15m
  labels:
    severity: warning
```

## Cost attribution caveat

The OpenAI Cost API returns line items per bucket (not per model). F009
attributes bucket cost across the bucket's usage rows using a token-weighted
share (input + output tokens). This is good enough for FinOps dashboards at
MVP but is NOT the canonical model-aware cost mapping engine — that lands in
F017 and may replace this heuristic. The OpenAI-specific weighting lives
entirely inside `internal/adapter/`; no downstream module depends on it.

## Security posture

- The OpenAI Admin API key is read once from the env at startup. Never
  logged, never emitted to traces or metrics, never echoed back in error
  messages (`config.MaskAPIKey` and `openaiclient.scrubKey` provide
  defense-in-depth).
- The poller never fetches or stores prompts, completions, messages,
  embeddings, or any other payload field. The schema-lint test in
  `tests/provider-adapters/openai` fails the build if any emitted event
  contains a forbidden field.
- Every emitted event carries `tenant`. Cross-tenant deployments run one
  binary per tenant scope (or per provider key scope) — there is no shared
  tenant context inside the binary.
- TLS to OpenAI is enforced by Go's default `http.Client`. No insecure
  fallback path exists.

## Development

```bash
# Module is in the workspace. From the repo root:
go build ./apps/worker/usage-poller/openai/...
go test  ./apps/worker/usage-poller/openai/...

# The contract tests (using recorded fixtures) live in
# tests/provider-adapters/openai/ and validate the round-trip from
# OpenAI JSON shapes through the adapter and out to schemalint.
go test ./tests/provider-adapters/openai/...
```
