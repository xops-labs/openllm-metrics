import { ReactElement } from 'react';
import { diffLines, Change } from 'diff';

interface Props {
  readonly left: Record<string, unknown>;
  readonly right: Record<string, unknown>;
  readonly leftLabel?: string;
  readonly rightLabel?: string;
}

function renderSide(side: 'left' | 'right', changes: ReadonlyArray<Change>): ReactElement[] {
  const out: ReactElement[] = [];
  let lineNo = 0;
  changes.forEach((c, i) => {
    const skip = side === 'left' ? c.added : c.removed;
    if (skip) return;
    const cls = c.added
      ? 'bg-ok/20 text-text'
      : c.removed
        ? 'bg-danger/20 text-text'
        : 'text-muted';
    const lines = c.value.split('\n');
    // strip trailing newline split artifact
    const trimmed = lines[lines.length - 1] === '' ? lines.slice(0, -1) : lines;
    trimmed.forEach((ln, j) => {
      lineNo += 1;
      out.push(
        <div key={`${i}-${j}`} className={`flex gap-2 px-2 ${cls}`}>
          <span className="w-8 select-none text-right text-muted">{lineNo}</span>
          <pre className="flex-1 whitespace-pre font-mono text-xs">{ln}</pre>
        </div>,
      );
    });
  });
  return out;
}

export function DiffViewer({ left, right, leftLabel, rightLabel }: Props) {
  const a = JSON.stringify(left, null, 2);
  const b = JSON.stringify(right, null, 2);
  const changes = diffLines(a, b);
  return (
    <div className="grid grid-cols-2 gap-3">
      <div className="rounded border border-border bg-panel">
        <div className="border-b border-border px-3 py-2 text-xs text-muted">
          {leftLabel ?? 'Left'}
        </div>
        <div className="py-2">{renderSide('left', changes)}</div>
      </div>
      <div className="rounded border border-border bg-panel">
        <div className="border-b border-border px-3 py-2 text-xs text-muted">
          {rightLabel ?? 'Right'}
        </div>
        <div className="py-2">{renderSide('right', changes)}</div>
      </div>
    </div>
  );
}
