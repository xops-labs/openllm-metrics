<!-- Copyright (c) 2026 Yasvanth Udayakumar. -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Demo Traffic Generator

Synthetic, **schema-conformant** LLM telemetry for the local stack — so every
analytics screen, FinOps dashboard, and reconciliation panel lights up with
**zero provider API keys**.

```bash
docker compose --profile demo up -d
# then open http://localhost:3030/analytics and http://localhost:3000
curl -s http://localhost:8089/healthz   # emit counters
```

## What it emits

| Topic                    | Cadence             | Drives                                                                                                                                                                                    |
| ------------------------ | ------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `llm.runtime.normalized` | ~4 req/s (jittered) | `llm_requests_total`, token counters, `llm_errors_total`, `llm_timeouts_total`, `llm_rate_limit_events_total`, `llm_retries_total`; the cost-mapper turns these into `llm.cost.estimated` |
| `llm.usage.normalized`   | every 30s           | `llm_cost_usd_total` (pull-mode billing rollups)                                                                                                                                          |
| `llm.usage.reconciled`   | every 60s           | reconciler joins → `llm_reconciliation_*` drift series                                                                                                                                    |

The simulated fleet covers all five providers (OpenAI, Anthropic, Google,
Azure OpenAI, Bedrock) across the seeded **Acme Corp** tenant's teams and apps
(`platform-eng/chat-assistant`, `analytics/batch-processor`), with realistic
latency tails, ~4% error/throttle/timeout traffic, and a 0–6% billing drift so
the drift panels are non-zero.

Every event carries `source_service: examples/demo-generator`, so demo signal
is always distinguishable from real traffic. Events conform to the F008
contracts in `packages/contracts/telemetry/go/schemas/` —
`events_test.go` enforces required fields and `additionalProperties: false`
against the embedded schemas.

## Configuration (env)

| Variable                       | Default         | Meaning                             |
| ------------------------------ | --------------- | ----------------------------------- |
| `OLM_DEMO_BROKERS`             | `redpanda:9092` | Comma-separated bus brokers         |
| `OLM_DEMO_TENANT`              | Acme seed UUID  | Tenant label on all events          |
| `OLM_DEMO_RPS`                 | `4`             | Runtime events per second (max 100) |
| `OLM_DEMO_USAGE_INTERVAL`      | `30s`           | Pull-mode rollup window             |
| `OLM_DEMO_RECONCILED_INTERVAL` | `60s`           | Billing-truth window                |
| `OLM_DEMO_LISTEN_ADDR`         | `:8089`         | `/healthz` listen address           |

## Privacy invariant

Even synthetic traffic carries **no prompt or completion text** — there is
none to carry. Counts, timings, labels, status codes, and costs only, same as
the real capture surfaces.

This is a demo aid, not a product service: it ships in `examples/`, is gated
behind the `demo` compose profile, and is not part of the release image
matrix.
