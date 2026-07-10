# Cardinality Budget

This document defines the canonical label set for all `llm_*` metrics in
OpenLLM Metrics and establishes the per-dimension cardinality budget.

The label schema lives here as a planning contract. The enforcement mechanism
(lint guard) is added in **F008 - Common Telemetry Schema**.

---

## Canonical Label Set

| Label             | Values (examples or enum)                                          | Max cardinality | Notes                                                 |
| ----------------- | ------------------------------------------------------------------ | --------------- | ----------------------------------------------------- |
| `provider`        | `openai`, `anthropic`, `google`, `azure_openai`, `bedrock`         | 5               | Fixed enum per TIP Phase 2                            |
| `model`           | `gpt-4o`, `claude-3-5-sonnet`, `gemini-1.5-pro`, …                 | 250             | ~50 per provider; updated as providers release models |
| `operation`       | `chat`, `completion`, `embedding`, `image`, `audio`, `batch`       | 10              |                                                       |
| `app`             | deployment-specific                                                | 50              | Bounded by operator config                            |
| `team`            | deployment-specific                                                | 20              |                                                       |
| `env`             | `development`, `staging`, `production`                             | 3               | Fixed enum                                            |
| `tenant`          | deployment-specific                                                | 100             | Required once F005 lands                              |
| `project`         | deployment-specific                                                | 100             |                                                       |
| `status_code`     | `200`, `400`, `401`, `429`, `500`, `503`                           | 10              | HTTP status codes                                     |
| `error_type`      | `rate_limit`, `timeout`, `auth`, `server_error`, `network`, `none` | 10              |                                                       |
| `region`          | `us-east-1`, `eu-west-1`, `us-central1`, …                         | 30              | Provider region strings                               |
| `routing_reason`  | `cost`, `reliability`, `quota`, `policy`, `fallback`, `default`    | 10              | Phase 6+ label                                        |
| `policy_name`     | deployment-specific                                                | 50              | Phase 5+ label                                        |
| `fallback_reason` | `rate_limit`, `timeout`, `error`, `cost_cap`, `quota`              | 10              | Phase 6+ label                                        |

---

## Worst-Case Cardinality Estimate

A single metric with all labels active across a typical mid-size deployment:

```text
provider(5) × model(50) × operation(6) × app(20) × team(10) × env(3)
× tenant(50) × project(50) × status_code(8) × error_type(6) × region(10)
= ~3.2 billion (theoretical)
```

In practice, most combinations never occur (not every team uses every
provider/model/region). Observed deployments should target **< 100 k active
series** for the core request and token metrics.

---

## Rules

1. **No free-form string labels.** Labels that are derived from user input
   (prompt text, completion text, model response IDs) are forbidden at the
   metrics layer. Only token counts, latencies, error category enums, and
   hashed identifiers are permitted.

2. **No provider credentials, API keys, or secrets in label values.** Ever.

3. **No LLM payload content in label values.** Prompt and completion text
   must never appear in a metric label or value.

4. **`tenant`, `team`, `app`, `project` are required on every metric once F005
   lands.** Prior to F005 they are optional dev-only labels.

5. **Phase-gated labels.** Labels marked "Phase 6+" (`routing_reason`,
   `fallback_reason`) and "Phase 5+" (`policy_name`) must not be emitted by
   services in earlier phases. Use empty-string or omit the label entirely.

6. **Lint enforcement (F008).** The F008 schema package ships a Go validator
   that rejects any metric registration using labels outside this set or label
   values that match the forbidden-pattern list (long strings, UUID-like values,
   numeric sequences longer than 20 chars).

---

## Cardinality Runbook

If a cardinality alert fires (see `alerts/scrape_failure.yml`):

1. Run `topk(20, count by (__name__, job)({__name__=~"llm_.*"}))` in Prometheus
   to find the high-cardinality series.
2. Identify the label dimension driving the explosion.
3. Apply a `metric_relabel_configs` drop or replace rule in `prometheus.yml`
   to cap the dimension while the root cause is fixed in the emitting service.
4. File an issue referencing the F008 lint guard to add the pattern to the
   forbidden list.
