import { BarDatum } from '@/components/charts';
import { EmptyState } from '@/components/empty-state';
import {
  buildSeriesQuery,
  queryInstant,
  InstantSample,
  RangeSeries,
  seriesLabel,
} from '@/lib/api/metrics';

/**
 * Shared bits for the native analytics screens: the "no query URL configured"
 * empty-state and the time-range presets. Kept local to the analytics route so
 * the generic component library stays unopinionated about Prometheus.
 */

export interface RangePreset {
  readonly key: string;
  readonly label: string;
  readonly rangeSeconds: number;
  readonly stepSeconds: number;
}

export const RANGE_PRESETS: ReadonlyArray<RangePreset> = [
  { key: '6h', label: 'Last 6h', rangeSeconds: 6 * 3600, stepSeconds: 300 },
  { key: '24h', label: 'Last 24h', rangeSeconds: 24 * 3600, stepSeconds: 3600 },
  { key: '7d', label: 'Last 7d', rangeSeconds: 7 * 24 * 3600, stepSeconds: 6 * 3600 },
  { key: '30d', label: 'Last 30d', rangeSeconds: 30 * 24 * 3600, stepSeconds: 24 * 3600 },
];

export const DEFAULT_RANGE = RANGE_PRESETS[1] as RangePreset; // 24h

export function resolveRange(rangeKey: string | undefined): RangePreset {
  return RANGE_PRESETS.find((r) => r.key === rangeKey) ?? DEFAULT_RANGE;
}

/** Empty-state shown when OLM_METRICS_QUERY_URL is unset. */
export function MetricsNotConfigured() {
  return (
    <EmptyState
      title="OLM_METRICS_QUERY_URL not configured"
      hint="Native analytics query a Prometheus-compatible TSDB (Prometheus, VictoriaMetrics, or Mimir) that scrapes the metrics-endpoint service. Set OLM_METRICS_QUERY_URL in apps/web/admin-console/.env.local to enable these screens."
    />
  );
}

/** Empty-state shown when the query URL is set but returned no series. */
export function NoData({ hint }: { readonly hint?: string }) {
  return (
    <EmptyState
      title="No telemetry in the selected window"
      hint={
        hint ??
        'The query reached the TSDB but returned no samples for this tenant and time range. Confirm the metrics-endpoint is being scraped and that traffic exists for the active tenant.'
      }
    />
  );
}

interface RangeTabsProps {
  readonly basePath: string;
  readonly active: string;
}

/** Range selector rendered as links (server-component friendly, no client JS). */
export function RangeTabs({ basePath, active }: RangeTabsProps) {
  return (
    <div className="flex gap-1 text-xs">
      {RANGE_PRESETS.map((p) => {
        const isActive = p.key === active;
        return (
          <a
            key={p.key}
            href={`${basePath}?range=${p.key}`}
            className={
              isActive
                ? 'rounded border border-accent bg-accent px-2 py-1 text-white'
                : 'rounded border border-border px-2 py-1 text-muted hover:text-text'
            }
          >
            {p.label}
          </a>
        );
      })}
    </div>
  );
}

export function formatUsd(v: number): string {
  return `$${v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

export function formatCompact(v: number): string {
  return Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(v);
}

/**
 * Take the last sample of each `increase()` series → one bar per series,
 * dropping zero-value series and sorting descending. Shared by every bar view
 * so the "full-window total" transform cannot drift between screens.
 */
export function lastSampleBarData(series: RangeSeries[], labelKeys: readonly string[]): BarDatum[] {
  return series
    .map((s) => {
      const last = s.values.length > 0 ? s.values[s.values.length - 1] : undefined;
      return {
        label: seriesLabel(s.metric, labelKeys),
        value: last ? last[1] : 0,
      };
    })
    .filter((d) => d.value > 0)
    .sort((a, b) => b.value - a.value);
}

export interface AgentModelRow {
  readonly key: string;
  readonly app: string;
  readonly model: string;
  readonly input: number;
  readonly output: number;
  readonly total: number;
}

/**
 * The agent × model token breakdown: three instant queries (input, output,
 * total tokens) joined in TS into one row per (app, model) pair, defaulting any
 * missing dimension to 0 and sorting by total descending. Tenant scoping is
 * injected by `buildSeriesQuery` (`tenant="$tenant"`), never here. Shared by
 * agent-model/page.tsx and the dashboards table view.
 */
export async function fetchAgentModelRows(
  windowSeconds: number,
  filters?: Record<string, string>,
  wrap: 'increase' | 'rate' | 'none' = 'increase',
): Promise<AgentModelRow[]> {
  const queryFor = (metric: string) =>
    buildSeriesQuery({
      metric,
      groupBy: ['app', 'model'],
      wrap,
      windowSeconds,
      ...(filters ? { filters } : {}),
    });

  const [inputSamples, outputSamples, totalSamples] = await Promise.all([
    queryInstant(queryFor('llm_input_tokens_total')),
    queryInstant(queryFor('llm_output_tokens_total')),
    queryInstant(queryFor('llm_total_tokens_total')),
  ]);

  // Join the three vectors into rows keyed by `${app}|${model}`, defaulting
  // any missing dimension to 0.
  const rows = new Map<
    string,
    { app: string; model: string; input: number; output: number; total: number }
  >();
  const merge = (samples: InstantSample[], field: 'input' | 'output' | 'total') => {
    for (const s of samples) {
      const app = s.metric.app ?? 'unknown';
      const model = s.metric.model ?? 'unknown';
      const key = `${app}|${model}`;
      const existing = rows.get(key) ?? { app, model, input: 0, output: 0, total: 0 };
      existing[field] = s.value;
      rows.set(key, existing);
    }
  };
  merge(inputSamples, 'input');
  merge(outputSamples, 'output');
  merge(totalSamples, 'total');

  return Array.from(rows.entries())
    .map(([key, v]) => ({ key, ...v }))
    .sort((a, b) => b.total - a.total);
}
