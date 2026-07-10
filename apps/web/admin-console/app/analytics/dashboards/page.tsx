import { ReactNode } from 'react';
import { PageHeader } from '@/components/page-header';
import { BarChart, LineChart, LineSeries } from '@/components/charts';
import { Table, Column } from '@/components/table';
import {
  buildSeriesQuery,
  metricsConfigured,
  queryRange,
  queryInstant,
  seriesLabel,
  SeriesQuerySpec,
} from '@/lib/api/metrics';
import { listSavedViews, SavedView, SavedViewSpec } from '@/lib/api/saved-views';
import {
  MetricsNotConfigured,
  NoData,
  RangeTabs,
  resolveRange,
  RangePreset,
  formatCompact,
  lastSampleBarData,
  AgentModelRow,
  fetchAgentModelRows,
} from '../shared';

interface Props {
  readonly searchParams: Promise<{ range?: string }>;
}

/**
 * Analytics dashboards (F038 Phase 3). A grid of saved views, each rendered per
 * its spec.viz into a chart or table off the normalized llm_* series. The four
 * built-in DEFAULT views always render (zero backend required); additional
 * user-saved views require the optional analytics backend service — see the
 * note rendered at the foot of the page. Raw normalized telemetry only — no
 * scoring, routing, or anomaly logic.
 */
export default async function DashboardsPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="Dashboards" description="Saved analytics views." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);
  const views = await listSavedViews();

  return (
    <>
      <PageHeader
        title="Dashboards"
        description="Built-in default analytics views rendered off the normalized llm_* series. Each card is a saved view spec (metric × dimensions × visualization)."
        actions={<RangeTabs basePath="/analytics/dashboards" active={preset.key} />}
      />
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        {views.map((view) => (
          <ViewCard key={view.id} view={view} preset={preset} />
        ))}
      </div>
      <p className="mt-6 text-xs text-muted">
        These are built-in default views. User-saved views require the optional analytics backend
        service (<code className="text-text">OLM_ANALYTICS_SERVICE_URL</code>); when it is absent
        the dashboards screen degrades gracefully to these four defaults. Every query is scoped to
        the active tenant via the <code className="text-text">tenant</code> label.
      </p>
    </>
  );
}

/** One dashboard card: title + description, then the rendered visualization. */
async function ViewCard({
  view,
  preset,
}: {
  readonly view: SavedView;
  readonly preset: RangePreset;
}) {
  return (
    <div className="rounded border border-border bg-panel p-4">
      <div className="mb-3">
        <div className="text-sm font-semibold text-text">{view.name}</div>
        {view.description ? (
          <div className="mt-1 text-xs text-muted">{view.description}</div>
        ) : null}
      </div>
      <ViewBody spec={view.spec} preset={preset} />
    </div>
  );
}

/** Dispatch on spec.viz to the matching renderer. */
async function ViewBody({
  spec,
  preset,
}: {
  readonly spec: SavedViewSpec;
  readonly preset: RangePreset;
}): Promise<ReactNode> {
  if (spec.viz === 'table') {
    return <TableView spec={spec} preset={preset} />;
  }
  if (spec.viz === 'timeseries') {
    return <TimeseriesView spec={spec} preset={preset} />;
  }
  return <BarView spec={spec} preset={preset} />;
}

/**
 * Build a `SeriesQuerySpec` from a view spec, including `filters` only when it
 * is actually present. Building the object this way (rather than passing
 * `filters: spec.filters`) keeps it assignable under `exactOptionalPropertyTypes`,
 * which forbids an explicit `undefined` for an optional property.
 */
function seriesSpec(args: {
  metric: string;
  groupBy: readonly string[];
  filters?: Record<string, string> | undefined;
  wrap: 'increase' | 'rate' | 'none';
  windowSeconds: number;
}): SeriesQuerySpec {
  const base: SeriesQuerySpec = {
    metric: args.metric,
    groupBy: args.groupBy,
    wrap: args.wrap,
    windowSeconds: args.windowSeconds,
  };
  return args.filters ? { ...base, filters: args.filters } : base;
}

/** bar → query_range, take the last sample per series → horizontal BarChart. */
async function BarView({
  spec,
  preset,
}: {
  readonly spec: SavedViewSpec;
  readonly preset: RangePreset;
}): Promise<ReactNode> {
  const query = buildSeriesQuery(
    seriesSpec({
      metric: spec.metric,
      groupBy: spec.groupBy,
      filters: spec.filters,
      wrap: spec.wrap ?? 'increase',
      windowSeconds: preset.rangeSeconds,
    }),
  );

  const series = await queryRange({
    query,
    rangeSeconds: preset.rangeSeconds,
    stepSeconds: preset.stepSeconds,
  });

  const data = lastSampleBarData(series, spec.groupBy);

  if (data.length === 0) {
    return (
      <NoData
        hint={`No ${spec.metric} samples carrying ${spec.groupBy.join('/')} labels for this tenant.`}
      />
    );
  }
  return <BarChart data={data} formatValue={formatCompact} />;
}

