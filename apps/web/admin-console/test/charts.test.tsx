import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { LineChart, BarChart, seriesColor } from '@/components/charts';

describe('charts', () => {
  it('renders a line chart with a legend entry per series', () => {
    render(
      <LineChart
        series={[
          {
            label: 'openai',
            points: [
              [1000, 1],
              [2000, 3],
              [3000, 2],
            ],
          },
          {
            label: 'anthropic',
            points: [
              [1000, 0.5],
              [2000, 1],
              [3000, 4],
            ],
          },
        ]}
      />,
    );
    expect(screen.getByText('openai')).toBeInTheDocument();
    expect(screen.getByText('anthropic')).toBeInTheDocument();
    expect(screen.getByRole('img', { name: /line chart/i })).toBeInTheDocument();
  });

  it('shows an empty state when no points are provided', () => {
    render(<LineChart series={[]} />);
    expect(screen.getByText(/no samples/i)).toBeInTheDocument();
  });

  it('renders bars sorted as given with formatted values', () => {
    render(
      <BarChart
        data={[
          { label: 'platform', value: 1200 },
          { label: 'research', value: 300 },
        ]}
        formatValue={(v) => `${v}t`}
      />,
    );
    expect(screen.getByText('platform')).toBeInTheDocument();
    expect(screen.getByText('1200t')).toBeInTheDocument();
    expect(screen.getByText('300t')).toBeInTheDocument();
  });

  it('cycles a deterministic palette', () => {
    expect(seriesColor(0)).toBe(seriesColor(6));
    expect(seriesColor(0)).not.toBe(seriesColor(1));
  });
});
