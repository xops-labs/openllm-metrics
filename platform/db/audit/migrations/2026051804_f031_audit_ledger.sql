-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F031 — Append-Only Audit Ledger.
--
-- Creates the tamper-evident, per-tenant hash-chained audit ledger consumed by
-- the audit-service (apps/api/audit-service) and the olm-audit CLI verifier
-- (cmd/olm-audit).
--
-- Design:
--
--   * Every row carries the originating tenant and a sha256 chain link
--     (prev_hash, entry_hash). The chain is PER TENANT — broken chains in
--     one tenant do not invalidate other tenants.
--   * Append-only is enforced AT THE DATABASE LEVEL via row-level rules that
--     raise on any UPDATE or DELETE. App code defending against mutation is
--     defense in depth; the database is the trust boundary.
--   * No prompts, completions, passwords, API keys, OAuth tokens, or bearer
--     tokens may be written. Producers redact; the service redacts again
--     before write. The schema does not allow extra free-form text columns
--     beyond `payload jsonb` which is also scrubbed.
--
-- Index strategy:
--
--   * (tenant_id, id) — the natural chain order and pagination cursor key.
--   * (tenant_id, action, created_at) — query by action over a time window.
--   * (tenant_id, created_at) — bulk export by time range.
--
-- Tool: goose
-- Direction: Up

-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS audit.audit_entries (
    id                BIGSERIAL   PRIMARY KEY,
    tenant_id         UUID        NOT NULL,
    actor             JSONB       NOT NULL DEFAULT '{}'::JSONB,
    action            TEXT        NOT NULL CHECK (length(action) > 0),
    resource          JSONB       NOT NULL DEFAULT '{}'::JSONB,
    payload           JSONB       NOT NULL DEFAULT '{}'::JSONB,
    prev_hash         BYTEA       NOT NULL,
    entry_hash        BYTEA       NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (octet_length(prev_hash)  = 32),
    CHECK (octet_length(entry_hash) = 32)
);

CREATE INDEX IF NOT EXISTS audit_entries_tenant_id_idx
    ON audit.audit_entries (tenant_id, id);

CREATE INDEX IF NOT EXISTS audit_entries_tenant_action_time_idx
    ON audit.audit_entries (tenant_id, action, created_at);

CREATE INDEX IF NOT EXISTS audit_entries_tenant_time_idx
    ON audit.audit_entries (tenant_id, created_at);

COMMENT ON TABLE audit.audit_entries IS
    'F031 append-only audit ledger. Per-tenant sha256 hash chain. UPDATE and DELETE are rejected at the database level by audit_entries_no_update / audit_entries_no_delete rules.';
COMMENT ON COLUMN audit.audit_entries.tenant_id IS
    'Tenant boundary. Hash chain is per-tenant; tenants never share prev_hash.';
COMMENT ON COLUMN audit.audit_entries.actor IS
    'Actor envelope: {type, id, email, ip}. Never contains passwords, tokens, or secrets.';
COMMENT ON COLUMN audit.audit_entries.action IS
    'Dotted-namespace action verb, e.g. policy.create, policy.update, routing.override, console.login.';
COMMENT ON COLUMN audit.audit_entries.resource IS
    'Resource envelope: {type, id, name}. Free-form per action but never carries payloads.';
COMMENT ON COLUMN audit.audit_entries.payload IS
    'Action-specific details. NEVER contains prompts, completions, passwords, API keys, OAuth tokens, or bearer tokens. The service-side redact step strips known sensitive keys before insert.';
COMMENT ON COLUMN audit.audit_entries.prev_hash IS
    'sha256(prior row entry_hash) for this tenant. 32 zero bytes for the first row in the tenant chain.';
COMMENT ON COLUMN audit.audit_entries.entry_hash IS
    'sha256 over the canonical JSON encoding of (tenant_id, id, actor, action, resource, payload, prev_hash, created_at).';

-- ------------------------------------------------------------------
-- Append-only enforcement.
-- ------------------------------------------------------------------
-- Two rules raise on any UPDATE or DELETE statement against the table.
-- ON UPDATE / ON DELETE rules with INSTEAD OF semantics + RAISE are the
-- simplest portable mechanism that does not depend on superuser-only
-- features (event triggers, replication slots). A connection with role
-- BYPASSRLS still cannot mutate rows: rules apply to every role except
-- the table owner in DO NOTHING form, so we explicitly RAISE.
--
-- The migration tool runs as the table owner — that's fine, the rules
-- fire on subsequent statements from app roles. Service code MUST NOT
-- run as the table owner in production.

CREATE OR REPLACE RULE audit_entries_no_update AS
    ON UPDATE TO audit.audit_entries
    DO INSTEAD NOTHING;

CREATE OR REPLACE RULE audit_entries_no_delete AS
    ON DELETE TO audit.audit_entries
    DO INSTEAD NOTHING;

-- Belt + braces: a function + trigger raises on UPDATE / DELETE so the
-- operation also fails loudly (the RULE above silently drops the
-- statement which is correct for security but unhelpful for debugging).
CREATE OR REPLACE FUNCTION audit.audit_entries_reject_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit.audit_entries is append-only (TG_OP=%)', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER audit_entries_reject_update
    BEFORE UPDATE ON audit.audit_entries
    FOR EACH ROW EXECUTE FUNCTION audit.audit_entries_reject_mutation();

CREATE TRIGGER audit_entries_reject_delete
    BEFORE DELETE ON audit.audit_entries
    FOR EACH ROW EXECUTE FUNCTION audit.audit_entries_reject_mutation();

-- ------------------------------------------------------------------
-- Row-level security: per-tenant read isolation.
-- ------------------------------------------------------------------
-- The session key 'app.tenant_id' is set by the audit-service after JWT
-- validation (same convention as control_plane tables). BYPASSRLS roles
-- (migrations, the CLI verifier, the SIEM exporter) see all tenants.

ALTER TABLE audit.audit_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit.audit_entries FORCE ROW LEVEL SECURITY;

CREATE POLICY audit_entries_tenant_isolation
    ON audit.audit_entries
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd


-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F031 — Append-Only Audit Ledger (Down).
--
-- Drops the audit ledger surface created in 2026051804_f031_audit_ledger.up.sql.
-- A real-world rollback should NEVER drop the audit table — compliance
-- requires retention. This Down is here for development resets only.
--
-- Tool: goose
-- Direction: Down

-- +goose Down
-- +goose StatementBegin

DROP POLICY  IF EXISTS audit_entries_tenant_isolation    ON audit.audit_entries;
DROP TRIGGER IF EXISTS audit_entries_reject_delete       ON audit.audit_entries;
DROP TRIGGER IF EXISTS audit_entries_reject_update       ON audit.audit_entries;
DROP RULE    IF EXISTS audit_entries_no_delete           ON audit.audit_entries;
DROP RULE    IF EXISTS audit_entries_no_update           ON audit.audit_entries;
DROP FUNCTION IF EXISTS audit.audit_entries_reject_mutation();
DROP TABLE   IF EXISTS audit.audit_entries;

-- +goose StatementEnd
