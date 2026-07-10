# Adding a New LLM Provider

OpenLLM Metrics gets its pull-mode (billing/usage) telemetry from the
upstream [`llm-usage-exporter`](https://github.com/xops-labs/llm-usage-exporter).
Adding a sixth (or seventh, or nth) provider follows an **upstream-first**
path: the adapter belongs in the upstream project, not in this repository.
This document explains why, and walks through every step.

## Why upstream-first?

The hard constraint for the bundled `llm-usage-exporter`:

> Do not modify, vendor, or fork `llm-usage-exporter` source. The integration
> is composition-only: container image, scrape config, FOCUS endpoint
> consumption. If a change is needed in the exporter, open an upstream PR —
> never patch downstream.

This constraint exists for two reasons:

1. **License integrity.** The exporter is Apache-2.0 open source. Patching
   its binary downstream would effectively create an undisclosed fork —
   bad for the community and potentially bad for license compliance.
2. **Upgrade cadence.** The bundled exporter is pinned at a single version
   in `platform/adoption/llm-usage-exporter.version`. A downstream patch
   would have to be re-applied on every pin bump, creating maintenance debt
   and divergence risk.

The upstream-first path keeps this repository's integration surface clean:
we consume a sealed artifact, we never modify it.

## Step-by-step guide

### Step 1: Assess whether the provider is already partially supported

Before opening a PR, check:

1. Does `llm-usage-exporter` already have an adapter for this provider, even
   if incomplete? Search issues and PRs at
   <https://github.com/xops-labs/llm-usage-exporter>.
2. Does the provider expose a usage API with token counts and cost? If not,
   only latency and request counts may be available. Document this in your
   upstream PR.
3. Does the provider have a FOCUS-compatible billing export? If yes, the F023
   reconciler can join cost records without any changes to this repo.

### Step 2: Propose the adapter upstream

Open a GitHub issue in `xops-labs/llm-usage-exporter` describing:

- The provider name and API endpoint.
- The authentication mechanism (API key, OAuth, service account, etc.).
- The usage API response shape (fields, types, granularity).
- The FOCUS billing export availability (if any).
- The `tenancy_id` shape — what uniquely identifies a billing boundary for
  this provider.

Once the issue is acknowledged, submit a pull request implementing the adapter
following the upstream project's contribution guidelines.

### Step 3: Update the upstream pin (once the upstream PR merges)

After the upstream PR merges and a new release is cut:

1. Update `platform/adoption/llm-usage-exporter.version` to the new release
   tag.
2. Update the image tag in `platform/deployment/compose/quickstart.yml` to
   match.
3. Verify the cosign signature of the new upstream image against the
   `xops-labs` Sigstore identity.
4. Run the integration suite: label-translator round-trip, FOCUS ingester
   schema round-trip, dashboard render.

This is a **one-PR change** to this repository. It should not require
touching any Go, TypeScript, or schema files — only the version pin.

### Step 4: Add a mapping doc in this repository

Create `docs/architecture/providers/<provider-slug>.md` following the
structure of the existing provider docs:

- How the exporter identifies `tenancy_id` for this provider.
- The recommended `label_mappings` table seed pattern.
- Credential configuration environment variables.
- Metrics emitted after label translation.
- Provider-specific cardinality and granularity notes.
- Links to the upstream API documentation and pricing page.

The doc lands in this repository because it describes the integration
contract between the bundled exporter and this platform — not the exporter's
own internals.

### Step 5: Validate end-to-end

Configure one tenancy for the new provider in `llm-usage-exporter`.
Verify:

- Events arrive in the bus via the label translator with the canonical schema.
- The label translator resolves `tenancy_id` to the expected
  `{tenant, team, app, env, project}` from the mapping table.
- The Prometheus dashboards show the new provider side-by-side with the
  existing ones.
- No `llm_label_translation_unmapped_total` spikes for this provider.

## Runtime-mode coverage (no upstream required)

The steps above cover **pull mode** (billing/usage API polling). If you want
**runtime-mode** coverage (per-call latency, token counts from the request
path rather than the billing API), instrument applications with one of the
language SDKs in `packages/sdk-*`. Runtime-mode instrumentation emits events
directly to the bus and does not require any changes to the exporter.

Runtime-mode and pull-mode signals are reconciled by the F023 worker. The
two planes complement each other: pull mode gives you accurate billing cost;
runtime mode gives you sub-second latency and per-request context.

## What NOT to do

- Do not add a provider-specific poller to this repository. The exporter is
  the canonical home for provider adapters. Adding pollers here would split
  the integration surface and violate the bundling contract.
- Do not fork or patch the upstream exporter image. See the constraint above.
- Do not submit a PR to this repository that adds a provider Go package.
  The correct PR destination is `xops-labs/llm-usage-exporter`.

## See also

- [`docs/architecture/adopted-components.md`](./adopted-components.md) —
  overall adoption rationale and the upstream-PR-only modification rule.
- [`docs/architecture/bundled-vs-external.md`](./bundled-vs-external.md) —
  the bundling decision and what it does and does not mean.
- [`platform/adoption/llm-usage-exporter.version`](../../platform/adoption/llm-usage-exporter.version) —
  the version pin file.
- Existing provider docs: [`providers/`](./providers/)
