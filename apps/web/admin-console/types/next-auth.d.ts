import type { DefaultSession } from 'next-auth';

/**
 * Session/JWT type augmentation for the F005 OIDC scaffold. The console needs
 * a stable `id` (actor) and a `tenantId` on the session user so every backend
 * call can forward tenant + actor headers.
 */
declare module 'next-auth' {
  interface Session {
    user: {
      id: string;
      tenantId: string;
    } & DefaultSession['user'];
  }
}

declare module 'next-auth/jwt' {
  interface JWT {
    tenantId?: string;
  }
}
