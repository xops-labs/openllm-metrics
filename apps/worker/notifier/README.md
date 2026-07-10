# notifier

The notification and alerting fan-out worker (F033). Subscribes to
`alert.event.v1` on the streaming bus,
matches each event against per-tenant routing rules in Postgres, fans out to
the configured **generic webhook** and **SMTP** sinks, retries transient
failures with exponential backoff, and records every attempt in
`control_plane.notification_deliveries`.

The same binary serves the config CRUD HTTP API plus `/metrics` and
`/healthz`. Every successful mutation emits `audit.event.v1` for F031 to
hash-chain into the append-only audit ledger.

## OSS scope (deliberately narrow)

| Sink              | Status   | Where it lives                             |
| ----------------- | -------- | ------------------------------------------ |
| Generic webhook   | shipped  | `internal/sink/webhook.go`                 |
| SMTP (plain text) | shipped  | `internal/sink/smtp.go`                    |
| Slack             | deferred | not included in this repo                  |
| PagerDuty         | deferred | not included in this repo                  |
| Microsoft Teams   | deferred | not included in this repo                  |
| ServiceNow / Jira | deferred | not included in this repo                  |

The DB `CHECK (kind IN ('webhook', 'smtp'))` constraint is the hard wall.
Adding a vendor-branded sink is a deliberate migration in
this repository, not a config flip.

## HTTP surface

| Method | Path                             | Purpose                        |
| ------ | -------------------------------- | ------------------------------ |
| GET    | `/v1/notification/channels`      | List channels (current tenant) |
| POST   | `/v1/notification/channels`      | Create a channel               |
| GET    | `/v1/notification/channels/{id}` | Read a channel                 |
| PUT    | `/v1/notification/channels/{id}` | Update a channel               |
| DELETE | `/v1/notification/channels/{id}` | Soft-delete a channel          |
| GET    | `/v1/notification/rules`         | List routing rules             |
| POST   | `/v1/notification/rules`         | Create a rule                  |
| GET    | `/v1/notification/rules/{id}`    | Read a rule                    |
| PUT    | `/v1/notification/rules/{id}`    | Update a rule                  |
| DELETE | `/v1/notification/rules/{id}`    | Soft-delete a rule             |
| GET    | `/v1/notification/deliveries`    | Read delivery history          |
| GET    | `/metrics`                       | Prometheus exposition          |
| GET    | `/healthz`                       | Liveness                       |

Every CRUD call requires the `X-Tenant-Id` header carrying the caller's
tenant UUID. OSS does **not** authenticate at this layer; operators front the binary with a reverse proxy or service-mesh policy that enforces tenancy.

`GET /v1/notification/deliveries` accepts `rule_id`, `from`, and `to` query
parameters (`from`/`to` are RFC 3339 timestamps). Results are capped at 500
rows and ordered newest-first.

## Configuration

The compose stack mounts
`platform/deployment/compose/configs/notifier.yaml` at
`/etc/openllm-notifier/config.yaml` (host port `8092` → container `8085`).
The Postgres DSN is read from the env var named by `database.dsn_env`
(compose sets `OPENLLM_CONTROL_PLANE_DSN`) — never log it. SMTP passwords
resolve at send time from `OLM_SECRET_<REF>` env vars (see the SMTP channel
below).

| YAML path                           | Default                     | Notes                                                |
| ----------------------------------- | --------------------------- | ---------------------------------------------------- |
| `server.port`                       | `8085`                      | Config CRUD API + `/metrics` + `/healthz` HTTP port. |
| `database.dsn_env`                  | `OPENLLM_CONTROL_PLANE_DSN` | Env var holding the Postgres DSN.                    |
| `bus.brokers`                       | —                           | Required. Kafka/Redpanda broker `host:port` list.    |
| `bus.client_id`                     | `openllm-notifier`          | Surfaced in broker logs.                             |
| `bus.group_id`                      | `openllm-notifier`          | Consumer group.                                      |
| `bus.alert_topic`                   | `alert.event.v1`            | Input topic (alert events).                          |
| `bus.audit_topic`                   | `audit.event.v1`            | Output topic for config-mutation audit events.       |
| `retry.max_attempts`                | `5`                         | Delivery attempts before a failure is terminal.      |
| `retry.initial_backoff_ms`          | `500`                       | First retry delay.                                   |
| `retry.max_backoff_ms`              | `30000`                     | Backoff cap.                                         |
| `retry.per_attempt_timeout_seconds` | `10`                        | Bound on a single delivery attempt.                  |

## Channel configurations

### Webhook (`kind: "webhook"`)

```json
{
  "url": "https://hooks.example.internal/openllm",
  "headers": { "Authorization": "Bearer …" },
  "secret_hmac": "supersecret"
}
```

- `headers` are forwarded verbatim. The `Authorization` value is **never
  logged** (the notifier does not log request/response bodies or auth
  headers).
