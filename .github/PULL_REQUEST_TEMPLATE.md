<!-- PR title must follow Conventional Commits: <type>(<scope>): <short summary> -->
<!-- Types: feat fix chore docs test refactor perf ci · Scope: feature ID or package, e.g. feat(F010), fix(gateway) -->

## Summary

<!-- What does this PR change and why? Link any relevant issue or design doc. -->

## Test Plan

<!-- How was this verified? Paste the commands and relevant output, e.g.:
     ./tools/scripts/lint.sh
     ./tools/scripts/test.sh
     docker compose up -d (if compose/services changed) -->

---

**Before requesting review** (see [CONTRIBUTING.md](../CONTRIBUTING.md)):

- [ ] `./tools/scripts/lint.sh` and `./tools/scripts/test.sh` pass locally
- [ ] No prompts/completions, provider API keys, or secrets in code, logs, or fixtures
- [ ] New cross-service messages conform to a versioned contract under `packages/contracts/`
- [ ] Tenant labels (`tenant`/`team`/`app`/`env`/`project`) carried on any new signal
- [ ] This PR does **not** implement OSS-deferred algorithms (scoring, routing, fallback, policy evaluation, anomaly detection) — see [CONTRIBUTING.md → Confidentiality Rules](../CONTRIBUTING.md#confidentiality-rules)
