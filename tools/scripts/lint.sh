#!/usr/bin/env bash
# Run all linters across Go and TypeScript workspaces.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

FAILURES=0

echo "==> Go lint..."
if ls go.work &>/dev/null; then
  modules="$(go list -f '{{.Dir}}' -m)"
  while IFS= read -r dir; do
    [ -z "$dir" ] && continue
    echo "  -> $dir"
    (cd "$dir" && golangci-lint run --config "$ROOT/.golangci.yml" ./...) || FAILURES=$((FAILURES + 1))
  done <<< "$modules"
else
  echo "  No go.work found — skipping Go lint"
fi

echo ""
echo "==> TypeScript format check..."
pnpm format:check || FAILURES=$((FAILURES + 1))

echo ""
echo "==> TypeScript lint..."
# --if-present keeps this green for packages without a lint script,
# while real lint failures still count.
pnpm -r --if-present lint || FAILURES=$((FAILURES + 1))

echo ""
echo "==> Markdown lint..."
if command -v markdownlint-cli2 &>/dev/null; then
  markdownlint-cli2 "**/*.md" "!**/node_modules/**" --config .markdownlint.json || FAILURES=$((FAILURES + 1))
else
  echo "  markdownlint-cli2 not found — skipping (run: pnpm add -g markdownlint-cli2)"
fi

if [ "$FAILURES" -gt 0 ]; then
  echo ""
  echo "ERROR: $FAILURES linter(s) failed."
  exit 1
fi

echo ""
echo "All linters passed."
