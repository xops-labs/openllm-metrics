# Runbook: LLMHighErrorRate

**Alert source:** `packages/dashboards/prometheus-alerts/llm-error-rate.yml`

## Trigger

The rolling 5-minute error fraction (`rate(llm_errors_total) / rate(llm_requests_total)`)
for a `(tenant, provider, model)` series exceeds 5% (warning, sustained 10m)
or 15% (critical, sustained 5m).

## Why this fires

A non-trivial share of provider API calls is failing. The alert is per
`(tenant, provider, model)` so a single failing model on a single tenant
pages only the team that owns that model — not the whole platform.

## Immediate triage (5 minutes)

1. Open the **Error rate by provider** panel on the FinOps dashboard and
   filter to the alert's tenant + provider + model. Confirm the spike is
   sustained, not a one-shot blip.
2. Hit the provider status page:
   - OpenAI: <https://status.openai.com>
   - Anthropic: <https://status.anthropic.com>
   - Google AI: <https://status.cloud.google.com>
   - Azure OpenAI: <https://status.azure.com>
   - AWS Bedrock: <https://status.aws.amazon.com>
3. Inspect `llm_errors_total` grouped by `error_type` in the Prometheus UI:
   ```promql
   sum by (error_type) (rate(llm_errors_total{tenant="<t>",provider="<p>",model="<m>"}[5m]))
   ```
   The dominant `error_type` (e.g. `5xx`, `429`, `timeout`, `4xx`) usually
   identifies the failure mode.
4. Inspect `llm_rate_limit_events_total` and `llm_timeouts_total` for the
   same series. Rate-limit-driven failures and timeout-driven failures have
   different mitigations.

## Common root causes

| Dominant `error_type` | Likely cause                | Mitigation                                            |
| --------------------- | --------------------------- | ----------------------------------------------------- |
| `429`                 | Provider rate limit         | Reduce concurrency; request quota uplift              |
| `5xx`                 | Provider degradation        | Wait it out or trigger fallback to alternate provider |
| `timeout`             | Long-tail latency / network | Reduce request size; tune timeouts                    |
| `4xx`                 | Client bug or invalid input | Roll back recent client change                        |

## Escalation

- **Warning**: notify the owning team's async channel. Continue to monitor.
- **Critical**: page on-call. Trigger any pre-configured fallback policy.
  If the dominant `error_type` is `5xx` and the provider status page is
  green, open a provider support ticket.

## Related dashboards

- `OpenLLM Metrics -> Phase 1 FinOps` (Error rate by provider, Per-model table).

## Related alerts

- `LLMCostSpike` — error spikes often correlate with retry-driven cost spikes.
- `LLMExporterStale` — if no metrics are arriving, an error-rate spike from
  zero may be artificial.
