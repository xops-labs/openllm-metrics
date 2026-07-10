import NextAuth, { type NextAuthConfig } from 'next-auth';
import type { Provider } from 'next-auth/providers';
import Credentials from 'next-auth/providers/credentials';

/**
 * Real OIDC auth scaffold (F005) with a local dev fallback.
 *
 * When the OIDC_* env vars are present, a generic OIDC provider is configured
 * (any standards-compliant IdP: Auth0, Keycloak, Okta, Entra ID, Dex, ...).
 * When they are absent, the console falls back to a single Credentials
 * provider that mints a local dev identity so the app still boots and is fully
 * usable with zero configuration — exactly the local-dev story the existing
 * stub provided.
 *
 * Required env for real OIDC:
 *   - AUTH_SECRET            (random string; `openssl rand -base64 32`)
 *   - OIDC_ISSUER            (issuer URL, e.g. https://idp.example.com/realms/olm)
 *   - OIDC_CLIENT_ID
 *   - OIDC_CLIENT_SECRET
 * Optional:
 *   - OIDC_TENANT_CLAIM      (JWT claim carrying the tenant id; default: 'tenant')
 *
 * The session carries the resolved `tenantId`, which the tenant switcher can
 * override via cookie (multi-tenant operators frequently belong to several
 * tenants). Every backend call still forwards X-Actor / X-OLM-User / X-Tenant-ID.
 */

export const OIDC_ENABLED = Boolean(
  process.env.OIDC_ISSUER && process.env.OIDC_CLIENT_ID && process.env.OIDC_CLIENT_SECRET,
);

const TENANT_CLAIM = process.env.OIDC_TENANT_CLAIM ?? 'tenant';
const DEFAULT_TENANT = process.env.OLM_DEFAULT_TENANT ?? '11111111-2222-3333-4444-555555555555';

function oidcProvider(): Provider {
  return {
    id: 'oidc',
    name: 'OIDC',
    type: 'oidc',
    // Guarded by OIDC_ENABLED at the call site — these are always set here.
    issuer: process.env.OIDC_ISSUER as string,
    clientId: process.env.OIDC_CLIENT_ID as string,
    clientSecret: process.env.OIDC_CLIENT_SECRET as string,
    // Standard OIDC scope; IdP profile + email used for the actor identity.
    authorization: { params: { scope: 'openid profile email' } },
  };
}

function devCredentialsProvider(): Provider {
  // Local-only fallback. No password is checked — this is for `pnpm dev` and
  // CI smoke tests where no IdP is wired. It is unreachable once OIDC_* is set.
  return Credentials({
    id: 'dev-login',
    name: 'Local dev login',
    credentials: {
      email: { label: 'Email', type: 'text' },
    },
    authorize: (creds) => {
      const email =
        typeof creds?.email === 'string' && creds.email.trim() !== ''
          ? creds.email.trim()
          : (process.env.OLM_DEV_USER ?? 'dev@local');
      return { id: email, email, name: email };
    },
  });
}

const providers: Provider[] = OIDC_ENABLED ? [oidcProvider()] : [devCredentialsProvider()];

export const authConfig: NextAuthConfig = {
  providers,
  session: { strategy: 'jwt' },
  trustHost: true,
  pages: OIDC_ENABLED ? {} : { signIn: '/dev-login' },
  callbacks: {
    jwt({ token, profile }) {
      // Hoist the tenant claim from the IdP profile into the session token.
      if (profile && typeof profile === 'object') {
        const claim = (profile as Record<string, unknown>)[TENANT_CLAIM];
        if (typeof claim === 'string' && claim) {
          token.tenantId = claim;
        }
      }
      if (!token.tenantId) {
        token.tenantId = DEFAULT_TENANT;
      }
      return token;
    },
    session({ session, token }) {
      session.user = {
        ...session.user,
        id: (token.sub ?? token.email ?? 'unknown') as string,
        tenantId: (token.tenantId as string) ?? DEFAULT_TENANT,
      };
      return session;
    },
  },
};

export const { handlers, auth, signIn } = NextAuth(authConfig);
