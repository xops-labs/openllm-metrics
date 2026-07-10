-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F036 — Routing-Decision Explainability ledger.
--
-- Append-only operational record of routing decisions, consumed by the
-- decision-service (routing.decision.v1) and rendered read-only in the admin
-- console. OSS-safe: this is storage + display of whatever routing.Decider
-- emitted; the decision logic itself is outside this table.
--
-- The decision-service treats tenant_id and the label columns as opaque text
-- and filters by tenant_id directly (no RLS GUC), so no row-level security is
-- attached here.

-- +goose Up
-- +goose StatementBegin
CREATE SCHEMA IF NOT EXISTS routing;

CREATE TABLE IF NOT EXISTS routing.routing_decisions (
    id                 BIGSERIAL PRIMARY KEY,
    decision_id        TEXT NOT NULL UNIQUE,
    tenant_id          TEXT NOT NULL,
    team               TEXT NOT NULL DEFAULT '',
    app                TEXT NOT NULL DEFAULT '',
    env                TEXT NOT NULL DEFAULT '',
    project            TEXT NOT NULL DEFAULT '',
    provider_requested TEXT NOT NULL DEFAULT '',
    model_requested    TEXT NOT NULL DEFAULT '',
    route_requested    TEXT NOT NULL DEFAULT '',
    request_id_hash    TEXT NOT NULL DEFAULT '',
    provider_chosen    TEXT NOT NULL,
    model_chosen       TEXT NOT NULL,
    route_chosen       TEXT NOT NULL DEFAULT '',
    reason_chain       JSONB NOT NULL DEFAULT '[]'::jsonb,
    alternatives       JSONB NOT NULL DEFAULT '[]'::jsonb,
    decider_version    TEXT NOT NULL DEFAULT '',
    decided_at         TIMESTAMPTZ NOT NULL,
    ingested_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS routing_decisions_tenant_decided_idx
    ON routing.routing_decisions (tenant_id, decided_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS routing.routing_decisions;
-- Schema intentionally left in place; it may be in use by other migrations.
-- +goose StatementEnd
