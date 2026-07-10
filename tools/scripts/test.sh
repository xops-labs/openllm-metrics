#!/usr/bin/env bash
# Run all test suites across Go and TypeScript workspaces.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

FAILURES=0

echo "==> Go tests..."
if ls go.work &>/dev/null; then
  # go.work sits at the repo root but has no module of its own,
  # so `go test ./...` from here errors. Iterate the workspace modules.
  modules="$(go list -f '{{.Dir}}' -m)"
  while IFS= read -r dir; do
    [ -z "$dir" ] && continue
    echo "  -> $dir"
    (cd "$dir" && go test -race ./...) || FAILURES=$((FAILURES + 1))
  done <<< "$modules"
else
  echo "  No go.work found — skipping Go tests"
fi

echo ""
echo "==> TypeScript tests..."
# --if-present keeps this green for packages without a test script,
# while real test failures still count.
pnpm -r --if-present test || FAILURES=$((FAILURES + 1))

if [ "$FAILURES" -gt 0 ]; then
  echo ""
  echo "ERROR: $FAILURES test suite(s) failed."
  exit 1
fi

echo ""
echo "All tests passed."
