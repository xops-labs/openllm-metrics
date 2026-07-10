#!/usr/bin/env bash
# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# migrate.sh — goose migration driver for OpenLLM Metrics.
#
# Usage:
#   ./tools/scripts/migrate.sh <command> <schema>
#
# Commands:
#   apply      Apply all pending migrations
#   dry-run    Print pending SQL without executing
#   rollback   Rollback the last applied migration
#   rehearse   Apply and immediately rollback (smoke test)
#   status     Show applied/pending migration status
#
# Schema names: control_plane | gateway | scoring | audit
#
# Environment variables (override via .env):
#   POSTGRES_HOST  (default: localhost)
#   POSTGRES_PORT  (default: 5433 — the host port docker-compose publishes)
#   POSTGRES_DB    (default: openllm_metrics)
#   POSTGRES_USER  (default: openllm)
#   POSTGRES_PASSWORD

set -euo pipefail

COMMAND="${1:-}"
SCHEMA="${2:-}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MIGRATIONS_DIR="$ROOT/platform/db/$SCHEMA/migrations"

usage() {
  echo "Usage: $0 <apply|dry-run|rollback|rehearse|status> <schema>"
  echo "Schemas: control_plane | gateway | scoring | audit"
  exit 1
}

[[ -z "$COMMAND" || -z "$SCHEMA" ]] && usage
[[ -d "$MIGRATIONS_DIR" ]] || { echo "ERROR: No migrations directory at $MIGRATIONS_DIR"; exit 1; }

# Load .env if present
[[ -f "$ROOT/.env" ]] && set -o allexport && source "$ROOT/.env" && set +o allexport

PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5433}"
PG_DB="${POSTGRES_DB:-openllm_metrics}"
PG_USER="${POSTGRES_USER:-openllm}"
PG_PASS="${POSTGRES_PASSWORD:-devpassword}"

DSN="postgres://$PG_USER:$PG_PASS@$PG_HOST:$PG_PORT/$PG_DB?sslmode=disable"

# Per-schema version tracking. Goose's default (public.goose_db_version)
# is shared across every schema in the same database — applying
# control_plane's migration 00002 would then mark migration 1 as
# globally applied, causing gateway/scoring/audit's own 00001 baselines
# to be silently skipped. We pin a per-schema table so each schema
# tracks its own state independently.
GOOSE_TABLE="$SCHEMA.goose_db_version"

require_goose() {
  if ! command -v goose &>/dev/null; then
    echo "ERROR: goose not found. Install: go install github.com/pressly/goose/v3/cmd/goose@latest"
    exit 1
  fi
}

require_goose

case "$COMMAND" in
  apply)
    echo "==> Applying migrations for schema: $SCHEMA"
    goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" up
    ;;
  dry-run)
    echo "==> Pending migrations for schema: $SCHEMA (dry-run)"
    goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" up --dry-run 2>/dev/null || \
      goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" status
    ;;
  rollback)
    echo "==> Rolling back last migration for schema: $SCHEMA"
    goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" down
    ;;
  rehearse)
    echo "==> Rehearsing migrations for schema: $SCHEMA (apply then rollback)"
    goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" up
    goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" down-to 0
    echo "Rehearsal passed."
    ;;
  status)
    echo "==> Migration status for schema: $SCHEMA"
    goose -dir "$MIGRATIONS_DIR" -table "$GOOSE_TABLE" postgres "$DSN" status
    ;;
  *)
    usage
    ;;
esac
