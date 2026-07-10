-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: scoring baseline.

-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS scoring.schema_migrations (
    version     BIGINT      NOT NULL PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS scoring.schema_migrations;
-- +goose StatementEnd
