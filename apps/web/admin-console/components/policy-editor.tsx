'use client';

import { useState, useTransition } from 'react';
import { useRouter } from 'next/navigation';
import Form from '@rjsf/core';
import type { RJSFSchema } from '@rjsf/utils';
import validator from '@rjsf/validator-ajv8';

import { SubmitButton, TextField, FormRow } from './form';

interface Props {
  readonly mode: 'create' | 'edit';
  readonly policyId?: string;
  readonly initialName?: string;
  readonly initialDocument?: Record<string, unknown>;
  readonly schema: RJSFSchema;
  readonly action: (
    name: string,
    document: Record<string, unknown>,
  ) => Promise<{ id: string } | { error: string }>;
}

export function PolicyEditor({
  mode,
  policyId,
  initialName,
  initialDocument,
  schema,
  action,
}: Props) {
  const router = useRouter();
  const [name, setName] = useState<string>(initialName ?? '');
  const [doc, setDoc] = useState<Record<string, unknown>>(initialDocument ?? {});
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [pending, start] = useTransition();

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        setErrorMsg(null);
        start(async () => {
          const r = await action(name, doc);
          if ('error' in r) {
            setErrorMsg(r.error);
            return;
          }
          router.push(`/policies/${r.id}`);
          router.refresh();
        });
      }}
    >
      {mode === 'create' ? (
        <FormRow label="Policy name">
          <TextField
            required
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="acme-prod-budget-monthly"
          />
        </FormRow>
      ) : (
        <p className="mb-3 text-xs text-muted">
          Editing policy <span className="font-mono text-text">{policyId}</span>. Saving appends a
          new version.
        </p>
      )}

      <div className="rounded border border-border bg-panel p-4">
        <Form
          schema={schema}
          formData={doc}
          validator={validator}
          onChange={(e) => setDoc(e.formData as Record<string, unknown>)}
          className="rjsf"
          uiSchema={{ 'ui:submitButtonOptions': { norender: true } }}
        />
      </div>

      {errorMsg ? <p className="mt-3 text-sm text-danger">{errorMsg}</p> : null}

      <div className="mt-4">
        <SubmitButton pending={pending}>
          {mode === 'create' ? 'Create policy' : 'Save new version'}
        </SubmitButton>
      </div>
    </form>
  );
}
