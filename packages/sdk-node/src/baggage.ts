// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

import { context as otelContext, propagation, type Context } from '@opentelemetry/api';

import {
  BAGGAGE_APP,
  BAGGAGE_ENV,
  BAGGAGE_PROJECT,
  BAGGAGE_TEAM,
  BAGGAGE_TENANT,
} from './semconv.js';

/**
 * Multi-tenant context attached to every LLM call. Every field is optional at
 * the type level so the SDK can be adopted incrementally, but production
 * deployments are expected to populate all five — the policy/audit layer
 * keys off them.
 */
export interface TenantContext {
  tenant?: string;
  team?: string;
  app?: string;
  env?: string;
  project?: string;
}

/**
 * Returns a new OTel context with the supplied tenant fields written onto
 * baggage. Empty/undefined fields are skipped so we never propagate a zero
 * value that downstream services would have to special-case.
 *
 * Use this when you want to scope an entire async region — e.g. an HTTP
 * request handler — to a tenant, without threading parameters through every
 * call:
 *
 * ```ts
 * await otelContext.with(withTenantContext(ctx), async () => {
 *   await openllm.withLlmCall({ provider: 'openai', model: 'gpt-4o-mini' }, ...)
 * })
 * ```
 */
export function withTenantContext(
  ctx: TenantContext,
  parent: Context = otelContext.active(),
): Context {
  const baggage = propagation.getBaggage(parent) ?? propagation.createBaggage();
  const entries: Array<[string, string]> = [];
  if (ctx.tenant) entries.push([BAGGAGE_TENANT, ctx.tenant]);
  if (ctx.team) entries.push([BAGGAGE_TEAM, ctx.team]);
  if (ctx.app) entries.push([BAGGAGE_APP, ctx.app]);
  if (ctx.env) entries.push([BAGGAGE_ENV, ctx.env]);
  if (ctx.project) entries.push([BAGGAGE_PROJECT, ctx.project]);

  let next = baggage;
  for (const [key, value] of entries) {
    next = next.setEntry(key, { value });
  }
  return propagation.setBaggage(parent, next);
}

/**
 * Reads tenant fields out of the supplied context's baggage. Returns an empty
 * object if no baggage is present.
 */
export function getTenantContext(parent: Context = otelContext.active()): TenantContext {
  const baggage = propagation.getBaggage(parent);
  if (!baggage) return {};
  const out: TenantContext = {};
  const tenant = baggage.getEntry(BAGGAGE_TENANT)?.value;
  const team = baggage.getEntry(BAGGAGE_TEAM)?.value;
  const app = baggage.getEntry(BAGGAGE_APP)?.value;
  const env = baggage.getEntry(BAGGAGE_ENV)?.value;
  const project = baggage.getEntry(BAGGAGE_PROJECT)?.value;
  if (tenant) out.tenant = tenant;
  if (team) out.team = team;
  if (app) out.app = app;
  if (env) out.env = env;
  if (project) out.project = project;
  return out;
}
