# Schema Evolution Rules

All event schemas are canonical under
[`packages/contracts/telemetry/go/schemas/`](../../packages/contracts/telemetry/go/schemas/)
(F008 — Common Operational Telemetry Schema). Topic-to-schema mapping lives in
[`topics.yaml`](topics.yaml).

## Compatibility mode

**Backward-compatible only** at MVP. New schema versions must be consumable by
consumers pinned to the previous version without crashes or data loss.

## Allowed changes

- Add new **optional** fields (new required fields break backward compatibility).
- Extend `enum` with new values (consumers must handle unknown enum values gracefully).
- Broaden numeric types (e.g., `integer` → `number`).

## Prohibited changes

- Remove or rename existing fields.
- Add new **required** fields to an existing version.
- Change a field's type in a breaking way.
- Remove `enum` values that existing consumers may produce.

## Versioning

Schemas are versioned via the `$id` URI and a `vN` suffix in the filename (e.g.
`llm.usage.normalized.v1.json`, `llm.usage.normalized.v2.json`). When a
breaking change is unavoidable:

1. Add a new versioned schema file under `packages/contracts/telemetry/go/schemas/`.
2. Update `topics.yaml` to reference the new schema path.
3. Run a dual-publish window: produce both v1 and v2 until all consumers migrate.
4. Remove v1 only after all consumers confirm migration.

## Privacy rule

No event payload may contain:

- LLM prompt text or completion text
- Provider API keys or secrets
- User identifiers beyond `tenant` and `team`
- PII

This rule is enforced at schema-lint time (CI, via
`packages/telemetry/schema-lint/`) and at the gateway layer (F018).
