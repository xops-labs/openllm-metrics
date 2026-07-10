# packages/telemetry/schema-lint

Validator for OpenLLM Metrics streaming-bus events and Prometheus-style
metric observations, owned by **F008 - Common Operational Telemetry
Schema**.

The linter enforces the OpenLLM-Metrics-specific guard rails on top of the
canonical schemas in `packages/contracts/telemetry` and the metric
registry in `packages/contracts/metrics`.

## What it checks

For event payloads (`LintEvent`):

- topic is one of the topics in the F008 contract set
- `schema_version` matches the current contract version
- every mandatory field (per F008) is present and non-empty
- no forbidden LLM-payload key (`prompt`, `completion`, `input`,
  `output`, `messages`, `embedding`, `content`, `request_body`,
  `response_body`) appears at any depth
- `tenant` is present
- `request_id_hash`, when present, is a 64-character lowercase hex
  SHA-256 (raw IDs are forbidden by F008 §11)

For metric observations (`LintMetric`):

- metric name is registered in `packages/contracts/metrics`
- every emitted label is in the metric's `AllowedLabels` set
- every mandatory label (`provider`, `model`, `tenant`, `env`) is
  present and non-empty
- no label key matches a forbidden LLM-payload key

## Library

```go
import schemalint "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go"

result := schemalint.LintEvent("llm.usage.normalized", payloadBytes)
if !result.OK() {
    return result.Error()
}
```

## CLI

```bash
# From a file:
schema-lint --topic llm.usage.normalized --file event.json

# From stdin (CI pipelines):
cat event.json | schema-lint --topic llm.runtime.normalized
```

Exit codes:

- `0` — payload passes every project-specific rule
- `1` — payload has one or more lint issues (printed to stderr)
- `2` — invocation error (unknown flag, unreadable file)

## Issue codes

Stable identifiers suitable for CI rule references:

| Code            | Rule                                                 |
| --------------- | ---------------------------------------------------- |
| `OLLM-LINT-001` | Unknown topic                                        |
| `OLLM-LINT-002` | Missing or empty `tenant`                            |
| `OLLM-LINT-003` | Missing mandatory field                              |
| `OLLM-LINT-004` | Forbidden LLM-payload field present                  |
| `OLLM-LINT-005` | Unauthorized label on a metric                       |
| `OLLM-LINT-006` | Unknown metric name                                  |
| `OLLM-LINT-007` | Field has the wrong JSON type                        |
| `OLLM-LINT-008` | `request_id_hash` is not a SHA-256 lowercase hex     |
| `OLLM-LINT-009` | `schema_version` does not match the current contract |

## Scope

This linter enforces the project-specific F008 guard rails. Full
JSON-Schema draft validation can be layered on top by services that
require it via any standard JSON-Schema validator using the bytes
returned by `telemetrycontracts.Schema(topic)`.
