import { currentUser } from '@/lib/auth';

/**
 * Catalog lookups that power the guided metrics explorer (F038 Phase 2).
 *
 * These helpers hit the SAME Prometheus-compatible TSDB as `metrics.ts`
 * (resolved from `OLM_METRICS_QUERY_URL`) but use the label-introspection
 * endpoints (`/api/v1/label/...`) to enumerate the metric names and label
 * values an operator can pick from in the explorer dropdowns. They never run
 * user-supplied PromQL — they only list options.
 *
 * Privacy / isolation invariants (mirrors `metrics.ts`):
 *   - Only the F008 normalized `llm_*` series and label dimensions are ever
 *     surfaced; no prompt or completion text exists in this data path.
 *   - Label-VALUE lookups are ALWAYS tenant-scoped via a
 *     `match[]={tenant="<tenantId>"}` selector so one tenant's dropdowns can
 *     never reveal another tenant's apps, models, teams, etc.
 *
 * Every helper tolerates an unset/unreachable TSDB by resolving to a safe
 * fallback (the whitelist for metric names, `[]` for label values) so the
 * explorer degrades gracefully instead of crashing.
 */

// Resolved per-call (not cached at module load) so it stays correct under hot
// reload / test env mutation. Mirrors lib/api/metrics-capabilities.ts.
function queryBase(): string {
  return process.env.OLM_METRICS_QUERY_URL ?? 'http://localhost:9090';
}

/**
 * Metrics the explorer is allowed to offer. These are the F008 normalized
 * `llm_*` counters that the metrics-endpoint exposes; the explorer never lets
 * an operator query anything outside this set.
 */
export const METRIC_WHITELIST: readonly string[] = [
  'llm_requests_total',
  'llm_input_tokens_total',
  'llm_output_tokens_total',
  'llm_total_tokens_total',
  'llm_errors_total',
  'llm_timeouts_total',
  'llm_rate_limit_events_total',
  'llm_retries_total',
  'llm_cost_usd_total',
];

/**
 * Label dimensions the explorer is allowed to group by / filter on. These are
 * the normalized F008 labels — never anything carrying free-form text.
 */
export const LABEL_WHITELIST: readonly string[] = [
  'app',
  'model',
  'team',
  'provider',
  'env',
  'project',
  'operation',
  'status_code',
];

interface PromLabelResponse {
  status: 'success' | 'error';
  data?: string[];
  error?: string;
}

/**
 * Minimal label-API fetch. Returns the `data` string array on success, or
 * `null` on any failure (service offline, non-2xx, parse/JSON error). Live
 * catalog data — never cached.
 */
async function labelFetch(path: string, params?: URLSearchParams): Promise<string[] | null> {
  try {
    const qs = params && params.toString().length > 0 ? `?${params.toString()}` : '';
    const res = await fetch(`${queryBase()}${path}${qs}`, {
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    });
    if (!res.ok) {
      return null;
    }
    const body = (await res.json()) as PromLabelResponse;
    if (body.status !== 'success' || !Array.isArray(body.data)) {
      return null;
    }
    return body.data;
  } catch {
    return null;
  }
}

/**
 * Metric names the explorer offers: the intersection of {@link METRIC_WHITELIST}
 * with the names the TSDB actually exposes (so we never offer a metric that has
 * no data). On any failure we fall back to the full whitelist — better to offer
 * a metric that returns no samples than to render an empty dropdown.
 *
 * Metric names are global (not tenant-specific), so no tenant selector is
 * needed here; tenant scoping happens at query time via `buildSeriesQuery`'s
 * always-injected `tenant="$tenant"` matcher.
 */
export async function listMetricNames(): Promise<string[]> {
  const exposed = await labelFetch('/api/v1/label/__name__/values');
  if (!exposed) {
    return [...METRIC_WHITELIST];
  }
  const exposedSet = new Set(exposed);
  return METRIC_WHITELIST.filter((m) => exposedSet.has(m));
}

/** Escape a tenant id so it is safe inside a double-quoted label matcher. */
function escapeMatcherValue(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

/**
 * Values seen for `label`, TENANT-SCOPED so dropdowns never reveal other
 * tenants' values. We constrain the lookup with `match[]={tenant="<tenantId>"}`
 * (the tenant id comes from {@link currentUser}), so the TSDB only returns
 * values that co-occur with the active tenant's series.
 *
 * `label` must be one of {@link LABEL_WHITELIST}; anything else returns `[]`
 * without touching the TSDB. Returns `[]` on any failure.
 */
export async function listLabelValues(label: string): Promise<string[]> {
  if (!LABEL_WHITELIST.includes(label)) {
    return [];
  }

  const user = await currentUser();
  const params = new URLSearchParams();
  // CRITICAL isolation point: every value lookup is scoped to the active
  // tenant. Without this match[] selector the endpoint would list values
  // across ALL tenants.
  params.set('match[]', `{tenant="${escapeMatcherValue(user.tenantId)}"}`);

  const values = await labelFetch(`/api/v1/label/${encodeURIComponent(label)}/values`, params);
  if (!values) {
    return [];
  }
  return [...values].sort((a, b) => a.localeCompare(b));
}
