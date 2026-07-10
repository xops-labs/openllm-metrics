import { PageHeader } from '@/components/page-header';
import { PolicyEditor } from '@/components/policy-editor';
import { createPolicy, getPolicySchema } from '@/lib/api/policy';
import type { RJSFSchema } from '@rjsf/utils';

async function submit(name: string, document: Record<string, unknown>) {
  'use server';
  try {
    const p = await createPolicy(name, document);
    return { id: p.id };
  } catch (e) {
    return { error: (e as Error).message };
  }
}

export default async function NewPolicyPage() {
  const schema = (await getPolicySchema()) as RJSFSchema;
  return (
    <>
      <PageHeader
        title="New policy"
        description="The form is generated from the JSON Schema served by policy-service so the UI cannot drift from the validator."
      />
      <PolicyEditor mode="create" schema={schema} action={submit} />
    </>
  );
}
