import { NextRequest, NextResponse } from 'next/server';
import { ExportsConfigInput, validateExportsConfig } from '@/lib/api/exports';
import { getExportsConfig, saveExportsConfig } from '@/lib/api/exports-server';

/**
 * Typed config contract for outbound exports (F039).
 *
 *   GET  /api/exports  -> current tenant's ExportsConfig
 *   PUT  /api/exports  -> validate + persist ExportsConfigInput, returns ExportsConfig
 *
 * The active tenant is resolved server-side from the auth/session layer
 * (lib/auth.ts), so the body never carries a tenant id. Validation is enforced
 * here against the typed contract in lib/api/exports.ts; a malformed body is
 * rejected with 422 and a list of issues.
 *
 * This route persists configuration only. The export RUNTIME (the worker that
 * ships series to Grafana / Prometheus remote-write / OTel) is out of scope for
 * this console and consumes the persisted config separately (F039 worker).
 */

export const dynamic = 'force-dynamic';

export async function GET(): Promise<NextResponse> {
  const config = await getExportsConfig();
  return NextResponse.json(config);
}

export async function PUT(req: NextRequest): Promise<NextResponse> {
  let body: unknown;
  try {
    body = await req.json();
  } catch {
    return NextResponse.json({ error: 'invalid JSON body' }, { status: 400 });
  }

  const issues = validateExportsConfig(body);
  if (issues.length > 0) {
    return NextResponse.json({ error: 'validation failed', issues }, { status: 422 });
  }

  try {
    const saved = await saveExportsConfig(body as ExportsConfigInput);
    return NextResponse.json(saved);
  } catch (err) {
    const message = err instanceof Error ? err.message : 'failed to persist exports config';
    return NextResponse.json({ error: message }, { status: 502 });
  }
}
