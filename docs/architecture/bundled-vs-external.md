# Bundled vs External: The `llm-usage-exporter` Decision

OpenLLM Metrics adopts the upstream
[`llm-usage-exporter`](https://github.com/xops-labs/llm-usage-exporter) as
its pull-mode foundation. For most
of the project's first weeks the operating assumption was that the
exporter would run as a **customer-managed peer** — installed and
upgraded independently of this product. We have moved off that
assumption. The exporter now ships **inside** the product as an internal
compose service, pulled from a pinned upstream image. This document
captures why that decision is the right one and, more importantly, why it
does not contradict the long-standing constraint that we never modify the
exporter source.

> **Direction update (v0.1.0) — superseding the bundle-by-default stance below.**
> This document originally argued for bundling the exporter as the _default_
> deployment. That is no longer the plan. The product is now **runtime-first**:
> the in-repo Go gateway + SDKs provide provider telemetry with no external
> exporter, and pull-mode (this exporter) is an **optional add-on** enabled with
> `docker compose --profile exporter up -d` and a bring-your-own image
> (`LLM_USAGE_EXPORTER_IMAGE`). The motivation is fewer dependencies and less
> complexity in the default product. The **source-boundary** principle in this
> document still holds — we do **not** vendor or fork the exporter; the optional
> path consumes a published image. The sections below are retained for the
> bundling rationale and the no-vendor rule; read "bundled" as "the optional
> pull-mode add-on" rather than "the default deployment."

## The decision

The `llm-usage-exporter` runs as an internal service of this product, not
as a customer-managed peer. Customers deploy one thing — the OpenLLM
Metrics stack — and the exporter is wired in for them. The boundary
between this repo and the upstream project is **not** that customers
assemble the two; it is that this repo consumes a sealed upstream image
and never patches its source.

## Two constraints, one decision

The bundling decision sits at the intersection of two constraints that
sound contradictory until you separate the **source boundary** from the
**deployment boundary**.

**Constraint 1: we don't modify the exporter source.** This was the
correct rule before bundling and it remains the correct rule after.
Composition is one-way: we consume the exporter as a binary, we pass it
configuration, we scrape its endpoints. We do not vendor its C# tree, we
do not maintain a downstream patch set, and we do not ship a forked
image. If a behaviour change is needed, the path is an upstream PR — not
a private rewrite.

**Constraint 2: customers should not assemble a stack of tools.** This
was true before bundling but it was unmet. With the exporter as a peer,
the integration story was "install the exporter, install OpenLLM
Metrics, wire them together, and hope the versions match." That is
correct architecture and bad adoption ergonomics. With the exporter
bundled, the integration story is "install OpenLLM Metrics" — one
artifact, one upgrade cadence, one set of release notes.

The two constraints are compatible because they apply at different
boundaries:

- **Source boundary** (unchanged): the upstream repository owns the
  exporter's code, tests, and release process. This repository never
  carries a downstream fork or a vendored copy.
- **Deployment boundary** (this change): the upstream **artifact** is
  bundled into this product's compose, Helm chart, and release pipeline
  at a pinned version. The customer sees one product.

Bundling moved the integration responsibility off the customer and onto
this repo, without moving the source responsibility off the upstream
project.

## What "bundled" means technically

- **Pinned upstream image.** `platform/deployment/compose/quickstart.yml`
  (and the future Helm chart) references
  `ghcr.io/xops-labs/llm-usage-exporter:<version>` where `<version>` is
  the string in `platform/adoption/llm-usage-exporter.version`. The pin
  is single-sourced; the deployment surface reads it, no other config
  file restates it.
- **Internal Docker network.** The exporter binds to the
  `openllm_quickstart` network alongside the pollers and workers. The
  label-translator and focus-ingester scrape
  `http://llm-usage-exporter:9090/metrics` and poll
  `http://llm-usage-exporter:9090/focus.json` over that internal network.
  No customer-side reverse proxy, port forward, or service-mesh entry
  is required for the integration to work.
- **Pass-through credentials.** Provider API keys (OpenAI admin key,
  Anthropic admin key, Azure subscription, Bedrock role, Vertex service
  account) are surfaced as environment variables on the compose service
  and the Helm chart. This product forwards them to the bundled
  exporter; it does not read, log, trace, or persist them. The same
  rule applies to the all-in-one Helm chart once it lands.
- **Sigstore-verified pull.** The release workflow verifies the upstream
  image's cosign signature against the xops-labs Sigstore identity
  before publishing this product's release. If the upstream signature
  fails to verify, the release does not ship.
- **Version single-sourced in one file.** The pinned tag lives in
  `platform/adoption/llm-usage-exporter.version`. The compose file, the
  Helm chart, the release workflow, and the SBOM generator all read
  from that one string. Bumping the pin is a one-line PR.

## What "bundled" does NOT mean

- **Not vendored source.** The upstream C# tree is not copied into this
  repo. No `third_party/llm-usage-exporter/` directory exists; no Git
  submodule references it. The integration is purely at the artifact
  layer.
- **Not forked.** No `xops-labs/llm-usage-exporter` fork is maintained
  under this project's org. We do not carry a downstream branch.
- **Not patched downstream.** If a behaviour change is needed, the
  channel is an upstream PR (`https://github.com/xops-labs/llm-usage-exporter/pulls`).
  We do not apply patches at image-build time, runtime, or via sidecar
  injection. Bundling is composition, not customization.
- **Not built from source.** The release workflow pulls the published
  image by tag and digest; it does not check out the upstream tree and
  rebuild. SBOM and provenance lineage come from the upstream artifact,
  not from this repo.
- **Not a license relicense.** The upstream license terms apply to the
  bundled binary unchanged. This product's Apache-2.0 license covers
  this repo's own code (gateway, workers, schemas, dashboards, admin
  console shell). The bundling does not relicense the exporter and does
  not assert ownership over upstream code.

