import Link from 'next/link';
import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import { listPolicies, Policy } from '@/lib/api/policy';

export default async function PoliciesPage() {
  let policies: Policy[] = [];
  let error: string | null = null;
  try {
    const res = await listPolicies();
    policies = res.policies;
  } catch (e) {
    error = (e as Error).message;
  }

  return (
    <>
      <PageHeader
        title="Policies"
        description="Declarative governance policies (F029). Mutations are audited via F031."
        actions={
          <Link
            href="/policies/new"
            className="rounded border border-accent bg-accent px-3 py-1 text-xs text-white"
          >
            New policy
          </Link>
        }
      />
      {error ? (
        <p className="mb-3 text-xs text-danger">policy-service unreachable: {error}</p>
      ) : null}
      <Table<Policy>
        columns={[
          {
            key: 'name',
            header: 'Name',
            render: (r) => (
              <Link href={`/policies/${r.id}`} className="text-accent hover:underline">
                {r.name}
              </Link>
            ),
          },
          {
            key: 'id',
            header: 'ID',
            render: (r) => <span className="font-mono text-xs text-muted">{r.id}</span>,
          },
          {
            key: 'version',
            header: 'Version',
            render: (r) => `v${r.current_version}`,
          },
          {
            key: 'updated',
            header: 'Updated',
            render: (r) => <span className="text-xs text-muted">{r.updated_at}</span>,
          },
        ]}
        rows={policies}
        rowKey={(r) => r.id}
        empty="No policies defined yet."
      />
    </>
  );
}
