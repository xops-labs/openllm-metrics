// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

import {
  context as otelContext,
  SpanStatusCode,
  type Attributes,
  type Span,
} from '@opentelemetry/api';

import { getTenantContext, withTenantContext, type TenantContext } from './baggage.js';
import { getSdk, type GenAiInstruments, type OpenLlmSdk } from './init.js';
import {
  GEN_AI_ERROR_TYPE,
  GEN_AI_OPERATION_NAME,
  GEN_AI_REQUEST_MODEL,
  GEN_AI_RESPONSE_MODEL,
  GEN_AI_SERVER_ADDRESS,
  GEN_AI_SYSTEM,
  GEN_AI_TOKEN_TYPE,
  LLM_APP,
  LLM_ENV,
  LLM_PROJECT,
  LLM_ROUTE,
  LLM_TEAM,
  LLM_TENANT,
  OPERATION_CHAT,
  TOKEN_TYPE_INPUT,
  TOKEN_TYPE_OUTPUT,
} from './semconv.js';

/**
 * Options describing a single LLM call. All fields except `provider` and
 * `model` are optional, but production deployments are expected to populate
 * the full multi-tenant set so the policy/audit/scoring layer has rich
 * labels to key off.
 *
 * Never include prompts, completions, or any user-data field in this struct.
 * The SDK does not look at custom fields, but the type intentionally has no
 * `prompt`/`messages` slot to make accidental capture impossible.
 */
export interface LlmCallOptions extends TenantContext {
  provider: string;
  model: string;
  /**
   * Operation kind (`chat`, `embedding`, `completion`, ...). Defaults to
   * `chat` to match the dominant call shape and the OTel GenAI spec's
   * recommended value.
   */
  operation?: string;
  /**
   * Logical route label — e.g. `chat-primary`, `chat-fallback`. Mirrors the
   * Phase D gateway's per-route series and lets dashboards stack runtime vs.
   * exporter-side cost when both modes are deployed.
   */
  route?: string;
  /** Provider endpoint host (e.g. `api.openai.com`). */
  serverAddress?: string;
}

/**
 * Live handle for a single in-flight LLM call. Returned by
 * {@link startLlmCall}; auto-ended inside {@link withLlmCall}.
 *
 * The `set*` mutators are intentionally permissive (`null` clears, undefined
 * is a no-op) so callers can build the scope incrementally as they receive
 * data from the provider — particularly important for streaming responses
 * where token counts only arrive in the final chunk.
 */
export class LlmCallScope {
  private readonly start: bigint;
  private readonly span: Span;
  private readonly instruments: GenAiInstruments;
  private readonly opts: LlmCallOptions;
  private readonly operation: string;

  private promptTokens: number | undefined;
  private completionTokens: number | undefined;
  private responseModel: string | undefined;
  private errorKind: string | null = null;
  private usageDollars: number | null = null;
  private ended = false;

  constructor(span: Span, instruments: GenAiInstruments, opts: LlmCallOptions) {
    this.span = span;
    this.instruments = instruments;
    this.opts = opts;
    this.operation = opts.operation ?? OPERATION_CHAT;
    this.start = process.hrtime.bigint();
  }

  /** Underlying OTel span. Exposed so callers can attach extra non-payload attributes. */
  get otelSpan(): Span {
    return this.span;
  }

  setPromptTokens(n: number): this {
    if (Number.isFinite(n) && n >= 0) this.promptTokens = Math.trunc(n);
    return this;
  }

  setCompletionTokens(n: number): this {
    if (Number.isFinite(n) && n >= 0) this.completionTokens = Math.trunc(n);
    return this;
  }

  setResponseModel(model: string | null | undefined): this {
    if (model) this.responseModel = model;
    return this;
  }

  setErrorKind(kind: string | null): this {
    this.errorKind = kind && kind.length > 0 ? kind : null;
    return this;
  }

  setUsageDollars(amount: number | null): this {
    if (amount === null) {
      this.usageDollars = null;
    } else if (Number.isFinite(amount) && amount >= 0) {
      this.usageDollars = amount;
    }
    return this;
  }

  /**
   * Ends the call: emits the OTel duration histogram, token counters, and
   * the `llm_requests_total` counter, then ends the span. Idempotent — a
   * second call is a no-op so `withLlmCall`'s auto-end is safe alongside an
   * explicit `op.end()` in user code.
   */
  end(): void {
    if (this.ended) return;
    this.ended = true;

    const durationNs = process.hrtime.bigint() - this.start;
    const durationSeconds = Number(durationNs) / 1e9;

    const baseAttrs = this.buildBaseAttributes();

    this.instruments.clientOperationDuration.record(durationSeconds, baseAttrs);

    if (this.promptTokens !== undefined) {
      this.instruments.clientTokenUsage.add(this.promptTokens, {
        ...baseAttrs,
        [GEN_AI_TOKEN_TYPE]: TOKEN_TYPE_INPUT,
      });
    }
    if (this.completionTokens !== undefined) {
      this.instruments.clientTokenUsage.add(this.completionTokens, {
        ...baseAttrs,
        [GEN_AI_TOKEN_TYPE]: TOKEN_TYPE_OUTPUT,
      });
    }

    this.instruments.requestsTotal.add(1, {
      ...baseAttrs,
      error_kind: this.errorKind ?? '',
    });

    if (this.usageDollars !== null && this.usageDollars > 0) {
      this.instruments.usageDollarsTotal.add(this.usageDollars, baseAttrs);
    }

    if (this.errorKind) {
      this.span.setAttribute(GEN_AI_ERROR_TYPE, this.errorKind);
      this.span.setStatus({ code: SpanStatusCode.ERROR, message: this.errorKind });
    } else {
      this.span.setStatus({ code: SpanStatusCode.OK });
    }
    this.span.end();
  }

