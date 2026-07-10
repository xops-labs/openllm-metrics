import Link from 'next/link';
import { revalidatePath } from 'next/cache';
import { PageHeader } from '@/components/page-header';
import { Table } from '@/components/table';
import {
  Channel,
  ChannelKind,
  OSS_CHANNEL_KINDS,
  createChannel,
  listChannels,
} from '@/lib/api/notification';

async function createChannelAction(formData: FormData) {
  'use server';
  const name = String(formData.get('name') ?? '').trim();
  const kindRaw = String(formData.get('kind') ?? '');
  if (!OSS_CHANNEL_KINDS.includes(kindRaw as ChannelKind)) {
    // Hard rule: OSS only - reject anything not in the locked list.
    throw new Error(`channel kind ${kindRaw} is not OSS-safe`);
  }
  const kind = kindRaw as ChannelKind;
  const target = String(formData.get('target') ?? '').trim();
  const config =
    kind === 'webhook'
      ? { url: target }
      : { from: 'alerts@local', to: target.split(',').map((s) => s.trim()) };
  await createChannel({ name, kind, config });
  revalidatePath('/notifications/channels');
}

export default async function ChannelsPage() {
  const channels = await listChannels();

  return (
    <>
      <PageHeader
        title="Notification channels"
        description="OSS supports webhook and SMTP only (F033). Slack, PagerDuty, and Teams ship in the open-source build."
        actions={
          <Link
            href="/notifications/rules"
            className="rounded border border-border px-3 py-1 text-xs text-muted hover:text-text"
          >
            Rules
          </Link>
        }
      />

      <form
        action={createChannelAction}
        className="mb-6 grid grid-cols-1 gap-2 rounded border border-border bg-panel p-3 md:grid-cols-4"
      >
        <input
          name="name"
          required
          placeholder="channel name"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <select
          name="kind"
          required
          defaultValue="webhook"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        >
          {/* Hard-coded list - DO NOT add Slack/PagerDuty/Teams here. */}
          <option value="webhook">webhook</option>
          <option value="smtp">smtp</option>
        </select>
        <input
          name="target"
          required
          placeholder="webhook URL or comma-separated emails"
          className="rounded border border-border bg-canvas px-2 py-1 text-xs text-text"
        />
        <button
          type="submit"
          className="rounded border border-accent bg-accent px-2 py-1 text-xs text-white"
        >
          Add channel
        </button>
      </form>

      <Table<Channel>
        columns={[
          { key: 'name', header: 'Name', render: (r) => r.name },
          { key: 'kind', header: 'Kind', render: (r) => r.kind },
          {
            key: 'config',
            header: 'Target',
            render: (r) => (
              <span className="font-mono text-xs text-muted">
                {r.kind === 'webhook'
                  ? String((r.config as { url?: string }).url ?? '')
                  : ((r.config as { to?: string[] }).to ?? []).join(', ')}
              </span>
            ),
          },
          {
            key: 'enabled',
            header: 'Enabled',
            render: (r) => (r.enabled ? 'yes' : 'no'),
          },
        ]}
        rows={channels}
        rowKey={(r) => r.id}
        empty="No channels configured."
      />
    </>
  );
}
