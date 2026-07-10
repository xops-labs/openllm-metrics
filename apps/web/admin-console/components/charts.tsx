import { ReactNode } from 'react';

/**
 * Dependency-free SVG charts for the native analytics screens (F038).
 *
 * The admin console deliberately avoids a heavy charting runtime — it is an
 * internal operational tool with a minimal neutral palette, not a marketing
 * surface. These server-rendered SVGs are enough to plot the raw normalized
 * telemetry the analytics screens consume (cost over time, tokens by team,
 * error rate by provider, reconciliation drift). No client JS is required.
 *
 * Colors come from the Tailwind theme tokens defined in tailwind.config.ts.
 */

// A small fixed palette drawn from the theme. Kept deterministic so the same
// series always renders the same color across reloads.
const SERIES_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#a855f7', '#06b6d4'] as const;

export function seriesColor(index: number): string {
  return SERIES_COLORS[index % SERIES_COLORS.length] as string;
}

export interface LineSeries {
  readonly label: string;
  /** [unixSeconds, value] pairs, ascending by time. */
  readonly points: ReadonlyArray<readonly [number, number]>;
}

interface LineChartProps {
  readonly series: ReadonlyArray<LineSeries>;
  readonly height?: number;
  /** Formats a y-axis value for the legend / tooltip-less labels. */
  readonly formatValue?: (v: number) => string;
  /** Optional unit label rendered after the max value. */
  readonly unit?: string;
}

const PAD = { top: 8, right: 8, bottom: 20, left: 48 };
const WIDTH = 720;

function niceMax(max: number): number {
  if (max <= 0) return 1;
  const pow = Math.pow(10, Math.floor(Math.log10(max)));
  const scaled = max / pow;
  const step = scaled <= 1 ? 1 : scaled <= 2 ? 2 : scaled <= 5 ? 5 : 10;
  return step * pow;
}

/**
 * Multi-series time-series line chart. All series must share the same time
 * domain (they do, because they come from a single query_range step).
 */
export function LineChart({ series, height = 240, formatValue, unit }: LineChartProps) {
  const fmt = formatValue ?? ((v: number) => v.toLocaleString());
  const allPoints = series.flatMap((s) => s.points);
  if (allPoints.length === 0) {
    return <ChartEmpty />;
  }

  const xs = allPoints.map(([t]) => t);
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const maxYraw = Math.max(...allPoints.map(([, v]) => v), 0);
  const maxY = niceMax(maxYraw);

  const plotW = WIDTH - PAD.left - PAD.right;
  const plotH = height - PAD.top - PAD.bottom;

  const xScale = (t: number) =>
    maxX === minX ? PAD.left : PAD.left + ((t - minX) / (maxX - minX)) * plotW;
  const yScale = (v: number) => PAD.top + plotH - (maxY === 0 ? 0 : (v / maxY) * plotH);

  const yTicks = [0, 0.25, 0.5, 0.75, 1].map((f) => f * maxY);

  return (
    <figure className="rounded border border-border bg-panel p-3">
      <svg
        viewBox={`0 0 ${WIDTH} ${height}`}
        role="img"
        aria-label="time series line chart"
        className="w-full"
        preserveAspectRatio="none"
      >
        {/* Gridlines + y labels */}
        {yTicks.map((v, i) => {
          const y = yScale(v);
          return (
            <g key={i}>
              <line
                x1={PAD.left}
                x2={WIDTH - PAD.right}
                y1={y}
                y2={y}
                stroke="#262a33"
                strokeWidth={1}
              />
              <text x={PAD.left - 6} y={y + 3} textAnchor="end" fontSize={9} fill="#9ca3af">
                {fmt(v)}
              </text>
            </g>
          );
        })}
        {/* Series polylines */}
        {series.map((s, i) => {
          const d = s.points.map(([t, v]) => `${xScale(t)},${yScale(v)}`).join(' ');
          return (
            <polyline
              key={s.label}
              points={d}
              fill="none"
              stroke={seriesColor(i)}
              strokeWidth={1.5}
              strokeLinejoin="round"
            />
          );
        })}
      </svg>
      <ChartLegend
        items={series.map((s, i) => ({ label: s.label, color: seriesColor(i) }))}
        trailing={unit ? `peak ~ ${fmt(maxYraw)} ${unit}` : undefined}
      />
    </figure>
  );
}

export interface BarDatum {
  readonly label: string;
  readonly value: number;
}

interface BarChartProps {
  readonly data: ReadonlyArray<BarDatum>;
  readonly formatValue?: (v: number) => string;
  readonly colorIndex?: (datum: BarDatum, i: number) => number;
}

/** Horizontal bar chart — used for "tokens by team" and "error rate by provider". */
export function BarChart({ data, formatValue, colorIndex }: BarChartProps) {
  const fmt = formatValue ?? ((v: number) => v.toLocaleString());
  if (data.length === 0) {
    return <ChartEmpty />;
  }
  const max = Math.max(...data.map((d) => d.value), 0) || 1;
  return (
    <div className="space-y-2 rounded border border-border bg-panel p-3">
      {data.map((d, i) => {
        const pct = Math.max(2, (d.value / max) * 100);
        const color = seriesColor(colorIndex ? colorIndex(d, i) : i);
        return (
          <div key={d.label} className="grid grid-cols-[10rem_1fr_auto] items-center gap-2">
            <span className="truncate font-mono text-xs text-muted" title={d.label}>
              {d.label}
            </span>
            <div className="h-3 rounded bg-canvas">
              <div className="h-3 rounded" style={{ width: `${pct}%`, backgroundColor: color }} />
            </div>
            <span className="text-right font-mono text-xs text-text">{fmt(d.value)}</span>
          </div>
        );
      })}
    </div>
  );
}

function ChartLegend({
  items,
  trailing,
}: {
  items: ReadonlyArray<{ label: string; color: string }>;
  trailing?: string | undefined;
}) {
  return (
    <figcaption className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted">
      {items.map((it) => (
        <span key={it.label} className="inline-flex items-center gap-1">
          <span className="inline-block h-2 w-2 rounded-sm" style={{ backgroundColor: it.color }} />
          <span className="font-mono">{it.label}</span>
        </span>
      ))}
      {trailing ? <span className="ml-auto">{trailing}</span> : null}
    </figcaption>
  );
}

function ChartEmpty(): ReactNode {
  return (
    <div className="flex h-32 items-center justify-center rounded border border-border bg-panel text-xs text-muted">
      No samples in the selected window.
    </div>
  );
}