/** timeseries → query_range → multi-series LineChart. */
async function TimeseriesView({
  spec,
  preset,
}: {
  readonly spec: SavedViewSpec;
  readonly preset: RangePreset;
}): Promise<ReactNode> {
  const query = buildSeriesQuery(
    seriesSpec({
      metric: spec.metric,
      groupBy: spec.groupBy,
      filters: spec.filters,
      wrap: spec.wrap ?? 'increase',
      windowSeconds: preset.rangeSeconds,
    }),
  );

  const series = await queryRange({
    query,
    rangeSeconds: preset.rangeSeconds,
    stepSeconds: preset.stepSeconds,
  });

  const lines: LineSeries[] = series
    .map((s) => ({
      label: seriesLabel(s.metric, spec.groupBy),
      points: s.values,
    }))
    .filter((l) => l.points.length > 0);

  if (lines.length === 0) {
    return (
      <NoData
        hint={`No ${spec.metric} time series carrying ${spec.groupBy.join('/')} labels for this tenant.`}
      />
    );
  }
  return <LineChart series={lines} formatValue={formatCompact} />;
}

interface SingleDimRow {
  readonly key: string;
  readonly dim: string;
  readonly value: number;
}

/**
 * table → for the two-dim ['app','model'] case, join input/output/total via the
 * shared fetchAgentModelRows helper (same rows as agent-model/page.tsx). For
 * any single-dimension table, one query → [dim, Value].
 */
async function TableView({
  spec,
  preset,
}: {
  readonly spec: SavedViewSpec;
  readonly preset: RangePreset;
}): Promise<ReactNode> {
  const windowSeconds = preset.rangeSeconds;
  const wrap = spec.wrap ?? 'increase';

  const isAgentModel =
    spec.groupBy.length === 2 && spec.groupBy[0] === 'app' && spec.groupBy[1] === 'model';

  if (isAgentModel) {
    const tableRows = await fetchAgentModelRows(windowSeconds, spec.filters, wrap);

    if (tableRows.length === 0) {
      return (
        <NoData hint="No token samples carrying app/model labels for this tenant in the selected window." />
      );
    }

    const columns: ReadonlyArray<Column<AgentModelRow>> = [
      {
        key: 'app',
        header: 'Agent',
        render: (r) => <span className="font-mono text-xs">{r.app}</span>,
      },
      {
        key: 'model',
        header: 'Model',
        render: (r) => <span className="font-mono text-xs">{r.model}</span>,
      },
      {
        key: 'input',
        header: 'Input',
        className: 'text-right font-mono',
        render: (r) => formatCompact(r.input),
      },
      {
        key: 'output',
        header: 'Output',
        className: 'text-right font-mono',
        render: (r) => formatCompact(r.output),
      },
      {
        key: 'total',
        header: 'Total',
        className: 'text-right font-mono',
        render: (r) => formatCompact(r.total),
      },
    ];

    return <Table columns={columns} rows={tableRows} rowKey={(r) => r.key} />;
  }

  // Single-dimension table: one query → [dim, Value].
  const dim = spec.groupBy[0] ?? 'app';
  const samples = await queryInstant(
    buildSeriesQuery(
      seriesSpec({
        metric: spec.metric,
        groupBy: spec.groupBy,
        filters: spec.filters,
        wrap,
        windowSeconds,
      }),
    ),
  );

  const singleRows: SingleDimRow[] = samples
    .map((s) => ({
      key: seriesLabel(s.metric, spec.groupBy),
      dim: seriesLabel(s.metric, spec.groupBy),
      value: s.value,
    }))
    .filter((r) => r.value > 0)
    .sort((a, b) => b.value - a.value);

  if (singleRows.length === 0) {
    return <NoData hint={`No ${spec.metric} samples carrying a ${dim} label for this tenant.`} />;
  }

  const columns: ReadonlyArray<Column<SingleDimRow>> = [
    { key: 'dim', header: dim, render: (r) => <span className="font-mono text-xs">{r.dim}</span> },
    {
      key: 'value',
      header: 'Value',
      className: 'text-right font-mono',
      render: (r) => formatCompact(r.value),
    },
  ];

  return <Table columns={columns} rows={singleRows} rowKey={(r) => r.key} />;
}
