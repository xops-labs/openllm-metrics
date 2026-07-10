import { currentUser, tenantHeaders } from '@/lib/auth';

/**
 * Typed tenant client (F005). The tenant switcher and the tenants screen read
 * the set of tenants the current actor may operate on.
 *
 * Endpoint: `GET {OLM_CONTROL_PLANE_URL}/v1/tenants` — returns the tenants the
 * authenticated actor is entitled to. When OLM_CONTROL_PLANE_URL is unset (local
 * dev), a seed list is returned so the switcher and tenants page work offline.
 * The control-plane tenant CRUD API itself is F005 work tracked separately.
 */

const BASE = process.env.OLM_CONTROL_PLANE_URL;

export interface Tenant {
  readonly id: string;
  readonly name: string;
  readonly teams?: number;
  readonly apps?: number;
}

const SEED: ReadonlyArray<Tenant> = [
  { id: '11111111-2222-3333-4444-555555555555', name: 'acme-dev', teams: 2, apps: 4 },
  { id: '22222222-3333-4444-5555-666666666666', name: 'acme-prod', teams: 3, apps: 7 },
];

export async function listTenants(): Promise<Tenant[]> {
  if (!BASE) {
    return [...SEED];
  }
  try {
    const user = await currentUser();
    const res = await fetch(`${BASE}/v1/tenants`, {
      headers: { ...tenantHeaders(user) },
      cache: 'no-store',
    });
    if (!res.ok) {
      return [...SEED];
    }
    const body = (await res.json()) as { tenants?: Tenant[] };
    return body.tenants ?? [...SEED];
  } catch {
    return [...SEED];
  }
}
