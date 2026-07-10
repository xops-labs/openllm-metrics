import { ReactNode } from 'react';
import Link from 'next/link';

/**
 * Native analytics section (F038 Phase A). These screens render RAW normalized
 * telemetry only — cost, tokens, errors, reconciliation drift — straight off
 * the Prometheus-compatible query API. They do NOT render scoring, routing, anomaly, or fallback output; those surfaces are outside this analytics section.
 */

const TABS: ReadonlyArray<{ href: string; label: string }> = [
  { href: '/analytics', label: 'Overview' },
  { href: '/analytics/cost', label: 'Cost over time' },
  { href: '/analytics/tokens', label: 'Tokens by team' },
  { href: '/analytics/errors', label: 'Error rate by provider' },
  { href: '/analytics/reconciliation', label: 'Reconciliation drift' },
  { href: '/analytics/agents', label: 'By agent' },
  { href: '/analytics/models', label: 'By model' },
  { href: '/analytics/agent-model', label: 'Agent × model' },
  { href: '/analytics/dashboards', label: 'Dashboards' },
];

// Optional deep-link to Grafana. Grafana is an OPTIONAL extension for power
// users, not where core answers live — the console is self-sufficient. The
// link only renders when OLM_GRAFANA_URL is configured, so a console without a
// Grafana deployment shows no broken link.
const GRAFANA_URL = process.env.OLM_GRAFANA_URL;

interface Props {
  readonly children: ReactNode;
}

export default function AnalyticsLayout({ children }: Props) {
  return (
    <div>
      <nav className="mb-6 flex flex-wrap items-center gap-2 border-b border-border pb-3 text-sm">
        {TABS.map((t) => (
          <Link
            key={t.href}
            href={t.href}
            className="rounded border border-border px-3 py-1 text-muted hover:border-accent hover:text-text"
          >
            {t.label}
          </Link>
        ))}
        {GRAFANA_URL ? (
          <a
            href={GRAFANA_URL}
            target="_blank"
            rel="noreferrer"
            className="ml-auto rounded border border-border px-3 py-1 text-muted hover:border-accent hover:text-text"
            title="Optional power-user deep-dive — the console is self-sufficient without it"
          >
            Open in Grafana ↗
          </a>
        ) : null}
      </nav>
      {children}
    </div>
  );
}
