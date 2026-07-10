<!-- Copyright (c) 2026 Yasvanth Udayakumar. -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Key Sequences

Step-by-step interactions for the flows that matter most. These complement the
pipeline view in [data-flow.md](./data-flow.md) by showing ordering, timing, and
who calls whom.

## Table of Contents

- [1. Proxy-mode request capture](#1-proxy-mode-request-capture)
- [2. Reconciliation: runtime estimate vs billed cost](#2-reconciliation-runtime-estimate-vs-billed-cost)
- [3. Policy mutation with audit](#3-policy-mutation-with-audit)
- [4. Routing decision (interface + ledger)](#4-routing-decision-interface--ledger)
- [See also](#see-also)

## 1. Proxy-mode request capture

The defining "no code change" path: an app points its provider base URL at the
gateway. The gateway forwards the call unchanged and captures telemetry from
the boundary — **after** the response has been streamed to the client, so
capture never adds latency to the user's call and never touches body content
beyond extracting integer token counts.

```mermaid
sequenceDiagram
    autonumber
    participant App as Application
    participant GW as gateway
    participant P as LLM Provider
    participant Bus as Redpanda
    participant ME as metrics-endpoint
    participant Prom as Prometheus

    App->>GW: POST /v1/chat/completions<br/>(Authorization + X-OLM-Tenant/Team/App)
    Note over GW: classify route → provider/operation/model<br/>resolve tenancy from headers (or defaults)
    GW->>P: forward request (provider key from caller)
    P-->>GW: response (streamed)
    GW-->>App: response streamed through unchanged
    Note over GW: parse usage from sampled bytes (tokens only)<br/>compute latency, status, retries
    GW->>Bus: publish llm.runtime.normalized (no bodies)
    Bus->>ME: consume event
    ME->>ME: fold into llm_* counters
    Prom->>ME: scrape /metrics
```

Key properties: provider keys are forwarded from the caller and never stored;
the response reaches the client before any parsing; only token integers and
labels are published. See the parser contracts in
[`apps/gateway/internal/usage/`](../../apps/gateway/internal/usage/).

## 2. Reconciliation: runtime estimate vs billed cost

Two independent planes converge. The runtime plane prices tokens immediately;
the billing plane arrives hours later with authoritative dollars. The reconciler
buckets both by `(tenant, provider, model, window)` and emits the drift once the
grace period elapses.

```mermaid
sequenceDiagram
    autonumber
    participant GW as gateway
    participant CM as cost-mapper
    participant EXP as exporter
    participant FI as focus-ingester
    participant REC as reconciler
    participant PG as Postgres
    participant Con as console / Grafana

    GW->>CM: llm.runtime.normalized (tokens)
    CM->>CM: price tokens × catalog rate
    CM->>REC: llm.cost.estimated (fast, app-rich)
    EXP-->>FI: /focus.json poll (hourly)
    FI->>REC: llm.usage.reconciled (slow, $-authoritative)
    Note over REC: bucket by (tenant, provider, model, hour)<br/>wait for grace period (default 48h)
    REC->>REC: drift = reconciled − estimated
    REC->>PG: write reconciliation_results + gauges
    REC->>REC: emit llm.reconciliation.window
    Con->>PG: render drift panel
```

The math is intentionally trivial (a subtraction and a ratio) — see
[reconciliation.md](./reconciliation.md). Acting on the drift (alerting,
routing, budget enforcement) is downstream and, for routing/scoring, custom.

## 3. Policy mutation with audit

Every governance mutation produces a tamper-evident audit record. The
policy-service writes the policy and publishes an audit event; the audit-service
hash-chains it into the append-only ledger.

```mermaid
sequenceDiagram
    autonumber
    participant Ops as Operator
    participant Con as admin-console
    participant POL as policy-service
    participant PG as Postgres
    participant Bus as Redpanda
    participant AUD as audit-service

    Ops->>Con: edit policy (JSON-schema validated in UI)
    Con->>POL: PUT /v1/policies/{id}
    POL->>POL: validate document vs policy.schema.json
    POL->>PG: insert new policy_version (immutable history)
    POL->>Bus: publish audit.event.v1 (actor, action, resource)
    Bus->>AUD: consume audit event
    AUD->>AUD: redact (defense-in-depth) + hash-chain
    AUD->>PG: append entry (prev_hash → entry_hash)
    Note over AUD: olm-audit verify recomputes the chain<br/>to prove no row was altered or removed
```

The OSS distribution stores and versions policies and proves the audit chain;
**evaluating** a policy against live traffic is the custom `policy.Evaluator`
provider (interface in OSS, implementation deferred).

## 4. Routing decision (interface + ledger)

OSS ships the decision _ledger_ and a no-op decider; custom ships the real
decider. Both write the same record shape, so the explainability surface is
identical regardless of which provider is registered.

```mermaid
sequenceDiagram
    autonumber
    participant GW as gateway
    participant Dec as routing.Decider<br/>(no-op in OSS / real in custom)
    participant Bus as Redpanda
    participant DS as decision-service
    participant Con as console /decisions

    GW->>Dec: Decide(request envelope, eligible targets)
    Note over Dec: OSS no-op: choose requested target, minimal reason<br/>custom: telemetry-conditioned selection (deferred)
    Dec-->>GW: Decision{provider, model, reason_chain, alternatives}
    Dec->>Bus: routing.decision.v1 (shape only)
    Bus->>DS: consume decision record
    DS->>DS: store input snapshot + output + reason chain
    Con->>DS: render a single decision, human-readable
```

The `routing.decision.v1` schema is a **render contract** — it defines the
fields the console displays, not the selection logic. The reason-chain and
alternatives are opaque JSON owned by whichever decider is registered.

## See also

- [overview.md](./overview.md) — the control loop these sequences implement.
- [data-flow.md](./data-flow.md) — the same flows as a topic pipeline.
- [extension-boundary.md](./extension-boundary.md) — implementing your own
  decider/scorer/evaluator.
- [reconciliation.md](./reconciliation.md) — the drift lifecycle in depth.
