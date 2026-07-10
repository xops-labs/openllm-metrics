/**
 * Smoke tests for @openllm/metrics Node.js SDK.
 *
 * Uses Node.js built-in `node:test` + `node:assert` — no extra test runner
 * dependency needed. Runs against the compiled ESM output in dist/esm/.
 *
 * OTel providers are set up with in-memory exporters so no live OTLP
 * collector is required during testing.
 */

import { describe, it, before, after, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

import {
  metrics as otelMetrics,
  trace as otelTrace,
  SpanStatusCode,
} from '@opentelemetry/api';
import {
  BasicTracerProvider,
  InMemorySpanExporter,
  SimpleSpanProcessor,
} from '@opentelemetry/sdk-trace-node';
import {
  MeterProvider,
  PeriodicExportingMetricReader,
  InMemoryMetricExporter,
  AggregationTemporality,
} from '@opentelemetry/sdk-metrics';

// ---------------------------------------------------------------------------
// In-memory OTel providers
// ---------------------------------------------------------------------------

const spanExporter = new InMemorySpanExporter();
const tracerProvider = new BasicTracerProvider();
tracerProvider.addSpanProcessor(new SimpleSpanProcessor(spanExporter));

const metricExporter = new InMemoryMetricExporter(AggregationTemporality.CUMULATIVE);
const meterProvider = new MeterProvider({
  readers: [
    new PeriodicExportingMetricReader({
      exporter: metricExporter,
      exportIntervalMillis: 100,
    }),
  ],
});

// Set global providers BEFORE importing the SDK so getSdk() picks them up.
otelTrace.setGlobalTracerProvider(tracerProvider);
otelMetrics.setGlobalMeterProvider(meterProvider);

// ---------------------------------------------------------------------------
// SDK import (after providers are set)
// ---------------------------------------------------------------------------

const { init, withLlmCall, startLlmCall, withTenantContext, getTenantContext } =
  await import('../dist/esm/index.js');

// Boot the SDK without re-bootstrapping OTel (we already set global providers).
await init({ serviceName: 'smoke-test', bootstrapOtel: false });

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getSpans() {
  return spanExporter.getFinishedSpans();
}

function clearSpans() {
  spanExporter.reset();
}

async function collectMetricNames() {
  // Force flush so periodic reader exports any pending data.
  await meterProvider.forceFlush();
  const metrics = metricExporter.getMetrics();
  const names = new Set();
  for (const rm of metrics) {
    for (const sm of rm.scopeMetrics ?? []) {
      for (const m of sm.metrics ?? []) {
        names.add(m.descriptor.name);
      }
    }
  }
  return names;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('@openllm/metrics smoke tests', { concurrency: false }, () => {
  beforeEach(() => {
    clearSpans();
    metricExporter.reset();
  });

  after(async () => {
    await tracerProvider.shutdown();
    await meterProvider.shutdown();
  });

  it('withLlmCall emits a span that includes provider and model in the name', async () => {
    await withLlmCall(
      {
        provider: 'openai',
        model: 'gpt-4o-mini',
        tenant: 'acme',
        team: 'platform',
        app: 'smoke',
        env: 'test',
        project: 'openllm-test',
      },
      async (op) => {
        op.setPromptTokens(10).setCompletionTokens(25);
      },
    );

    const spans = getSpans();
    assert.ok(spans.length >= 1, `expected ≥1 span, got ${spans.length}`);
    const span = spans[spans.length - 1];
    assert.ok(
      span.name.includes('openai') && span.name.includes('gpt-4o-mini'),
      `unexpected span name: "${span.name}"`,
    );
  });

  it('withLlmCall emits a span whose name encodes provider and model', async () => {
    await withLlmCall(
      { provider: 'anthropic', model: 'claude-3-haiku', tenant: 'acme' },
      async () => {},
    );

    const spans = getSpans();
    assert.ok(spans.length >= 1, `expected ≥1 span, got ${spans.length}`);
    const span = spans[spans.length - 1];
    // Span name is "<operation> <provider>/<model>" — verify it encodes both.
    assert.ok(
      span.name.includes('anthropic') && span.name.includes('claude-3-haiku'),
      `unexpected span name: "${span.name}"`,
    );
  });

  it('withLlmCall sets span status OK on success', async () => {
    await withLlmCall({ provider: 'openai', model: 'gpt-4o-mini' }, async () => {});

    const spans = getSpans();
    assert.ok(spans.length >= 1);
    assert.equal(spans[spans.length - 1].status.code, SpanStatusCode.OK);
  });

  it('withLlmCall records error kind and ERROR span status on rejection', async () => {
    await assert.rejects(
      () =>
        withLlmCall({ provider: 'openai', model: 'gpt-4o-mini' }, async () => {
          throw new TypeError('simulated error');
        }),
      TypeError,
    );

    const spans = getSpans();
    assert.ok(spans.length >= 1);
    const span = spans[spans.length - 1];
    assert.equal(span.status.code, SpanStatusCode.ERROR);
    assert.equal(span.attributes['error.type'], 'TypeError');
  });

  it('startLlmCall end() is idempotent', () => {
    const scope = startLlmCall({ provider: 'openai', model: 'gpt-4o-mini', tenant: 'acme' });
    scope.setPromptTokens(5).setCompletionTokens(10);
    scope.end();
    scope.end(); // second call must be a no-op

    const spans = getSpans();
    assert.equal(spans.length, 1, `expected 1 span, got ${spans.length}`);
  });

  it('withTenantContext writes tenant fields to OTel baggage', () => {
    const ctx = withTenantContext({
      tenant: 'acme',
      team: 'sre',
      app: 'gateway',
      env: 'prod',
      project: 'main',
    });
    const got = getTenantContext(ctx);
    assert.equal(got.tenant, 'acme');
    assert.equal(got.team, 'sre');
    assert.equal(got.app, 'gateway');
    assert.equal(got.env, 'prod');
    assert.equal(got.project, 'main');
  });

  it('all four metric instruments are emitted after withLlmCall', async () => {
    await withLlmCall(
      {
        provider: 'openai',
        model: 'gpt-4o-mini',
        tenant: 'acme',
        team: 'platform',
        app: 'metrics-test',
        env: 'test',
        project: 'openllm-test',
      },
      async (op) => {
        op.setPromptTokens(42).setCompletionTokens(128).setUsageDollars(0.001);
      },
    );

    const names = await collectMetricNames();
    const want = [
      'gen_ai.client.operation.duration',
      'gen_ai.client.token.usage',
      'llm_requests_total',
      'llm_usage_dollars_total',
    ];
    for (const name of want) {
      assert.ok(names.has(name), `missing metric "${name}"; found: [${[...names].join(', ')}]`);
    }
  });
});
