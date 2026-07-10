'use client';

import { useTransition } from 'react';
import { useRouter } from 'next/navigation';
import type { Tenant } from '@/lib/api/tenant';

interface Props {
  readonly current: string;
  readonly tenants: ReadonlyArray<Tenant>;
}

/**
 * Tenant switcher backed by the typed tenant client (lib/api/tenant.ts). The
 * list is fetched server-side from the control-plane tenant endpoint (or the
 * dev seed when unconfigured) and passed in. Selecting a tenant writes the
 * `olm_tenant` cookie that lib/auth.ts reads to scope every backend call.
 */
export function TenantSwitcher({ current, tenants }: Props) {
  const router = useRouter();
  const [pending, start] = useTransition();
  const known = tenants.some((t) => t.id === current);

  return (
    <select
      value={current}
      disabled={pending}
      onChange={(e) => {
        const id = e.target.value;
        document.cookie = `olm_tenant=${id}; path=/; SameSite=Lax`;
        start(() => router.refresh());
      }}
      className="rounded border border-border bg-panel px-2 py-1 text-xs text-text"
    >
      {known ? null : <option value={current}>{current.slice(0, 8)}</option>}
      {tenants.map((t) => (
        <option key={t.id} value={t.id}>
          {t.name}
        </option>
      ))}
    </select>
  );
}
