import { PageHeader } from '@/components/page-header';
import { BarChart } from '@/components/charts';
import { metricsConfigured, queryRange } from '@/lib/api/metrics';
import {
  MetricsNotConfigured,
  NoData,
  RangeTabs,
  resolveRange,
  formatCompact,
  lastSampleBarData,
} from '../shared';

interface Props {
  readonly searchParams: Promise<{ range?: string }>;
}

/**
 * Tokens by team (F038). Total tokens consumed per team across all providers,
 * summed over the selected window via increase() on the llm_total_tokens_total
 * counter. Raw consumption only — no allocation or budget enforcement logic.
 */
export default async function TokensPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="Tokens by team" description="Total tokens consumed per team." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);

  // Tokens consumed per team in the window. We take the max instant value of
  // increase() across the range so the bar reflects the full-window total.
  const series = await queryRange({
    query: `sum by (team) (increase(llm_total_tokens_total{tenant="$tenant"}[${preset.rangeSeconds}s]))`,
    rangeSeconds: preset.rangeSeconds,
    stepSeconds: preset.stepSeconds,
  });

  const data = lastSampleBarData(series, ['team']);

  return (
    <>
      <PageHeader
        title="Tokens by team"
        description="Total tokens consumed per team in the selected window. Source: llm_total_tokens_total."
        actions={<RangeTabs basePath="/analytics/tokens" active={preset.key} />}
      />
      {data.length === 0 ? (
        <NoData hint="No llm_total_tokens_total samples carrying a team label for this tenant." />
      ) : (
        <BarChart data={data} formatValue={formatCompact} />
      )}
    </>
  );
}
