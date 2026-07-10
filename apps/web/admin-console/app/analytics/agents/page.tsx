import { PageHeader } from '@/components/page-header';
import { BarChart } from '@/components/charts';
import { buildSeriesQuery, metricsConfigured, queryRange } from '@/lib/api/metrics';
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
 * By agent (F038). Tokens and requests broken out per agent (the `app` label),
 * for the active tenant, summed over the selected window. There is no
 * latency/duration series in this stack, so this view is consumption + volume
 * only — raw normalized telemetry, no scoring or routing inference.
 */
export default async function AgentsPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="By agent" description="Tokens and requests per agent." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);

  // Total tokens and request volume per agent. We take the last sample of the
  // increase() series so each bar reflects the full-window total.
  const [tokenSeries, requestSeries] = await Promise.all([
    queryRange({
      query: buildSeriesQuery({
        metric: 'llm_total_tokens_total',
        groupBy: ['app'],
        wrap: 'increase',
        windowSeconds: preset.rangeSeconds,
      }),
      rangeSeconds: preset.rangeSeconds,
      stepSeconds: preset.stepSeconds,
    }),
    queryRange({
      query: buildSeriesQuery({
        metric: 'llm_requests_total',
        groupBy: ['app'],
        wrap: 'increase',
        windowSeconds: preset.rangeSeconds,
      }),
      rangeSeconds: preset.rangeSeconds,
      stepSeconds: preset.stepSeconds,
    }),
  ]);

  const tokens = lastSampleBarData(tokenSeries, ['app']);
  const requests = lastSampleBarData(requestSeries, ['app']);

  return (
    <>
      <PageHeader
        title="By agent"
        description="Tokens consumed and requests issued per agent (app) in the selected window. Source: llm_total_tokens_total, llm_requests_total."
        actions={<RangeTabs basePath="/analytics/agents" active={preset.key} />}
      />
      <div className="space-y-6">
        <section>
          <h2 className="mb-2 text-sm font-semibold text-text">Tokens by agent</h2>
          {tokens.length === 0 ? (
            <NoData hint="No llm_total_tokens_total samples carrying an app label for this tenant." />
          ) : (
            <BarChart data={tokens} formatValue={formatCompact} />
          )}
        </section>
        <section>
          <h2 className="mb-2 text-sm font-semibold text-text">Requests by agent</h2>
          {requests.length === 0 ? (
            <NoData hint="No llm_requests_total samples carrying an app label for this tenant." />
          ) : (
            <BarChart data={requests} formatValue={formatCompact} />
          )}
        </section>
      </div>
    </>
  );
}
