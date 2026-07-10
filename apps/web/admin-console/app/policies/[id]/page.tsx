import Link from 'next/link';
import { notFound } from 'next/navigation';
import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import { DiffViewer } from '@/components/diff-viewer';
import { getPolicy, getVersion, listVersions, PolicyVersion } from '@/lib/api/policy';

interface Props {
  readonly params: Promise<{ id: string }>;
  readonly searchParams: Promise<{ a?: string; b?: string }>;
}

export default async function PolicyDetailPage({ params, searchParams }: Props) {
  const { id } = await params;
  const { a, b } = await searchParams;

  let policy;
  let versions: PolicyVersion[] = [];
  try {
    policy = await getPolicy(id);
    const v = await listVersions(id);
    versions = v.versions;
  } catch {
    notFound();
  }

  const va = a ? Number.parseInt(a, 10) : null;
  const vb = b ? Number.parseInt(b, 10) : null;
  let left: PolicyVersion | null = null;
  let right: PolicyVersion | null = null;
  if (va && vb && Number.isFinite(va) && Number.isFinite(vb)) {
    [left, right] = await Promise.all([getVersion(id, va), getVersion(id, vb)]);
  }

  return (
    <>
      <PageHeader
        title={policy.name}
        description={`Policy ${policy.id} · current v${policy.current_version}`}
        actions={
          <Link
            href={`/policies/${id}/edit`}
            className="rounded border border-accent bg-accent px-3 py-1 text-xs text-white"
          >
            Edit (append version)
          </Link>
        }
      />

      <section className="mb-8">
        <h2 className="mb-2 text-sm font-semibold">Current document</h2>
        <pre className="overflow-auto rounded border border-border bg-panel p-3 font-mono text-xs text-text">
          {JSON.stringify(policy.document, null, 2)}
        </pre>
      </section>

      <section className="mb-8">
        <h2 className="mb-2 text-sm font-semibold">Version history</h2>
        <Table<PolicyVersion>
          columns={[
            { key: 'v', header: 'Version', render: (r) => `v${r.version}` },
            {
              key: 'actor',
              header: 'Actor',
              render: (r) => <span className="font-mono text-xs">{r.actor}</span>,
            },
            {
              key: 'at',
              header: 'Created',
              render: (r) => <span className="text-xs text-muted">{r.created_at}</span>,
            },
            {
              key: 'diff',
              header: 'Compare',
              render: (r) => {
                const other =
                  policy.current_version === r.version ? r.version - 1 : policy.current_version;
                if (other < 1) return null;
                return (
                  <Link
                    href={`/policies/${id}?a=${r.version}&b=${other}`}
                    className="text-accent hover:underline"
                  >
                    diff vs v{other}
                  </Link>
                );
              },
            },
          ]}
          rows={versions}
          rowKey={(r) => `${r.policy_id}-${r.version}`}
          empty="No prior versions."
        />
      </section>

      {left && right ? (
        <section>
          <h2 className="mb-2 text-sm font-semibold">
            Diff v{left.version} vs v{right.version}
          </h2>
          <DiffViewer
            left={left.document}
            right={right.document}
            leftLabel={`v${left.version} · ${left.created_at}`}
            rightLabel={`v${right.version} · ${right.created_at}`}
          />
        </section>
      ) : null}
    </>
  );
}
