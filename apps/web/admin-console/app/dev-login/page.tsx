import { redirect } from 'next/navigation';
import { PageHeader } from '@/components/page-header';
import { OIDC_ENABLED, signIn } from '@/lib/auth-config';

/**
 * Local dev login (F005 fallback). Only reachable when OIDC is NOT configured;
 * if OIDC_* is present this redirects to the real provider's sign-in. No
 * password is checked — this exists so `pnpm dev` and CI boot with zero config.
 */
export default function DevLoginPage() {
  if (OIDC_ENABLED) {
    redirect('/api/auth/signin');
  }

  async function login(formData: FormData) {
    'use server';
    const email = String(formData.get('email') ?? '').trim() || 'dev@local';
    await signIn('dev-login', { email, redirectTo: '/' });
  }

  return (
    <div className="mx-auto max-w-sm">
      <PageHeader
        title="Local dev login"
        description="OIDC is not configured (OIDC_ISSUER / OIDC_CLIENT_ID / OIDC_CLIENT_SECRET unset). Sign in with a local dev identity. Configure OIDC env vars to enable real SSO."
      />
      <form action={login} className="space-y-3 rounded border border-border bg-panel p-4">
        <label className="flex flex-col gap-1 text-xs text-muted">
          Email / actor id
          <input
            name="email"
            defaultValue="dev@local"
            className="rounded border border-border bg-canvas px-2 py-1 text-sm text-text"
          />
        </label>
        <button
          type="submit"
          className="w-full rounded border border-accent bg-accent px-3 py-2 text-sm text-white"
        >
          Continue
        </button>
      </form>
    </div>
  );
}
