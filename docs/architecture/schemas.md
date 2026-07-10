# Schema Versioning Rules

OpenLLM Metrics is multi-tenant telemetry infrastructure. Every cross-service
contract — bus events, Prometheus series, REST API payloads, Postgres table
rows — is treated as a **public API** with explicit versioning rules.
This document defines those rules so contributors know when a change is
safe, when it needs a migration, and when it requires a new schema version.

## Guiding principle

> A schema change is safe if and only if **every existing consumer continues
> to function correctly without any code change**. Any change that breaks a
> consumer without a code change is a **breaking change** and requires a new
> major version.

This principle applies to all schema surfaces listed below. When in doubt,
treat the change as breaking and follow the breaking-change path.

---

## 1. Bus event schemas (JSON Schema 2020-12)

Bus events are the primary cross-service contract. They travel from producers
(gateway, SDKs, exporter adapter) to consumers (aggregator, label-translator,
OTel receiver, scoring worker, audit ledger). Consumers must not crash on an
event they do not fully understand.

### Version identifiers

Every bus event schema has a `schema_version` field (string) with the value
`v<N>` (e.g., `v1`). This field is the first thing every consumer checks.

### Safe (backward-compatible) changes

These changes do **not** require a new version:

- **Adding a new optional field** with `"omitempty"` semantics. Existing
  consumers that do not know the field will ignore it.
- **Relaxing a constraint** (e.g., widening a `maxLength`, making a required
  field optional). Existing consumers that expected the old constraint will
  still work on data that satisfies the looser constraint.
- **Adding a new enum value** to an extensible enum (one that explicitly
  allows unknown values). Document the new value in the schema comment.

### Breaking changes (require a new version)

These changes require bumping from `v<N>` to `v<N+1>`:

- **Removing a field** that consumers may rely on.
- **Renaming a field** (equivalent to remove + add).
- **Changing a field's type** (e.g., `string` to `int`).
- **Tightening a constraint** (e.g., making an optional field required,
  reducing `maxLength`).
- **Changing the semantics** of an existing field (even if the type stays
  the same).
- **Removing an enum value** from a closed enum.

### Version transition procedure

1. Draft the `v<N+1>` JSON Schema document and place it alongside `v<N>` in
   `packages/contracts/telemetry/`.
2. Update the code generator to emit both `v<N>` and `v<N+1>` Go/TypeScript
   types.
3. Update producers to emit `v<N+1>` events (optionally with a flag to
   emit `v<N>` for rollback safety).
4. Update consumers to accept both `v<N>` and `v<N+1>` (dual-read period).
5. After all consumers are deployed and no `v<N>` events are in flight,
   remove `v<N>` support from consumers.
6. Remove the `v<N>` schema document in the release that completes the
   transition.

The `schema_version` field on every event makes the dual-read period
implementable with a simple `switch schema_version` branch.

---

## 2. Prometheus series (metric names and labels)

Prometheus metric names and label sets are consumed by dashboards, alert
rules, recording rules, and external tools that operators may have written
against this platform. Changes to metric names or labels are breaking changes
for those consumers.

### Safe changes

- **Adding a new metric** with a new name. Existing dashboards and alerts
  that do not reference the new metric are unaffected.
- **Adding a new label** to an existing metric, provided the label has a
  low-cardinality default value (not an unbounded string) and all existing
  queries that omit the label continue to return correct results. Document
  the new label in `packages/contracts/metrics/go/registry.go`.

### Breaking changes

- **Removing a metric** that dashboards or alerts reference.
- **Renaming a metric** (equivalent to remove + add).
- **Removing a label** from an existing metric.
- **Renaming a label** (equivalent to remove + add).
- **Changing the type** of a metric (counter vs gauge vs histogram).
- **Changing the unit** of a metric (e.g., microseconds to seconds) without
  renaming — same series name, different scale, silently wrong dashboards.

### Version transition procedure for breaking Prometheus changes

1. **Add** the new metric or label under the new name/semantics. Do not
   remove the old one.
2. **Dual-emit period**: emit both old and new names/labels from all producers
   for at least one minor release cycle.
3. **Update** all dashboards, recording rules, and alert rules to reference
   the new name/label.
4. **Remove** the old metric/label in the next release after all consumers
   are updated.

Prefix all project-specific metrics with `llm_`. Do not remove the prefix
in future renames — it avoids namespace collisions with upstream OTel semconv
metrics that use `gen_ai.*`.

---

## 3. SLO definition schema (JSON Schema 2020-12)

