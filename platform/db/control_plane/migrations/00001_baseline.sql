-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: control_plane baseline — schema and migration tracking table.
-- Tool: goose (https://github.com/pressly/goose)
-- Direction: Up / Down

-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS control_plane.schema_migrations (
    version     BIGINT      NOT NULL PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE control_plane.schema_migrations IS
    'Migration tracking table for the control_plane schema.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS control_plane.schema_migrations;
-- +goose StatementEnd
