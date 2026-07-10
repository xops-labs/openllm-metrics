# LLM Proxy Gateway (F018)

A Go reverse proxy that sits in front of provider LLM APIs, forwards
requests transparently, and captures normalized runtime telemetry
(latency, errors, retries, status codes, token counts) at the request
boundary. **No request or response body is ever logged, traced, or
persisted.**

## Supported routes

| Provider      | Route                                                             |
| ------------- | ----------------------------------------------------------------- |
| OpenAI        | `POST /v1/chat/completions`, `/v1/embeddings`, `/v1/responses`    |
| Anthropic     | `POST /v1/messages`                                               |
| Google Gemini | `POST /v1beta/models/{model}:generateContent`                     |
| AWS Bedrock   | `POST /model/{modelId}/invoke` and `:invoke-with-response-stream` |
| Azure OpenAI  | `POST /openai/deployments/{deployment}/chat/completions`          |

The inbound path determines provider/operation/model. Streaming
responses (OpenAI SSE, Anthropic SSE, Bedrock event-stream) pass through
with flushing — the gateway never buffers a full streaming body.

## Configuration

Minimal `gateway.yaml`:

```yaml
server:
  port: 8080
  metrics_port: 8081
upstreams:
  openai: https://api.openai.com
  anthropic: https://api.anthropic.com
  gemini: https://generativelanguage.googleapis.com
  bedrock: https://bedrock-runtime.us-east-1.amazonaws.com
  azure_openai: https://my-resource.openai.azure.com
bus:
  brokers: [redpanda:9092]
defaults:
  tenant: acme
  team: platform
  env: development
```

Per-provider upstream URLs may also be supplied via environment
variables (env wins over YAML):

- `OLM_UPSTREAM_OPENAI_URL`
- `OLM_UPSTREAM_ANTHROPIC_URL`
- `OLM_UPSTREAM_GEMINI_URL`
- `OLM_UPSTREAM_BEDROCK_URL`
- `OLM_UPSTREAM_AZURE_OPENAI_URL`

## Inbound headers

| Header              | Purpose                                                               |
| ------------------- | --------------------------------------------------------------------- | --------- | -------------- |
| `Authorization`     | Forwarded to upstream untouched. Never logged.                        |
| `X-OLM-Tenant`      | Required label for multi-tenant routing.                              |
| `X-OLM-Team`        | Required label.                                                       |
| `X-OLM-App`         | Optional label.                                                       |
| `X-OLM-Env`         | Required label (`development`                                         | `staging` | `production`). |
| `X-OLM-Project`     | Optional label.                                                       |
| `X-OLM-Retry-Count` | Optional integer for client-side retry attribution.                   |
| `traceparent`       | W3C trace context. Forwarded to upstream.                             |
| `tracestate`        | W3C trace state. Forwarded to upstream.                               |
| `X-Request-ID`      | If present, its SHA-256 is recorded; raw ID never leaves the process. |

When an `X-OLM-*` header is missing the configured `defaults` block
fills it in.

## Try it

The compose stack ships a ready-made config
([platform/deployment/compose/configs/gateway.yaml](../../platform/deployment/compose/configs/gateway.yaml))
and publishes the proxy on host port `8085` (metrics on `8086`). From the
repo root:

```bash
docker compose up -d gateway   # also starts its Redpanda dependency

curl http://localhost:8085/v1/chat/completions \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -H "X-OLM-Tenant: acme" \
  -H "X-OLM-Team: platform" \
  -H "X-OLM-Env: development" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role":"user","content":"hi"}],
    "stream_options": {"include_usage": true}
  }'

# Scrape the operational metrics on the side channel:
curl http://localhost:8086/metrics
```

To run from source on the host instead, save the minimal YAML from the
[Configuration](#configuration) section above as `./gateway.yaml` — using
`brokers: [localhost:19092]` (Redpanda's host-published port) instead of
the in-network `redpanda:9092` — then:

```bash
go run ./cmd/gateway --config ./gateway.yaml
# proxy on localhost:8080, metrics on localhost:8081 (ports from your YAML)
```

For streaming requests, set `"stream": true`. Token counts come from
the OpenAI `stream_options.include_usage` final chunk, Anthropic's
`message_delta` event, or Gemini's `usageMetadata` block. If the
provider does not emit token counts the runtime event is published with
no token fields rather than an error.

## What this service exposes

- **Proxy port** (`:8080`): forwards every supported provider route.
  No `/metrics`, no `/healthz` — operational endpoints live on the
  side-channel port.
- **Metrics port** (`:8081`): `/metrics`, `/healthz`, `/readyz`.

Metric families emitted:

- `llm_gateway_requests_total{provider, model, tenant, env, status, status_code}`
- `llm_gateway_errors_total{...}`
- `llm_gateway_retries_total{...}`
- `llm_gateway_streaming_total{...}`
- `llm_gateway_usage_observed_total{...}` / `llm_gateway_usage_unknown_total{...}`
- `llm_gateway_latency_seconds_bucket{...}` (histogram; feeds the SLO pack's latency rules)
- `llm_gateway_bus_publish_total`, `llm_gateway_bus_publish_errors_total`

Every successful request also publishes one event on the
`llm.runtime.normalized` topic with `source=gateway` and the canonical
F008 schema.

## Privacy & security posture

- Request and response bodies are NEVER logged, traced, or persisted.
- Only the integer `usage` fields are parsed from the response body;
  the per-provider parsers are deliberately scoped to a handful of
  named integer fields.
- `Authorization` is forwarded untouched and is never logged. Provider
  API keys never enter the gateway's logs.
- Raw `X-Request-ID` values are hashed (SHA-256) before being recorded.

## What's intentionally out of scope here

The gateway captures and normalizes telemetry; it does **not** make
decisions. Routing, fallback, scoring, budget enforcement, and
rate-limiting are OSS-deferred features (F024/F025/F029/F030/F034/F035) or
are intentionally not implemented here per the open-source scope.

**Caller authentication is out of scope by design.** The gateway forwards
the caller-supplied `Authorization` header to the upstream provider and
does not authenticate callers itself. Run it on a trusted network or front
it with your own auth proxy — see the production expectations in
[deployment.md](../../docs/architecture/deployment.md#production-expectations).

The service is built, containerized ([Dockerfile](./Dockerfile)), wired
into [docker-compose.yml](../../docker-compose.yml) and `go.work`, and
covered by CI. The usage parsers have fixture tests in
[`internal/usage`](./internal/usage/); broader proxy/streaming integration
tests are still pending.
