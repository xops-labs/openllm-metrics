# Runbook: LLMCostSpike

**Alert source:** `packages/dashboards/prometheus-alerts/llm-cost-spike.yml`

## Trigger

The current 1-hour spend rate for a `(tenant, team, provider)` series is at
least 2x (warning) or 3x (critical) the 7-day rolling baseline, sustained for
15 minutes (warning) or 10 minutes (critical).

## Why this fires

The alert is a budget early-warning. Cost-rate spikes typically come from:

- A new application or pipeline rolling out without a budget cap.
- A retry storm (a recently-deployed prompt change is failing and the client
  retries aggressively).
- An unintentional switch from a cheap model to an expensive one (e.g. a
  config flip from `gpt-4o-mini` to `gpt-4o`).
- A prompt-size regression that 10x's input tokens per request.

## Immediate triage (5 minutes)

1. Open the FinOps dashboard
   (`OpenLLM Metrics -> Phase 1 FinOps`). Filter by the alert's `tenant`,
   `team`, and `provider` labels.
2. Inspect **Spend by model (top 10)** to see whether the spike concentrates
   on a single model. If yes, jump to step 4.
3. Inspect **Spend by team (top 10)** and **Top apps by spend** to see if a
   single app is driving the cost.
4. Cross-check the **Error rate by provider** panel. If error rate is also
   elevated, this is likely a retry storm — page on-call for the owning team.
5. Cross-check **Token consumption by model**. If input tokens have jumped
   while requests have not, a prompt-size regression is the most likely root
   cause.

## Common root causes

| Symptom                                          | Likely cause                                | Mitigation                              |
| ------------------------------------------------ | ------------------------------------------- | --------------------------------------- |
| Spike + high error rate                          | Retry storm                                 | Cap retries; fix the failing model call |
| Spike + stable error rate                        | New rollout / config change                 | Roll back the recent deploy             |
| Spike + jumped input tokens, stable request rate | Prompt regression                           | Revert prompt template                  |
| Spike concentrated on a single model             | Accidental switch to a more expensive model | Reset model selection in app config     |

## Escalation

- **Warning**: notify the owning team in their async channel.
- **Critical**: page on-call. If the spike persists for >30 minutes, escalate
  to the platform lead and consider applying a temporary policy denial on
  the offending model.

## Related dashboards

- `OpenLLM Metrics -> Phase 1 FinOps` (this alert's primary view).
- `docs/product/dashboards.md` for variable explanations.

## Related alerts

- `LLMHighErrorRate` — typically co-fires with cost spikes caused by retry storms.
