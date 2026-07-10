# openllm-metrics Helm chart

Phase 1 chart for OpenLLM Metrics. Installs the F009 OpenAI poller and the
F010 `/metrics` aggregator. The streaming bus (Redpanda/Kafka), Postgres,
and the TSDB are intentionally **external dependencies** — point the chart
at whichever cluster already runs them.

## Install

```bash
# Create the API-key secret out of band so rotation does not require helm.
kubectl create secret generic openllm-metrics-openai \
  --from-literal=OPENAI_ADMIN_API_KEY="sk-admin-..."

# Pull (once the chart is published) or render from this directory.
helm install openllm platform/deployment/helm/openllm-metrics \
  --namespace openllm-metrics --create-namespace \
  --set openaiPoller.labels.tenant=tenant-001 \
  --set openaiPoller.bus.brokers='{redpanda.bus.svc.cluster.local:9092}' \
  --set metricsEndpoint.bus.brokers='{redpanda.bus.svc.cluster.local:9092}'
```

## Values

See [`values.yaml`](./values.yaml) for the full schema with inline
documentation. The most commonly overridden keys are:

| Key                                   | Default                               | Description                                           |
| ------------------------------------- | ------------------------------------- | ----------------------------------------------------- |
| `global.imageRegistry`                | `ghcr.io/yasvanth511`                 | Registry hosting both images.                         |
| `openaiPoller.replicaCount`           | `1`                                   | Recommended: 1 per tenant scope.                      |
| `openaiPoller.pollingIntervalSeconds` | `300`                                 | Vision MVP default.                                   |
| `openaiPoller.labels.*`               | —                                     | Tenant/env/team context. Mandatory.                   |
| `metricsEndpoint.bus.brokers`         | `redpanda.bus.svc.cluster.local:9092` | Bus broker DNS.                                       |
| `metricsEndpoint.service.port`        | `9090`                                | ClusterIP port for the scrape surface.                |
| `prometheus.serviceMonitor.enabled`   | `false`                               | Render a ServiceMonitor if the operator is installed. |

## Verification

```bash
helm lint platform/deployment/helm/openllm-metrics
helm template platform/deployment/helm/openllm-metrics | kubectl apply --dry-run=client -f -
```
