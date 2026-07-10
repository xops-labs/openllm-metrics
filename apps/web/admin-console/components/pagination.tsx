import Link from 'next/link';

interface Props {
  readonly basePath: string;
  readonly currentQuery: Record<string, string | undefined>;
  readonly nextCursor: string | null;
}

function buildQuery(
  base: Record<string, string | undefined>,
  override: Record<string, string | undefined>,
) {
  const merged: Record<string, string> = {};
  for (const [k, v] of Object.entries({ ...base, ...override })) {
    if (v) merged[k] = v;
  }
  const qs = new URLSearchParams(merged).toString();
  return qs ? `?${qs}` : '';
}

export function Pagination({ basePath, currentQuery, nextCursor }: Props) {
  return (
    <div className="mt-4 flex items-center justify-end gap-2 text-xs text-muted">
      <Link
        href={`${basePath}${buildQuery(currentQuery, { cursor: undefined })}`}
        className="rounded border border-border px-2 py-1 hover:text-text"
      >
        First
      </Link>
      {nextCursor ? (
        <Link
          href={`${basePath}${buildQuery(currentQuery, { cursor: nextCursor })}`}
          className="rounded border border-border px-2 py-1 hover:text-text"
        >
          Next
        </Link>
      ) : (
        <span className="rounded border border-border px-2 py-1 opacity-50">Next</span>
      )}
    </div>
  );
}