  private buildBaseAttributes(): Attributes {
    const attrs: Attributes = {
      [GEN_AI_SYSTEM]: this.opts.provider,
      [GEN_AI_REQUEST_MODEL]: this.opts.model,
      [GEN_AI_OPERATION_NAME]: this.operation,
    };
    if (this.responseModel) attrs[GEN_AI_RESPONSE_MODEL] = this.responseModel;
    if (this.opts.serverAddress) attrs[GEN_AI_SERVER_ADDRESS] = this.opts.serverAddress;
    if (this.opts.route) attrs[LLM_ROUTE] = this.opts.route;

    // Tenant context: explicit per-call fields win; missing fields fall back
    // to baggage on the active OTel context, then to the SDK's defaultTags.
    const sdk = getSdk();
    const fromBaggage = getTenantContext();
    const tenant = this.opts.tenant ?? fromBaggage.tenant ?? sdk.defaultTags.tenant;
    const team = this.opts.team ?? fromBaggage.team ?? sdk.defaultTags.team;
    const app = this.opts.app ?? fromBaggage.app ?? sdk.defaultTags.app;
    const env = this.opts.env ?? fromBaggage.env ?? sdk.defaultTags.env;
    const project = this.opts.project ?? fromBaggage.project ?? sdk.defaultTags.project;

    if (tenant) attrs[LLM_TENANT] = tenant;
    if (team) attrs[LLM_TEAM] = team;
    if (app) attrs[LLM_APP] = app;
    if (env) attrs[LLM_ENV] = env;
    if (project) attrs[LLM_PROJECT] = project;

    return attrs;
  }
}

/**
 * Starts an LLM call and returns a {@link LlmCallScope}. The caller is
 * responsible for invoking `scope.end()` exactly once. For most code paths
 * prefer {@link withLlmCall}, which guarantees end-on-throw.
 */
export function startLlmCall(opts: LlmCallOptions, sdk: OpenLlmSdk = getSdk()): LlmCallScope {
  const operation = opts.operation ?? OPERATION_CHAT;
  const spanName = `${operation} ${opts.provider}/${opts.model}`;

  // Activate baggage for the duration of the call so downstream HTTP
  // clients that honour OTel propagation pick up the tenant fields without
  // any extra wiring.
  const tenantForBaggage: TenantContext = {};
  const resolvedTenant = opts.tenant ?? sdk.defaultTags.tenant;
  const resolvedTeam = opts.team ?? sdk.defaultTags.team;
  const resolvedApp = opts.app ?? sdk.defaultTags.app;
  const resolvedEnv = opts.env ?? sdk.defaultTags.env;
  const resolvedProject = opts.project ?? sdk.defaultTags.project;
  if (resolvedTenant) tenantForBaggage.tenant = resolvedTenant;
  if (resolvedTeam) tenantForBaggage.team = resolvedTeam;
  if (resolvedApp) tenantForBaggage.app = resolvedApp;
  if (resolvedEnv) tenantForBaggage.env = resolvedEnv;
  if (resolvedProject) tenantForBaggage.project = resolvedProject;
  const ctxWithBaggage = withTenantContext(tenantForBaggage);
  const span = sdk.tracer.startSpan(spanName, undefined, ctxWithBaggage);
  return new LlmCallScope(span, sdk.instruments, opts);
}

/**
 * Async-friendly twin of {@link startLlmCall}: runs `fn(scope)`, ensures
 * `scope.end()` is called exactly once, and records the thrown error's
 * constructor name as the `error_kind` if `fn` rejects. Re-throws the
 * original error after recording.
 */
export async function withLlmCall<T>(
  opts: LlmCallOptions,
  fn: (scope: LlmCallScope) => Promise<T>,
  sdk: OpenLlmSdk = getSdk(),
): Promise<T> {
  const scope = startLlmCall(opts, sdk);
  const withLlmCtx: TenantContext = {};
  if (opts.tenant) withLlmCtx.tenant = opts.tenant;
  if (opts.team) withLlmCtx.team = opts.team;
  if (opts.app) withLlmCtx.app = opts.app;
  if (opts.env) withLlmCtx.env = opts.env;
  if (opts.project) withLlmCtx.project = opts.project;
  const ctx = withTenantContext(withLlmCtx);
  try {
    return await otelContext.with(ctx, () => fn(scope));
  } catch (err) {
    scope.setErrorKind(classifyError(err));
    throw err;
  } finally {
    scope.end();
  }
}

function classifyError(err: unknown): string {
  if (err instanceof Error) {
    return err.name && err.name !== 'Error' ? err.name : 'error';
  }
  return 'error';
}
