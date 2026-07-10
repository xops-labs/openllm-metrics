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
 * By model (F038). Tokens and requests broken out per model, for the active
 * tenant, summed over the selected window. Raw normalized telemetry only.
 *
 * NOTE: real model names depend on the gateway model-capture fix (Phase 0).
 * Until that lands, the `model` label may be `"unknown"` for traffic whose
 * upstream model was not captured.
 */
export default async function ModelsPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="By model" description="Tokens and requests per model." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);

  // Total tokens and request volume per model. We take the last sample of the
  // increase() series so each bar reflects the full-window total.
  const [tokenSeries, requestSeries] = await Promise.all([
    queryRange({
      query: buildSeriesQuery({
        metric: 'llm_total_tokens_total',
        groupBy: ['model'],
        wrap: 'increase',
        windowSeconds: preset.rangeSeconds,
      }),
      rangeSeconds: preset.rangeSeconds,
      stepSeconds: preset.stepSeconds,
    }),
    queryRange({
      query: buildSeriesQuery({
        metric: 'llm_requests_total',
        groupBy: ['model'],
        wrap: 'increase',
        windowSeconds: preset.rangeSeconds,
      }),
      rangeSeconds: preset.rangeSeconds,
      stepSeconds: preset.stepSeconds,
    }),
  ]);

  const tokens = lastSampleBarData(tokenSeries, ['model']);
  const requests = lastSampleBarData(requestSeries, ['model']);

  return (
    <>
      <PageHeader
        title="By model"
        description={
          'Tokens consumed and requests issued per model in the selected window. Real model names require the gateway model-capture fix; until then series may show model="unknown". Source: llm_total_tokens_total, llm_requests_total.'
        }
        actions={<RangeTabs basePath="/analytics/models" active={preset.key} />}
      />
      <div className="space-y-6">
        <section>
          <h2 className="mb-2 text-sm font-semibold text-text">Tokens by model</h2>
          {tokens.length === 0 ? (
            <NoData hint='No llm_total_tokens_total samples carrying a model label for this tenant. Note: traffic without a captured model appears as model="unknown" pending the Phase 0 gateway fix.' />
          ) : (
            <BarChart data={tokens} formatValue={formatCompact} />
          )}
        </section>
        <section>
          <h2 className="mb-2 text-sm font-semibold text-text">Requests by model</h2>
          {requests.length === 0 ? (
            <NoData hint='No llm_requests_total samples carrying a model label for this tenant. Note: traffic without a captured model appears as model="unknown" pending the Phase 0 gateway fix.' />
          ) : (
            <BarChart data={requests} formatValue={formatCompact} />
          )}
        </section>
      </div>
    </>
  );
}
