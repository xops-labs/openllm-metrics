<!-- Copyright (c) 2026 Yasvanth Udayakumar. -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# OpenLLM Metrics Extension Interfaces

Go interfaces that define the contract between the open-source OpenLLM Metrics
platform and pluggable scoring, routing, policy, and fallback implementations.

The open-source distribution registers safe **no-op default** implementations of
every interface so the platform runs standalone. Deployments that need custom
behavior can register their own implementations at boot.

## Why this exists

The following features are modeled as pluggable interfaces:

- F024 Reliability Scoring Engine
- F025 Cost-Efficiency Scoring Engine
- F030 Policy and Budget Evaluator
- F034 Routing Decision Engine
- F035 Bounded Fallback Engine

This package keeps the **integration surface** of those features stable:
interfaces, inputs, outputs, error semantics, and safe defaults. It does not
prescribe the algorithms a deployment may register.

## Packages

| Package    | Interface (method)                                      | Default                                                         |
| ---------- | ------------------------------------------------------- | --------------------------------------------------------------- |
| `scoring`  | `Provider` (`Score`)                                    | No-op: returns 1.0 (treat every target as healthy)              |
| `routing`  | `Decider` (`Decide`)                                    | No-op: returns the first candidate                              |
| `policy`   | `Evaluator` (`Evaluate`)                                | No-op: every request `VerdictAllow`, reason `oss-default-allow` |
| `fallback` | `Controller` (`Next`)                                   | No-op: `Stop=true`; original error returned                     |
| `registry` | Boot-time registry binaries use to wire implementations | -                                                               |

> See [`docs/architecture/extension-boundary.md`](../../../docs/architecture/extension-boundary.md)
> for a worked example of implementing and registering a provider. The packages
> above are the complete interface set.

## How to use

Services and binaries depend only on this package. At process boot, a service
calls `registry.Use(...)` with whatever implementation it wants. The default
binary registers no-ops; custom deployments can register their own providers.

```go
registry.Use(registry.Defaults())

registry.Use(registry.Providers{
    Scoring:  customscoring.New(...),
    Routing:  customrouting.New(...),
    Policy:   custompolicy.New(...),
    Fallback: customfallback.New(...),
})
```

## Stability

This package is **public API**. Breaking changes follow semver; never break the
wire without a major bump.

## Scope

This package contains interfaces and safe defaults. If you want to add new
scoring weights, routing rules, policy formulas, or fallback ordering to the
open-source repo, open a design issue first so the public contract and test
strategy can be reviewed.
