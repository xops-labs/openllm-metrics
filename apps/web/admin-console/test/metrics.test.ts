import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock the auth layer so the metrics client resolves a deterministic tenant.
vi.mock('@/lib/auth', () => ({
  currentUser: async () => ({ id: 'tester@local', tenantId: 'tenant-xyz' }),
  tenantHeaders: () => ({}),
}));

import { queryRange, queryInstant, seriesLabel, metricsConfigured } from '@/lib/api/metrics';

const origFetch = globalThis.fetch;

describe('metrics query client', () => {
  beforeEach(() => {
    process.env.OLM_METRICS_QUERY_URL = 'http://prom.local';
  });
  afterEach(() => {
    globalThis.fetch = origFetch;
    vi.restoreAllMocks();
    delete process.env.OLM_METRICS_QUERY_URL;
  });

  it('reports configured when the env var is set', () => {
    expect(metricsConfigured()).toBe(true);
  });

  it('substitutes $tenant and parses a matrix range response', async () => {
    const seen: string[] = [];
    globalThis.fetch = vi.fn(async (url: string | URL | Request) => {
      seen.push(String(url));
      return new Response(
        JSON.stringify({
          status: 'success',
          data: {
            resultType: 'matrix',
            result: [
              {
                metric: { provider: 'openai' },
                values: [
                  [1000, '1.5'],
                  [2000, '2.5'],
                ],
              },
            ],
          },
        }),
        { status: 200 },
      );
    }) as unknown as typeof fetch;

    const series = await queryRange({
      query: 'sum by (provider) (rate(llm_cost_usd_total{tenant="$tenant"}[300s]))',
      rangeSeconds: 3600,
      stepSeconds: 300,
    });

    expect(series).toHaveLength(1);
    expect(series[0]?.metric.provider).toBe('openai');
    expect(series[0]?.values).toEqual([
      [1000, 1.5],
      [2000, 2.5],
    ]);
    expect(seen[0]).toContain('tenant%3D%22tenant-xyz%22');
    expect(seen[0]).toContain('/api/v1/query_range');
  });

  it('parses an instant vector response', async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            status: 'success',
            data: {
              resultType: 'vector',
              result: [{ metric: { provider: 'anthropic' }, value: [1000, '0.02'] }],
            },
          }),
          { status: 200 },
        ),
    ) as unknown as typeof fetch;

    const samples = await queryInstant(
      'sum by (provider) (rate(llm_errors_total{tenant="$tenant"}[1h]))',
    );
    expect(samples).toEqual([{ metric: { provider: 'anthropic' }, value: 0.02 }]);
  });

  it('returns [] when the TSDB is unreachable', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('ECONNREFUSED');
    }) as unknown as typeof fetch;

    const series = await queryRange({ query: 'up', rangeSeconds: 60, stepSeconds: 15 });
    expect(series).toEqual([]);
  });

  it('builds a readable series label from metric labels', () => {
    expect(seriesLabel({ provider: 'openai', model: 'gpt-4o' }, ['provider', 'model'])).toBe(
      'openai / gpt-4o',
    );
    expect(seriesLabel({}, ['provider'])).toBe('(unlabeled)');
  });
});