## Upgrade path

When upstream cuts a new release, this repo bumps the pin in **one PR**:

1. Update the version string in `platform/adoption/llm-usage-exporter.version`.
2. Update the image tag in `platform/deployment/compose/quickstart.yml`
   and in the Helm chart `values.yaml` once it exists.
3. Re-verify the cosign signature against the new upstream tag.
4. Run the integration suite against the new image (label translator
   round-trip, FOCUS ingester schema round-trip, dashboard render).
5. Note the upstream version in this repo's release notes.

There is no merge step, no patch step, no rebase against upstream. The
pin is the only mutation.

## For users running the exporter standalone

The upstream project at
[`xops-labs/llm-usage-exporter`](https://github.com/xops-labs/llm-usage-exporter)
remains independently usable. Teams that already deploy the exporter as
a standalone Prometheus exporter — feeding their own Grafana, their own
alerting, their own pipeline — keep that deployment shape with no
change. The bundling decision is about the **default deployment story of
this product**, not about the standalone availability of the exporter.

A site can still:

- Run the upstream exporter only, on its own schedule, without any of
  OpenLLM Metrics.
- Run OpenLLM Metrics with its bundled exporter, ignoring any separately
  installed instance.
- Run OpenLLM Metrics and disable the bundled exporter in favor of an
  externally managed one — the compose and Helm surfaces expose the
  exporter endpoint as a configurable URL, so this is supported by
  configuration, not by code change.

The upstream remains the source of record. This product is one consumer
among many.

## See also

- [`docs/architecture/extension-boundary.md`](./extension-boundary.md) —
  how the bundled exporter sits inside the broader open-source
  split.
- [`docs/architecture/reconciliation.md`](./reconciliation.md) — how the
  bundled exporter's FOCUS endpoint feeds the F023 reconciler.
- [`platform/adoption/README.md`](../../platform/adoption/README.md) —
  the pin file's home and the upgrade-path companion.
