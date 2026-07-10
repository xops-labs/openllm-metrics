import Link from 'next/link';
import { PageHeader } from '@/components/page-header';
import { metricsConfigured } from '@/lib/api/metrics';
import { MetricsNotConfigured } from './shared';

const CARDS: ReadonlyArray<{ href: string; title: string; description: string; query: string }> = [
  {
    href: '/analytics/cost',
    title: 'Cost over time',
    description:
      'USD spend per provider, bucketed over the selected window. · requires cost profile',
    query: 'sum by (provider) (rate(llm_cost_usd_total[step]))',
  },
  {
    href: '/analytics/tokens',
    title: 'Tokens by team',
    description: 'Total tokens consumed per team across all providers.',
    query: 'sum by (team) (llm_total_tokens_total)',
  },
  {
    href: '/analytics/errors',
    title: 'Error rate by provider',
    description: 'Error + timeout + rate-limit share of requests per provider.',
    query: 'sum by (provider) (rate(llm_errors_total[step]))',
  },
  {
    href: '/analytics/reconciliation',
    title: 'Reconciliation drift',
    description:
      'Estimated vs reconciled cost — the F023 drift signal. · requires reconciliation profile',
    query: 'llm_reconciliation_drift_usd',
  },
  {
    href: '/analytics/agents',
    title: 'By agent',
    description: 'Tokens and requests per agent (app) for the active tenant.',
    query: 'sum by (app) (increase(llm_total_tokens_total[window]))',
  },
  {
    href: '/analytics/models',
    title: 'By model',
    description: 'Tokens and requests per model. Unknown until the gateway model-capture fix.',
    query: 'sum by (model) (increase(llm_total_tokens_total[window]))',
  },
  {
    href: '/analytics/agent-model',
    title: 'Agent × model',
    description: 'Input / output / total tokens for every agent × model pair.',
    query: 'sum by (app, model) (increase(llm_total_tokens_total[window]))',
  },
];

export default function AnalyticsOverviewPage() {
  return (
    <>
      <PageHeader
        title="Native analytics"
        description="First-party telemetry views rendered directly off the normalized llm_* series. Raw data only — no scoring, routing, or anomaly inference."
      />
      {!metricsConfigured() ? (
        <MetricsNotConfigured />
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {CARDS.map((c) => (
            <Link
              key={c.href}
              href={c.href}
              className="block rounded border border-border bg-panel p-4 hover:border-accent"
            >
              <div className="text-sm font-semibold text-text">{c.title}</div>
              <div className="mt-1 text-xs text-muted">{c.description}</div>
              <code className="mt-3 block overflow-x-auto rounded bg-canvas px-2 py-1 text-[11px] text-muted">
                {c.query}
              </code>
            </Link>
          ))}
        </div>
      )}
      <p className="mt-6 text-xs text-muted">
        Data source: <code className="text-text">OLM_METRICS_QUERY_URL</code> — a
        Prometheus-compatible query API that scrapes the metrics-endpoint service (F010). Every
        query is scoped to the active tenant via the <code className="text-text">tenant</code>{' '}
        label.
      </p>
    </>
  );
}
