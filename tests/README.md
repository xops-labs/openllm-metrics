# tests

Cross-application test suites.

## Folders

- `contract`: API contract tests — currently `metrics-endpoint` (Go, asserts the Prometheus `/metrics` surface against the canonical event contract).
- `dashboards`: PromQL fixtures (`cost-spike`, `error-rate`, `exporter-stale`) exercised by the CI `dashboards` job against the shipped alert rules.
- `provider-adapters`: contract tests per provider adapter — currently `openai` (Go, JSON fixtures verifying conformance to the common operational schema).

Per-app unit and integration tests live alongside the app under `apps/<app>/tests` or equivalent. This folder is for cross-cutting suites only.
