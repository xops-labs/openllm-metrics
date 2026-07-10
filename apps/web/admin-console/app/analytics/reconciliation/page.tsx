import { PageHeader } from '@/components/page-header';
import { LineChart, LineSeries } from '@/components/charts';
import { Table } from '@/components/table';
import { EmptyState } from '@/components/empty-state';
import {
  metricsConfigured,
  queryRange,
  queryInstant,
  seriesLabel,
  RangeSeries,
  InstantSample,
} from '@/lib/api/metrics';
import { reconciliationEnabled } from '@/lib/api/metrics-capabilities';
import { MetricsNotConfigured, NoData, RangeTabs, resolveRange, formatUsd } from '../shared';

// Explains that the reconciliation gauges are structurally absent without the
// F023 reconciler, naming the profile that produces them.
const RECONCILER_HINT =
  'Reconciliation gauges (llm_reconciliation_drift_usd and the matching ' +
  'estimated/reconciled series) are refreshed only by the F023 reconciler as ' +
  'cost windows close past the grace period. They are structurally absent in a ' +
  'stack without it — enable the reconciliation profile to populate this view.';

interface Props {
  readonly searchParams: Promise<{ range?: string }>;
}

interface DriftRow {
  readonly tuple: string;
  readonly estimated: number;
  readonly reconciled: number;
  readonly drift: number;
}

/**
 * Reconciliation drift (F038 / F023). Estimated (runtime/SDK) vs reconciled
 * (vendor FOCUS) cost, and the drift between them. The F023 reconciler refreshes
 * per-tuple gauges on each closed window:
 *
 *   - llm_reconciliation_estimated_cost_usd{provider,model,...}
 *   - llm_reconciliation_reconciled_cost_usd{provider,model,...}
 *   - llm_reconciliation_drift_usd{provider,model,...}   (reconciled - estimated)
 *
 * This screen renders those raw gauges. The drift math (a subtraction and a
 * ratio) is OSS-safe per docs/architecture/reconciliation.md; anything richer
 * (adaptive baselines, tolerances, change-point detection) is custom.
 */
export default async function ReconciliationPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="Reconciliation drift" description="Estimated vs reconciled cost." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);

  // Probe capability up front so the empty-state can name the required profile
  // accurately even before querying the per-tuple gauges.
  const reconcilerEnabled = await reconciliationEnabled();

  const [driftSeries, estInstant, recInstant]: [RangeSeries[], InstantSample[], InstantSample[]] =
    reconcilerEnabled
      ? await Promise.all([
          queryRange({
            query: 'sum(llm_reconciliation_drift_usd{tenant="$tenant"})',
            rangeSeconds: preset.rangeSeconds,
            stepSeconds: preset.stepSeconds,
          }),
          queryInstant(
            'sum by (provider, model) (llm_reconciliation_estimated_cost_usd{tenant="$tenant"})',
          ),
          queryInstant(
            'sum by (provider, model) (llm_reconciliation_reconciled_cost_usd{tenant="$tenant"})',
          ),
        ])
      : [[], [], []];

  const driftLines: LineSeries[] = driftSeries.map((s) => ({
    label: 'drift (reconciled − estimated)',
    points: s.values,
  }));

  // Join estimated + reconciled instant samples by (provider/model) tuple.
  const byTuple = new Map<string, { estimated: number; reconciled: number }>();
  for (const s of estInstant) {
    const key = seriesLabel(s.metric, ['provider', 'model']);
    byTuple.set(key, { estimated: s.value, reconciled: byTuple.get(key)?.reconciled ?? 0 });
  }
  for (const s of recInstant) {
    const key = seriesLabel(s.metric, ['provider', 'model']);
    const prev = byTuple.get(key);
    byTuple.set(key, { estimated: prev?.estimated ?? 0, reconciled: s.value });
  }

  const rows: DriftRow[] = [...byTuple.entries()]
    .map(([tuple, v]) => ({
      tuple,
      estimated: v.estimated,
      reconciled: v.reconciled,
      drift: v.reconciled - v.estimated,
    }))
    .sort((a, b) => Math.abs(b.drift) - Math.abs(a.drift));

  const hasData = driftLines.some((l) => l.points.length > 0) || rows.length > 0;

  return (
    <>
      <PageHeader
        title="Reconciliation drift"
        description="Estimated (runtime/SDK) vs reconciled (vendor FOCUS) cost. Drift = reconciled − estimated. Source: llm_reconciliation_* gauges (F023)."
        actions={<RangeTabs basePath="/analytics/reconciliation" active={preset.key} />}
      />
      {!reconcilerEnabled ? (
        // Structural absence: the F023 reconciler is not producing the gauges.
        <EmptyState title="Reconciliation profile not enabled" hint={RECONCILER_HINT} />
      ) : !hasData ? (
        // Reconciler enabled but no gauges for this tenant yet.
        <NoData hint="No llm_reconciliation_* gauges for this tenant yet. The F023 reconciler refreshes these as windows close past the grace period." />
      ) : (
        <div className="space-y-6">
          {driftLines.some((l) => l.points.length > 0) ? (
            <section>
              <h2 className="mb-2 text-sm font-semibold text-text">Total drift over time</h2>
              <LineChart series={driftLines} formatValue={formatUsd} unit="USD" />
            </section>
          ) : null}
          <section>
            <h2 className="mb-2 text-sm font-semibold text-text">Per-tuple drift (current)</h2>
            <Table<DriftRow>
              columns={[
                {
                  key: 'tuple',
                  header: 'Provider / model',
                  render: (r) => <span className="font-mono text-xs">{r.tuple}</span>,
                },
                { key: 'est', header: 'Estimated', render: (r) => formatUsd(r.estimated) },
                { key: 'rec', header: 'Reconciled', render: (r) => formatUsd(r.reconciled) },
                {
                  key: 'drift',
                  header: 'Drift',
                  render: (r) => (
                    <span
                      className={
                        r.drift > 0 ? 'text-warn' : r.drift < 0 ? 'text-danger' : 'text-ok'
                      }
                    >
                      {r.drift >= 0 ? '+' : ''}
                      {formatUsd(r.drift)}
                    </span>
                  ),
                },
              ]}
              rows={rows}
              rowKey={(r) => r.tuple}
              empty="No per-tuple reconciliation gauges yet."
            />
          </section>
        </div>
      )}
    </>
  );
}
