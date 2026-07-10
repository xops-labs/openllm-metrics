import { PageHeader } from '@/components/page-header';
import { BarChart, LineChart, LineSeries } from '@/components/charts';
import { Table, Column } from '@/components/table';
import {
  buildSeriesQuery,
  metricsConfigured,
  queryInstant,
  queryRange,
  seriesLabel,
} from '@/lib/api/metrics';
import { listLabelValues, listMetricNames, LABEL_WHITELIST } from '@/lib/api/metrics-catalog';
import {
  MetricsNotConfigured,
  NoData,
  RANGE_PRESETS,
  resolveRange,
  formatCompact,
  lastSampleBarData,
} from '../analytics/shared';

/**
 * Guided metrics explorer (F038 Phase 2).
 *
 * A first-party, dropdown-driven PromQL builder so operators can answer ad-hoc
 * questions ("output tokens by model for the payments app in the last 24h")
 * without ever opening Grafana. This is a SERVER component with NO client JS:
 * the controls are a plain GET `<form>` of native `<select>`s whose selections
 * become URL query params, which this same component reads back to compile and
 * run the query.
 *
 * Isolation invariant: the operator picks ONLY from a fixed whitelist of
 * `llm_*` metrics and normalized label dimensions, and tenant scoping is
 * injected server-side by `buildSeriesQuery` (`tenant="$tenant"`) and then by
 * the metrics client's `$tenant` substitution. There is deliberately no
 * raw-PromQL textbox in this version: a future enhancement could accept raw
 * PromQL but would still have to re-inject the tenant matcher server-side so it
 * can never be removed by the user.
 */

type VizMode = 'timeseries' | 'bar' | 'table';
const VIZ_MODES: ReadonlyArray<{ value: VizMode; label: string }> = [
  { value: 'timeseries', label: 'Time series' },
  { value: 'bar', label: 'Bar' },
  { value: 'table', label: 'Table' },
];

const DEFAULT_METRIC = 'llm_total_tokens_total';
const DEFAULT_GROUP_BY = 'app';
const DEFAULT_VIZ: VizMode = 'bar';
const DEFAULT_RANGE = '24h';

interface Props {
  readonly searchParams: Promise<{
    metric?: string;
    groupBy?: string;
    filterLabel?: string;
    filterValue?: string;
    range?: string;
    viz?: string;
  }>;
}

function isVizMode(v: string | undefined): v is VizMode {
  return v === 'timeseries' || v === 'bar' || v === 'table';
}

interface TableRow {
  readonly key: string;
  readonly dim: string;
  readonly value: number;
}

