// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

import {
  metrics,
  trace,
  type Counter,
  type Histogram,
  type Meter,
  type Tracer,
} from '@opentelemetry/api';

import type { TenantContext } from './baggage.js';
import {
  METRIC_CLIENT_OPERATION_DURATION,
  METRIC_CLIENT_TOKEN_USAGE,
  METRIC_LLM_REQUESTS_TOTAL,
  METRIC_LLM_USAGE_DOLLARS_TOTAL,
} from './semconv.js';

/**
 * Options accepted by {@link init}. The shape is intentionally minimal — the
 * underlying OpenTelemetry SDK accepts a much larger surface, but for the
 * 95% case (boot an OTel pipeline that ships GenAI metrics to a collector)
 * these five fields are enough.
 *
 * If `bootstrapOtel` is `false` (or `init` is never called) the SDK still
 * works as long as the host has already configured a global OTel SDK — we
 * always resolve the meter and tracer through the OTel API, never through a
 * captured SDK instance.
 */
export interface InitOptions {
  /** Logical service name, used for the OTel `service.name` resource. */
  serviceName: string;
  /**
   * OTLP/HTTP base URL of the destination collector — e.g.
   * `http://otel-collector:4318`. Trace + metrics signals append their own
   * `/v1/traces` and `/v1/metrics` paths.
   *
   * When omitted, the SDK assumes the host has wired up an OTel exporter
   * already (or set `OTEL_EXPORTER_OTLP_ENDPOINT` in the environment).
   */
  exporterEndpoint?: string;
  /** Default tenant fields applied to every operation that doesn't override. */
  defaultTags?: TenantContext;
  /**
   * When `true` (default) the SDK boots a NodeSDK with OTLP exporters. Set
   * to `false` if the host application already starts its own OTel pipeline
   * and you only want @openllm/metrics to record instruments against the
   * existing global providers.
   */
  bootstrapOtel?: boolean;
  /** Optional meter/tracer instrumentation scope. Defaults to the SDK name. */
  instrumentationScope?: string;
}

const DEFAULT_INSTRUMENTATION_SCOPE = '@openllm/metrics';

/**
 * Resolved SDK handle returned by {@link init} and consumed by
 * {@link startLlmCall} / {@link withLlmCall}. Holding the handle is optional
 * — once `init` has been called, subsequent calls resolve through the global
 * OTel API.
 */
export interface OpenLlmSdk {
  readonly serviceName: string;
  readonly defaultTags: TenantContext;
  readonly meter: Meter;
  readonly tracer: Tracer;
  readonly instruments: GenAiInstruments;
  /** Tears down the OTel SDK if {@link init} bootstrapped one. */
  shutdown(): Promise<void>;
}

/** OTel GenAI + project-specific instruments shared across LLM operations. */
export interface GenAiInstruments {
  clientOperationDuration: Histogram;
  clientTokenUsage: Counter;
  requestsTotal: Counter;
  usageDollarsTotal: Counter;
}

let activeSdk: OpenLlmSdk | undefined;
let bootstrappedShutdown: (() => Promise<void>) | undefined;

/**
 * Initializes the OpenLLM Metrics SDK. Idempotent: subsequent calls return
 * the same handle and do not re-bootstrap OTel.
 *
 * The SDK never collects prompt or completion text. Only OTel GenAI semconv
 * keys (provider, model, operation, token counts) and the project-specific
 * `llm_*` extension keys (tenant, team, app, env, project, route, error
 * kind) flow through the exporter.
 */
