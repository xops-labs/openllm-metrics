'use client';

import { useState } from 'react';
import {
  EXPORT_KINDS,
  ExportKind,
  ExportTarget,
  ExportTargetInput,
  ExportsConfig,
  ValidationIssue,
  validateExportsConfig,
} from '@/lib/api/exports';

/**
 * Client editor for the outbound export config (F039). Edits a typed list of
 * targets and PUTs it to /api/exports. Validation is shared with the server
 * route via validateExportsConfig so the client and server enforce the same
 * contract.
 */

interface Props {
  readonly initial: ExportsConfig;
}

type DraftTarget = ExportTarget | (ExportTargetInput & { id?: string });

function blankTarget(kind: ExportKind): DraftTarget {
  switch (kind) {
    case 'grafana':
      return { kind, name: '', enabled: true, url: '', apiKeyEnv: '' };
    case 'prometheus_remote_write':
      return { kind, name: '', enabled: true, endpoint: '' };
    case 'otel':
      return { kind, name: '', enabled: true, endpoint: '', protocol: 'http/protobuf' };
  }
}

const inputCls = 'rounded border border-border bg-canvas px-2 py-1 text-xs text-text';

export function ExportsEditor({ initial }: Props) {
  const [targets, setTargets] = useState<DraftTarget[]>(initial.targets.map((t) => ({ ...t })));
  const [issues, setIssues] = useState<ValidationIssue[]>([]);
  const [status, setStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
  const [updatedAt, setUpdatedAt] = useState(initial.updatedAt);

  function update(i: number, patch: Record<string, unknown>) {
    setTargets((prev) => prev.map((t, idx) => (idx === i ? { ...t, ...patch } : t)));
  }

  function add(kind: ExportKind) {
    setTargets((prev) => [...prev, blankTarget(kind)]);
  }

  function remove(i: number) {
    setTargets((prev) => prev.filter((_, idx) => idx !== i));
  }

  async function save() {
    const body = { targets: targets.map(({ id: _id, ...rest }) => rest) };
    const localIssues = validateExportsConfig(body);
    if (localIssues.length > 0) {
      setIssues(localIssues);
      setStatus('error');
      return;
    }
    setIssues([]);
    setStatus('saving');
    try {
      const res = await fetch('/api/exports', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const data = (await res.json().catch(() => ({}))) as { issues?: ValidationIssue[] };
        setIssues(data.issues ?? [{ path: '', message: `save failed (${res.status})` }]);
        setStatus('error');
        return;
      }
      const saved = (await res.json()) as ExportsConfig;
      setUpdatedAt(saved.updatedAt);
      setStatus('saved');
    } catch {
      setIssues([{ path: '', message: 'network error saving config' }]);
      setStatus('error');
    }
  }

  return (
    <div className="space-y-4">
      {targets.length === 0 ? (
        <div className="rounded border border-border bg-panel p-6 text-sm text-muted">
          No export targets configured. Native analytics still work — add a target below only to
          mirror telemetry into an external system.
        </div>
      ) : (
        targets.map((t, i) => (
          <fieldset key={i} className="rounded border border-border bg-panel p-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="font-mono text-xs uppercase tracking-wider text-accent">
                {t.kind}
              </span>
              <button
                type="button"
                onClick={() => remove(i)}
                className="text-xs text-danger hover:underline"
              >
                remove
              </button>
            </div>
            <div className="grid grid-cols-1 gap-2 md:grid-cols-2">
              <label className="flex flex-col gap-1 text-xs text-muted">
                Name
                <input
                  className={inputCls}
                  value={t.name}
                  onChange={(e) => update(i, { name: e.target.value })}
                  placeholder="prod-grafana"
                />
              </label>
              <label className="flex items-center gap-2 text-xs text-muted">
                <input
                  type="checkbox"
                  checked={t.enabled}
                  onChange={(e) => update(i, { enabled: e.target.checked })}
                />
                Enabled
              </label>
              {t.kind === 'grafana' ? (
                <>
                  <label className="flex flex-col gap-1 text-xs text-muted">
                    URL
                    <input
                      className={inputCls}
                      value={t.url}
                      onChange={(e) => update(i, { url: e.target.value })}
                      placeholder="https://grafana.example.com"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-muted">
                    API key env var
                    <input
                      className={inputCls}
                      value={t.apiKeyEnv}
                      onChange={(e) => update(i, { apiKeyEnv: e.target.value })}
                      placeholder="GRAFANA_API_KEY"
                    />
                  </label>
                </>
              ) : null}
              {t.kind === 'prometheus_remote_write' ? (
                <>
                  <label className="flex flex-col gap-1 text-xs text-muted">
                    Remote-write endpoint
                    <input
                      className={inputCls}
                      value={t.endpoint}
                      onChange={(e) => update(i, { endpoint: e.target.value })}
                      placeholder="https://prometheus.example.com/api/v1/write"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-muted">
                    Password env var (optional)
                    <input
                      className={inputCls}
                      value={t.passwordEnv ?? ''}
                      onChange={(e) => update(i, { passwordEnv: e.target.value || undefined })}
                      placeholder="PROM_RW_PASSWORD"
                    />
                  </label>
                </>
              ) : null}
              {t.kind === 'otel' ? (
                <>
                  <label className="flex flex-col gap-1 text-xs text-muted">
                    OTLP endpoint
                    <input
                      className={inputCls}
                      value={t.endpoint}
                      onChange={(e) => update(i, { endpoint: e.target.value })}
                      placeholder="https://otel-collector.example.com:4318"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-muted">
                    Protocol
                    <select
                      className={inputCls}
                      value={t.protocol}
                      onChange={(e) => update(i, { protocol: e.target.value })}
                    >
                      <option value="http/protobuf">http/protobuf</option>
                      <option value="grpc">grpc</option>
                    </select>
                  </label>
                </>
              ) : null}
            </div>
          </fieldset>
        ))
      )}

      <div className="flex flex-wrap items-center gap-2">
        {EXPORT_KINDS.map((k) => (
          <button
            key={k}
            type="button"
            onClick={() => add(k)}
            className="rounded border border-border px-2 py-1 text-xs text-muted hover:text-text"
          >
            + {k}
          </button>
        ))}
        <button
          type="button"
          onClick={save}
          disabled={status === 'saving'}
          className="ml-auto rounded border border-accent bg-accent px-3 py-1 text-xs text-white disabled:opacity-50"
        >
          {status === 'saving' ? 'Saving…' : 'Save exports'}
        </button>
      </div>

      {status === 'saved' ? (
        <p className="text-xs text-ok">Saved. Last updated {updatedAt}.</p>
      ) : null}
      {issues.length > 0 ? (
        <ul className="rounded border border-danger/40 bg-panel p-3 text-xs text-danger">
          {issues.map((iss, idx) => (
            <li key={idx}>
              <span className="font-mono">{iss.path || '(root)'}</span>: {iss.message}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
