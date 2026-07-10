# Runbook: LLMExporterStale

**Alert source:** `packages/dashboards/prometheus-alerts/llm-exporter-stale.yml`

## Trigger

`time() - llm_exporter_last_success_timestamp` exceeds 600 seconds
(warning) or 1200 seconds (critical) for a `(provider, tenant)` series,
sustained for 5 minutes.

The thresholds are derived from the default poller `polling_interval_seconds`
of 300s (2x / 4x the interval). If you have overridden the polling interval,
adjust the rule expression in
`packages/dashboards/prometheus-alerts/llm-exporter-stale.yml`.

## Why this matters

When the exporter stops, every downstream FinOps signal goes blind:

- Cost panels stop updating.
- Forecasts decay toward the last known value.
- Cost-spike alerts cannot fire (no rate data).
- Reliability scoring loses freshness.

This alert is the canary for the entire Phase 1 pipeline.

## Immediate triage (5 minutes)

1. Check the poller pod / container health:
   ```bash
   docker ps --filter "name=openai-poller"
   docker logs openai-poller --tail 200
   # or for Kubernetes:
   kubectl get pods -l app=openai-poller -n openllm-metrics
   kubectl logs -l app=openai-poller -n openllm-metrics --tail=200
   ```
2. Hit the poller's `/metrics` endpoint directly to confirm whether it is
   reachable at all:

   ```bash
   curl -sS http://openai-poller:8080/metrics | grep llm_exporter_
   ```

   - No response: the process is dead or the network is wrong.
   - `llm_exporter_scrape_failure` climbing: the poller is up but the
     provider API is not reachable.

3. Check `llm_provider_api_errors_total` by `error_type` for the affected
   provider to see whether the issue is `network`, `4xx` (auth), `5xx`
   (provider), or `circuit_open` (the poller circuit-breaker tripped).

## Common root causes

| Symptom                             | Likely cause                                 | Mitigation                                                 |
| ----------------------------------- | -------------------------------------------- | ---------------------------------------------------------- |
| Poller process not running          | OOMKilled / crash loop                       | Restart pod; inspect logs for panic                        |
| `error_type=4xx` (e.g. 401)         | Expired or rotated provider admin key        | Rotate `OPENAI_ADMIN_API_KEY` secret; restart              |
| `error_type=5xx` or `network`       | Provider outage or DNS / egress issue        | Wait it out; verify outbound network from the pod          |
| `error_type=circuit_open`           | Repeated failures opened the circuit breaker | Address underlying upstream error; circuit will self-close |
| Provider rate-limited the admin API | Polling too aggressively                     | Increase `polling_interval_seconds`                        |

## Escalation

- **Warning**: notify the platform team's async channel. The data lag is
  recoverable within 1-2 polling cycles.
- **Critical**: page on-call. Cost dashboards are >20 minutes stale and
  FinOps decisions made now are based on stale data.

## Related dashboards

- `OpenLLM Metrics -> Phase 1 FinOps` (any panel showing flat-lined data
  is a symptom).

## Related alerts

- `OpenLLMScrapeFailed` (in `scrape_failure.yml`) — fires if Prometheus
  itself cannot scrape the poller. Usually co-fires for the same root cause.
