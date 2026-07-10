# apps

Application surfaces for OpenLLM Metrics.

## Folders

- `gateway/`: LLM proxy gateway. Forwards provider requests transparently and captures runtime telemetry (latency, tokens, errors, retries) at the request boundary. It does **not** route, score, or enforce policy — those are OSS-deferred (see extension interfaces).
- `api/`: control-plane HTTP services, one Postgres-backed binary each — `metrics-endpoint` (Prometheus `/metrics` aggregator), `policy-service` (policy CRUD + versioning), `audit-service` (hash-chained audit ledger), `decision-service` (routing-decision ledger), `analytics-service` (saved analytics views for the console, host port `8096`).
- `worker/`: event-driven stream processors — `cost-mapper`, `reconciler`, `quota-risk`, `notifier`, `label-translator`, `focus-ingester`, and `usage-poller/openai` (the in-repo OpenAI billing poller; other providers' pull-mode billing flows through the optional `llm-usage-exporter` add-on).
- `web/admin-console/`: the admin and governance console (Next.js) — native analytics, policy authoring, audit review, decision explorer.

The OTel Collector receiver lives under `platform/otel-collector/receiver/llmproviderreceiver/`, not here. The OSS-deferred scoring, routing, policy-evaluation, and fallback engines are not in this repo; they register against the interfaces in [`packages/extensions/go`](../packages/extensions/go).

Do not duplicate business logic across services. Each pipeline stage owns its slice and exposes a stable contract; cross-service reads happen through the owning service's API or the streaming bus.
