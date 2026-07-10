/**
 * Outbound export configuration CONTRACT (F039).
 *
 * OSS bundles first-class analytics in this console; exporting telemetry to
 * Grafana / Prometheus remote-write / OTel is an OPTIONAL add-on, not a
 * prerequisite. This module holds the typed config types and the pure
 * validator — it imports NO server-only modules, so it is safe to import from
 * both client components and the API route. Server-side persistence lives in
 * `exports-server.ts`.
 *
 * The export RUNTIME (the worker that actually ships series out) is not
 * implemented here — the persisted config is consumed by the F039 export
 * worker. The contract below is real and enforced at the API boundary
 * (see app/api/exports/route.ts).
 */

export const EXPORT_KINDS = ['grafana', 'prometheus_remote_write', 'otel'] as const;
export type ExportKind = (typeof EXPORT_KINDS)[number];

interface ExportBase {
  readonly id: string;
  readonly name: string;
  readonly enabled: boolean;
}

/** Grafana data-source / dashboard provisioning bridge. */
export interface GrafanaExport extends ExportBase {
  readonly kind: 'grafana';
  readonly url: string;
  /** Header name used to pass the API token; value lives in a secret store. */
  readonly apiKeyEnv: string;
  readonly orgId?: number;
}

/** Prometheus remote-write sink. */
export interface PrometheusRemoteWriteExport extends ExportBase {
  readonly kind: 'prometheus_remote_write';
  readonly endpoint: string;
  /** Optional basic-auth username; password resolved from a secret store. */
  readonly username?: string;
  readonly passwordEnv?: string;
}

/** OpenTelemetry (GenAI semantic conventions) OTLP export. */
export interface OtelExport extends ExportBase {
  readonly kind: 'otel';
  readonly endpoint: string;
  readonly protocol: 'grpc' | 'http/protobuf';
  /** Static OTLP headers (e.g. auth). Values must reference secret-store keys. */
  readonly headers?: Readonly<Record<string, string>>;
}

export type ExportTarget = GrafanaExport | PrometheusRemoteWriteExport | OtelExport;

export interface ExportsConfig {
  readonly tenantId: string;
  readonly targets: readonly ExportTarget[];
  readonly updatedAt: string;
}

export type ExportTargetInput =
  | Omit<GrafanaExport, 'id'>
  | Omit<PrometheusRemoteWriteExport, 'id'>
  | Omit<OtelExport, 'id'>;

export interface ExportsConfigInput {
  readonly targets: readonly ExportTargetInput[];
}

export interface ValidationIssue {
  readonly path: string;
  readonly message: string;
}

/**
 * Validate a posted exports config against the typed contract. Returns the set
 * of issues; empty array means valid. Pure function — shared by the API route
 * and any client-side pre-validation.
 */
export function validateExportsConfig(input: unknown): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  if (typeof input !== 'object' || input === null) {
    return [{ path: '', message: 'body must be an object' }];
  }
  const targets = (input as { targets?: unknown }).targets;
  if (!Array.isArray(targets)) {
    return [{ path: 'targets', message: 'targets must be an array' }];
  }

  targets.forEach((t, i) => {
    const at = (field: string) => `targets[${i}].${field}`;
    if (typeof t !== 'object' || t === null) {
      issues.push({ path: `targets[${i}]`, message: 'target must be an object' });
      return;
    }
    const target = t as Record<string, unknown>;
    if (typeof target.name !== 'string' || target.name.trim() === '') {
      issues.push({ path: at('name'), message: 'name is required' });
    }
    if (typeof target.enabled !== 'boolean') {
      issues.push({ path: at('enabled'), message: 'enabled must be a boolean' });
    }
    const kind = target.kind;
    if (!EXPORT_KINDS.includes(kind as ExportKind)) {
      issues.push({ path: at('kind'), message: `kind must be one of ${EXPORT_KINDS.join(', ')}` });
      return;
    }
    switch (kind as ExportKind) {
      case 'grafana':
        if (typeof target.url !== 'string' || !isUrl(target.url)) {
          issues.push({ path: at('url'), message: 'url must be a valid http(s) URL' });
        }
        if (typeof target.apiKeyEnv !== 'string' || target.apiKeyEnv.trim() === '') {
          issues.push({
            path: at('apiKeyEnv'),
            message: 'apiKeyEnv (secret env var name) is required',
          });
        }
        break;
      case 'prometheus_remote_write':
        if (typeof target.endpoint !== 'string' || !isUrl(target.endpoint)) {
          issues.push({ path: at('endpoint'), message: 'endpoint must be a valid http(s) URL' });
        }
        break;
      case 'otel':
        if (typeof target.endpoint !== 'string' || !isUrl(target.endpoint)) {
          issues.push({ path: at('endpoint'), message: 'endpoint must be a valid http(s) URL' });
        }
        if (target.protocol !== 'grpc' && target.protocol !== 'http/protobuf') {
          issues.push({
            path: at('protocol'),
            message: "protocol must be 'grpc' or 'http/protobuf'",
          });
        }
        break;
    }
  });

  return issues;
}

function isUrl(v: string): boolean {
  try {
    const u = new URL(v);
    return u.protocol === 'http:' || u.protocol === 'https:';
  } catch {
    return false;
  }
}
