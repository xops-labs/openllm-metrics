-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F029 — Declarative Policy Schema, Storage, and Versioning.
--
-- Adds the OSS-safe policy data layer:
--
--   * control_plane.policies               — one row per policy (header).
--   * control_plane.policy_versions        — append-only version history;
--                                            each row is one immutable
--                                            version of the policy document
--                                            stored as JSONB.
--   * control_plane.policy_validation_errors
--                                          — append-only validation findings
--                                            recorded against a specific
--                                            policy_version row. Holds JSON
--                                            Schema and structural errors only;
--                                            NO evaluation / decisioning data.
--
-- This migration intentionally encodes ONLY the data shape and storage rules.
-- Policy evaluation, budget arithmetic, exception precedence, and enforcement
-- decisions are owned by F030 in this repository and are
-- explicitly NOT modeled here.
--
-- Multi-tenant from day one: every table carries tenant_id and is protected
-- by row-level security in the same pattern as the F005 baseline (00002).
--
-- Tool: goose
-- Direction: Up / Down

-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE SCHEMA IF NOT EXISTS control_plane;

-- ------------------------------------------------------------------
-- control_plane.policies
-- ------------------------------------------------------------------
-- Header row per logical policy. current_version is the most recently
-- written version number; the policy_versions table holds the full
-- history including the current row. Soft delete via deleted_at so
-- audit/historical queries can still reference the policy.
CREATE TABLE IF NOT EXISTS control_plane.policies (
    id              UUID        NOT NULL PRIMARY KEY,
    tenant_id       UUID        NOT NULL,
    name            TEXT        NOT NULL,
    current_version INTEGER     NOT NULL DEFAULT 1
                        CHECK (current_version >= 1),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS policies_tenant_id_idx
    ON control_plane.policies (tenant_id);
CREATE INDEX IF NOT EXISTS policies_tenant_active_idx
    ON control_plane.policies (tenant_id)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE control_plane.policies IS
    'F029 policy header. One row per logical policy per tenant. Soft-deleted rows are retained for audit/historical lookups.';
COMMENT ON COLUMN control_plane.policies.current_version IS
    'Pointer to the most recently written row in control_plane.policy_versions for this policy.';

-- ------------------------------------------------------------------
-- control_plane.policy_versions
-- ------------------------------------------------------------------
-- Append-only version history. Each row is one immutable version of
-- the policy document. The full document is stored as JSONB; readers
-- and downstream evaluators (F030, out of scope here) consume this row
-- by (policy_id, version).
CREATE TABLE IF NOT EXISTS control_plane.policy_versions (
    id          UUID        NOT NULL PRIMARY KEY,
    policy_id   UUID        NOT NULL REFERENCES control_plane.policies(id) ON DELETE CASCADE,
    tenant_id   UUID        NOT NULL,
    version     INTEGER     NOT NULL CHECK (version >= 1),
    document    JSONB       NOT NULL,
    created_by  TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    comment     TEXT        NOT NULL DEFAULT '',
    UNIQUE (policy_id, version)
);

CREATE INDEX IF NOT EXISTS policy_versions_tenant_id_idx
    ON control_plane.policy_versions (tenant_id);
CREATE INDEX IF NOT EXISTS policy_versions_policy_id_idx
    ON control_plane.policy_versions (policy_id);
CREATE INDEX IF NOT EXISTS policy_versions_document_gin
    ON control_plane.policy_versions USING GIN (document jsonb_path_ops);

COMMENT ON TABLE control_plane.policy_versions IS
    'F029 append-only policy version history. Each row is one immutable JSONB document. No UPDATE / DELETE permitted at the app layer; a new version is appended on every write.';
COMMENT ON COLUMN control_plane.policy_versions.document IS
    'Full policy document as JSONB. Conforms to packages/contracts/policy/v1/policy.schema.json.';

-- ------------------------------------------------------------------
-- control_plane.policy_validation_errors
-- ------------------------------------------------------------------
-- Append-only audit of structural / JSON Schema validation failures
-- recorded against a specific policy_version row. Holds ONLY data
-- shape findings (missing fields, type mismatches, enum violations);
-- does NOT hold evaluation, scoring, or routing outcomes.
CREATE TABLE IF NOT EXISTS control_plane.policy_validation_errors (
    id                 UUID        NOT NULL PRIMARY KEY,
    policy_version_id  UUID        NOT NULL REFERENCES control_plane.policy_versions(id) ON DELETE CASCADE,
    tenant_id          UUID        NOT NULL,
    code               TEXT        NOT NULL,
    message            TEXT        NOT NULL,
    path               TEXT        NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS policy_validation_errors_tenant_id_idx
    ON control_plane.policy_validation_errors (tenant_id);
CREATE INDEX IF NOT EXISTS policy_validation_errors_policy_version_id_idx
    ON control_plane.policy_validation_errors (policy_version_id);

COMMENT ON TABLE control_plane.policy_validation_errors IS
    'F029 append-only validation findings for a specific policy version. Schema / structural errors only — never evaluation or enforcement outcomes.';

-- ------------------------------------------------------------------
-- Row Level Security
-- ------------------------------------------------------------------
ALTER TABLE control_plane.policies                  ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.policies                  FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.policy_versions           ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.policy_versions           FORCE ROW LEVEL SECURITY;
ALTER TABLE control_plane.policy_validation_errors  ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.policy_validation_errors  FORCE ROW LEVEL SECURITY;

CREATE POLICY policies_tenant_isolation
    ON control_plane.policies
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

CREATE POLICY policy_versions_tenant_isolation
    ON control_plane.policy_versions
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

CREATE POLICY policy_validation_errors_tenant_isolation
    ON control_plane.policy_validation_errors
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd


-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F029 — Declarative Policy Schema, Storage, and Versioning (DOWN).
--
-- Tool: goose
-- Direction: Down

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS policy_validation_errors_tenant_isolation ON control_plane.policy_validation_errors;
DROP POLICY IF EXISTS policy_versions_tenant_isolation          ON control_plane.policy_versions;
DROP POLICY IF EXISTS policies_tenant_isolation                 ON control_plane.policies;

DROP TABLE IF EXISTS control_plane.policy_validation_errors;
DROP TABLE IF EXISTS control_plane.policy_versions;
DROP TABLE IF EXISTS control_plane.policies;

-- Extensions and schema intentionally left in place; they may be in
-- use by other migrations.

-- +goose StatementEnd
