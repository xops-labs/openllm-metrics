<!-- Copyright (c) 2026 Yasvanth Udayakumar. -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Architecture

System diagrams, component boundaries, schema and adapter contracts, and the
cross-cutting design of OpenLLM Metrics. Diagrams are Mermaid (GitHub renders
them inline) and kept next to the prose that explains them.

## Start here

New to the codebase? Read these in order:

1. **[overview.md](./overview.md)** — conceptual model, design invariants, and
   the system-context (C1) diagram. The control loop in one picture.
2. **[components.md](./components.md)** — the container/component (C2) view and
   the service catalog: what every binary does and on which port.
3. **[data-flow.md](./data-flow.md)** — the telemetry pipeline: capture sources,
   bus topics, the canonical event, and how a signal becomes a metric.
4. **[sequences.md](./sequences.md)** — request-level sequence diagrams (proxy
   capture, reconciliation, policy+audit, routing decision).
5. **[deployment.md](./deployment.md)** — Compose profiles, host port map, and
   the Kubernetes/Helm topology.

## Boundaries & contracts

- **[extension-boundary.md](./extension-boundary.md)** — how to implement a
  custom scoring/routing/policy/fallback provider against the public interfaces.
- **[schemas.md](./schemas.md)** — versioning rules for every cross-service
  contract (bus events, Prometheus series, SLO schema, Postgres, REST,
  extension module).
- **[otel-genai-mapping.md](./otel-genai-mapping.md)** — how `llm_*` metrics map
  onto and extend the OpenTelemetry GenAI semantic conventions.

## Subsystems & integrations

- **[reconciliation.md](./reconciliation.md)** — runtime-estimate vs billed-cost
  drift: math, window lifecycle, alert recipes.
- **[adopted-components.md](./adopted-components.md)** — upstream components the
  platform adopts (and the upstream-PR-only modification rule).
- **[bundled-vs-external.md](./bundled-vs-external.md)** — the bundled
  `llm-usage-exporter` rationale (optional pull-mode add-on).
- **[adding-a-provider.md](./adding-a-provider.md)** — the upstream-first path to
  add a sixth provider.
- **[providers/](./providers/)** — per-provider integration notes (OpenAI,
  Anthropic, Gemini/Vertex, Azure OpenAI, Bedrock).

## Conventions

- Prefer Mermaid for diagrams; keep each diagram in the doc that explains it.
- Keep diagrams renderable on GitHub — favor `flowchart`, `sequenceDiagram`,
  and `erDiagram` over DSLs that may not render.
- Document the **shape** of events and the **existence** of decisions without baking production algorithms into shared contracts.
