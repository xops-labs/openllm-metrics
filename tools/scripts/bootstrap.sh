#!/usr/bin/env bash
# Bootstrap local development environment for OpenLLM Metrics.
#
# What it does: checks prerequisites (go/node/pnpm/docker), runs
# `pnpm install`, and installs golangci-lint. It does NOT run DB
# migrations (those run inside `docker compose up` via the db-migrate
# one-shot) and is not needed for a Docker-only run of the stack.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

echo "==> Checking prerequisites..."

require_cmd() {
  if ! command -v "$1" &>/dev/null; then
    echo "ERROR: $1 is required but not installed. $2"
    exit 1
  fi
  echo "  [ok] $1"
}

require_cmd go    "Install from https://go.dev/dl/"
require_cmd node  "Install Node.js 20+ from https://nodejs.org/"
require_cmd pnpm  "Run: corepack enable pnpm  (preferred — uses the version pinned in package.json#packageManager)"
require_cmd docker "Install from https://docs.docker.com/get-docker/"

# Fail early when a detected tool is older than the documented minimum
# (see README "Requirements" and CONTRIBUTING prerequisites). Compares
# major.minor numerically; skips with a warning when the version string
# is not plain numbers (e.g. a Go devel build).
require_min_version() {
  tool=$1 detected=$2 min_major=$3 min_minor=$4 hint=$5
  major=${detected%%.*}
  rest=${detected#*.}
  minor=${rest%%.*}
  case "${major}${minor}" in
    *[!0-9]*|'')
      echo "  [warn] cannot parse $tool version '$detected' (expected >= $min_major.$min_minor); skipping check"
      return 0
      ;;
  esac
  if [ "$major" -lt "$min_major" ] || { [ "$major" -eq "$min_major" ] && [ "$minor" -lt "$min_minor" ]; }; then
    echo "ERROR: $tool $detected is older than the required minimum $min_major.$min_minor. $hint"
    exit 1
  fi
  echo "  [ok] $tool $detected (>= $min_major.$min_minor)"
}

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
NODE_VERSION=$(node --version | sed 's/v//')
PNPM_VERSION=$(pnpm --version)

echo ""
echo "==> Checking minimum versions..."
require_min_version "Go"      "$GO_VERSION"   1 25 "Install Go 1.25+ from https://go.dev/dl/"
require_min_version "Node.js" "$NODE_VERSION" 20 0 "Install Node.js 20+ from https://nodejs.org/"
require_min_version "pnpm"    "$PNPM_VERSION" 9  0 "Run: corepack enable pnpm  (uses the version pinned in package.json#packageManager)"

echo ""
echo "==> Environment"
echo "  Go:   $GO_VERSION"
echo "  Node: $NODE_VERSION"
echo "  pnpm: $PNPM_VERSION"
echo ""

echo "==> Installing Node dependencies..."
pnpm install

echo ""
echo "==> Installing Go tools..."
if command -v golangci-lint &>/dev/null; then
  echo "  [ok] golangci-lint already installed"
else
  echo "  Installing golangci-lint..."
  # Must be v2.x — .golangci.yml uses the v2 config schema.
  # Keep in sync with GOLANGCI_LINT_VERSION in .github/workflows/ci.yml.
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0
fi

if command -v gitleaks &>/dev/null; then
  echo "  [ok] gitleaks already installed"
else
  echo "  NOTE: gitleaks not found. Install from https://github.com/gitleaks/gitleaks/releases"
fi

echo ""
echo "Bootstrap complete. Run ./tools/scripts/lint.sh and ./tools/scripts/test.sh."
