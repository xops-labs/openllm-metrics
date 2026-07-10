/**
 * Capability probes for the native analytics screens.
 *
 * Some `llm_*` series only exist when an OPTIONAL pipeline is running, as
 * opposed to the always-on gateway runtime stack:
 *
 *   - `llm_cost_usd_total` is emitted only when the COST pipeline runs
 *     (cost-mapper -> focus-ingester -> reconciler/exporter). In a
 *     gateway-only deployment the series is STRUCTURALLY ABSENT — the TSDB has
 *     never seen it, so a query returns an empty result not because there is no
 *     traffic but because nothing produces the metric.
 *   - `llm_reconciliation_drift_usd` (and the other `llm_reconciliation_*`
 *     gauges) exist only when the F023 RECONCILER runs.
 *
 * Contrast with lazily-initialized runtime counters such as `llm_errors_total`,
 * which ARE wired into the gateway runtime path but stay absent until the first
 * event of that class — an empty result there means "no failures observed", not
 * "missing pipeline". That distinction is handled in the error screen itself;
 * this module is only about pipelines that are structurally on or off.
 *
 * These probes ask the TSDB whether it KNOWS a series by name. They are used to
 * render an accurate empty-state ("enable the cost profile") instead of a
 * generic "no data" panel when an optional pipeline is simply not deployed.
 *
 * Privacy invariant: probes touch only metric NAMES via `__name__`; no prompt
 * or completion text and no per-label tenant values are read here. Dependency
 * free — mirrors the base-URL resolution and failure-tolerant fetch used by
 * `lib/api/metrics.ts`, kept separate so the data helpers stay focused on
 * rendering series.
 */

// Same base-URL resolution as lib/api/metrics.ts: an explicit query URL via
// env, otherwise a local Prometheus default. Resolved per-call (not cached at
// module load) so it stays correct under hot reload / test env mutation.
function queryBase(): string {
  return process.env.OLM_METRICS_QUERY_URL ?? 'http://localhost:9090';
}

/**
 * Minimal, failure-tolerant GET against the TSDB query API. Mirrors `promFetch`
 * in metrics.ts but is intentionally local so this module has no dependency on
 * the data-fetching helpers. Returns the parsed JSON, or `null` on any failure
 * (offline, non-2xx, unparseable) so callers can fall back to "absent".
 */
async function tsdbFetch(path: string, params: URLSearchParams): Promise<unknown | null> {
  try {
    const res = await fetch(`${queryBase()}${path}?${params.toString()}`, {
      // Capability is a live property of the TSDB — never cache.
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    });
    if (!res.ok) {
      return null;
    }
    return (await res.json()) as unknown;
  } catch {
    // Service offline / DNS / parse error -> treat the metric as unknown.
    return null;
  }
}

/** Narrowing helpers for the two TSDB response shapes we probe. */
function isInstantNonEmpty(body: unknown): boolean {
  // { status: 'success', data: { resultType: 'vector', result: [...] } }
  if (typeof body !== 'object' || body === null) return false;
  const b = body as { status?: unknown; data?: { result?: unknown } };
  if (b.status !== 'success') return false;
  return Array.isArray(b.data?.result) && b.data.result.length > 0;
}

function labelValuesInclude(body: unknown, name: string): boolean {
  // { status: 'success', data: ['llm_requests_total', 'llm_cost_usd_total', ...] }
  if (typeof body !== 'object' || body === null) return false;
  const b = body as { status?: unknown; data?: unknown };
  if (b.status !== 'success' || !Array.isArray(b.data)) return false;
  return b.data.includes(name);
}

/**
 * True if the TSDB KNOWS the named series, i.e. the metric is structurally
 * present (regardless of whether the current tenant/window has samples).
 *
 * Strategy: first try an instant `query=<name>` and accept any non-empty
 * result. If that comes back empty (which can happen when the metric exists but
 * has aged out of the active sample set), fall back to `__name__` label-values
 * membership, which reflects the series the TSDB has ever ingested. Offline or
 * on any error -> `false` (treated as absent), so screens degrade to an
 * explanatory empty-state rather than crashing.
 *
 * Only a bare metric name is expected here; no label selectors, so no tenant
 * value or other label is ever sent.
 */
export async function metricExists(name: string): Promise<boolean> {
  // Fast path: does an instant query for the metric return any series?
  const instant = await tsdbFetch('/api/v1/query', new URLSearchParams({ query: name }));
  if (isInstantNonEmpty(instant)) {
    return true;
  }

  // Fallback: is the name in the TSDB's set of known metric names? This catches
  // metrics that exist but currently have no live samples.
  const labels = await tsdbFetch(
    '/api/v1/label/__name__/values',
    new URLSearchParams({ 'match[]': name }),
  );
  return labelValuesInclude(labels, name);
}

/**
 * Whether the COST pipeline is producing data. `llm_cost_usd_total` is emitted
 * by cost-mapper -> focus-ingester -> reconciler/exporter; absent in a
 * gateway-only stack. Drives the "Cost over time" empty-state.
 */
export async function costPipelineEnabled(): Promise<boolean> {
  return metricExists('llm_cost_usd_total');
}

/**
 * Whether the F023 RECONCILER is producing data. `llm_reconciliation_drift_usd`
 * is refreshed only by the reconciler as windows close. Drives the
 * "Reconciliation drift" empty-state.
 */
export async function reconciliationEnabled(): Promise<boolean> {
  return metricExists('llm_reconciliation_drift_usd');
}
