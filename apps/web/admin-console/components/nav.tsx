import Link from 'next/link';
import { TenantSwitcher } from './tenant-switcher';
import { currentUser } from '@/lib/auth';
import { listTenants } from '@/lib/api/tenant';

const LINKS: ReadonlyArray<{ href: string; label: string }> = [
  { href: '/', label: 'Overview' },
  { href: '/analytics', label: 'Analytics' },
  { href: '/explore', label: 'Explore' },
  { href: '/tenants', label: 'Tenants' },
  { href: '/policies', label: 'Policies' },
  { href: '/audit', label: 'Audit' },
  { href: '/decisions', label: 'Decisions' },
  { href: '/slo', label: 'SLO' },
  { href: '/notifications/channels', label: 'Notifications' },
  { href: '/settings/exports', label: 'Exports' },
];

export async function Nav() {
  const [user, tenants] = await Promise.all([currentUser(), listTenants()]);
  return (
    <header className="border-b border-border bg-panel">
      <div className="mx-auto flex max-w-7xl items-center gap-6 px-6 py-3">
        <div className="font-mono text-sm text-text">
          openllm-metrics <span className="text-muted">/ admin</span>
        </div>
        <nav className="flex flex-1 gap-4 text-sm">
          {LINKS.map((l) => (
            <Link key={l.href} href={l.href} className="text-muted hover:text-text">
              {l.label}
            </Link>
          ))}
        </nav>
        <TenantSwitcher current={user.tenantId} tenants={tenants} />
        <span className="text-xs text-muted">{user.id}</span>
      </div>
    </header>
  );
}
