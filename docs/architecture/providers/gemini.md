# Provider Integration: Google Gemini / Vertex AI

How the bundled `llm-usage-exporter` resolves `tenancy_id` for Google
Gemini (AI Studio) and Vertex AI, and the recommended label-mapping pattern
to canonical `{tenant, team, app, env, project}`.

## Two Google LLM surfaces

Google exposes Gemini models through two distinct APIs with different
authentication and billing models:

| Surface              | API                                 | Auth                                | Billing                                           |
| -------------------- | ----------------------------------- | ----------------------------------- | ------------------------------------------------- |
| **Gemini AI Studio** | `generativelanguage.googleapis.com` | API key                             | Per-key billing, linked to a Google Cloud project |
| **Vertex AI**        | `aiplatform.googleapis.com`         | Service account / Workload Identity | Google Cloud project billing                      |

The exporter handles both surfaces. The `tenancy_id` shape differs.

## Gemini AI Studio

The exporter authenticates with a **Gemini API key** and polls usage from the
Google Cloud Billing API or the AI Studio usage endpoint.

| Upstream label | Source                  | Example                              |
| -------------- | ----------------------- | ------------------------------------ |
| `provider`     | hardcoded               | `gemini`                             |
| `tenancy_id`   | Google Cloud Project ID | `my-project-123456`                  |
| `model`        | usage API               | `gemini-1.5-pro`, `gemini-2.0-flash` |
| `operation`    | derived                 | `generateContent`, `embedContent`    |

The `tenancy_id` is the **Google Cloud Project ID** associated with the
API key. One API key maps to one project.

## Vertex AI

The exporter authenticates using a **Google Cloud Service Account** with the
`roles/aiplatform.user` and `roles/billing.viewer` IAM roles.

| Upstream label | Source                    | Example                                    |
| -------------- | ------------------------- | ------------------------------------------ |
| `provider`     | hardcoded                 | `vertex_ai`                                |
| `tenancy_id`   | Google Cloud Project ID   | `my-project-123456`                        |
| `region`       | Vertex AI endpoint region | `us-central1`, `europe-west4`              |
| `model`        | usage API                 | `gemini-1.5-pro-001`, `text-embedding-004` |
| `operation`    | derived                   | `generateContent`, `predict`               |

For Vertex AI, the `tenancy_id` is still the **Google Cloud Project ID**.
Multiple regions within one project share the same `tenancy_id`; the `region`
label differentiates them.

## Recommended label-mapping pattern

```yaml
# platform/db/seeds/label-mappings-gemini.yaml  (example seed)

# Gemini AI Studio mapping
- provider: gemini
  tenancy_id: my-genai-project-prod
  tenant: acme
  team: data-science
  app: summarizer
  env: prod
  project: acme-ds

# Vertex AI mapping (same project, different provider label)
- provider: vertex_ai
  tenancy_id: my-genai-project-prod
  tenant: acme
  team: data-science
  app: summarizer
  env: prod
  project: acme-ds
```

### Multi-project organizations

Large organizations typically have one GCP project per team or per
environment. Create one mapping row per project. The label translator
resolves each `tenancy_id` independently.

### Granularity within a project

If a single GCP project hosts multiple applications or teams, the
`tenancy_id`-level breakdown will not distinguish them. Options:

- **Use separate GCP projects per team**: cleanest billing isolation.
- **Runtime instrumentation**: the SDK (`packages/sdk-*`) propagates
  `team`, `app`, and `project` as OTel baggage; the F023 reconciler
  joins runtime events with pull-mode cost records.

## Credential configuration

```env
# For Gemini AI Studio
GEMINI_API_KEY=AIza...

# For Vertex AI (path to service-account JSON or Workload Identity)
GOOGLE_APPLICATION_CREDENTIALS=/run/secrets/vertex-sa.json
VERTEX_PROJECT_ID=my-genai-project-prod
VERTEX_LOCATION=us-central1
```

## Metrics emitted

| Series                             | Type      | Key dimensions                                                                             |
| ---------------------------------- | --------- | ------------------------------------------------------------------------------------------ |
| `llm_estimated_cost_usd`           | counter   | `tenant`, `team`, `app`, `env`, `project`, `provider=gemini\|vertex_ai`, `model`, `region` |
| `gen_ai.client.token.usage`        | histogram | + `gen_ai.token.type=input\|output`                                                        |
| `gen_ai.client.operation.duration` | histogram | + `status`, `error.type`                                                                   |

## See also

- [`docs/architecture/adopted-components.md`](../adopted-components.md)
- [`docs/architecture/adding-a-provider.md`](../adding-a-provider.md)
- [`platform/pricing/gemini.yaml`](../../../platform/pricing/gemini.yaml)
- Vertex AI pricing: <https://cloud.google.com/vertex-ai/generative-ai/pricing>
- Gemini AI Studio pricing: <https://ai.google.dev/pricing>
