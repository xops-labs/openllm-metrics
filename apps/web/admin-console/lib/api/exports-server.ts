import 'server-only';
import { currentUser } from '@/lib/auth';
import { ExportTarget, ExportsConfig, ExportsConfigInput } from '@/lib/api/exports';

/**
 * Server-only persistence for the F039 export config. Kept separate from
 * `exports.ts` (the pure contract) so client components can import the types +
 * validator without dragging in `next/headers`.
 *
 * Persistence is pluggable: when OLM_EXPORTS_CONFIG_URL is set the config is
 * proxied to a control-plane endpoint; otherwise it is held in a process-local
 * dev store so the page is fully functional with no backend.
 */

const devStore = new Map<string, ExportsConfig>();

function withIds(input: ExportsConfigInput): ExportTarget[] {
  return input.targets.map((t, i) => ({ ...t, id: `${t.kind}-${i + 1}` }) as ExportTarget);
}

export async function getExportsConfig(): Promise<ExportsConfig> {
  const user = await currentUser();
  const remote = process.env.OLM_EXPORTS_CONFIG_URL;
  if (remote) {
    try {
      const res = await fetch(`${remote}/v1/exports?tenant=${encodeURIComponent(user.tenantId)}`, {
        cache: 'no-store',
      });
      if (res.ok) {
        return (await res.json()) as ExportsConfig;
      }
    } catch {
      // fall through to empty config
    }
    return { tenantId: user.tenantId, targets: [], updatedAt: new Date(0).toISOString() };
  }
  return (
    devStore.get(user.tenantId) ?? {
      tenantId: user.tenantId,
      targets: [],
      updatedAt: new Date(0).toISOString(),
    }
  );
}

export async function saveExportsConfig(input: ExportsConfigInput): Promise<ExportsConfig> {
  const user = await currentUser();
  const config: ExportsConfig = {
    tenantId: user.tenantId,
    targets: withIds(input),
    updatedAt: new Date().toISOString(),
  };
  const remote = process.env.OLM_EXPORTS_CONFIG_URL;
  if (remote) {
    const res = await fetch(`${remote}/v1/exports`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    });
    if (!res.ok) {
      throw new Error(`exports config service -> ${res.status}`);
    }
    return (await res.json()) as ExportsConfig;
  }
  devStore.set(user.tenantId, config);
  return config;
}
