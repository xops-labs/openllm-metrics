import { currentUser, tenantHeaders } from '@/lib/auth';

/**
 * Saved analytics views data layer (F038 Phase 3).
 *
 * A "saved view" is a declarative spec (metric, groupBy labels, optional
 * filters, range wrapper, visualization kind) that the analytics dashboards
 * screen renders into a card. The spec shape is intentionally compatible with
 * `buildSeriesQuery` in `lib/api/metrics.ts` (same metric/groupBy/filters/wrap/
 * windowSeconds fields) so a view round-trips straight into a tenant-scoped
 * PromQL selector.
 *
 * GRACEFUL DEGRADATION: persisting user-defined views requires an analytics
 * backend service (OLM_ANALYTICS_SERVICE_URL). That service is OPTIONAL and is
 * NOT part of the OSS admin console — the console talks to backend
 * microservices over HTTP only and has no direct DB access. With no service
 * reachable:
 *   - listSavedViews() still returns the four built-in DEFAULT_SAVED_VIEWS, so
 *     the dashboards screen is always useful with zero backend.
 *   - createSavedView() / deleteSavedView() become soft no-ops (they tolerate
 *     404 / >=500 / network errors) rather than throwing.
 * When the service IS present, persisted views are merged on top of the
 * built-ins, deduped by name.
 *
 * Privacy invariant: specs reference only normalized llm_* metrics and label
 * dimensions — never prompt/completion text. Tenant scoping is enforced two
 * ways: tenantHeaders() on the service call, and the `tenant="$tenant"` matcher
 * that buildSeriesQuery always injects when the spec is rendered.
 */

// Default matches the local compose stack, which publishes analytics-service
// on host port 8096 (host 8095 belongs to Redpanda Console).
const BASE = process.env.OLM_ANALYTICS_SERVICE_URL ?? 'http://localhost:8096';

/**
 * Declarative view spec. Mirrors the `SeriesQuerySpec` fields consumed by
 * `buildSeriesQuery`, plus a `viz` hint for how the dashboards screen renders
 * the resulting series.
 */
export interface SavedViewSpec {
  /** Counter/gauge metric name, e.g. `llm_total_tokens_total`. */
  metric: string;
  /** Labels to aggregate by, e.g. `['app']` or `['app', 'model']`. */
  groupBy: string[];
  /** Extra label matchers, ANDed into the selector. label -> exact value. */
  filters?: Record<string, string>;
  /** Range-vector wrapper. Default `'increase'` when rendered. */
  wrap?: 'increase' | 'rate' | 'none';
  /** Range window in seconds. Filled from the active range preset at render. */
  windowSeconds?: number;
  /** How the dashboards screen visualizes the resulting series. */
  viz: 'timeseries' | 'bar' | 'table';
}

export interface SavedView {
  id: string;
  name: string;
  description: string;
  spec: SavedViewSpec;
  position: number;
  /** True for the in-code defaults; absent/false for service-persisted views. */
  builtin?: boolean;
}

/**
 * Built-in default dashboards. These mirror the Phase 1 native analytics views
 * and require zero backend — they always render as long as a metrics query URL
 * is configured. Names here are the dedupe keys against service-persisted
 * views.
 */
export const DEFAULT_SAVED_VIEWS: SavedView[] = [
  {
    id: 'builtin-tokens-by-agent',
    name: 'Tokens by agent',
    description: 'Total tokens consumed per agent (app) in the selected window.',
    position: 0,
    builtin: true,
    spec: {
      metric: 'llm_total_tokens_total',
      groupBy: ['app'],
      wrap: 'increase',
      viz: 'bar',
    },
  },
  {
    id: 'builtin-tokens-by-model',
    name: 'Tokens by model',
    description: 'Total tokens consumed per model in the selected window.',
    position: 1,
    builtin: true,
    spec: {
      metric: 'llm_total_tokens_total',
      groupBy: ['model'],
      wrap: 'increase',
      viz: 'bar',
    },
  },
  {
    id: 'builtin-requests-by-agent',
    name: 'Requests by agent',
    description: 'Request count per agent (app) in the selected window.',
    position: 2,
    builtin: true,
    spec: {
      metric: 'llm_requests_total',
      groupBy: ['app'],
      wrap: 'increase',
      viz: 'bar',
    },
  },
  {
    id: 'builtin-agent-model-tokens',
    name: 'Agent × model tokens',
    description: 'Input / output / total tokens for every agent × model pair.',
    position: 3,
    builtin: true,
    spec: {
      metric: 'llm_total_tokens_total',
      groupBy: ['app', 'model'],
      wrap: 'increase',
      viz: 'table',
    },
  },
];

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const user = await currentUser();
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...tenantHeaders(user),
      ...(init?.headers ?? {}),
    },
    cache: 'no-store',
  });
  if (!res.ok) {
    if (res.status === 404 || res.status >= 500) {
      // Tolerate offline service - the OSS analytics service is optional.
      return {} as T;
    }
    throw new Error(`analytics-service ${path} -> ${res.status}`);
  }
  return (await res.json()) as T;
}

/**
 * List saved views: the four built-in defaults FIRST, then any
 * service-persisted views deduped by name (built-ins win). With no backend
 * reachable the service call resolves to `{}` and you still get the four
 * defaults. Returns sorted by position so the dashboards grid is stable.
 */
export async function listSavedViews(): Promise<SavedView[]> {
  let persisted: SavedView[] = [];
  try {
    const r = await call<{ views?: SavedView[] }>('/v1/saved-views');
    persisted = r.views ?? [];
  } catch {
    // Network error / unreachable service: fall back to defaults only.
    persisted = [];
  }

  const byName = new Map<string, SavedView>();
  for (const v of DEFAULT_SAVED_VIEWS) byName.set(v.name, v);
  for (const v of persisted) {
    if (!byName.has(v.name)) byName.set(v.name, v);
  }

  return Array.from(byName.values()).sort((a, b) => a.position - b.position);
}

export interface CreateSavedViewInput {
  name: string;
  description?: string;
  spec: SavedViewSpec;
  position?: number;
}

export interface SavedViewMutationResult {
  /** True only when an analytics backend actually persisted the change. */
  persisted: boolean;
  view?: SavedView;
}

/**
 * Create a saved view via the analytics backend. NO-OP without a backend: the
 * call tolerates 404 / >=500 / network errors and returns
 * `{ persisted: false }` instead of throwing, so the UI degrades gracefully.
 */
export async function createSavedView(
  input: CreateSavedViewInput,
): Promise<SavedViewMutationResult> {
  try {
    const view = await call<SavedView>('/v1/saved-views', {
      method: 'POST',
      body: JSON.stringify(input),
    });
    // The offline-tolerant `call` returns `{}` on 404/5xx; treat a view with
    // no id as "not persisted".
    if (!view || !view.id) return { persisted: false };
    return { persisted: true, view };
  } catch {
    return { persisted: false };
  }
}

/**
 * Delete a saved view via the analytics backend. NO-OP without a backend:
 * tolerates absence and returns `{ persisted: false }` instead of throwing.
 * Built-in defaults are not persisted and cannot be deleted this way.
 */
export async function deleteSavedView(id: string): Promise<SavedViewMutationResult> {
  try {
    await call<unknown>(`/v1/saved-views/${encodeURIComponent(id)}`, { method: 'DELETE' });
    return { persisted: true };
  } catch {
    return { persisted: false };
  }
}