export default async function ExplorePage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader
          title="Metrics explorer"
          description="Guided, tenant-scoped queries over your normalized LLM telemetry."
        />
        <MetricsNotConfigured />
      </>
    );
  }

  const sp = await searchParams;

  // ---- Resolve selections (with defaults), all constrained to whitelists ----
  const metricNames = await listMetricNames();
  const metric = metricNames.includes(sp.metric ?? '') ? (sp.metric as string) : DEFAULT_METRIC;

  const groupBy = LABEL_WHITELIST.includes(sp.groupBy ?? '')
    ? (sp.groupBy as string)
    : DEFAULT_GROUP_BY;

  // Filter label is optional; '' / unset means "no filter".
  const filterLabel =
    sp.filterLabel && LABEL_WHITELIST.includes(sp.filterLabel) ? sp.filterLabel : '';

  // Only fetch values once a (whitelisted) filter label is chosen — and the
  // lookup is tenant-scoped inside listLabelValues so other tenants' values
  // are never surfaced.
  const filterValueOptions = filterLabel ? await listLabelValues(filterLabel) : [];
  const filterValue =
    filterLabel && sp.filterValue && filterValueOptions.includes(sp.filterValue)
      ? sp.filterValue
      : '';

  const preset = resolveRange(sp.range ?? DEFAULT_RANGE);
  const viz: VizMode = isVizMode(sp.viz) ? sp.viz : DEFAULT_VIZ;

  // ---- Compile to PromQL via the shared, tenant-injecting query builder ----
  const filters = filterLabel && filterValue ? { [filterLabel]: filterValue } : undefined;
  const compiled = buildSeriesQuery({
    metric,
    groupBy: [groupBy],
    ...(filters ? { filters } : {}),
    wrap: 'increase',
    windowSeconds: preset.rangeSeconds,
  });

  // ---- Run + render the chosen visualization ----
  let result: React.ReactNode;
  if (viz === 'timeseries') {
    const series = await queryRange({
      query: compiled,
      rangeSeconds: preset.rangeSeconds,
      stepSeconds: preset.stepSeconds,
    });
    const lines: LineSeries[] = series
      .map((s) => ({
        label: seriesLabel(s.metric, [groupBy]),
        points: s.values,
      }))
      .filter((l) => l.points.length > 0);
    result =
      lines.length === 0 ? <NoData /> : <LineChart series={lines} formatValue={formatCompact} />;
  } else if (viz === 'bar') {
    const series = await queryRange({
      query: compiled,
      rangeSeconds: preset.rangeSeconds,
      stepSeconds: preset.stepSeconds,
    });
    const data = lastSampleBarData(series, [groupBy]);
    result = data.length === 0 ? <NoData /> : <BarChart data={data} formatValue={formatCompact} />;
  } else {
    const samples = await queryInstant(compiled);
    const rows: TableRow[] = samples
      .map((s) => ({
        key: seriesLabel(s.metric, [groupBy]),
        dim: seriesLabel(s.metric, [groupBy]),
        value: s.value,
      }))
      .sort((a, b) => b.value - a.value);
    const columns: ReadonlyArray<Column<TableRow>> = [
      {
        key: 'dim',
        header: groupBy,
        render: (r) => <span className="font-mono text-xs">{r.dim}</span>,
      },
      {
        key: 'value',
        header: 'Value',
        className: 'text-right font-mono',
        render: (r) => formatCompact(r.value),
      },
    ];
    result = <Table columns={columns} rows={rows} rowKey={(r) => r.key} empty={<NoData />} />;
  }

  const selectClass =
    'rounded border border-border bg-canvas px-2 py-1 text-xs text-text focus:border-accent focus:outline-none';

  return (
    <>
      <PageHeader
        title="Metrics explorer"
        description="Build a guided, tenant-scoped query over your normalized LLM telemetry. Every selection is restricted to the whitelisted llm_* metrics and labels; tenant scoping is always applied server-side."
      />

      {/* GET form: native selects → URL query params → server recompiles. No
          client JS. There is intentionally no tenant control here — scoping is
          injected by buildSeriesQuery and can never be removed by the user. */}
      <form
        method="get"
        className="mb-6 flex flex-wrap items-end gap-3 rounded border border-border bg-panel p-4"
      >
        <label className="flex flex-col gap-1 text-xs text-muted">
          Metric
          <select name="metric" defaultValue={metric} className={selectClass}>
            {metricNames.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-muted">
          Group by
          <select name="groupBy" defaultValue={groupBy} className={selectClass}>
            {LABEL_WHITELIST.map((l) => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-muted">
          Filter label
          <select name="filterLabel" defaultValue={filterLabel} className={selectClass}>
            <option value="">{'(none)'}</option>
            {LABEL_WHITELIST.map((l) => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-muted">
          Filter value
          <select
            name="filterValue"
            defaultValue={filterValue}
            disabled={!filterLabel}
            className={selectClass}
          >
            <option value="">{filterLabel ? '(any)' : '(choose a label)'}</option>
            {filterValueOptions.map((v) => (
              <option key={v} value={v}>
                {v}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-muted">
          Range
          <select name="range" defaultValue={preset.key} className={selectClass}>
            {RANGE_PRESETS.map((p) => (
              <option key={p.key} value={p.key}>
                {p.label}
              </option>
            ))}
          </select>
        </label>

        <label className="flex flex-col gap-1 text-xs text-muted">
          Visualization
          <select name="viz" defaultValue={viz} className={selectClass}>
            {VIZ_MODES.map((v) => (
              <option key={v.value} value={v.value}>
                {v.label}
              </option>
            ))}
          </select>
        </label>

        <button
          type="submit"
          className="rounded border border-accent bg-accent px-3 py-1 text-xs font-medium text-white hover:opacity-90"
        >
          Run query
        </button>
      </form>

      {/* Compiled PromQL — `$tenant` is substituted with the active tenant id
          by the metrics client before execution. Shown for transparency. A
          future version could allow raw PromQL input, but would still have to
          re-inject the tenant matcher server-side so it can never be removed. */}
      <div className="mb-6">
        <p className="mb-1 text-xs uppercase tracking-wider text-muted">Compiled PromQL</p>
        <code className="block overflow-x-auto rounded border border-border bg-canvas px-3 py-2 font-mono text-xs text-text">
          {compiled}
        </code>
      </div>

      {result}
    </>
  );
}
