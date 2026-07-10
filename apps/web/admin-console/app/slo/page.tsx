import { PageHeader } from '@/components/page-header';
import { EmptyState } from '@/components/empty-state';

interface PanelSpec {
  readonly key: string;
  readonly title: string;
  readonly uid: string;
}

// F027 SLO dashboards. The Grafana folder UID assignment is documented in
// packages/dashboards/. The console only renders iframes — no metrics are
// proxied through this app.
const PANELS: ReadonlyArray<PanelSpec> = [
  { key: 'latency', title: 'Latency SLO', uid: 'olm-slo-latency' },
  { key: 'availability', title: 'Availability SLO', uid: 'olm-slo-availability' },
  { key: 'cost', title: 'Cost SLO', uid: 'olm-slo-cost' },
];

export default function SloPage() {
  const grafana = process.env.OLM_GRAFANA_URL ?? '';
  if (!grafana) {
    return (
      <>
        <PageHeader
          title="SLO dashboards"
          description="Embedded Grafana dashboards from the F027 pack."
        />
        <EmptyState
          title="OLM_GRAFANA_URL not configured"
          hint="Set OLM_GRAFANA_URL to a browser-reachable Grafana base URL (the compose stack uses http://localhost:3000) — in .env.local for a host-mode console, or via the admin-console environment in docker-compose.yml."
        />
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="SLO dashboards"
        description="Embedded from the F027 dashboard pack. Panels show error-budget burn rates only; no LLM payloads."
      />
      <div className="space-y-6">
        {PANELS.map((p) => (
          <section key={p.key}>
            <h2 className="mb-2 text-sm font-semibold">{p.title}</h2>
            <iframe
              src={`${grafana}/d/${p.uid}?kiosk=tv&theme=dark`}
              title={p.title}
              className="h-[420px] w-full rounded border border-border bg-panel"
              sandbox="allow-scripts allow-same-origin"
            />
          </section>
        ))}
      </div>
    </>
  );
}
