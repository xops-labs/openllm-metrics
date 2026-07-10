import { PageHeader } from '@/components/page-header';
import { getExportsConfig } from '@/lib/api/exports-server';
import { ExportsEditor } from '@/components/exports-editor';

/**
 * Outbound exports settings (F039). OSS ships first-party analytics in this
 * console; exporting to Grafana / Prometheus remote-write / OTel is optional.
 * This page edits the typed export config persisted via PUT /api/exports.
 */
export default async function ExportsSettingsPage() {
  const config = await getExportsConfig();

  return (
    <>
      <PageHeader
        title="Exports"
        description="Optional outbound telemetry exports. Native analytics work without these — configure a target only if you also want the data in Grafana, a Prometheus remote-write store, or an OTel collector."
      />
      <p className="mb-6 rounded border border-border bg-panel p-3 text-xs text-muted">
        Secrets (API tokens, passwords) are never stored here. Each target references the{' '}
        <em>name</em> of an env var / secret-store key that the F039 export worker resolves at
        runtime. The export runtime itself is out of scope for the console.
      </p>
      <ExportsEditor initial={config} />
    </>
  );
}
