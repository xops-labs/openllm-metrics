-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: audit baseline.
-- The audit schema holds append-only records. No UPDATE or DELETE allowed
-- on audit rows; enforced via row-level security in F031.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS audit.schema_migrations (
    version     BIGINT      NOT NULL PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit.schema_migrations;
-- +goose StatementEnd
