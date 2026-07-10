# Proxy Demo — Zero-Code Instrumentation via Gateway

This demo shows how an existing application gets full OpenLLM Metrics
telemetry — latency, token counts, cost, tenant labels — by pointing its
OpenAI client at the OpenLLM Metrics gateway instead of `api.openai.com`.
No SDK instrumentation code changes are required.

## How it works

The OpenLLM Metrics gateway (`apps/gateway`) acts as an OpenAI-compatible
reverse proxy. It intercepts every request, records the timing and usage
metadata, and forwards the call to the upstream provider. Because it speaks
the OpenAI HTTP API, any client that supports the `base_url` / `OPENAI_BASE_URL`
override works without modification.

```
Your app  ──► OPENAI_BASE_URL=http://localhost:8085/v1  ──► OpenLLM Gateway
                                                                  │
                                                                  ├── records latency, tokens, cost
                                                                  ├── emits llm_gateway_* metrics on :8086/metrics
                                                                  └── forwards to api.openai.com
```

## Prerequisites

1. The compose stack is running locally (this also brings up the
   metrics-endpoint, Prometheus, and Grafana that make the results visible):

   ```sh
   docker compose up -d
   ```

   The gateway proxy is published on `http://localhost:8085` (host port; see
   `docker-compose.yml`).

2. A valid `OPENAI_API_KEY` environment variable is set. The gateway passes
   it through to the upstream provider unchanged; it is never logged or stored.

## Per-language scripts

Note on `/v1`: the OpenAI Python and Node SDKs append `/chat/completions` to
whatever base URL you give them, so the base URL must end in `/v1`. The Go
script builds the full `/v1/chat/completions` path itself, so its base URL
omits `/v1`.

### Python

```sh
cd examples/proxy-demo
OPENAI_BASE_URL=http://localhost:8085/v1 python proxy_demo.py
```

### Node.js / TypeScript

```sh
cd examples/proxy-demo
OPENAI_BASE_URL=http://localhost:8085/v1 node proxy_demo.mjs
```

### Go

```sh
cd examples/proxy-demo
OPENAI_BASE_URL=http://localhost:8085 go run proxy_demo.go
```

## What you will see

After the script completes, the gateway's own metrics listener at
`http://localhost:8086/metrics` shows the request in the `llm_gateway_*`
families:

```
llm_gateway_requests_total{provider="openai",model="gpt-4o-mini",tenant="acme",env="development",status="success",status_code="200"} 1
llm_gateway_latency_seconds_bucket{provider="openai",model="gpt-4o-mini",...,le="5"} 1
```

The gateway also publishes a runtime event to the bus, which the
metrics-endpoint aggregates into the `llm_*` series at
`http://localhost:9092/metrics` (scraped by the bundled Prometheus at
`http://localhost:9090`):

```
llm_requests_total{provider="openai",model="gpt-4o-mini",tenant="acme",...} 1
llm_input_tokens_total{...}
llm_output_tokens_total{...}
```

And in Grafana (`http://localhost:3000`) the request appears on the
"OpenLLM Metrics — Phase 1 FinOps" dashboard, e.g. in the
"Successful requests (24h)" stat and the "Per-model requests, errors, cost"
table.

## Tenant labeling from headers

The gateway reads `X-OLM-Tenant`, `X-OLM-Team`, `X-OLM-App`,
`X-OLM-Env`, and `X-OLM-Project` request headers and attaches them
as metric labels. Set them on the client to get multi-tenant breakdowns
without changing your application code:

```python
client = openai.OpenAI(
    base_url="http://localhost:8085/v1",
    default_headers={
        "X-OLM-Tenant": "acme",
        "X-OLM-Team":   "platform",
        "X-OLM-App":    "chatbot",
        "X-OLM-Env":    "production",
        "X-OLM-Project":"customer-support",
    },
)
```

## Comparison: proxy mode vs. SDK mode

| Capability                        | Proxy (this demo) | SDK (packages/sdk-\*) |
| --------------------------------- | :---------------: | :-------------------: |
| Zero application code changes     |        Yes        |   No (one `init()`)   |
| Works with any HTTP OpenAI client |        Yes        |   Language-specific   |
| Emits per-call span/trace         | Yes (server-side) |   Yes (client-side)   |
| W3C Baggage propagation           |        No         |          Yes          |
| Works without a running gateway   |        No         |          Yes          |
| Streaming support                 |        Yes        |          Yes          |
