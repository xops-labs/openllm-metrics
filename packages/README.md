# packages

Shared packages and generated assets.

## Folders

- `bus-client`: shared Go Kafka/Redpanda producer + consumer helpers used by the services and workers.
- `contracts`: OpenAPI contracts and related schemas for OpenLLM Metrics APIs (one per service plus shared types).
- `dashboards`: Grafana dashboard JSON (FinOps and SLO views) and Prometheus alert rule templates.
- `extensions`: Go extension interfaces (scoring, routing, policy evaluation, fallback) with safe no-op defaults; custom deployments can register implementations against these.
- `sdk-dotnet`: .NET runtime instrumentation SDK for proxy-mode telemetry.
- `sdk-go`: Go runtime instrumentation SDK for proxy-mode telemetry.
- `sdk-node`: Node.js runtime instrumentation SDK for proxy-mode telemetry.
- `sdk-python`: Python runtime instrumentation SDK for proxy-mode telemetry.
- `telemetry`: shared Go telemetry helpers (GenAI semantic conventions, redaction, propagation) plus the `schema-lint` event-schema linter.

Runtime instrumentation SDKs (`sdk-dotnet`, `sdk-python`, `sdk-node`, `sdk-go`) share a normalization contract with the gateway and are versioned together.
