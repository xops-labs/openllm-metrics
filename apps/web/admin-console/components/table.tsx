import { ReactNode } from 'react';

export interface Column<T> {
  readonly key: string;
  readonly header: string;
  readonly render: (row: T) => ReactNode;
  readonly className?: string;
}

interface Props<T> {
  readonly columns: ReadonlyArray<Column<T>>;
  readonly rows: ReadonlyArray<T>;
  readonly rowKey: (row: T) => string;
  readonly empty?: ReactNode;
}

export function Table<T>({ columns, rows, rowKey, empty }: Props<T>) {
  if (rows.length === 0) {
    return (
      <div className="rounded border border-border bg-panel p-6 text-sm text-muted">
        {empty ?? 'No rows.'}
      </div>
    );
  }
  return (
    <div className="overflow-x-auto rounded border border-border">
      <table className="w-full border-collapse text-sm">
        <thead className="bg-panel text-left text-xs uppercase tracking-wider text-muted">
          <tr>
            {columns.map((c) => (
              <th
                key={c.key}
                className={`border-b border-border px-3 py-2 font-semibold ${c.className ?? ''}`}
              >
                {c.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={rowKey(row)} className="border-b border-border last:border-0">
              {columns.map((c) => (
                <td key={c.key} className={`px-3 py-2 align-top ${c.className ?? ''}`}>
                  {c.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
