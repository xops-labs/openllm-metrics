# docs

Documentation beyond the product vision and implementation plan. F-numbers
(`F009`, `F038`, …) reference the
[feature registry](../README.md#features--capabilities--38-modules-across-9-phases)
in the root README.

## Table of Contents

- [Start here](#start-here)
- [Product guides](#product-guides)
- [Folders](#folders)

## Start here

- **[quickstart.md](./quickstart.md)** — launch the full stack locally with one
  `docker compose` command.
- **[architecture/overview.md](./architecture/overview.md)** — the conceptual
  model and system-context diagram; the entry point to the architecture set
  (overview → components → data-flow → sequences → deployment).

## Product guides

- **[product/slo-framework.md](./product/slo-framework.md)** — declaring and
  operating latency / availability / cost SLOs on the shipped metrics.
- **[product/config-reference.md](./product/config-reference.md)** — YAML and
  env-var reference for the F009 poller and F010 aggregator (per-service
  config lives in each service's README).
- **[product/dashboards.md](./product/dashboards.md)** — the Grafana FinOps
  dashboard and Prometheus alert-rules pack.

## Folders

- `architecture`: architecture diagrams and implementation notes — see the
  [architecture index](./architecture/README.md) for the full set (conceptual
  overview, component view, telemetry data flow, key sequences, deployment
  topology, extension boundary, schemas, reconciliation, OTel mapping).
- `decisions`: architecture decision records (ADRs).
- `product`: operator-facing product guides (see [Product guides](#product-guides) above).
- `compliance`: compliance, privacy, and audit-export planning.
