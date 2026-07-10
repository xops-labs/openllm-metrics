import Link from 'next/link';
import { PageHeader } from '@/components/page-header';
import { listPolicies } from '@/lib/api/policy';
import { recentCount } from '@/lib/api/audit';
import { listDecisions } from '@/lib/api/decision';

async function safe<T>(p: Promise<T>, fallback: T): Promise<T> {
  try {
    return await p;
  } catch {
    return fallback;
  }
}

function Tile({
  title,
  value,
  href,
  hint,
}: {
  title: string;
  value: string | number;
  href: string;
  hint?: string;
}) {
  return (
    <Link
      href={href}
      className="block rounded border border-border bg-panel p-4 hover:border-accent"
    >
      <div className="text-xs uppercase tracking-wider text-muted">{title}</div>
      <div className="mt-2 text-2xl font-semibold text-text">{value}</div>
      {hint ? <div className="mt-1 text-xs text-muted">{hint}</div> : null}
    </Link>
  );
}

export default async function OverviewPage() {
  const [policies, auditCount, decisions] = await Promise.all([
    safe(listPolicies(), { policies: [] }),
    safe(recentCount(24), 0),
    safe(listDecisions(10), { decisions: [], next: 0 }),
  ]);

  return (
    <>
      <PageHeader
        title="Overview"
        description="Tenant-scoped operational snapshot. Numbers are counts only — no prompt or completion text is rendered."
      />
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
        <Tile title="Tenants" value={2} href="/tenants" hint="known in dev stub" />
        <Tile title="Policies" value={policies.policies.length} href="/policies" />
        <Tile title="Audit (24h)" value={auditCount} href="/audit" hint="entries appended" />
        <Tile
          title="Routing decisions"
          value={decisions.decisions.length}
          href="/decisions"
          hint="last 10 (OSS no-op decider may emit 0)"
        />
      </div>

      <section className="mt-8">
        <h2 className="mb-3 text-sm font-semibold text-text">Recent routing decisions</h2>
        {decisions.decisions.length === 0 ? (
          <p className="rounded border border-border bg-panel p-4 text-sm text-muted">
            No decisions in the read store yet. The OSS default decider is a no-op; rich decision
            data depends on F036.
          </p>
        ) : (
          <ul className="divide-y divide-border rounded border border-border bg-panel">
            {decisions.decisions.map((d) => (
              <li key={d.decision_id} className="px-4 py-2 text-sm">
                <span className="font-mono text-xs text-muted">{d.decided_at}</span>
                <span className="ml-3 text-text">
                  {d.provider_chosen}/{d.model_chosen}
                </span>
                <span className="ml-3 text-xs text-muted">
                  {d.team}/{d.app}@{d.env} · requested {d.provider_requested}/{d.model_requested} ·{' '}
                  {d.decider_version}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  );
}
