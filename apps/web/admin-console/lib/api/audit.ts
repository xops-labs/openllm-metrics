import { currentUser, tenantHeaders } from '@/lib/auth';

const BASE = process.env.OLM_AUDIT_SERVICE_URL ?? 'http://localhost:8091';

export interface AuditEntry {
  readonly id: number;
  readonly tenant_id: string;
  readonly actor: { id: string; type?: string };
  readonly action: string;
  readonly resource: { id: string; name: string; type: string };
  readonly payload: Record<string, unknown>;
  readonly created_at: string;
  readonly entry_hash: string;
}

export interface AuditQuery {
  action?: string;
  actor_id?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
}

export interface AuditPage {
  readonly entries: AuditEntry[];
  readonly next: string | null;
}

async function call<T>(path: string): Promise<T> {
  const user = await currentUser();
  const url = new URL(`${BASE}${path}`);
  if (!url.searchParams.has('tenant')) {
    url.searchParams.set('tenant', user.tenantId);
  }
  const res = await fetch(url.toString(), {
    headers: { ...tenantHeaders(user) },
    cache: 'no-store',
  });
  if (!res.ok) {
    throw new Error(`audit-service ${path} -> ${res.status}`);
  }
  return (await res.json()) as T;
}

export function listEntries(q: AuditQuery = {}): Promise<AuditPage> {
  const params = new URLSearchParams();
  if (q.action) params.set('action', q.action);
  if (q.actor_id) params.set('actor_id', q.actor_id);
  if (q.from) params.set('from', q.from);
  if (q.to) params.set('to', q.to);
  if (q.limit) params.set('limit', String(q.limit));
  if (q.cursor) params.set('cursor', q.cursor);
  const qs = params.toString();
  return call(`/v1/audit/entries${qs ? `?${qs}` : ''}`);
}

export async function recentCount(hours: number): Promise<number> {
  const from = new Date(Date.now() - hours * 3600 * 1000).toISOString();
  const page = await listEntries({ from, limit: 500 });
  return page.entries.length;
}
