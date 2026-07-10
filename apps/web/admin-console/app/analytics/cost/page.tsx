import { PageHeader } from '@/components/page-header';
import { LineChart, LineSeries } from '@/components/charts';
import { EmptyState } from '@/components/empty-state';
import { metricsConfigured, queryRange, seriesLabel } from '@/lib/api/metrics';
import { costPipelineEnabled } from '@/lib/api/metrics-capabilities';
import { MetricsNotConfigured, NoData, RangeTabs, resolveRange, formatUsd } from '../shared';

// Explains that the cost series is structurally absent in a gateway-only stack
// (not just "no traffic"), and names the pipeline that populates it.
const COST_PIPELINE_HINT =
  'The cost series llm_cost_usd_total is produced by the cost pipeline ' +
  '(cost-mapper → focus-ingester → reconciler/exporter), not by the gateway-only ' +
  'runtime stack. It is structurally absent until that pipeline runs — enable the ' +
  'cost profile to populate this view.';

interface Props {
  readonly searchParams: Promise<{ range?: string }>;
}

/**
 * Cost over time (F038). USD spend per provider, derived from the
 * llm_cost_usd_total counter via rate() over the selected step. Raw spend
 * only — no cost-efficiency scoring (that is F025, custom).
 */
export default async function CostPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="Cost over time" description="USD spend per provider." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);

  // Probe capability up front so the empty-state can name the missing pipeline
  // accurately even before (or independently of) running the range query.
  const costEnabled = await costPipelineEnabled();

  // Per-provider USD spend rate, scaled to spend-per-step so the area under the
  // line approximates total spend in the window. $tenant is substituted by the
  // metrics client to scope to the active tenant. Skip the query entirely when
  // the cost pipeline is not even producing the series.
  const series = costEnabled
    ? await queryRange({
        query: `sum by (provider) (rate(llm_cost_usd_total{tenant="$tenant"}[${preset.stepSeconds}s])) * ${preset.stepSeconds}`,
        rangeSeconds: preset.rangeSeconds,
        stepSeconds: preset.stepSeconds,
      })
    : [];

  const lines: LineSeries[] = series.map((s) => ({
    label: seriesLabel(s.metric, ['provider']),
    points: s.values,
  }));

  return (
    <>
      <PageHeader
        title="Cost over time"
        description="USD spend per provider, bucketed over the selected window. Source: llm_cost_usd_total."
        actions={<RangeTabs basePath="/analytics/cost" active={preset.key} />}
      />
      {!costEnabled ? (
        // Structural absence: the cost pipeline is not producing the series.
        <EmptyState title="Cost pipeline not enabled" hint={COST_PIPELINE_HINT} />
      ) : lines.length === 0 ? (
        // Pipeline enabled but no samples for this tenant/window.
        <NoData hint="No llm_cost_usd_total samples for this tenant in the selected window." />
      ) : (
        <LineChart series={lines} formatValue={formatUsd} unit="per bucket" />
      )}
    </>
  );
}
