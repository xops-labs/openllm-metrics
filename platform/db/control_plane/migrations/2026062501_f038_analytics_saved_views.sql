-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F038 — Analytics Saved Views (OSS).
--
-- One table in the control_plane schema:
--   * analytics_saved_views — per-tenant saved dashboard definitions for the
--     native analytics screens. A "saved view" is a declarative spec (metric,
--     groupBy labels, filters, range wrapper, visualization kind) rendered by
--     the admin console's analytics dashboard grid.
--
-- OSS scope is intentionally narrow: the spec column carries only the
-- normalized llm_* selector shape (metric name + label dimensions). No prompt
-- or completion text, scoring weights, routing logic, or anomaly rules ever
-- live here. The admin console ships four built-in
-- DEFAULT views in code so the dashboards screen works with zero rows; this
-- table persists any additional user-defined views once an analytics backend
-- service is present to read/write it. Without that service the table is
-- simply unused and the console degrades to the built-in defaults.
--
-- Multi-tenant invariants:
--   - Every row carries tenant_id.
--   - RLS is enabled and forced on the table.
--   - UNIQUE (tenant_id, name) keeps view names stable per tenant so the
--     console can dedupe built-in defaults against persisted views by name.
--
-- Tool: goose
-- Direction: Up

-- +goose Up
-- +goose StatementBegin

-- ------------------------------------------------------------------
-- analytics_saved_views
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS control_plane.analytics_saved_views (
    id           UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    spec         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    description  TEXT        NOT NULL DEFAULT '',
    position     INTEGER     NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS analytics_saved_views_tenant_idx
    ON control_plane.analytics_saved_views (tenant_id)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE control_plane.analytics_saved_views IS
    'F038 OSS saved dashboard views. spec is a declarative llm_* selector (metric, groupBy, filters, wrap, viz). Built-in DEFAULT views ship in the admin console; this table persists additional user-defined views when an analytics backend service is present.';
COMMENT ON COLUMN control_plane.analytics_saved_views.spec IS
    'Declarative view spec: { metric, groupBy[], filters{}, wrap, windowSeconds, viz }. Normalized llm_* selector shape only — no prompt/completion text, scoring, routing, or anomaly logic.';

-- ------------------------------------------------------------------
-- Row-level security
-- ------------------------------------------------------------------
ALTER TABLE control_plane.analytics_saved_views  ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.analytics_saved_views  FORCE  ROW LEVEL SECURITY;

CREATE POLICY analytics_saved_views_tenant_isolation
    ON control_plane.analytics_saved_views
    USING      (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd


-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Down migration for 2026062501_f038_analytics_saved_views.up.sql.

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS analytics_saved_views_tenant_isolation
    ON control_plane.analytics_saved_views;

DROP TABLE IF EXISTS control_plane.analytics_saved_views;

-- +goose StatementEnd
