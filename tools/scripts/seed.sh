#!/usr/bin/env bash
# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# seed.sh — load demo seed data into the local development database.
#
# Usage:
#   ./tools/scripts/seed.sh [--dry-run]
#
# Options:
#   --dry-run   Print the SQL that would be applied without executing it.
#
# Environment variables (override via .env):
#   POSTGRES_HOST      (default: localhost)
#   POSTGRES_PORT      (default: 5433 — the host port docker-compose publishes)
#   POSTGRES_DB        (default: openllm_metrics)
#   POSTGRES_USER      (default: openllm)
#   POSTGRES_PASSWORD  (default: devpassword)

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SEED_DIR="$ROOT/platform/db/seeds"
# Every numbered seed file, in order (001 identity, 002 governance, ...).
SEED_FILES=("$SEED_DIR"/0*.sql)

DRY_RUN=false
for arg in "$@"; do
  [[ "$arg" == "--dry-run" ]] && DRY_RUN=true
done

[[ -f "${SEED_FILES[0]}" ]] || { echo "ERROR: No seed files found at $SEED_DIR/0*.sql"; exit 1; }

# Load .env if present
[[ -f "$ROOT/.env" ]] && set -o allexport && source "$ROOT/.env" && set +o allexport

PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5433}"
PG_DB="${POSTGRES_DB:-openllm_metrics}"
PG_USER="${POSTGRES_USER:-openllm}"
export PGPASSWORD="${POSTGRES_PASSWORD:-devpassword}"

require_psql() {
  if ! command -v psql &>/dev/null; then
    echo "ERROR: psql not found. Install PostgreSQL client tools."
    exit 1
  fi
}

require_psql

if [[ "$DRY_RUN" == "true" ]]; then
  echo "==> Dry run: seed SQL that would be applied"
  for f in "${SEED_FILES[@]}"; do
    echo "-- ===== $f ====="
    cat "$f"
  done
  exit 0
fi

echo "==> Applying seed data to $PG_DB on $PG_HOST:$PG_PORT"
for f in "${SEED_FILES[@]}"; do
  echo "==> $f"
  psql \
    --host="$PG_HOST" \
    --port="$PG_PORT" \
    --dbname="$PG_DB" \
    --username="$PG_USER" \
    --file="$f" \
    --no-password
done
echo "==> Done."
