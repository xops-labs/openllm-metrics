-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F017 — cost_reconciliation_drift table.
--
-- The cost-mapper worker joins runtime estimates (computed from token counts
-- multiplied by the pricing catalog) with reconciledCostUsd events produced
-- by the focus-ingester. Drift records are written to this table.
--
-- Correlation key (and the table's natural-key UNIQUE constraint):
--     (tenant_id, provider, model, period_start, period_end)
--
-- The unique key plus an ON CONFLICT DO UPDATE write pattern keeps the
-- consumer idempotent: replaying the same FOCUS line item against the same
-- runtime estimate produces exactly one row.
--
-- This is the Phase C runtime-side companion to control_plane.focus_records
-- (Phase B). It lives in the control_plane schema for the same RLS and
-- ownership reasons documented in platform/db/CONVENTIONS.md.
--
-- Tool: goose (file name uses a unique 2026051801 timestamp prefix so a
--             parallel agent's numbered migrations cannot collide).
-- Direction: Up

-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS control_plane.cost_reconciliation_drift (
    id                            UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                     UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    team                          TEXT        NOT NULL DEFAULT '',
    app                           TEXT        NOT NULL DEFAULT '',
    env                           TEXT        NOT NULL
                                      CHECK (env IN ('development', 'staging', 'production')),
    project                       TEXT        NOT NULL DEFAULT '',
    provider                      TEXT        NOT NULL
                                      CHECK (provider IN ('openai', 'anthropic', 'google', 'azure_openai', 'bedrock')),
    model                         TEXT        NOT NULL DEFAULT '',
    period_start                  TIMESTAMPTZ NOT NULL,
    period_end                    TIMESTAMPTZ NOT NULL
                                      CHECK (period_end >= period_start),
    estimated_cost_usd_minor_units    BIGINT NOT NULL DEFAULT 0
                                              CHECK (estimated_cost_usd_minor_units >= 0),
    reconciled_cost_usd_minor_units   BIGINT NOT NULL DEFAULT 0
                                              CHECK (reconciled_cost_usd_minor_units >= 0),
    drift_usd_minor_units             BIGINT NOT NULL DEFAULT 0,
    drift_ratio                       DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    catalog_version               TEXT        NOT NULL DEFAULT '',
    correlation_key               TEXT        NOT NULL,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, provider, model, period_start, period_end)
);

CREATE INDEX IF NOT EXISTS cost_reconciliation_drift_tenant_id_idx
    ON control_plane.cost_reconciliation_drift (tenant_id);
CREATE INDEX IF NOT EXISTS cost_reconciliation_drift_lookup_idx
    ON control_plane.cost_reconciliation_drift (tenant_id, provider, model, period_start, period_end);
CREATE INDEX IF NOT EXISTS cost_reconciliation_drift_correlation_idx
    ON control_plane.cost_reconciliation_drift (correlation_key);

COMMENT ON TABLE control_plane.cost_reconciliation_drift IS
    'F017 drift records joining runtime estimatedCostUsd (catalog × tokens) with reconciledCostUsd (FOCUS). Owned by apps/worker/cost-mapper. One row per (tenant, provider, model, period) tuple.';
COMMENT ON COLUMN control_plane.cost_reconciliation_drift.correlation_key IS
    'Deterministic join key: <tenant>:<provider>:<model>:<period_start_unix>:<period_end_unix>. Same input → same row.';
COMMENT ON COLUMN control_plane.cost_reconciliation_drift.drift_ratio IS
    'Signed ratio: (reconciled - estimated) / max(reconciled, 1). Positive => exporter billed more than the runtime estimate.';

ALTER TABLE control_plane.cost_reconciliation_drift ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.cost_reconciliation_drift FORCE ROW LEVEL SECURITY;

CREATE POLICY cost_reconciliation_drift_tenant_isolation
    ON control_plane.cost_reconciliation_drift
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd


-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Down migration for 2026051801_f017_cost_reconciliation_drift.up.sql.

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS cost_reconciliation_drift_tenant_isolation
    ON control_plane.cost_reconciliation_drift;

DROP TABLE IF EXISTS control_plane.cost_reconciliation_drift;

-- +goose StatementEnd