The SLO definition schema at `platform/slo/schemas/slo-definition.v1.json`
is consumed by the admin console (form rendering), the recording rule
generator, and the alert rule generator. The schema carries a `version`
integer field (document version) and the file name carries the schema
version (`v1`).

### Safe changes

- Adding new optional fields.
- Adding new `objective_type` enum values (requires updating recording rules
  and alert rules simultaneously, but does not break existing SLO documents).

### Breaking changes

- Removing or renaming existing required fields.
- Changing the semantics of `objective_type` values.
- Changing how `target` maps to burn-rate thresholds (would silently change
  alert behavior for existing SLO documents).

When a breaking change is necessary, create
`platform/slo/schemas/slo-definition.v2.json` and provide a migration tool
that rewrites `v1` documents to `v2` format. Existing SLO documents in the
database carry the schema version in their `version` field; the console and
the rule generator select the correct schema version from this field.

---

## 4. Postgres table schemas (SQL migrations)

Postgres migrations are managed with [goose](https://github.com/pressly/goose)
(see `platform/db/CONVENTIONS.md`). Each migration is a **single** SQL file
under `platform/db/<schema>/migrations/` containing a `-- +goose Up` block and a
`-- +goose Down` block. Migrations must be idempotent
(`CREATE TABLE IF NOT EXISTS`, …).

### Safe changes

- Adding a new column with a default value (nullable or `NOT NULL DEFAULT
<constant>`). Existing queries that do not select the new column are
  unaffected.
- Adding a new table.
- Adding a new index.
- Relaxing a constraint (e.g., dropping a `NOT NULL` with a `DEFAULT`).

### Breaking changes

- Removing a column that code reads.
- Renaming a column (equivalent to remove + add).
- Changing a column's type in an incompatible way.
- Adding a `NOT NULL` constraint to a column that code may leave null.

### Migration naming convention

```
<sequence-or-timestamp>_<feature-id>_<description>.sql
```

Baseline migrations use a zero-padded sequence (`00001_baseline.sql`); feature
migrations use a UTC `YYYYMMDDhh` timestamp plus the feature ID
(`2026051804_f031_audit_ledger.sql`).

### Rollback policy

Every migration's `-- +goose Down` block must cleanly reverse its `-- +goose Up`
block. If a rollback is not possible (e.g., a destructive data change), the
`Down` block must contain a comment explaining why, and the migration must be
gated by a **stop-and-ask** in the release notes.

---

## 5. REST API and admin-console payloads

REST API endpoints in `apps/api/*/` and admin-console Next.js API routes in
`apps/web/admin-console/` follow semantic versioning embedded in the URL path:

- `/v1/` — current stable.
- `/v2/` — introduced alongside `/v1/` during a breaking change cycle.

The transition procedure mirrors the bus event dual-read period: new version
added, old version deprecated (returns a `Deprecation` response header), old
version removed after one minor release cycle.

---

## 6. Extension interface module (`packages/extensions/go/`)

The public Go interfaces for scoring, routing, policy, and fallback
are versioned as a Go module with a tagged release
(`packages/extensions/go/v1.0.0` once the interfaces are frozen). Breaking changes
to these interfaces require a new Go module major version (`v2`), which is a
**stop-and-ask gate** — all consumers must update to the new major version before the old major version is removed.

---

## Summary table

| Schema surface       | Versioning mechanism              | Breaking change path                              |
| -------------------- | --------------------------------- | ------------------------------------------------- |
| Bus events           | `schema_version` field + filename | New file `v<N+1>.json`; dual-read period          |
| Prometheus metrics   | Metric name prefix                | Dual-emit; remove old after consumers updated     |
| SLO definition       | File name + `version` field       | New file `slo-definition.v2.json`; migration tool |
| Postgres tables      | Migration files (up+down)         | Schema migration; `down` file required            |
| REST API             | URL path version (`/v1/`, `/v2/`) | Add `/v2/`; deprecate `/v1/`; remove after cycle  |
| Extension interfaces | Go module semver                  | New major version; consumer update          |

## See also

- `platform/bus/SCHEMA_EVOLUTION.md` — bus-specific evolution rules and
  the idempotency contract for consumer replay.
- `platform/db/CONVENTIONS.md` — Postgres naming conventions and RLS policy.
- `packages/contracts/` — the canonical JSON Schema documents for bus events
  and the metric registry.
- `docs/architecture/adopted-components.md` — how upstream component schema
  changes propagate into this platform.
