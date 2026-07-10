import Link from 'next/link';
import { revalidatePath } from 'next/cache';
import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import { Rule, createRule, listChannels, listRules } from '@/lib/api/notification';

async function createRuleAction(formData: FormData) {
  'use server';
  const name = String(formData.get('name') ?? '').trim();
  const severity = String(formData.get('severity') ?? 'warn') as Rule['severity'];
  const channelIds = (formData.getAll('channel_ids') as string[]).filter(Boolean);
  await createRule({ name, severity, channel_ids: channelIds });
  revalidatePath('/notifications/rules');
}

export default async function RulesPage() {
  const [rules, channels] = await Promise.all([listRules(), listChannels()]);

  return (
    <>
      <PageHeader
        title="Notification rules"
        description="Severity → channel mapping. Tenant-scoped."
        actions={
          <Link
            href="/notifications/channels"
            className="rounded border border-border px-3 py-1 text-xs text-muted hover:text-text"
          >
            Channels
          </Link>
        }
      />

      <form
        action={createRuleAction}
        className="mb-6 grid grid-cols-1 gap-2 rounded border border-border bg-panel p-3 md:grid-cols-4"
      >
        <input
          name="name"
          required
          placeholder="rule name"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <select
          name="severity"
          defaultValue="warn"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        >
          <option value="info">info</option>
          <option value="warn">warn</option>
          <option value="critical">critical</option>
        </select>
        <select
          name="channel_ids"
          multiple
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        >
          {channels.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name} ({c.kind})
            </option>
          ))}
        </select>
        <button
          type="submit"
          className="rounded border border-accent bg-accent px-2 py-1 text-xs text-white"
        >
          Add rule
        </button>
      </form>

      <Table<Rule>
        columns={[
          { key: 'name', header: 'Name', render: (r) => r.name },
          { key: 'severity', header: 'Severity', render: (r) => r.severity },
          {
            key: 'channels',
            header: 'Channels',
            render: (r) => (
              <span className="font-mono text-xs text-muted">{r.channel_ids.length}</span>
            ),
          },
          {
            key: 'enabled',
            header: 'Enabled',
            render: (r) => (r.enabled ? 'yes' : 'no'),
          },
        ]}
        rows={rules}
        rowKey={(r) => r.id}
        empty="No rules configured."
      />
    </>
  );
}
