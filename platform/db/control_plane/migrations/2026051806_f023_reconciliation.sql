-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F023 — Pull-Mode / Proxy-Mode Reconciliation Framework.
--
-- The reconciler worker (apps/worker/reconciler/) subscribes to two bus
-- topics:
--
--   * llm.cost.estimated   — runtime-side estimates produced by cost-mapper
--                            from gateway/SDK token counts and the pricing
--                            catalog (source = gateway | sdk).
--   * llm.usage.reconciled — vendor-reconciled cost derived from the
--                            upstream llm-usage-exporter /focus.json
--                            endpoint via focus-ingester (source = exporter).
--
-- It joins them in a windowed correlation on
--     (tenant_id, provider, model, window_start)
-- and writes one row per closed window to
--     control_plane.reconciliation_results.
--
-- Drift math (deliberately trivial — anything richer is not implemented here):
--     drift_usd   = reconciled_cost_usd - estimated_cost_usd
--     drift_ratio = drift_usd / max(estimated_cost_usd, 0.0001)
--
-- Window lifecycle (status column):
--     open         — current cycle is still inside the window
--     closed       — window_end has passed plus the reconciliation grace
--                    period; both sides may or may not be present
--     reconciled   — closed with both estimate and reconciled present
--     unreconciled — closed with exactly one side present (the other
--                    never arrived inside the grace period)
--
-- The unique key plus an ON CONFLICT DO UPDATE write pattern keeps the
-- consumer idempotent: replaying the same input pair against the same
-- window produces exactly one row.
--
-- Tool: goose (file name uses a unique 2026051806 timestamp prefix so
--             parallel agents' numbered migrations cannot collide).
-- Direction: Up

-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS control_plane.reconciliation_results (
    id                       BIGSERIAL    NOT NULL PRIMARY KEY,
    tenant_id                UUID         NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    team                     TEXT         NOT NULL DEFAULT '',
    app                      TEXT         NOT NULL DEFAULT '',
    env                      TEXT         NOT NULL
                                  CHECK (env IN ('development', 'staging', 'production')),
    project                  TEXT         NOT NULL DEFAULT '',
    provider                 TEXT         NOT NULL
                                  CHECK (provider IN ('openai', 'anthropic', 'google', 'azure_openai', 'bedrock')),
    model                    TEXT         NOT NULL DEFAULT '',
    window_start             TIMESTAMPTZ  NOT NULL,
    window_end               TIMESTAMPTZ  NOT NULL
                                  CHECK (window_end > window_start),
    estimated_cost_usd       NUMERIC(20,6) NOT NULL DEFAULT 0,
    reconciled_cost_usd      NUMERIC(20,6) NOT NULL DEFAULT 0,
    drift_usd                NUMERIC(20,6) NOT NULL DEFAULT 0,
    drift_ratio              DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    status                   TEXT         NOT NULL DEFAULT 'open'
                                  CHECK (status IN ('open', 'closed', 'reconciled', 'unreconciled')),
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, provider, model, window_start)
);

-- Lookup for joiner upserts (the natural key already has a unique index,
-- but we keep the explicit index name available for query plans).
CREATE INDEX IF NOT EXISTS reconciliation_results_tenant_id_idx
    ON control_plane.reconciliation_results (tenant_id);

-- Close-out scan: the closer worker walks 'open' rows whose window_end +
-- grace period has elapsed. The composite index keeps that scan cheap.
CREATE INDEX IF NOT EXISTS reconciliation_results_status_window_end_idx
    ON control_plane.reconciliation_results (tenant_id, status, window_end);

COMMENT ON TABLE control_plane.reconciliation_results IS
    'F023 windowed reconciliation rows joining runtime estimated_cost_usd (cost-mapper / llm.cost.estimated) with vendor reconciled_cost_usd (focus-ingester / llm.usage.reconciled). Owned by apps/worker/reconciler. One row per (tenant, provider, model, window_start) tuple.';
COMMENT ON COLUMN control_plane.reconciliation_results.window_start IS
    'Inclusive UTC start of the correlation window. The joiner truncates input event timestamps to the configured window size before bucketing.';
COMMENT ON COLUMN control_plane.reconciliation_results.window_end IS
    'Exclusive UTC end of the correlation window (window_start + window_size).';
COMMENT ON COLUMN control_plane.reconciliation_results.drift_usd IS
    'Signed difference: reconciled_cost_usd - estimated_cost_usd. Positive => vendor billed more than the runtime estimate predicted (stale catalog, missing discount).';
COMMENT ON COLUMN control_plane.reconciliation_results.drift_ratio IS
    'Signed ratio: drift_usd / max(estimated_cost_usd, 0.0001). Bounded denominator avoids divide-by-zero for windows that only carry a reconciled side.';
COMMENT ON COLUMN control_plane.reconciliation_results.status IS
    'open => inside the window or grace period; closed => grace elapsed; reconciled => closed with both sides present; unreconciled => closed with only one side present.';

ALTER TABLE control_plane.reconciliation_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.reconciliation_results FORCE  ROW LEVEL SECURITY;

CREATE POLICY reconciliation_results_tenant_isolation
    ON control_plane.reconciliation_results
    USING      (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd


-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Down migration for 2026051806_f023_reconciliation.up.sql.

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS reconciliation_results_tenant_isolation
    ON control_plane.reconciliation_results;

DROP TABLE IF EXISTS control_plane.reconciliation_results;

-- +goose StatementEnd
