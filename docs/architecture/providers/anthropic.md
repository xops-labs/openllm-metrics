# Provider Integration: Anthropic Claude

How the bundled `llm-usage-exporter` resolves `tenancy_id` for Anthropic and
the recommended label-mapping pattern to canonical
`{tenant, team, app, env, project}`.

## How `llm-usage-exporter` identifies Anthropic tenancy

The upstream exporter authenticates to the
[Anthropic Usage API](https://docs.anthropic.com/en/api/usage) using an
**Anthropic Admin API key**. Each call returns token, cost, and request
metrics scoped to the **Anthropic Organization** (workspace) associated with
the admin key.

The exporter exposes these metrics with the following upstream labels:

| Upstream label | Source                                | Example                                        |
| -------------- | ------------------------------------- | ---------------------------------------------- |
| `provider`     | hardcoded                             | `anthropic`                                    |
| `tenancy_id`   | Anthropic Workspace / Organization ID | `ws-01abc…`                                    |
| `model`        | returned by the Usage API             | `claude-opus-4-5`, `claude-3-5-haiku-20241022` |
| `operation`    | derived from endpoint                 | `messages`                                     |

The `tenancy_id` is the Anthropic **Workspace ID** (sometimes called
Organization ID). One admin key covers one workspace. Teams that use
multiple workspaces should provide one admin key per workspace; the bundled
exporter supports multi-workspace configuration.

### API key types

Anthropic distinguishes between:

- **User API keys** (`sk-ant-api03-...`): scoped to an individual user,
  cannot access organization-wide usage.
- **Admin API keys**: organization-scoped, required for usage reporting.
  The exporter requires an Admin API key.

## Recommended label-mapping pattern

```yaml
# platform/db/seeds/label-mappings-anthropic.yaml  (example seed)
- provider: anthropic
  tenancy_id: ws-01abc123
  tenant: acme
  team: ml-platform
  app: document-qa
  env: prod
  project: acme-docs
```

### Mapping resolution order

1. **Exact match** on `(provider=anthropic, tenancy_id)`.
2. **Fallback**: `team=unknown`, `app=unknown`, `env=unknown`,
   `project=unknown`; `llm_label_translation_unmapped_total{provider="anthropic"}` incremented.

### Per-workspace vs per-team granularity

Anthropic's Usage API reports at the workspace level. If multiple teams share
one workspace, runtime-mode instrumentation via `packages/sdk-*` is the
recommended way to capture per-team and per-app breakdowns, with the F023
reconciler joining the two planes.

## Credential configuration

```env
ANTHROPIC_ADMIN_API_KEY=sk-ant-admin-...
```

The control plane passes this credential directly to the exporter container
environment without reading or persisting it.

## Metrics emitted

| Series                             | Type      | Key dimensions                                                                   |
| ---------------------------------- | --------- | -------------------------------------------------------------------------------- |
| `llm_estimated_cost_usd`           | counter   | `tenant`, `team`, `app`, `env`, `project`, `provider=anthropic`, `model`         |
| `gen_ai.client.token.usage`        | histogram | + `gen_ai.token.type=input\|output` (includes cache-read tokens where available) |
| `gen_ai.client.operation.duration` | histogram | + `status`, `error.type`                                                         |

### Claude-specific token categories

Anthropic's API reports **cache write** and **cache read** tokens separately
from regular input tokens (prompt caching feature). The exporter upstream maps
these to `gen_ai.token.type=input_cache_creation` and
`gen_ai.token.type=input_cache_read` where the API surfaces them. Pricing
differs per category; the `llm_estimated_cost_usd` counter accounts for
cache-tier pricing when the pricing catalog entry includes it.

## See also

- [`docs/architecture/adopted-components.md`](../adopted-components.md)
- [`docs/architecture/adding-a-provider.md`](../adding-a-provider.md)
- [`platform/pricing/anthropic.yaml`](../../../platform/pricing/anthropic.yaml)
- Anthropic Usage API reference: <https://docs.anthropic.com/en/api/usage>
