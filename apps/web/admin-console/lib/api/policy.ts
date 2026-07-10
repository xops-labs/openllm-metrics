import { currentUser, tenantHeaders } from '@/lib/auth';

const BASE = process.env.OLM_POLICY_SERVICE_URL ?? 'http://localhost:8090';

export interface Policy {
  readonly id: string;
  readonly tenant_id: string;
  readonly name: string;
  readonly current_version: number;
  readonly document: Record<string, unknown>;
  readonly created_at: string;
  readonly updated_at: string;
}

export interface PolicyVersion {
  readonly policy_id: string;
  readonly version: number;
  readonly document: Record<string, unknown>;
  readonly actor: string;
  readonly created_at: string;
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
    throw new Error(`policy-service ${path} -> ${res.status}`);
  }
  return (await res.json()) as T;
}

export function listPolicies(): Promise<{ policies: Policy[] }> {
  return call('/v1/policies');
}

export function getPolicy(id: string): Promise<Policy> {
  return call(`/v1/policies/${encodeURIComponent(id)}`);
}

export function listVersions(id: string): Promise<{ versions: PolicyVersion[] }> {
  return call(`/v1/policies/${encodeURIComponent(id)}/versions`);
}

export function getVersion(id: string, n: number): Promise<PolicyVersion> {
  return call(`/v1/policies/${encodeURIComponent(id)}/versions/${n}`);
}

export function createPolicy(name: string, document: Record<string, unknown>): Promise<Policy> {
  return call('/v1/policies', {
    method: 'POST',
    body: JSON.stringify({ name, document }),
  });
}

export function appendVersion(id: string, document: Record<string, unknown>): Promise<Policy> {
  return call(`/v1/policies/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify({ document }),
  });
}

/**
 * Fetch the JSON Schema that the policy service validates against, so the
 * client-side @rjsf form is generated from the same contract the server
 * enforces. Falls back to a minimal stub if the policy service is offline.
 */
export async function getPolicySchema(): Promise<Record<string, unknown>> {
  try {
    return await call<Record<string, unknown>>('/v1/policies/schema');
  } catch {
    return {
      $schema: 'https://json-schema.org/draft/2020-12/schema',
      type: 'object',
      required: ['kind', 'scope'],
      properties: {
        kind: { type: 'string', enum: ['budget', 'allow_model', 'rate_limit'] },
        scope: {
          type: 'object',
          properties: {
            tenant: { type: 'string' },
            team: { type: 'string' },
            app: { type: 'string' },
          },
        },
        parameters: { type: 'object', additionalProperties: true },
      },
    };
  }
}
