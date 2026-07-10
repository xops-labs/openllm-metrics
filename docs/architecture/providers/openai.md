# Provider Integration: OpenAI

How the bundled `llm-usage-exporter` resolves `tenancy_id` for OpenAI and
the recommended label-mapping pattern to canonical
`{tenant, team, app, env, project}`.

## How `llm-usage-exporter` identifies OpenAI tenancy

The upstream exporter authenticates to the
[OpenAI Usage API](https://platform.openai.com/docs/api-reference/usage) using
an **OpenAI Admin API key** (format `sk-admin-...`). Each call to
`GET /v1/usage` returns cost and token metrics scoped to the **organization**
associated with that admin key.

The exporter exposes these metrics with the following upstream labels:

| Upstream label | Source                                                                 | Example                          |
| -------------- | ---------------------------------------------------------------------- | -------------------------------- |
| `provider`     | hardcoded                                                              | `openai`                         |
| `tenancy_id`   | OpenAI Organization ID (`org-...`) extracted from the API key metadata | `org-abc123`                     |
| `model`        | returned by the Usage API                                              | `gpt-4o`, `gpt-4o-mini`          |
| `operation`    | derived from Usage API endpoint                                        | `chat.completions`, `embeddings` |

The `tenancy_id` is the OpenAI **Organization ID** — a string of the form
`org-<alphanumeric>`. One admin key covers exactly one organization. If you
need to track multiple OpenAI organizations as separate tenants, run one
exporter instance per admin key (supported by the bundled compose service via
multi-credential environment variables).

## Recommended label-mapping pattern

The label-translator worker maps the upstream `{provider, tenancy_id}` tuple to
the canonical `{tenant, team, app, env, project}` label set using the
`label_mappings` table in the control-plane Postgres database.

### Mapping table seed example

```yaml
# platform/db/seeds/label-mappings-openai.yaml  (example seed)
- provider: openai
  tenancy_id: org-abc123
  tenant: acme
  team: platform-search
  app: rag-svc
  env: prod
  project: acme-search
```

### Mapping resolution order

1. **Exact match** — `(provider=openai, tenancy_id=org-abc123)` resolves to
   the configured `{tenant, team, app, env, project}` row.
2. **Provider wildcard** — if no exact match exists, the translator emits
   `team=unknown`, `app=unknown`, `env=unknown`, `project=unknown` and
   increments `llm_label_translation_unmapped_total{provider="openai"}`.

### Label cardinality notes

OpenAI's Usage API reports at the **organization** granularity — it does not
break down by individual API key or project within the organization. If your
OpenAI organization contains multiple teams sharing a single admin key, you
cannot distinguish team or app at the `tenancy_id` level alone. Mitigation
options:

- Use OpenAI **Projects**: the Usage API will eventually support per-project
  breakdowns. The exporter upstream will add support once the API is stable;
  track [xops-labs/llm-usage-exporter](https://github.com/xops-labs/llm-usage-exporter).
- Instrument applications with the runtime-mode SDK (`packages/sdk-*`) so
  per-call events carry `team`, `app`, and `project` context from the request
  side. Pull-mode and runtime-mode signals are reconciled by the F023 worker.

## Credential configuration

Pass the admin key as an environment variable to the bundled exporter service:

```env
OPENAI_ADMIN_API_KEY=sk-admin-...
```

The exporter never echoes the key in logs, metrics labels, or FOCUS records.
The control plane forwards credentials to the exporter container without
reading, persisting, or tracing them. See
`docs/architecture/bundled-vs-external.md` for the pass-through credential
contract.

## Metrics emitted

After label translation the following canonical series are available:

| Series                             | Type      | Key dimensions                                                        |
| ---------------------------------- | --------- | --------------------------------------------------------------------- |
| `llm_estimated_cost_usd`           | counter   | `tenant`, `team`, `app`, `env`, `project`, `provider=openai`, `model` |
| `gen_ai.client.token.usage`        | histogram | + `gen_ai.token.type=input\|output`                                   |
| `gen_ai.client.operation.duration` | histogram | + `status`, `error.type`                                              |

## See also

- [`docs/architecture/adopted-components.md`](../adopted-components.md)
- [`docs/architecture/adding-a-provider.md`](../adding-a-provider.md)
- [`platform/adoption/llm-usage-exporter.version`](../../../platform/adoption/llm-usage-exporter.version)
- OpenAI Usage API reference: <https://platform.openai.com/docs/api-reference/usage>
