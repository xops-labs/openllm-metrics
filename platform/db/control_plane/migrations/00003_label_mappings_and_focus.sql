-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: Phase B — label translator + FOCUS ingester.
--
-- Adds two control-plane tables consumed by the new Phase B workers:
--
--   * control_plane.label_mappings — maps the weak label tuple emitted by
--     upstream llm-usage-exporter ({provider, tenant_external_id, tenancy_id})
--     onto the canonical {tenant, team, app, env, project} the platform uses.
--     Without this table, the label translator has no way to attribute usage
--     beyond the provider/account level.
--
--   * control_plane.focus_records — append-only store of FOCUS line items
--     polled from the upstream /focus.json endpoint. Records the raw FOCUS
--     payload as JSONB plus extracted columns for the cost/period/account
--     fields the reconciliation dashboards join against.
--
-- Both tables are tenant-scoped and protected by RLS in the same pattern as
-- the F005 baseline tables (00002).
--
-- Tool: goose
-- Direction: Up / Down

-- +goose Up
-- +goose StatementBegin

-- ------------------------------------------------------------------
-- control_plane.label_mappings
-- ------------------------------------------------------------------
-- One row per (provider, tenant_external_id, tenancy_id) tuple coming from
-- upstream. The canonical {tenant, team, app, env, project} columns are the
-- enrichment target; rows missing this enrichment are the "unmapped" series
-- the label translator increments on.
--
-- (tenant_external_id, tenancy_id) is the natural key the upstream exporter
-- carries. tenancy_id may be empty for providers that have no concept of
-- sub-account (e.g. early OpenAI tenancy); we model that as the empty string
-- so the natural-key UNIQUE index always covers the row.
CREATE TABLE IF NOT EXISTS control_plane.label_mappings (
    id                   UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    team_id              UUID        NOT NULL REFERENCES control_plane.teams(id)   ON DELETE CASCADE,
    app_id               UUID                 REFERENCES control_plane.apps(id)    ON DELETE SET NULL,
    provider             TEXT        NOT NULL
                             CHECK (provider IN ('openai', 'anthropic', 'google', 'azure_openai', 'bedrock')),
    tenant_external_id   TEXT        NOT NULL,
    tenancy_id           TEXT        NOT NULL DEFAULT '',
    canonical_project    TEXT        NOT NULL DEFAULT '',
    canonical_env        TEXT        NOT NULL
                             CHECK (canonical_env IN ('development', 'staging', 'production')),
    canonical_region     TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, tenant_external_id, tenancy_id)
);

CREATE INDEX IF NOT EXISTS label_mappings_tenant_id_idx
    ON control_plane.label_mappings (tenant_id);
CREATE INDEX IF NOT EXISTS label_mappings_lookup_idx
    ON control_plane.label_mappings (provider, tenant_external_id, tenancy_id);

COMMENT ON TABLE control_plane.label_mappings IS
    'Maps upstream llm-usage-exporter {provider, tenant_external_id, tenancy_id} to canonical {tenant, team, app, env, project}. Owned by the Phase B label translator.';
COMMENT ON COLUMN control_plane.label_mappings.tenancy_id IS
    'Provider sub-account / project / organization identifier (varies by provider). Empty string when the provider has no sub-account concept.';
COMMENT ON COLUMN control_plane.label_mappings.tenant_external_id IS
    'The provider-side billing account or top-level identifier (e.g. OpenAI organization, Azure subscription).';

ALTER TABLE control_plane.label_mappings ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.label_mappings FORCE ROW LEVEL SECURITY;

CREATE POLICY label_mappings_tenant_isolation
    ON control_plane.label_mappings
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- ------------------------------------------------------------------
-- control_plane.focus_records
-- ------------------------------------------------------------------
-- Append-only FOCUS line item store. Every poll of /focus.json inserts rows;
-- updates to the same source_event_id are NEVER allowed — instead, a new row
-- supersedes the previous one (last-write-wins on read by ingested_at).
--
-- raw_focus is the unmodified FOCUS record as returned by upstream. Extracted
-- columns are denormalized for query speed; they MUST stay in sync with the
-- shape documented in docs/architecture/providers/<provider>.md.
CREATE TABLE IF NOT EXISTS control_plane.focus_records (
    id                                 UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                          UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    source_event_id                    TEXT        NOT NULL,
    provider                           TEXT        NOT NULL
                                           CHECK (provider IN ('openai', 'anthropic', 'google', 'azure_openai', 'bedrock')),
    model                              TEXT        NOT NULL DEFAULT '',
    billing_account_id                 TEXT        NOT NULL,
    invoice_id                         TEXT        NOT NULL DEFAULT '',
    service_name                       TEXT        NOT NULL DEFAULT '',
    charge_category                    TEXT        NOT NULL DEFAULT '',
    reconciled_cost_usd_minor_units    BIGINT      NOT NULL DEFAULT 0
                                           CHECK (reconciled_cost_usd_minor_units >= 0),
    list_cost_usd_minor_units          BIGINT      NOT NULL DEFAULT 0
                                           CHECK (list_cost_usd_minor_units >= 0),
    pricing_currency                   TEXT        NOT NULL DEFAULT 'USD',
    period_start                       TIMESTAMPTZ NOT NULL,
    period_end                         TIMESTAMPTZ NOT NULL
                                           CHECK (period_end >= period_start),
    raw_focus                          JSONB       NOT NULL,
    ingested_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only query patterns: by tenant for tenant dashboards, by
-- (tenant, provider, period_*) for reconciliation joins.
CREATE INDEX IF NOT EXISTS focus_records_tenant_id_idx
    ON control_plane.focus_records (tenant_id);
CREATE INDEX IF NOT EXISTS focus_records_lookup_idx
    ON control_plane.focus_records (tenant_id, provider, period_start, period_end);
CREATE INDEX IF NOT EXISTS focus_records_source_event_id_idx
    ON control_plane.focus_records (source_event_id);

COMMENT ON TABLE control_plane.focus_records IS
    'Append-only FOCUS line items polled from upstream llm-usage-exporter /focus.json. Owned by the Phase B FOCUS ingester. Last-write-wins on read by ingested_at.';
COMMENT ON COLUMN control_plane.focus_records.source_event_id IS
    'Stable handle back to the upstream FOCUS record. Format: focus:<billing_account_id>:<period_start>:<period_end>:<service_name>.';
COMMENT ON COLUMN control_plane.focus_records.raw_focus IS
    'Unmodified FOCUS line item as returned by upstream. Source of truth if extracted columns and the raw form drift.';

ALTER TABLE control_plane.focus_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.focus_records FORCE ROW LEVEL SECURITY;

CREATE POLICY focus_records_tenant_isolation
    ON control_plane.focus_records
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS focus_records_tenant_isolation   ON control_plane.focus_records;
DROP POLICY IF EXISTS label_mappings_tenant_isolation  ON control_plane.label_mappings;

DROP TABLE IF EXISTS control_plane.focus_records;
DROP TABLE IF EXISTS control_plane.label_mappings;

-- +goose StatementEnd
