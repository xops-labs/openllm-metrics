# Provider Integration: Azure OpenAI

How the bundled `llm-usage-exporter` resolves `tenancy_id` for Azure OpenAI
and the recommended label-mapping pattern to canonical
`{tenant, team, app, env, project}`.

## How `llm-usage-exporter` identifies Azure OpenAI tenancy

Azure OpenAI usage is billed through an **Azure Subscription** and exposed
via the [Azure Monitor Metrics API](https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/metrics-supported).
The exporter uses an **Azure Service Principal** (client credentials) or
**Managed Identity** to authenticate.

The exporter exposes these metrics with the following upstream labels:

| Upstream label | Source                                                | Example                                           |
| -------------- | ----------------------------------------------------- | ------------------------------------------------- |
| `provider`     | hardcoded                                             | `azure_openai`                                    |
| `tenancy_id`   | Azure Subscription ID + Resource Group + Account name | `sub-xxxxxxxx/rg-prod/my-aoai-acct`               |
| `model`        | resource metric dimension                             | `gpt-4o`, `gpt-4o-mini`, `text-embedding-ada-002` |
| `deployment`   | Azure deployment name                                 | `gpt4o-prod`, `embedding-v3`                      |
| `region`       | Azure region of the resource                          | `eastus`, `westeurope`                            |
| `operation`    | derived                                               | `ChatCompletions`, `Embeddings`                   |

### `tenancy_id` shape for Azure OpenAI

The `tenancy_id` for Azure OpenAI is a composite of:

```
<subscription-id>/<resource-group>/<azure-openai-account-name>
```

Example: `00000000-0000-0000-0000-000000000001/rg-prod-ai/acme-aoai-prod`

This composite is necessary because Azure does not have a single "tenant
identifier" at the Azure OpenAI resource level — the subscription, resource
group, and account name together uniquely identify a billable resource.

## Recommended label-mapping pattern

```yaml
# platform/db/seeds/label-mappings-azure-openai.yaml  (example seed)
- provider: azure_openai
  tenancy_id: 00000000-0000-0000-0000-000000000001/rg-prod-ai/acme-aoai-prod
  tenant: acme
  team: platform-search
  app: rag-svc
  env: prod
  project: acme-search

# Multiple regions, different resource groups
- provider: azure_openai
  tenancy_id: 00000000-0000-0000-0000-000000000001/rg-eu-ai/acme-aoai-eu
  tenant: acme
  team: platform-search
  app: rag-svc
  env: prod
  project: acme-search-eu
```

### Multi-subscription organizations

If your organization uses multiple Azure subscriptions, each subscription
generates its own `tenancy_id` prefix. The label translator handles all
subscriptions; provide one set of service principal credentials per
subscription or use a multi-subscription Managed Identity.

### Deployment vs model distinction

Azure OpenAI uses **deployments** — named instances of a model within an
account. A single model (e.g., `gpt-4o`) can have multiple deployments
(e.g., `gpt4o-prod`, `gpt4o-dev`). The exporter surfaces `deployment` as a
label alongside `model`. The canonical schema carries `model`; `deployment`
appears as an additional dimension in the raw Prometheus series.

## Credential configuration

```env
# Service Principal auth
AZURE_CLIENT_ID=00000000-0000-0000-0000-000000000002
AZURE_CLIENT_SECRET=<secret>
AZURE_TENANT_ID=00000000-0000-0000-0000-000000000003

# Subscription and resource scope
AZURE_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000001
AZURE_RESOURCE_GROUP=rg-prod-ai
AZURE_OPENAI_ACCOUNT=acme-aoai-prod
```

For Managed Identity deployments (AKS, Azure VMs), omit
`AZURE_CLIENT_ID` and `AZURE_CLIENT_SECRET`; the exporter picks up the
ambient credential automatically via the Azure SDK.

## Metrics emitted

| Series                             | Type      | Key dimensions                                                                        |
| ---------------------------------- | --------- | ------------------------------------------------------------------------------------- |
| `llm_estimated_cost_usd`           | counter   | `tenant`, `team`, `app`, `env`, `project`, `provider=azure_openai`, `model`, `region` |
| `gen_ai.client.token.usage`        | histogram | + `gen_ai.token.type=input\|output`                                                   |
| `gen_ai.client.operation.duration` | histogram | + `status`, `error.type`                                                              |

### PTU vs PayGo pricing

Azure OpenAI offers both **Pay-as-you-go** (token-based) and **Provisioned
Throughput Units** (PTU, capacity-based) billing. The pricing catalog at
`platform/pricing/azure-openai.yaml` contains per-token rates for PayGo
deployments. PTU deployments have a fixed hourly cost independent of token
volume; cost estimation for PTU deployments requires a separate capacity-cost
entry in the catalog, which the exporter upstream documents.

## See also

- [`docs/architecture/adopted-components.md`](../adopted-components.md)
- [`docs/architecture/adding-a-provider.md`](../adding-a-provider.md)
- [`platform/pricing/azure-openai.yaml`](../../../platform/pricing/azure-openai.yaml)
- Azure OpenAI pricing: <https://azure.microsoft.com/en-us/pricing/details/cognitive-services/openai-service/>
- Azure Monitor Metrics API: <https://learn.microsoft.com/en-us/azure/azure-monitor/essentials/metrics-supported>
