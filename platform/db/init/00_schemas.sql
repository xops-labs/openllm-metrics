-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Bootstrap: create per-service schema namespaces.
-- Run once on a fresh database; idempotent.

CREATE SCHEMA IF NOT EXISTS control_plane;
CREATE SCHEMA IF NOT EXISTS gateway;
CREATE SCHEMA IF NOT EXISTS scoring;
CREATE SCHEMA IF NOT EXISTS policy;
CREATE SCHEMA IF NOT EXISTS audit;

-- Migration tooling schema (goose)
CREATE SCHEMA IF NOT EXISTS migrations;
