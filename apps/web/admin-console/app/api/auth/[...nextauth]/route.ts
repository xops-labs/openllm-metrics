import { handlers } from '@/lib/auth-config';

/**
 * Auth.js (next-auth) catch-all route. Serves the OIDC sign-in / callback /
 * session endpoints when OIDC is configured, and the dev-login Credentials flow
 * otherwise. See lib/auth-config.ts for the provider selection.
 */
export const { GET, POST } = handlers;
