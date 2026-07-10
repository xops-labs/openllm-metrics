import Link from 'next/link';
import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';

interface Props {
  readonly params: Promise<{ id: string }>;
}

interface Team {
  readonly id: string;
  readonly name: string;
}

interface App {
  readonly id: string;
  readonly name: string;
  readonly team: string;
}

// Placeholder until F005 lands tenant CRUD. The shape mirrors what the
// control-plane API will return.
const TEAMS: Record<string, ReadonlyArray<Team>> = {
  '11111111-2222-3333-4444-555555555555': [
    { id: 't-platform', name: 'platform' },
    { id: 't-ml', name: 'ml' },
  ],
  '22222222-3333-4444-5555-666666666666': [
    { id: 't-platform', name: 'platform' },
    { id: 't-ml', name: 'ml' },
    { id: 't-data', name: 'data' },
  ],
};

const APPS: Record<string, ReadonlyArray<App>> = {
  '11111111-2222-3333-4444-555555555555': [
    { id: 'a-1', name: 'chat-assistant', team: 'platform' },
    { id: 'a-2', name: 'summarizer', team: 'ml' },
  ],
  '22222222-3333-4444-5555-666666666666': [
    { id: 'a-3', name: 'chat-assistant', team: 'platform' },
    { id: 'a-4', name: 'retrieval', team: 'ml' },
    { id: 'a-5', name: 'classifier', team: 'data' },
  ],
};

export default async function TenantDetailPage({ params }: Props) {
  const { id } = await params;
  const teams = TEAMS[id] ?? [];
  const apps = APPS[id] ?? [];

  return (
    <>
      <PageHeader
        title={`Tenant ${id.slice(0, 8)}…`}
        description="Teams and applications registered under this tenant."
        actions={
          <Link
            href="/tenants"
            className="rounded border border-border px-3 py-1 text-xs text-muted hover:text-text"
          >
            Back
          </Link>
        }
      />
      <div className="mb-3 text-xs font-mono text-muted">tenant_id: {id}</div>

      <section className="mb-8">
        <h2 className="mb-2 text-sm font-semibold">Teams</h2>
        <Table<Team>
          columns={[
            {
              key: 'id',
              header: 'ID',
              render: (r) => <span className="font-mono text-xs">{r.id}</span>,
            },
            { key: 'name', header: 'Name', render: (r) => r.name },
          ]}
          rows={teams}
          rowKey={(r) => r.id}
          empty="No teams registered for this tenant."
        />
      </section>

      <section>
        <h2 className="mb-2 text-sm font-semibold">Applications</h2>
        <Table<App>
          columns={[
            {
              key: 'id',
              header: 'ID',
              render: (r) => <span className="font-mono text-xs">{r.id}</span>,
            },
            { key: 'name', header: 'Name', render: (r) => r.name },
            { key: 'team', header: 'Team', render: (r) => r.team },
          ]}
          rows={apps}
          rowKey={(r) => r.id}
          empty="No applications registered for this tenant."
        />
      </section>
    </>
  );
}