- `secret_hmac` triggers HMAC-SHA256 signing of the JSON body. The hex
  digest is sent as `X-OLM-Signature: sha256=<hex>`.
- The alert event ID is mirrored in `X-OLM-Alert-Event-Id` so receivers can
  correlate without parsing the body.

**Receiver verification recipe (any language)**:

```text
expected = "sha256=" + hex(HMAC_SHA256(secret, request_body_bytes))
constant_time_compare(expected, request_header["X-OLM-Signature"])
```

Use a constant-time comparison (Go: `hmac.Equal`, Python: `hmac.compare_digest`,
Node: `crypto.timingSafeEqual`).

### SMTP (`kind: "smtp"`)

```json
{
  "server": "smtp.example.internal",
  "port": 587,
  "username": "openllm-notifier@example.com",
  "password_ref": "PROD_SMTP_PASS",
  "from": "openllm-notifier@example.com",
  "to": ["sre-oncall@example.com"]
}
```

- `password_ref` is an indirection — the worker resolves it from
  `OLM_SECRET_PROD_SMTP_PASS` (case-folded, non-alphanumeric → `_`) at send
  time. The plaintext password **never** lives in Postgres and is **never**
  logged.
- The body is plain text (subject + labelled key/value block + summary +
  description + labels). No HTML, no LLM prompt/completion content.

## Routing rules

`notification_rules.match` is a JSON document. Empty arrays (and missing
keys) mean "any value":

```json
{
  "severity": ["critical", "high"],
  "source": ["slo", "quota"]
}
```

A rule with `match: {}` (or `match: null`) fans every alert in its tenant
out to every channel in `channel_ids`. Soft-deleted channels are skipped at
send time.

## Idempotency

`notification_deliveries` carries a `UNIQUE (alert_event_id, channel_id)`
constraint. The consumer's `ClaimDelivery` query is the gate:

1. Try to INSERT a `pending` row.
2. On conflict, look up the existing row.
3. If `status='success'` → skip (re-emit from a Kafka replay never
   double-sends).
4. Otherwise → continue from the existing row.

The `attempts` column is monotonically increased; the `last_error` column
holds the most recent failure (with `Authorization`, `password=`, and
`X-OLM-Signature` substrings scrubbed defensively).

## Retry

Exponential backoff bounded by config:

```
delay_n = min(initial_backoff * 2^n, max_backoff)
```

Retries fire on:

- Webhook: any transport error or HTTP `5xx`.
- SMTP: any SMTP response in the `4yz` range or any transport-level error.

Terminal failures (HTTP `4xx`, missing `password_ref`, malformed config,
unknown channel kind) are recorded once and **not** retried.

## Self-observability

| Series                                  | Type    | Description                                      |
| --------------------------------------- | ------- | ------------------------------------------------ |
| `llm_notifier_alerts_consumed_total`    | counter | Alert events read from the bus.                  |
| `llm_notifier_alerts_matched_total`     | counter | Alerts that matched at least one rule.           |
| `llm_notifier_alerts_unmatched_total`   | counter | Alerts with no matching rule (no fan-out).       |
| `llm_notifier_deliveries_success_total` | counter | Sink dispatches that completed.                  |
| `llm_notifier_deliveries_failure_total` | counter | Sink dispatches that exhausted retries.          |
| `llm_notifier_deliveries_retry_total`   | counter | Individual retry attempts (excluding first try). |
| `llm_notifier_deliveries_skipped_total` | counter | Idempotency-guard skips.                         |
| `llm_notifier_webhook_sent_total`       | counter | Webhook attempts (any outcome).                  |
| `llm_notifier_smtp_sent_total`          | counter | SMTP attempts (any outcome).                     |
| `llm_notifier_config_mutations_total`   | counter | CRUD mutations via the HTTP API.                 |

## Hard rules / never logged

- SMTP passwords (resolved via `OLM_SECRET_<REF>`, never persisted).
- Webhook HMAC secrets.
- `Authorization` headers being forwarded to webhook receivers.
- Webhook response bodies (receivers may echo secrets).
- LLM prompt or completion content (must never appear in alert payloads —
  enforced upstream by producers; the alert schema has no field for it).

## TODOs (intentional)

- Hook the Alertmanager webhook receiver service that converts Alertmanager
  POSTs to `alert.event.v1` events on the bus (separate service; not in this
  package).
- A producer library for the F030 policy evaluator when it lands — emitting
  directly to `alert.event.v1` using the schema in
  `packages/contracts/notifications/v1/alert-event.schema.json`.
- Console UX for channel/rule CRUD lands in F032; the HTTP surface above is
  the contract.
- Schema validation of `match` documents (currently the worker treats a
  malformed `match` as non-matching to fail closed).

## Hard constraints

- Multi-tenant from day one. Every row carries `tenant_id`; RLS is forced.
- No prompt or completion content in delivered payloads.
- No vendor-branded sinks — that line is the open-source scope.
- Provider keys, SMTP passwords, and HMAC secrets never appear in logs or
  metric labels.
