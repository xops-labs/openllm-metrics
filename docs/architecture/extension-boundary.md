<!-- Copyright (c) 2026 Yasvanth Udayakumar. -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Extension Boundary — Implementing a Provider

OpenLLM Metrics keeps the **integration surface** of its decisioning features
(scoring, routing, policy, fallback) safe while keeping production algorithms out of shared service code. This is the contributor's guide to that boundary: the public Go
interfaces, the safe no-op defaults, how to register your own implementation,
and what belongs in the public interface versus implementation-specific code.

For where these hooks fire, see [sequences.md](./sequences.md).

## Table of Contents

- [The model](#the-model)
- [The four interfaces](#the-four-interfaces)
- [Registering a provider](#registering-a-provider)
- [Worked example: a custom scoring provider](#worked-example-a-custom-scoring-provider)
- [What is OSS-safe vs deferred](#what-is-oss-safe-vs-deferred)
- [Stability & versioning](#stability--versioning)
- [See also](#see-also)

## The model

Every decisioning feature is structured in three layers:

1. **A public interface** in
   [`packages/extensions/go/<feature>/`](../../packages/extensions/go/) —
   inputs, outputs, error semantics. Apache-2.0, in this repo.
2. **A safe no-op default** that satisfies the interface so the OSS
   distribution runs standalone. Apache-2.0, in this repo.
3. **A production implementation** registered at boot via `registry.Use(...)`.
   Yours.

Services depend only on layer 1+2. They never import a production
implementation directly; they call the registry accessor and get whatever was
wired at boot. Swapping behavior is a boot-time concern, not a code change in
the data plane.

## The four interfaces

All four live under [`packages/extensions/go`](../../packages/extensions/go) and
share three rules: implementations must be **deterministic** at a given
`RuleVersion`, must **never** receive raw prompt/completion content (only
`InputsHash` + routing-relevant context), and must **reject cross-tenant**
behavior by construction.

| Package    | Interface    | Method                                    | No-op default                              |
| ---------- | ------------ | ----------------------------------------- | ------------------------------------------ |
| `scoring`  | `Provider`   | `Score(ctx, kind, target) (Score, error)` | returns `1.0` (every target healthy)       |
| `routing`  | `Decider`    | `Decide(ctx, req) (Decision, error)`      | returns the first candidate                |
| `policy`   | `Evaluator`  | `Evaluate(ctx, req) (Decision, error)`    | `VerdictAllow`, reason `oss-default-allow` |
| `fallback` | `Controller` | `Next(ctx, req) (Decision, error)`        | `Stop=true` (no fallback)                  |

Common output fields — `Reason` and `RuleVersion` — are mandatory on every
decision so the audit ledger (F031) and the
decision-explainability surface (F036)
can attribute an outcome to a specific rule version.

## Registering a provider

The [`registry`](../../packages/extensions/go/registry/) package is the single
boot-time wiring point. Any `nil` field falls back to the matching no-op.

```go
// OSS service binary — all no-ops, runs standalone:
registry.Use(registry.Defaults())

// Your binary — wire your own implementations (nil fields stay no-op):
registry.Use(registry.Providers{
    Scoring: myscoring.New(...),
    // Routing, Policy, Fallback omitted → remain no-op defaults
})
```

Data-plane code reads the active provider through the accessors
(`registry.Scoring()`, `registry.Routing()`, `registry.Policy()`,
`registry.Fallback()`), which are safe for concurrent use.

## Worked example: a custom scoring provider

A minimal provider that scores reliability from your own source. It compiles
against the public module alone — no additional dependency.

```go
package myscoring

import (
    "context"

    "github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/scoring"
)

type Provider struct{ /* your data sources */ }

func New() *Provider { return &Provider{} }

func (p *Provider) Score(ctx context.Context, kind scoring.Kind, t scoring.Target) (scoring.Score, error) {
    // Compute a bounded [0,1] value however you like. Clamp before returning.
    value := 0.95
    return scoring.Score{
        Kind:        kind,
        Target:      t,
        Value:       value,
        RuleVersion: "myscoring-v1", // record which formula produced the value
        InputsHash:  "",             // hash of inputs, for explainability
    }, nil
}
```

Wire it at boot:

```go
registry.Use(registry.Providers{Scoring: myscoring.New()})
```

That is the whole contribution contract. The same shape applies to `routing`,
`policy`, and `fallback` — implement the one method, return the mandatory
`Reason` + `RuleVersion`, register at boot.

## What is OSS-safe vs deferred

**OSS-safe (publish freely):** interface signatures, struct fields, enum
values, error sentinels, the _semantics_ of inputs and outputs, no-op behavior,
and the _existence and shape_ of the events these providers emit
(`routing.decision.v1`, scoring gauges, etc.).

**Out of scope by default:** how a
score, route, verdict, or fallback is actually computed — formula weights,
factor lists, decay schedules, tie-break rules, target-eligibility filters,
policy precedence, fallback ordering, activation thresholds, anomaly baselines.

The rule of thumb: documenting _that_ a routing decision event exists and _what
fields_ it carries is fine; documenting _how_ the decision was reached is not.
See `CONTRIBUTING.md` for the full policy.

## Stability & versioning

The extension module is **public API**, versioned by Go-module semver. Breaking
an interface (removing a method, changing a signature, removing an enum value)
requires a new major version (`v2`) and is a stop-and-ask gate. See [schemas.md](./schemas.md#6-extension-interface-module-packagesextensionsgo).

## See also

- [`packages/extensions/go/README.md`](../../packages/extensions/go/README.md) —
  the package-level reference and no-op semantics.
- [sequences.md](./sequences.md) — where the routing and policy hooks fire.
