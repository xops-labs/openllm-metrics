# Retention Policy

This document defines retention defaults and downsampling strategy for the
OpenLLM Metrics TSDB layer.

---

## Defaults

| Tier              | Retention | Resolution                    | Backend                                 |
| ----------------- | --------- | ----------------------------- | --------------------------------------- |
| Raw               | 14 days   | Native scrape interval (15 s) | Local Prometheus or remote-write target |
| Downsampled       | 90 days   | 5-minute averages             | VictoriaMetrics / Mimir recording rules |
| Long-term archive | 1 year    | 1-hour aggregates             | Object storage (future)                 |

The 14-day raw retention is configured via the `--storage.tsdb.retention.time=14d`
flag in `docker-compose.yml`. For staging and production, override this with
the appropriate value for the deployment environment.

---

## Recording Rules for Downsampling

Downsampling recording rules will be added in **F012 - FinOps Dashboard Pack**
when the full label set is available. The patterns are:

```yaml
# 5-minute request rate, grouped by provider/model/tenant
- record: llm:requests:rate5m
  expr: sum by (provider, model, tenant, env) (rate(llm_requests_total[5m]))

# 5-minute input token rate
- record: llm:input_tokens:rate5m
  expr: sum by (provider, model, tenant, env) (rate(llm_input_tokens_total[5m]))

# 5-minute cost rate (USD/s, multiply × 3600 for hourly)
- record: llm:cost_usd:rate5m
  expr: sum by (provider, model, tenant, env) (rate(llm_cost_usd_total[5m]))
```

---

## Remote-Write and Long-Term Storage

See `remote_write_targets.yml` for backend-specific configuration blocks.

- **VictoriaMetrics** supports built-in downsampling (`--downsampling.period`).
  Set `--downsampling.period=5m:90d,1h:365d` for the tiered retention above.
- **Grafana Mimir** uses ruler recording rules and the object-store compactor
  for downsampling.
- **Self-hosted Prometheus** requires a separate Thanos or Cortex sidecar for
  downsampled long-term storage.

---

## Storage Sizing Guidance

Rule of thumb for Prometheus local storage:

```text
bytes ≈ active_series × samples_per_series_per_second × bytes_per_sample × retention_seconds
      ≈ 100_000 × (1/15) × 2 × (14 × 86_400)
      ≈ ~16 GB
```

Allocate at least 3× this estimate for WAL headroom and block compaction.
The `prometheus-data` Docker volume should be backed by a volume with at
least 64 GB available for a 14-day dev retention window.
