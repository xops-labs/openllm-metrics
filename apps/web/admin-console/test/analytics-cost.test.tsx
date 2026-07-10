import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';

vi.mock('@/lib/auth', () => ({
  currentUser: async () => ({ id: 'tester@local', tenantId: 'tenant-xyz' }),
  tenantHeaders: () => ({}),
}));

import CostPage from '@/app/analytics/cost/page';

const origFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = origFetch;
  vi.restoreAllMocks();
  delete process.env.OLM_METRICS_QUERY_URL;
});

describe('analytics: cost over time screen', () => {
  it('renders the not-configured empty state when OLM_METRICS_QUERY_URL is unset', async () => {
    delete process.env.OLM_METRICS_QUERY_URL;
    const ui = await CostPage({ searchParams: Promise.resolve({}) });
    render(ui);
    expect(screen.getByText(/OLM_METRICS_QUERY_URL not configured/i)).toBeInTheDocument();
  });

  it('renders a line chart from query_range data when configured', async () => {
    process.env.OLM_METRICS_QUERY_URL = 'http://prom.local';
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            status: 'success',
            data: {
              resultType: 'matrix',
              result: [
                {
                  metric: { provider: 'openai' },
                  values: [
                    [1000, '1.0'],
                    [2000, '2.0'],
                  ],
                },
              ],
            },
          }),
          { status: 200 },
        ),
    ) as unknown as typeof fetch;

    const ui = await CostPage({ searchParams: Promise.resolve({ range: '24h' }) });
    render(ui);
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /line chart/i })).toBeInTheDocument();
  });
});
