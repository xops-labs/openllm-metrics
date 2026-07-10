interface Props {
  readonly title: string;
  readonly hint?: string;
}

export function EmptyState({ title, hint }: Props) {
  return (
    <div className="rounded border border-border bg-panel p-8 text-center">
      <p className="text-sm font-medium text-text">{title}</p>
      {hint ? <p className="mt-1 text-xs text-muted">{hint}</p> : null}
    </div>
  );
}
