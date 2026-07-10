import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import { Pagination } from '@/components/pagination';
import { AuditEntry, AuditQuery, listEntries } from '@/lib/api/audit';

interface Props {
  readonly searchParams: Promise<{
    action?: string;
    actor_id?: string;
    from?: string;
    to?: string;
    cursor?: string;
  }>;
}

export default async function AuditPage({ searchParams }: Props) {
  const sp = await searchParams;
  // Build conditionally so exactOptionalPropertyTypes is satisfied (omit, not
  // assign-undefined, optional string fields).
  const q: AuditQuery = { limit: 50 };
  if (sp.action) q.action = sp.action;
  if (sp.actor_id) q.actor_id = sp.actor_id;
  if (sp.from) q.from = sp.from;
  if (sp.to) q.to = sp.to;
  if (sp.cursor) q.cursor = sp.cursor;

  let entries: AuditEntry[] = [];
  let next: string | null = null;
  let error: string | null = null;
  try {
    const page = await listEntries(q);
    entries = page.entries;
    next = page.next;
  } catch (e) {
    error = (e as Error).message;
  }

  return (
    <>
      <PageHeader
        title="Audit ledger"
        description="Append-only hash-chained audit entries (F031). Tenant-scoped, paginated, filterable."
      />

      <form
        method="get"
        className="mb-4 grid grid-cols-1 gap-2 rounded border border-border bg-panel p-3 md:grid-cols-5"
      >
        <input
          name="action"
          defaultValue={sp.action ?? ''}
          placeholder="action (e.g. policy.created)"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <input
          name="actor_id"
          defaultValue={sp.actor_id ?? ''}
          placeholder="actor id"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <input
          name="from"
          defaultValue={sp.from ?? ''}
          placeholder="from RFC3339"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <input
          name="to"
          defaultValue={sp.to ?? ''}
          placeholder="to RFC3339"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <button
          type="submit"
          className="rounded border border-accent bg-accent px-2 py-1 text-xs text-white"
        >
          Filter
        </button>
      </form>

      {error ? (
        <p className="mb-3 text-xs text-danger">audit-service unreachable: {error}</p>
      ) : null}

      <Table<AuditEntry>
        columns={[
          {
            key: 'id',
            header: 'ID',
            render: (r) => <span className="font-mono text-xs text-muted">{r.id}</span>,
          },
          {
            key: 'at',
            header: 'Created',
            render: (r) => <span className="text-xs text-muted">{r.created_at}</span>,
          },
          { key: 'action', header: 'Action', render: (r) => r.action },
          {
            key: 'actor',
            header: 'Actor',
            render: (r) => <span className="font-mono text-xs">{r.actor?.id ?? '-'}</span>,
          },
          {
            key: 'resource',
            header: 'Resource',
            render: (r) => (
              <span className="font-mono text-xs">
                {r.resource?.name ?? r.resource?.id ?? '-'}
                {r.resource?.type ? (
                  <span className="text-muted"> ({r.resource.type})</span>
                ) : null}
              </span>
            ),
          },
          {
            key: 'hash',
            header: 'Entry hash',
            render: (r) => (
              <span className="font-mono text-[10px] text-muted">{r.entry_hash.slice(0, 12)}…</span>
            ),
          },
        ]}
        rows={entries}
        rowKey={(r) => String(r.id)}
        empty="No audit entries match this filter."
      />

      <Pagination
        basePath="/audit"
        currentQuery={{
          action: sp.action,
          actor_id: sp.actor_id,
          from: sp.from,
          to: sp.to,
        }}
        nextCursor={next}
      />
    </>
  );
}
