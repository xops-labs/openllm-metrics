#!/usr/bin/env bash
# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# build-check.sh — compile-check the llmproviderreceiver module.
#
# This script is the CI gate for the OTel receiver. It verifies that the
# receiver's Go module compiles cleanly with `go build ./...` and that
# `go vet ./...` reports no issues. It does NOT run `ocb` (the full custom-
# distribution build); that step requires network access to download contrib
# dependencies and is handled separately by the release workflow.
#
# Usage:
#   bash platform/otel-collector/build-check.sh
#
# Exit codes:
#   0  — all checks passed
#   1  — go build or go vet failed; see output for details
#
# Requirements:
#   - Go 1.25 or later on PATH
#   - The receiver's go.sum is committed and covers all dependencies
#
# The receiver module is part of the top-level go.work file for development,
# but ocb consumes it as a standalone module. See the comment in
# platform/otel-collector/receiver/llmproviderreceiver/go.mod for the
# rationale. We cd into the module directory before running any Go commands.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RECEIVER_DIR="${SCRIPT_DIR}/receiver/llmproviderreceiver"

echo "==> llmproviderreceiver build check"
echo "    module dir: ${RECEIVER_DIR}"

if ! command -v go &>/dev/null; then
  echo "ERROR: 'go' not found on PATH. Install Go 1.25+ and retry." >&2
  exit 1
fi

GO_VERSION="$(go version)"
echo "    go version: ${GO_VERSION}"

echo ""
echo "==> go build ./..."
(cd "${RECEIVER_DIR}" && go build ./...)
echo "    PASS"

echo ""
echo "==> go vet ./..."
(cd "${RECEIVER_DIR}" && go vet ./...)
echo "    PASS"

echo ""
echo "All checks passed."
echo ""
echo "To build the full custom Collector distribution (requires network):"
echo "  go install go.opentelemetry.io/collector/cmd/builder@latest"
echo "  builder --config ${SCRIPT_DIR}/builder-config.yaml"
echo ""
echo "Then run with:"
echo "  ./_build/openllm-otelcol --config ${SCRIPT_DIR}/example-config.yaml"
