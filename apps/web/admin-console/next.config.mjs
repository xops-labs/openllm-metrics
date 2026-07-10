import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const here = dirname(fileURLToPath(import.meta.url));
// Monorepo root (apps/web/admin-console -> repo root) so the standalone bundle
// traces the pnpm-hoisted workspace dependencies correctly.
const repoRoot = resolve(here, '../../..');

// The standalone server bundle (for the container image) is produced when
// NEXT_OUTPUT=standalone — the Dockerfile sets this. We gate it behind a flag
// because the standalone file-tracing step copies node_modules via symlinks,
// which fails with EPERM on Windows dev machines using pnpm's symlinked store.
// The default `pnpm build` therefore stays green everywhere; the Linux
// container build opts in. The compiled app is identical either way.
const standalone = process.env.NEXT_OUTPUT === 'standalone';

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  ...(standalone ? { output: 'standalone' } : {}),
  // In standalone (container) mode trace from the monorepo root so hoisted
  // workspace deps are included; otherwise pin to this app to silence the
  // multi-lockfile workspace-root warning during local dev builds.
  outputFileTracingRoot: standalone ? repoRoot : here,
  experimental: {
    // RSC is the default in App Router; flag retained for clarity.
    serverActions: {
      bodySizeLimit: '1mb',
    },
  },
  // No prompts or completions ever rendered: the console only talks to
  // policy-service, audit-service, notification-service, and the
  // decision-service read API. No provider URLs are reachable from here.
};

export default nextConfig;
