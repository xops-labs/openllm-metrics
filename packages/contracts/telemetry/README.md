# packages/contracts/telemetry

Canonical event-payload schemas for the OpenLLM Metrics streaming bus, owned by
**F008 - Common Operational Telemetry Schema**.

These JSON-Schema files are the single source of truth for every producer and
consumer of the cross-provider normalized telemetry events. Provider adapters
(F009 / F013–F016), the gateway (F018), the scoring worker (F024–F025), the
policy evaluator (F029–F030), and the metrics-endpoint service (F010) all
validate against these definitions.

## Topics covered

| Topic                    | Schema file                                 |
| ------------------------ | ------------------------------------------- |
| `llm.usage.normalized`   | `go/schemas/llm.usage.normalized.v1.json`   |
| `llm.runtime.normalized` | `go/schemas/llm.runtime.normalized.v1.json` |

The schemas live inside the Go module so they can be `go:embed`-ed into the
linter, services, and tests. Future language clients (TypeScript, Python) will
load the same files via generated stubs or runtime fetch.
[`platform/bus/topics.yaml`](../../../platform/bus/topics.yaml) maps each
streaming-bus topic to its canonical schema path in this folder.

## Unit conventions (F008 §10)

- **Currency** — cost on the bus is `cost_usd_minor_units` (integer, 1 unit =
  USD 0.01). Prometheus metrics expose `llm_cost_usd_total` as a float.
- **Latency** — runtime events use `latency_us` / `ttfb_us` (microseconds,
  integer). Prometheus histograms expose latency in milliseconds.
- **Token counts** — always integers, never null.

## Forbidden fields (F008 §11)

The following keys are rejected at lint time and must never appear in any
topic, header, or metric label:

- `prompt`, `completion`, `input`, `output`, `messages`, `embedding`
- `content`, `body` when carrying request/response payloads
- provider API keys, secrets, raw user identifiers

`tenant` is mandatory on every event. Identifiers that could resolve to user
content (e.g. provider `request_id`) appear only as `request_id_hash`
(SHA-256, lowercase hex).

## Provenance

Every event records:

- `schema_version` — pinned by the schema `$id`.
- `source_mode` — `pull` or `proxy`.
- `source_service` — producing service identifier.

## Evolution

Backward-compatible only, per `platform/bus/SCHEMA_EVOLUTION.md`. Breaking
changes create a new `vN` schema file and a dual-publish window.

## Validation

Use `packages/telemetry/schema-lint` to validate payloads against these
contracts and against the metric/label registry in
`packages/contracts/metrics`.
