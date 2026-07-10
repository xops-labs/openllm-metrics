import Link from 'next/link';
import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import { Tenant, listTenants } from '@/lib/api/tenant';

// Tenants are read from the typed tenant client (lib/api/tenant.ts), which hits
// the control-plane tenant endpoint when OLM_CONTROL_PLANE_URL is set and falls
// back to a dev seed otherwise. Tenant CRUD ships with the F005 control plane.
export default async function TenantsPage() {
  const tenants = await listTenants();
  return (
    <>
      <PageHeader
        title="Tenants"
        description="Tenant, team, and app inventory. CRUD ships with the F005 control-plane API; this view is read-only."
      />
      <Table<Tenant>
        columns={[
          {
            key: 'name',
            header: 'Name',
            render: (r) => (
              <Link href={`/tenants/${r.id}`} className="text-accent hover:underline">
                {r.name}
              </Link>
            ),
          },
          {
            key: 'id',
            header: 'Tenant ID',
            render: (r) => <span className="font-mono text-xs text-muted">{r.id}</span>,
          },
          { key: 'teams', header: 'Teams', render: (r) => r.teams ?? '—' },
          { key: 'apps', header: 'Apps', render: (r) => r.apps ?? '—' },
        ]}
        rows={tenants}
        rowKey={(r) => r.id}
      />
    </>
  );
}
