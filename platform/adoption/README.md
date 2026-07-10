# Adoption Pin Files

Declared reference versions for the third-party components this repo
integrates with. Each file in this directory holds a single version string
so the reference version is stated in exactly one place. Compose cannot
read arbitrary files, so the compose files carry a hardcoded default image
tag that must match the pin.

## Entries

| File                         | Upstream                                                                          | Used as                                                                                                                        |
| ---------------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `llm-usage-exporter.version` | [`xops-labs/llm-usage-exporter`](https://github.com/xops-labs/llm-usage-exporter) | Default image tag for the optional `--profile exporter` pull-mode add-on. Bring your own image via `LLM_USAGE_EXPORTER_IMAGE`. |

The exporter is **not** part of the default stack and there is no bundled
internal service: telemetry is runtime-first (the in-repo Go gateway +
SDKs), and pull-mode billing reconciliation is an opt-in add-on started
with `docker compose --profile exporter up -d`. Always set
`LLM_USAGE_EXPORTER_IMAGE` to an exporter image you can pull — the pinned
upstream default in the compose files may not be publicly pullable. The
history of this decision lives in
[`docs/architecture/bundled-vs-external.md`](../../docs/architecture/bundled-vs-external.md)
(see the v0.1.0 direction update at the top).

Upgrading a pin is a one-PR change: bump the version string here and the
matching hardcoded default image tags in the root `docker-compose.yml` and
`platform/deployment/compose/quickstart.yml` (and the Helm `values.yaml`
once it exists).
