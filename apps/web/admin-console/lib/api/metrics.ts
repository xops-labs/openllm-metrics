import { currentUser } from '@/lib/auth';

/**
 * Native analytics data source (F038 Phase A).
 *
 * The metrics-endpoint service (F010) exposes the raw Prometheus text
 * exposition on /metrics; it does NOT answer range queries itself. A
 * Prometheus-compatible TSDB (Prometheus, VictoriaMetrics, or Mimir) scrapes
 * that endpoint and answers PromQL over its HTTP query API
 * (`/api/v1/query` and `/api/v1/query_range`). The native analytics screens
 * query that TSDB directly, server-side.
 *
 * `OLM_METRICS_QUERY_URL` is the base URL of the Prometheus-compatible query
 * API (e.g. `http://localhost:9090` for a local Prometheus). It defaults to a
 * local Prometheus port. When unset/unreachable, every helper resolves to an
 * empty result so the screens render an explanatory empty-state instead of
 * crashing.
 *
 * IMPORTANT (privacy invariant): these series carry only the F008 normalized
 * labels (`provider`, `model`, `tenant`, `env`, `team`, `app`, ...) and the
 * `llm_*` counter/gauge values. No prompt or completion text is ever present
 * in this data path. Analytics render RAW normalized telemetry only — no
 * scoring, routing, or anomaly logic (those are outside these raw metrics views).
 */

// Resolved per-call (not cached at module load) so it stays correct under hot
// reload / test env mutation. Mirrors lib/api/metrics-capabilities.ts.
function queryBase(): string {
  return process.env.OLM_METRICS_QUERY_URL ?? 'http://localhost:9090';
}

/** A single (labels -> value over time) series returned by query_range. */
export interface RangeSeries {
  readonly metric: Record<string, string>;
  /** [unixSeconds, value] sample pairs, value already parsed to a number. */
  readonly values: ReadonlyArray<readonly [number, number]>;
}

/** A single instant sample returned by an instant query. */
export interface InstantSample {
  readonly metric: Record<string, string>;
  readonly value: number;
}

interface PromVectorResult {
  metric: Record<string, string>;
  value?: [number, string];
  values?: Array<[number, string]>;
}

interface PromResponse {
  status: 'success' | 'error';
  data?: {
    resultType: 'matrix' | 'vector' | 'scalar' | 'string';
    result: PromVectorResult[];
  };
  error?: string;
}

/** True only when an explicit query URL is configured via env. */
export function metricsConfigured(): boolean {
  return Boolean(process.env.OLM_METRICS_QUERY_URL);
}

export interface RangeOptions {
  /** PromQL expression. `$tenant` is substituted with the active tenant id. */
  readonly query: string;
  /** Lookback window in seconds (e.g. 86400 for 24h). */
  readonly rangeSeconds: number;
  /** Step between samples in seconds (e.g. 3600 for hourly buckets). */
  readonly stepSeconds: number;
}

