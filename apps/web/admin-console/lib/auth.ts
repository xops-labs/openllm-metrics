import { cookies, headers } from 'next/headers';
import { auth } from '@/lib/auth-config';

/**
 * Identity for the OSS admin console (F005).
 *
 * Resolution order for the actor identity:
 *   1. Auth.js session (real OIDC, or the local dev-login fallback).
 *   2. `X-OLM-User` request header (mesh / sidecar dev passthrough).
 *   3. `OLM_DEV_USER` env, else `dev@local`.
 *
 * Tenant resolution order:
 *   1. `olm_tenant` cookie (set by the tenant switcher — operators may belong
 *      to several tenants and switch between them).
 *   2. The session's `tenantId` (from the OIDC tenant claim).
 *   3. `OLM_DEFAULT_TENANT` env, else the dev seed UUID.
 *
 * This keeps the app fully functional locally with no IdP configured while
 * giving production a real session-backed identity.
 */
export interface DevUser {
  readonly id: string;
  readonly tenantId: string;
}

const DEFAULT_TENANT = process.env.OLM_DEFAULT_TENANT ?? '11111111-2222-3333-4444-555555555555';

export async function currentUser(): Promise<DevUser> {
  const session = await auth().catch(() => null);
  const hdrs = await headers();
  const ck = await cookies();

  const sessionId = session?.user?.id;
  const headerUser = hdrs.get('x-olm-user');
  const envUser = process.env.OLM_DEV_USER ?? 'dev@local';
  const id = sessionId ?? headerUser ?? envUser;

  const tenantCookie = ck.get('olm_tenant')?.value;
  const sessionTenant = session?.user?.tenantId;
  const tenantId = tenantCookie ?? sessionTenant ?? DEFAULT_TENANT;

  return { id, tenantId };
}

export function tenantHeaders(user: DevUser): Record<string, string> {
  return {
    'X-Tenant-ID': user.tenantId,
    'X-Actor': user.id,
    'X-OLM-User': user.id,
  };
}
