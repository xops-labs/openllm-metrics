# llmproviderreceiver — OpenTelemetry Collector receiver for OpenLLM Metrics

`llmproviderreceiver` is a custom [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
receiver that bridges the OpenLLM Metrics streaming bus into any standard OTel
metrics pipeline. It consumes the canonical `llm.runtime.normalized` topic
(F008 `runtime.event.v1`) from Kafka / Redpanda, converts each event into
OTLP metrics following the [GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/),
and emits them through whatever exporters the operator has configured
(Prometheus, OTLP/HTTP, Datadog, Splunk, etc.).

## What it emits

| Metric                             | Type      | Source field                     | Notes                                                                                                                                              |
| ---------------------------------- | --------- | -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gen_ai.client.token.usage`        | histogram | `input_tokens` / `output_tokens` | One data point per token type per event; `gen_ai.token.type=input\|output`.                                                                        |
| `gen_ai.client.operation.duration` | histogram | `latency_us`                     | Seconds. Delta temporality.                                                                                                                        |
| `llm_estimated_cost_usd`           | sum       | `estimated_cost_usd_minor_units` | USD. Project extension to GenAI semconv (cost is not yet in the spec). Emitted only when the upstream producer joined against the pricing catalog. |

### Resource attributes

Every metric carries the multi-tenant fingerprint as resource attributes:

`tenant`, `team`, `app`, `env`, `project`, `gen_ai.system` (provider),
`gen_ai.request.model`, `route`, `gen_ai.operation.name`, `server.address`.

### Data-point attributes

Per-event dimensions live on the data points: `status`, `error.type`,
`routing_reason`, `policy_name`, `fallback_reason`, and (for `gen_ai.client.token.usage`)
`gen_ai.token.type`.

## Configuration

```yaml
receivers:
  llmprovider:
    bus:
      brokers: [redpanda:9092]
      topic: llm.runtime.normalized
      group_id: otelcol-llmprovider
      client_id: otelcol-llmprovider
    consumer:
      session_timeout: 30s
      poll_timeout: 5s
      max_records_per_poll: 1024
```

| Field                           | Required | Default                  | Notes                                                  |
| ------------------------------- | -------- | ------------------------ | ------------------------------------------------------ |
| `bus.brokers`                   | yes      | —                        | Seed brokers in `host:port` form.                      |
| `bus.group_id`                  | yes      | —                        | One consumer group per Collector deployment.           |
| `bus.topic`                     | no       | `llm.runtime.normalized` | F008 canonical runtime topic.                          |
| `bus.client_id`                 | no       | `otelcol-llmprovider`    | Reported to the broker for observability.              |
| `consumer.session_timeout`      | no       | `30s`                    | Forwarded to franz-go.                                 |
| `consumer.poll_timeout`         | no       | `5s`                     | Upper bound on a single poll iteration.                |
| `consumer.max_records_per_poll` | no       | `1024`                   | Caps batch size handed to the next consumer per cycle. |

## Building a Collector distribution

This receiver is not part of `otel-collector-contrib`. Operators who want it
must build a custom Collector binary with the
[OpenTelemetry Collector Builder (`ocb`)](https://github.com/open-telemetry/opentelemetry-collector/tree/main/cmd/builder).

A minimal manifest lives alongside this README at
[`../../builder-config.yaml`](../../builder-config.yaml). Build like this:

```bash
# 1. Install the OpenTelemetry Collector Builder.
go install go.opentelemetry.io/collector/cmd/builder@latest

# 2. Build the distribution (binary written to ./_build/openllm-otelcol by default).
builder --config platform/otel-collector/builder-config.yaml

# 3. Run with your collector config.
./_build/openllm-otelcol --config platform/otel-collector/example-config.yaml
```

The same manifest can be fed to a container build — copy the binary into a
distroless image and pin the binary alongside the upstream Collector release
the manifest pins.

## Logging policy

The receiver logs **counts and Kafka offsets only**. Event payloads carry
tenant identifiers and request-ID hashes that must never appear in stdout,
log files, or metrics labels. This rule is enforced by code review — the
`run()` loop in `receiver.go` deliberately does not have access to the event
struct outside the translator boundary.

## OSS-safe boundary

This component is OSS-safe. It does not contain routing weights, scoring
formulas, or policy enforcement logic. Those live in
`this repo` and are registered against the
extension interfaces in `packages/extensions/go/`. The receiver is signal
plumbing — the decisions happen elsewhere.

## Related

- GenAI semconv alignment: [`platform/observability/otel_alignment.md`](../../../observability/otel_alignment.md)
- Canonical event contract: [`packages/contracts/telemetry/go/schemas/llm.runtime.normalized.v1.json`](../../../../packages/contracts/telemetry/go/schemas/llm.runtime.normalized.v1.json)
