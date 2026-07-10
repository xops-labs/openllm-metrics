import type { Metadata } from 'next';
import { ReactNode } from 'react';
import { Nav } from '@/components/nav';
import './globals.css';

export const metadata: Metadata = {
  title: 'OpenLLM Metrics — Admin',
  description:
    'Internal admin and governance console for OpenLLM Metrics. No prompts or completions are rendered.',
  robots: { index: false, follow: false },
};

interface Props {
  readonly children: ReactNode;
}

export default function RootLayout({ children }: Props) {
  return (
    <html lang="en">
      <body className="min-h-screen bg-canvas text-text">
        <Nav />
        <main className="mx-auto max-w-7xl px-6 py-6">{children}</main>
        <footer className="mx-auto max-w-7xl px-6 pb-8 text-xs text-muted">
          OSS admin console (F032). Auth: OIDC when configured (OIDC_ISSUER), local dev-login
          fallback otherwise (F005).
        </footer>
      </body>
    </html>
  );
}
