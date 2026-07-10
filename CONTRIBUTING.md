# Contributing to OpenLLM Metrics

Thank you for considering a contribution. This document covers everything you need to get started.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Commit Conventions](#commit-conventions)
- [Pull Request Process](#pull-request-process)
- [Implementation Scope](#implementation-scope)
- [License](#license)

## Code of Conduct

This project follows the Contributor Covenant — see [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
By participating you agree to uphold these standards.

## Getting Started

```bash
git clone https://github.com/xops-labs/openllm-metrics.git
cd openllm-metrics

# One-time: enable pnpm via Corepack so `pnpm` is on PATH directly.
# Root npm scripts (`pnpm test`, `pnpm lint`, `pnpm typecheck`) shell out
# to bare `pnpm -r ...`, so `corepack pnpm ...` alone is not enough on
# a fresh machine — you need `pnpm` itself on PATH.
corepack enable pnpm

./tools/scripts/bootstrap.sh
```

Prerequisites: Go 1.25+, Node.js 20+, pnpm 9+ (via `corepack enable pnpm`), Docker.

## Development Workflow

1. **Create a branch** from `main`:

   ```bash
   git checkout -b feat/your-feature-name
   ```

2. **Implement** — follow the conventions in the surrounding services and the architecture docs under `docs/`.

3. **Lint and test**:

   ```bash
   ./tools/scripts/lint.sh
   ./tools/scripts/test.sh
   ```

   > **Windows note:** `test.sh` runs `go test -race`, which requires cgo and a
   > C toolchain on Windows. Either install one (e.g. mingw-w64 with
   > `CGO_ENABLED=1`), run the Go tests without `-race` (`go test ./...` per
   > module), or run the suite in Docker/WSL2.

4. **Open a PR** targeting `main`. All CI checks must pass before review.

### Protected Paths

The following paths require an additional explicit maintainer review on top of
the standard review (see `.github/CODEOWNERS`):

- `apps/gateway/`
- `apps/api/policy-service/`
- `apps/api/audit-service/`
- `apps/worker/usage-poller/`
- `platform/db/audit/`

## Commit Conventions

Use [Conventional Commits](https://www.conventionalcommits.org/):

```text
<type>(<scope>): <short summary>
```

Types: `feat`, `fix`, `chore`, `docs`, `test`, `refactor`, `perf`, `ci`.

Scope is the feature ID or package name: `feat(F003)`, `fix(gateway)`.

## Pull Request Process

1. Ensure your PR title follows the commit convention above.
2. Fill in the Summary and Test Plan sections of the PR description.
3. Link any relevant issue or design doc if applicable.
4. All six CI jobs (`.github/workflows/ci.yml`) must pass: lint, typecheck, test, dashboards (Grafana dashboard + alert-rule validation), build (every `go.work` module), and secret-scan.
5. At least one approval required; protected paths need an additional maintainer review.

## Implementation Scope

The open-source repository includes telemetry capture, normalization, storage,
dashboards, alerts, policy schemas, audit ledgers, decision ledgers, SDKs, and
safe default implementations for pluggable interfaces.

Before opening a PR that adds new scoring, routing, fallback, anomaly, or
policy-evaluation semantics, start with an issue or design discussion. Those
changes affect public contracts and need agreement on data shape, safety, and
test coverage before implementation.
## License

By contributing, you agree your contributions are licensed under [Apache 2.0](LICENSE).
