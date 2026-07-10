import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import { EmptyState } from '@/components/empty-state';
import { Decision, listDecisions } from '@/lib/api/decision';

function formatReasonChain(d: Decision): string {
  if (!Array.isArray(d.reason_chain) || d.reason_chain.length === 0) {
    return '—';
  }
  return d.reason_chain.map((s) => `${s.factor}=${String(s.value)}`).join(' → ');
}

export default async function DecisionsPage() {
  const { decisions } = await listDecisions(100);

  return (
    <>
      <PageHeader
        title="Routing decisions"
        description="Read-only decision explorer (F036). Rows show the requested and chosen provider/model/route plus the decider's reason chain — never prompt or completion text."
      />

      {decisions.length === 0 ? (
        <EmptyState
          title="No routing decisions in the read store"
          hint="The OSS no-op decider may not emit decision rows. F036 lands the rich decision ledger; a registered decider populates it."
        />
      ) : (
        <Table<Decision>
          columns={[
            {
              key: 'at',
              header: 'Decided',
              render: (r) => <span className="text-xs text-muted">{r.decided_at}</span>,
            },
            {
              key: 'app',
              header: 'App',
              render: (r) => (
                <span className="font-mono text-xs">
                  {r.team}/{r.app}@{r.env}
                </span>
              ),
            },
            {
              key: 'requested',
              header: 'Requested',
              render: (r) => (
                <span className="font-mono text-xs">
                  {r.provider_requested}/{r.model_requested}
                </span>
              ),
            },
            {
              key: 'chosen',
              header: 'Chosen',
              render: (r) => (
                <span
                  className={
                    r.provider_chosen === r.provider_requested &&
                    r.model_chosen === r.model_requested
                      ? 'text-text'
                      : 'text-warn'
                  }
                >
                  {r.provider_chosen}/{r.model_chosen}
                </span>
              ),
            },
            {
              key: 'reason',
              header: 'Reason chain',
              render: (r) => <span className="text-xs text-muted">{formatReasonChain(r)}</span>,
            },
            {
              key: 'alternatives',
              header: 'Alts',
              render: (r) => (
                <span className="text-xs">
                  {Array.isArray(r.alternatives) ? r.alternatives.length : 0}
                </span>
              ),
            },
            {
              key: 'decider',
              header: 'Decider',
              render: (r) => (
                <span className="font-mono text-xs text-muted">{r.decider_version}</span>
              ),
            },
          ]}
          rows={decisions}
          rowKey={(r) => r.decision_id}
        />
      )}
    </>
  );
}
