# Pull-Mode / Proxy-Mode Reconciliation (F023)

OpenLLM Metrics can ingest the same workload through two independent
pipelines: a **pull-mode** path that polls vendor billing data via the
upstream `llm-usage-exporter` (FOCUS) and a **proxy/runtime-mode** path
that captures token counts and prices them against a local catalog at the
moment a request is served. Both paths are real; both have known limits;
neither is ground truth on its own. The reconciler is the worker that
correlates them, surfaces the gap as a first-class signal, and lets
operators ask "where does our cost view disagree with the vendor's?"
without taking action on the answer.

This document is the architectural contract for the F023 reconciler. The
worker's package-level README is at `apps/worker/reconciler/README.md`;
this file covers the cross-component design.

## Input sources

| Source                | Topic                  | Producer                                                | Authority                                           |
| --------------------- | ---------------------- | ------------------------------------------------------- | --------------------------------------------------- |
| Runtime-side estimate | `llm.cost.estimated`   | `apps/worker/cost-mapper` (`source = gateway` or `sdk`) | Fast, includes app context, prone to catalog drift. |
| Vendor reconciliation | `llm.usage.reconciled` | `apps/worker/focus-ingester` (`source = exporter`)      | Slow, authoritative on $, lacks app context.        |

The cost-mapper price-stamps every runtime event against
`platform/pricing/<provider>.yaml` at near-real-time latency, so the
runtime estimate is rich in `{team, app, env, project, route}` context but
inherits any staleness, missing discount, or untracked add-on in the
catalog. The focus-ingester polls the vendor's FOCUS endpoint on a slow
cadence (default 1 hour) — the cost is authoritative once it arrives, but
arrival lags by hours to days and FOCUS rarely carries app-level
context. Reconciliation matters because both signals are needed: runtime
estimates for live FinOps, vendor data for invoice agreement.

## Window choice

The default correlation window is **1 hour** (`window.size_seconds=3600`).
The size is configurable; the rationale is empirical:

- Hourly buckets are coarse enough that small clock skew between the
  gateway, the SDK, and the upstream exporter's FOCUS bucketing never
  cross-contaminates adjacent windows.
- Hourly buckets are fine enough that drift dashboards can resolve a
  catalog-rate change within a single business day.
- Most vendor FOCUS endpoints emit at hourly or daily granularity already,
  so a smaller window would only carry noise from the timestamp at which
  a billing system happened to finalize a line item.

The joiner truncates every event's `recorded_at` (or `period_start` for
the FOCUS side) to the window size before bucketing, so two events that
fell in the same hour always land in the same bucket regardless of when
they reached the bus.

## Drift formula

```
drift_usd   = reconciled_cost_usd - estimated_cost_usd
drift_ratio = drift_usd / max(estimated_cost_usd, 0.0001)
```

The denominator is floored at $0.0001 to keep `drift_ratio` finite for
windows where the runtime side contributed nothing (the vendor billed the
tenant but no gateway/SDK runtime event made it to the bus — typically a
clock-skew issue or a direct-to-provider path that bypassed the gateway).
A positive ratio means the vendor billed more than the runtime estimate
predicted (stale catalog, missing discount, untracked add-on, region
surcharge). A negative ratio is rare and usually points at a runtime-side
token-counting drift; investigate the gateway or SDK before assuming the
vendor under-billed.

OSS scope ends at the formula above. Anything richer than this — adaptive
baselines, per-tenant tolerances, change-point detection, cost-efficiency
scoring weights — lives behind the F025 cost-efficiency boundary in
this repository (not implemented here).

## Why a grace period exists

The grace period (`window.grace_seconds`, default 48 hours) is the answer
to a simple operational question: how long does the reconciler wait for
the vendor before declaring a window unreconciled?

- **OpenAI / Anthropic** finalize per-request billing within minutes for
  most calls; rare disputes and refunds settle within hours.
