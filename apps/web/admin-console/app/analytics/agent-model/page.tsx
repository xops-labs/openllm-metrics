import { PageHeader } from '@/components/page-header';
import { Table, Column } from '@/components/table';
import { metricsConfigured } from '@/lib/api/metrics';
import {
  MetricsNotConfigured,
  NoData,
  RangeTabs,
  resolveRange,
  formatCompact,
  AgentModelRow,
  fetchAgentModelRows,
} from '../shared';

interface Props {
  readonly searchParams: Promise<{ range?: string }>;
}

/**
 * Agent x model (F038). The headline breakdown: input / output / total tokens
 * for every (app, model) pair the active tenant produced in the window. Three
 * instant queries (input, output, total tokens via increase()) are joined in
 * TS into one row per (app, model) pair. Raw normalized telemetry only.
 */
export default async function AgentModelPage({ searchParams }: Props) {
  if (!metricsConfigured()) {
    return (
      <>
        <PageHeader title="Agent × model" description="Token breakdown per agent and model." />
        <MetricsNotConfigured />
      </>
    );
  }

  const { range } = await searchParams;
  const preset = resolveRange(range);

  const tableRows = await fetchAgentModelRows(preset.rangeSeconds);

  const columns: ReadonlyArray<Column<AgentModelRow>> = [
    {
      key: 'app',
      header: 'Agent',
      render: (r) => <span className="font-mono text-xs">{r.app}</span>,
    },
    {
      key: 'model',
      header: 'Model',
      render: (r) => <span className="font-mono text-xs">{r.model}</span>,
    },
    {
      key: 'input',
      header: 'Input',
      className: 'text-right font-mono',
      render: (r) => formatCompact(r.input),
    },
    {
      key: 'output',
      header: 'Output',
      className: 'text-right font-mono',
      render: (r) => formatCompact(r.output),
    },
    {
      key: 'total',
      header: 'Total',
      className: 'text-right font-mono',
      render: (r) => formatCompact(r.total),
    },
  ];

  return (
    <>
      <PageHeader
        title="Agent × model"
        description="Input, output, and total tokens for every agent × model pair in the selected window. Source: llm_input_tokens_total, llm_output_tokens_total, llm_total_tokens_total."
        actions={<RangeTabs basePath="/analytics/agent-model" active={preset.key} />}
      />
      <Table
        columns={columns}
        rows={tableRows}
        rowKey={(r) => r.key}
        empty={
          <NoData hint="No token samples carrying app/model labels for this tenant in the selected window." />
        }
      />
    </>
  );
}
