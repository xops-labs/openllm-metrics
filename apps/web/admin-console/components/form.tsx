import { InputHTMLAttributes, ReactNode } from 'react';

export function FormRow({
  label,
  children,
  hint,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
}) {
  return (
    <label className="mb-3 block">
      <span className="mb-1 block text-xs text-muted">{label}</span>
      {children}
      {hint ? <span className="mt-1 block text-xs text-muted">{hint}</span> : null}
    </label>
  );
}

export function TextField(props: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className={`w-full rounded border border-border bg-panel px-2 py-1 text-sm text-text outline-none focus:border-accent ${props.className ?? ''}`}
    />
  );
}

export function SubmitButton({ children, pending }: { children: ReactNode; pending?: boolean }) {
  return (
    <button
      type="submit"
      disabled={pending}
      className="rounded border border-accent bg-accent px-3 py-1 text-sm text-white disabled:opacity-50"
    >
      {pending ? 'Saving' : children}
    </button>
  );
}