- **Azure OpenAI** can lag by several hours; PTU and reserved capacity
  bookings reconcile on a daily cadence.
- **AWS Bedrock** typically lags 24 hours and can take longer when
  cross-region or on-demand-burst pricing applies.
- **Vertex AI / Gemini** can lag up to 48 hours during high-traffic
  windows; promotional credit application sometimes lands days later.

A 48-hour grace period catches the slow tail of all five priority
providers without holding state forever. Operators with stricter SLAs can
tighten the value per-environment (e.g., 24h in staging where small
volumes settle fast, 48h+ in production). The grace period is intentionally
decoupled from the window size: window size controls bucket granularity,
grace period controls patience.

## Reconciliation states

The `status` column on `control_plane.reconciliation_results` is the
window's lifecycle marker:

- **`open`** — The cycle is still inside the window, or the window has
  ended but the grace period has not yet elapsed. The joiner upserts new
  contributions into this row; the closer leaves it alone.
- **`closed`** — A transitional state reserved for future use (e.g., a
  human-resolution workflow); the current closer skips directly to the
  terminal states below.
- **`reconciled`** — `window_end + grace_period <= now` and both sides
  contributed non-zero cost. The drift columns are the truth-of-record
  for that bucket.
- **`unreconciled`** — `window_end + grace_period <= now` but exactly one
  side contributed. Either the gateway/SDK saw traffic the vendor never
  billed for (drop, retry, or test-environment route) or the vendor billed
  traffic that bypassed the gateway/SDK (rogue caller, mis-configured
  base URL, or a vendor-side proration of a long-running booking).

Once a row reaches a terminal status, the closer emits a
`reconciliation.window.v1` event on `llm.reconciliation.window`, refreshes
the per-tuple Prometheus gauges, and forgets the in-memory bucket.

## Alert recipes (cite F033)

The reconciler emits the signal; the F033 notifications worker fans out
alerts. Recommended recipes (configured as match rules in
`control_plane.notification_rules`):

1. **Persistent positive drift.** Alert when `drift_ratio > 0.10` for
   three consecutive windows on the same `(tenant, provider, model)` and
   the per-window `reconciled_cost_usd` is above a tenant-configured
   minimum (avoids alarming on small-volume noise). Typical cause: the
   pricing catalog missed a per-1k rate change.
2. **Persistent negative drift.** Alert when `drift_ratio < -0.10` for
   three consecutive windows. Typical cause: a token-counting bug in the
   gateway or SDK; investigate `llm_runtime_*` series for the same tuple
   before reporting to the vendor.
3. **Unreconciled with non-zero estimate.** Alert when the closer flips a
   window to `unreconciled` and `estimated_cost_usd > 0`. Typical cause:
   FOCUS export delay beyond the grace period, or a vendor outage on the
   billing pipeline.
4. **Unreconciled with non-zero reconciliation.** Alert when the closer
   flips a window to `unreconciled` and `reconciled_cost_usd > 0`.
   Typical cause: traffic that bypasses the gateway/SDK entirely;
   investigate the `OPENAI_BASE_URL` and equivalent settings on every
   caller.

Routing on the drift signal — fall back, downgrade, deny, downgrade
tier — is **not** an OSS reconciliation concern. The reconciler only
computes the signal. Routing belongs to F034 (telemetry-conditioned
routing) and F035 (bounded fallback with provenance), both of which are
custom and not implemented here.

## OSS/custom posture

The reconciler is OSS-safe. The math is a subtraction and a ratio; the
window is a truncation; the lifecycle is `open → closed → terminal`. None
of that is novel. The novelty in the broader platform lives downstream
of this worker — in how scoring weights consume the reconciled cost, how
the routing decider conditions decisions on the drift, and how policy
enforcement uses both. Per the open-source scope, those
algorithms live in this repository. Nothing in this document, this
worker, or its emitted events leaks them.
