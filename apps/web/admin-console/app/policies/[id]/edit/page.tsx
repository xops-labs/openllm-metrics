import { PageHeader } from '@/components/page-header';
import { PolicyEditor } from '@/components/policy-editor';
import { appendVersion, getPolicy, getPolicySchema } from '@/lib/api/policy';
import type { RJSFSchema } from '@rjsf/utils';

interface Props {
  readonly params: Promise<{ id: string }>;
}

export default async function EditPolicyPage({ params }: Props) {
  const { id } = await params;
  const [policy, schema] = await Promise.all([getPolicy(id), getPolicySchema()]);

  async function submit(_name: string, document: Record<string, unknown>) {
    'use server';
    try {
      const p = await appendVersion(id, document);
      return { id: p.id };
    } catch (e) {
      return { error: (e as Error).message };
    }
  }

  return (
    <>
      <PageHeader
        title={`Edit ${policy.name}`}
        description={`Appends a new version. Current version is v${policy.current_version}.`}
      />
      <PolicyEditor
        mode="edit"
        policyId={id}
        initialName={policy.name}
        initialDocument={policy.document}
        schema={schema as RJSFSchema}
        action={submit}
      />
    </>
  );
}
