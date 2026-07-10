import { currentUser, tenantHeaders } from '@/lib/auth';

const BASE = process.env.OLM_NOTIFICATION_SERVICE_URL ?? 'http://localhost:8092';

/**
 * Locked to OSS-only channel kinds. Slack and PagerDuty live in the
 * this repository and must not be selectable here.
 */
export type ChannelKind = 'webhook' | 'smtp';

export const OSS_CHANNEL_KINDS: readonly ChannelKind[] = ['webhook', 'smtp'];

export interface Channel {
  readonly id: string;
  readonly tenant_id: string;
  readonly name: string;
  readonly kind: ChannelKind;
  readonly config: Record<string, unknown>;
  readonly enabled: boolean;
  readonly created_at: string;
}

export interface Rule {
  readonly id: string;
  readonly tenant_id: string;
  readonly name: string;
  readonly severity: 'info' | 'warn' | 'critical';
  readonly channel_ids: readonly string[];
  readonly enabled: boolean;
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const user = await currentUser();
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...tenantHeaders(user),
      ...(init?.headers ?? {}),
    },
    cache: 'no-store',
  });
  if (!res.ok) {
    if (res.status === 404 || res.status >= 500) {
      // Tolerate offline service - the OSS notification service is optional.
      return {} as T;
    }
    throw new Error(`notification-service ${path} -> ${res.status}`);
  }
  return (await res.json()) as T;
}

export async function listChannels(): Promise<Channel[]> {
  const r = await call<{ channels?: Channel[] }>('/v1/channels');
  return r.channels ?? [];
}

export async function listRules(): Promise<Rule[]> {
  const r = await call<{ rules?: Rule[] }>('/v1/rules');
  return r.rules ?? [];
}

export function createChannel(input: {
  name: string;
  kind: ChannelKind;
  config: Record<string, unknown>;
}): Promise<Channel> {
  if (!OSS_CHANNEL_KINDS.includes(input.kind)) {
    throw new Error(`channel kind ${input.kind} is not OSS-safe`);
  }
  return call('/v1/channels', { method: 'POST', body: JSON.stringify(input) });
}

export function createRule(input: {
  name: string;
  severity: Rule['severity'];
  channel_ids: string[];
}): Promise<Rule> {
  return call('/v1/rules', { method: 'POST', body: JSON.stringify(input) });
}
