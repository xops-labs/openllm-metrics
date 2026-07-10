import { PageHeader } from '@/components/page-header';
import { BarChart, BarDatum } from '@/components/charts';
import { EmptyState } from '@/components/empty-state';
import { metricsConfigured, queryInstant, seriesLabel } from '@/lib/api/metrics';
import { MetricsNotConfigured, RangeTabs, resolveRange } from '../shared';

interface Props {
  readonly searchParams: Promise<{ range?: string }>;
}

function formatPct(v: number): string {
  return `${(v * 100).toFixed(2)}%`;
}

/**
 * Error rate by provider (F038). The error-class share of requests per
 * provider over the window: (errors + timeouts + rate-limit events) divided by
 * total requests. This is a raw ratio computed in PromQL — NOT a reliability
 * score (reliability scoring is outside this raw metrics view).
 */
export default async function ErrorsPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="Error rate by provider" description="Error share of requests." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);
  const w = `${preset.rangeSeconds}s`;

  // Error-class events / total requests, per provider, over the window.
  const expr = `sum by (provider) (
      increase(llm_errors_total{tenant="$tenant"}[${w}])
    + increase(llm_timeouts_total{tenant="$tenant"}[${w}])
    + increase(llm_rate_limit_events_total{tenant="$tenant"}[${w}])
  ) / clamp_min(sum by (provider) (increase(llm_requests_total{tenant="$tenant"}[${w}])), 1)`;

  // Total request volume per provider, used only to tell "no traffic at all"
  // apart from "traffic, but zero error-class events". llm_errors_total is a
  // lazily-initialized counter wired into the runtime path, so its absence is a
  // HEALTHY state (no failures), NOT a missing pipeline — never frame it as a
  // profile that must be enabled.
  const requestsExpr = `sum by (provider) (increase(llm_requests_total{tenant="$tenant"}[${w}]))`;

  const [samples, requestSamples] = await Promise.all([
    queryInstant(expr),
    queryInstant(requestsExpr),
  ]);

  const hasTraffic = requestSamples.length > 0;

  const data: BarDatum[] = samples
    .map((s) => ({ label: seriesLabel(s.metric, ['provider']), value: s.value }))
    .sort((a, b) => b.value - a.value);

  // Color by severity: green under 1%, amber under 5%, red above.
  const colorIndex = (d: BarDatum): number => (d.value >= 0.05 ? 3 : d.value >= 0.01 ? 2 : 1);

  return (
    <>
      <PageHeader
        title="Error rate by provider"
        description="(errors + timeouts + rate-limit events) / requests, per provider. Raw ratio — not a reliability score."
        actions={<RangeTabs basePath="/analytics/errors" active={preset.key} />}
      />
      {data.length === 0 ? (
        hasTraffic ? (
          // Traffic exists, but the ratio produced no series — i.e. zero
          // error-class events. A healthy, expected state for a lazy counter.
          <EmptyState
            title="No errors recorded"
            hint={
              'No error-class events (errors, timeouts, rate-limits) recorded for this tenant ' +
              'in the selected window. llm_errors_total is emitted lazily on the first error, so ' +
              'an empty result means no failures were observed — not a missing pipeline.'
            }
          />
        ) : (
          // No request volume at all in the window: nothing to compute a ratio from.
          <EmptyState
            title="No request traffic in this window"
            hint={
              'No llm_requests_total samples for this tenant in the selected window, so there is ' +
              'no denominator for an error rate. Once requests are recorded, error-class events ' +
              '(llm_errors_total, emitted lazily on the first error) will appear here if any occur.'
            }
          />
        )
      ) : (
        <BarChart data={data} formatValue={formatPct} colorIndex={colorIndex} />
      )}
    </>
  );
}