export async function init(options: InitOptions): Promise<OpenLlmSdk> {
  if (activeSdk) {
    return activeSdk;
  }

  const bootstrap = options.bootstrapOtel ?? true;
  if (bootstrap) {
    bootstrappedShutdown = await bootstrapOtelSdk(options);
  }

  const scope = options.instrumentationScope ?? DEFAULT_INSTRUMENTATION_SCOPE;
  const meter = metrics.getMeter(scope);
  const tracer = trace.getTracer(scope);
  const instruments = createInstruments(meter);

  activeSdk = {
    serviceName: options.serviceName,
    defaultTags: { ...(options.defaultTags ?? {}) },
    meter,
    tracer,
    instruments,
    async shutdown() {
      if (bootstrappedShutdown) {
        const fn = bootstrappedShutdown;
        bootstrappedShutdown = undefined;
        await fn();
      }
      activeSdk = undefined;
    },
  };
  return activeSdk;
}

/**
 * Returns the SDK handle initialized by {@link init}. If `init` has not been
 * called, this lazily constructs a handle bound to the global OTel API
 * providers — useful when the host application owns OTel setup.
 */
export function getSdk(): OpenLlmSdk {
  if (activeSdk) return activeSdk;
  const scope = DEFAULT_INSTRUMENTATION_SCOPE;
  const meter = metrics.getMeter(scope);
  const tracer = trace.getTracer(scope);
  activeSdk = {
    serviceName: 'openllm-metrics',
    defaultTags: {},
    meter,
    tracer,
    instruments: createInstruments(meter),
    async shutdown() {
      activeSdk = undefined;
    },
  };
  return activeSdk;
}

function createInstruments(meter: Meter): GenAiInstruments {
  return {
    clientOperationDuration: meter.createHistogram(METRIC_CLIENT_OPERATION_DURATION, {
      description: 'LLM client operation duration',
      unit: 's',
    }),
    clientTokenUsage: meter.createCounter(METRIC_CLIENT_TOKEN_USAGE, {
      description: 'LLM token usage per request, split by gen_ai.token.type',
      unit: '{token}',
    }),
    requestsTotal: meter.createCounter(METRIC_LLM_REQUESTS_TOTAL, {
      description:
        'Total LLM requests, labelled by provider/model/route/tenant/team/app/env/project/error_kind',
      unit: '{request}',
    }),
    usageDollarsTotal: meter.createCounter(METRIC_LLM_USAGE_DOLLARS_TOTAL, {
      description:
        'Total estimated USD spend on LLM calls (runtime estimate; reconciled cost comes from the FOCUS ingester)',
      unit: 'USD',
    }),
  };
}

/**
 * Boots a NodeSDK with OTLP/HTTP exporters for traces and metrics. Imported
 * lazily so consumers who only call {@link getSdk} against an existing
 * pipeline do not pay the cost of pulling in `@opentelemetry/sdk-node`.
 */
async function bootstrapOtelSdk(options: InitOptions): Promise<() => Promise<void>> {
  const [
    { NodeSDK },
    { OTLPTraceExporter },
    { OTLPMetricExporter },
    { PeriodicExportingMetricReader },
    { Resource },
  ] = await Promise.all([
    import('@opentelemetry/sdk-node'),
    import('@opentelemetry/exporter-trace-otlp-http'),
    import('@opentelemetry/exporter-metrics-otlp-http'),
    import('@opentelemetry/sdk-metrics'),
    import('@opentelemetry/resources'),
  ]);

  const traceUrl = options.exporterEndpoint
    ? joinUrl(options.exporterEndpoint, '/v1/traces')
    : undefined;
  const metricsUrl = options.exporterEndpoint
    ? joinUrl(options.exporterEndpoint, '/v1/metrics')
    : undefined;

  const resource = new Resource({
    'service.name': options.serviceName,
  });

  const sdk = new NodeSDK({
    resource,
    traceExporter: new OTLPTraceExporter(traceUrl ? { url: traceUrl } : {}),
    metricReader: new PeriodicExportingMetricReader({
      exporter: new OTLPMetricExporter(metricsUrl ? { url: metricsUrl } : {}),
    }),
  });

  sdk.start();
  return async () => {
    await sdk.shutdown();
  };
}

function joinUrl(base: string, path: string): string {
  const trimmed = base.endsWith('/') ? base.slice(0, -1) : base;
  return `${trimmed}${path}`;
}