function tenantSelector(tenantId: string): string {
  // Escape any double-quotes defensively; tenant ids are UUIDs in practice.
  return tenantId.replace(/"/g, '\\"');
}

async function promFetch(path: string, params: URLSearchParams): Promise<PromResponse | null> {
  try {
    const res = await fetch(`${queryBase()}${path}?${params.toString()}`, {
      // Live operational data — never cache.
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    });
    if (!res.ok) {
      return null;
    }
    return (await res.json()) as PromResponse;
  } catch {
    return null;
  }
}

/**
 * Run a `query_range` and return per-series sample arrays. `$tenant` in the
 * query is replaced with the active tenant id so every panel is tenant-scoped.
 * Returns `[]` on any failure (service offline, bad query, parse error).
 */
export async function queryRange(opts: RangeOptions): Promise<RangeSeries[]> {
  const user = await currentUser();
  const end = Math.floor(Date.now() / 1000);
  const start = end - opts.rangeSeconds;
  const expr = opts.query.replaceAll('$tenant', tenantSelector(user.tenantId));

  const params = new URLSearchParams({
    query: expr,
    start: String(start),
    end: String(end),
    step: String(opts.stepSeconds),
  });

  const body = await promFetch('/api/v1/query_range', params);
  if (!body || body.status !== 'success' || body.data?.resultType !== 'matrix') {
    return [];
  }

  return body.data.result.map((r) => ({
    metric: r.metric,
    values: (r.values ?? [])
      .map(([ts, v]) => [ts, Number(v)] as const)
      .filter(([, v]) => Number.isFinite(v)),
  }));
}

/**
 * Run an instant `query` and return one sample per series. `$tenant` is
 * substituted as in {@link queryRange}. Returns `[]` on any failure.
 */
export async function queryInstant(query: string): Promise<InstantSample[]> {
  const user = await currentUser();
  const expr = query.replaceAll('$tenant', tenantSelector(user.tenantId));
  const params = new URLSearchParams({ query: expr });

  const body = await promFetch('/api/v1/query', params);
  if (!body || body.status !== 'success' || body.data?.resultType !== 'vector') {
    return [];
  }

  return body.data.result
    .map((r) => ({
      metric: r.metric,
      value: r.value ? Number(r.value[1]) : Number.NaN,
    }))
    .filter((s) => Number.isFinite(s.value));
}

/** A short, human-readable label for a series built from its metric labels. */
export function seriesLabel(metric: Record<string, string>, keys: readonly string[]): string {
  const parts = keys.map((k) => metric[k]).filter((v): v is string => Boolean(v));
  return parts.length > 0 ? parts.join(' / ') : '(unlabeled)';
}

/**
 * Declarative spec for a tenant-scoped `sum by (...)` PromQL query. Used by the
 * agent/model analytics screens so they all build identical, safe selectors
 * instead of hand-rolling PromQL strings.
 */
export interface SeriesQuerySpec {
  /** Counter/gauge metric name, e.g. `llm_total_tokens_total`. */
  readonly metric: string;
  /** Labels to aggregate by, e.g. `['app']` or `['app', 'model']`. */
  readonly groupBy: readonly string[];
  /** Extra label matchers, ANDed into the selector. label -> exact value. */
  readonly filters?: Readonly<Record<string, string>>;
  /** Range-vector wrapper. Default `'none'` (no `[...]`, raw instant value). */
  readonly wrap?: 'increase' | 'rate' | 'none';
  /** Range window in seconds. Required when `wrap !== 'none'`. */
  readonly windowSeconds?: number;
}

/** Escape a label-matcher value so it is safe inside double quotes. */
function escapeLabelValue(value: string): string {
  // Backslashes first, then double-quotes, so quotes are not double-escaped.
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

/**
 * Build a tenant-scoped `sum by (<groupBy>) (<wrap>(metric{...}[window]))`
 * query string. `tenant="$tenant"` is ALWAYS injected first and cannot be
 * removed or overridden via `filters` — so the metrics client's tenant
 * substitution always applies and every query stays tenant-scoped. Any
 * `filters` are appended as additional ANDed matchers with escaped values.
 * When `wrap === 'none'` the range selector (`[...]`) is omitted.
 */
export function buildSeriesQuery(spec: SeriesQuerySpec): string {
  const wrap = spec.wrap ?? 'none';

  // Tenant is always the first matcher and is immune to `filters` overrides:
  // we emit it ourselves and never read a `tenant` key out of `filters`.
  const matchers = [`tenant="$tenant"`];
  for (const [label, value] of Object.entries(spec.filters ?? {})) {
    if (label === 'tenant') continue; // never let a filter shadow tenant scoping
    matchers.push(`${label}="${escapeLabelValue(value)}"`);
  }

  const selector = `${spec.metric}{${matchers.join(', ')}}`;

  let inner: string;
  if (wrap === 'none') {
    inner = selector;
  } else {
    const window = spec.windowSeconds;
    if (window === undefined) {
      throw new Error(`buildSeriesQuery: windowSeconds is required when wrap='${wrap}'`);
    }
    inner = `${wrap}(${selector}[${window}s])`;
  }

  return `sum by (${spec.groupBy.join(', ')}) (${inner})`;
}
