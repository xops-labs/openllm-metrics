import { currentUser, tenantHeaders } from '@/lib/auth';

const BASE = process.env.OLM_DECISION_SERVICE_URL ?? 'http://localhost:8093';

/**
 * Routing decisions (F036), matching the decision-service wire contract
 * (DecisionWire in apps/api/decision-service/internal/handler/decisions.go
 * and packages/contracts/routing/v1/decision-event.schema.json).
 *
 * The service requires a `tenant` query parameter on every endpoint; in OSS
 * the no-op decider may not emit any rows, so every helper degrades to an
 * empty result instead of throwing and the console renders an explanatory
 * empty-state.
 *
 * IMPORTANT: A routing decision row never contains prompt text or completion
 * text. The shape below intentionally exposes only requested/chosen
 * provider-model-route identifiers, the decider's reason chain, and
 * timestamps. reason_chain and alternatives are rendered verbatim — the OSS
 * console does not interpret factor names or scores (decider logic is
 * OSS-deferred).
 */
export interface ReasonStep {
  readonly step: number;
  readonly factor: string;
  readonly value: string | number | boolean;
}

export interface Alternative {
  readonly provider: string;
  readonly model: string;
  readonly rejected_because?: string;
}

export interface Decision {
  readonly id: number;
  readonly decision_id: string;
  readonly tenant_id: string;
  readonly team: string;
  readonly app: string;
  readonly env: string;
  readonly project: string;
  readonly provider_requested: string;
  readonly model_requested: string;
  readonly route_requested: string;
  readonly request_id_hash: string;
  readonly provider_chosen: string;
  readonly model_chosen: string;
  readonly route_chosen: string;
  readonly reason_chain: ReasonStep[];
  readonly alternatives: Alternative[];
  readonly decider_version: string;
  readonly decided_at: string;
  readonly ingested_at: string;
}

export interface DecisionPage {
  readonly decisions: Decision[];
  /** Cursor (smallest row id of this page, DESC order); 0 when the page is empty. */
  readonly next: number;
}

const EMPTY_PAGE: DecisionPage = { decisions: [], next: 0 };

export async function listDecisions(limit = 50, cursor?: number): Promise<DecisionPage> {
  const user = await currentUser();
  const url = new URL(`${BASE}/v1/decisions`);
  url.searchParams.set('tenant', user.tenantId);
  url.searchParams.set('limit', String(limit));
  if (cursor && cursor > 0) {
    url.searchParams.set('cursor', String(cursor));
  }
  try {
    const res = await fetch(url.toString(), {
      headers: { ...tenantHeaders(user) },
      cache: 'no-store',
    });
    if (!res.ok) {
      return EMPTY_PAGE;
    }
    return (await res.json()) as DecisionPage;
  } catch {
    return EMPTY_PAGE;
  }
}
