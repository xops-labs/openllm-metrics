# Provider Integration: AWS Bedrock

How the bundled `llm-usage-exporter` resolves `tenancy_id` for AWS Bedrock
and the recommended label-mapping pattern to canonical
`{tenant, team, app, env, project}`.

## How `llm-usage-exporter` identifies Bedrock tenancy

AWS Bedrock usage is billed through an **AWS Account** and exposed via the
[AWS Cost and Usage Report (CUR)](https://docs.aws.amazon.com/cur/latest/userguide/what-is-cur.html)
or [AWS CloudWatch Bedrock metrics](https://docs.aws.amazon.com/bedrock/latest/userguide/monitoring.html).
The exporter uses an **IAM Role** (cross-account or same-account) with the
required `bedrock:*` and `cloudwatch:GetMetricData` permissions.

The exporter exposes these metrics with the following upstream labels:

| Upstream label | Source           | Example                                                          |
| -------------- | ---------------- | ---------------------------------------------------------------- |
| `provider`     | hardcoded        | `bedrock`                                                        |
| `tenancy_id`   | AWS Account ID   | `123456789012`                                                   |
| `model`        | Bedrock model ID | `anthropic.claude-opus-4-5-v1:0`, `amazon.titan-text-express-v1` |
| `region`       | AWS region       | `us-east-1`, `eu-west-1`                                         |
| `operation`    | derived from API | `InvokeModel`, `InvokeModelWithResponseStream`                   |

The `tenancy_id` is the **AWS Account ID** (12-digit number). This is the
natural billing boundary in AWS: all Bedrock charges roll up to the account.

### Multi-account organizations

AWS Organizations with separate accounts per team or environment produce
distinct `tenancy_id` values per account. This is the cleanest isolation
model. The exporter supports an **assume-role chain** so a single
exporter deployment can pull usage across multiple accounts without storing
long-lived credentials per account.

### Cost allocation tags

AWS Bedrock does not surface cost allocation tags at the CloudWatch metric
level. If you use AWS Cost Categories or tagging for internal chargeback, the
FOCUS endpoint (`/focus.json`) of the exporter aggregates CUR data that does
carry tag dimensions. The F023 reconciler joins CUR-sourced cost records with
runtime events to surface per-tag breakdowns in the reconciliation drift panel.

## Recommended label-mapping pattern

```yaml
# platform/db/seeds/label-mappings-bedrock.yaml  (example seed)

# Single-account org — map by account ID
- provider: bedrock
  tenancy_id: '123456789012'
  tenant: acme
  team: ml-platform
  app: content-gen
  env: prod
  project: acme-content

# Second AWS account for a different team
- provider: bedrock
  tenancy_id: '234567890123'
  tenant: acme
  team: customer-success
  app: support-bot
  env: prod
  project: acme-support
```

### Granularity within one AWS account

Bedrock CloudWatch metrics can be filtered by `ModelId` and
`InvocationLatency` but not by application or team within the same account.
Options for finer granularity:

- **Use separate AWS accounts per team**: recommended for large organizations;
  cleanest cost isolation.
- **Runtime instrumentation**: `packages/sdk-*` propagates `team`, `app`,
  and `project` as OTel resource attributes on every Bedrock call. The F023
  reconciler joins these with account-level CUR records.
- **AWS resource tags on IAM roles**: if each application uses a distinct
  assumed role, future versions of the exporter may be able to surface
  role-level cost breakdowns from CUR.

## Credential configuration

```env
# IAM Role ARN to assume for Bedrock usage access
BEDROCK_ROLE_ARN=arn:aws:iam::123456789012:role/openllm-bedrock-reader

# AWS region where Bedrock usage metrics are collected
BEDROCK_REGION=us-east-1

# Optional: external ID for cross-account role assumption
BEDROCK_ROLE_EXTERNAL_ID=openllm-metrics-reader
```

For same-account access on ECS or EC2 with a task/instance role, omit
`BEDROCK_ROLE_ARN` and the exporter uses the ambient credential from the
instance metadata service (IMDS).

## IAM policy

The exporter needs the following minimum permissions on the target account:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["bedrock:GetUsage", "cloudwatch:GetMetricData", "cloudwatch:ListMetrics"],
      "Resource": "*"
    }
  ]
}
```

The policy is read-only. The exporter never makes `InvokeModel` calls.

## Metrics emitted

| Series                             | Type      | Key dimensions                                                                   |
| ---------------------------------- | --------- | -------------------------------------------------------------------------------- |
| `llm_estimated_cost_usd`           | counter   | `tenant`, `team`, `app`, `env`, `project`, `provider=bedrock`, `model`, `region` |
| `gen_ai.client.token.usage`        | histogram | + `gen_ai.token.type=input\|output`                                              |
| `gen_ai.client.operation.duration` | histogram | + `status`, `error.type`                                                         |

### On-demand vs Provisioned Throughput

AWS Bedrock supports both **on-demand** (token-based) and **Provisioned
Throughput** (capacity-based) pricing. The pricing catalog
(`platform/pricing/bedrock.yaml`) covers on-demand rates. Provisioned
Throughput has an hourly rate independent of token volume; the catalog
supports a capacity-hour entry type for this case.

## See also

- [`docs/architecture/adopted-components.md`](../adopted-components.md)
- [`docs/architecture/adding-a-provider.md`](../adding-a-provider.md)
- [`platform/pricing/bedrock.yaml`](../../../platform/pricing/bedrock.yaml)
- AWS Bedrock pricing: <https://aws.amazon.com/bedrock/pricing/>
- AWS Bedrock CloudWatch metrics: <https://docs.aws.amazon.com/bedrock/latest/userguide/monitoring.html>
