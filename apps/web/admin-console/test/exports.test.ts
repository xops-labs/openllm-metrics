import { describe, it, expect } from 'vitest';
import { validateExportsConfig, EXPORT_KINDS } from '@/lib/api/exports';

describe('validateExportsConfig', () => {
  it('accepts a valid config with one target of each kind', () => {
    const issues = validateExportsConfig({
      targets: [
        {
          kind: 'grafana',
          name: 'g',
          enabled: true,
          url: 'https://g.example.com',
          apiKeyEnv: 'GRAFANA_API_KEY',
        },
        {
          kind: 'prometheus_remote_write',
          name: 'p',
          enabled: false,
          endpoint: 'https://p.example.com/api/v1/write',
        },
        {
          kind: 'otel',
          name: 'o',
          enabled: true,
          endpoint: 'https://otel.example.com:4318',
          protocol: 'grpc',
        },
      ],
    });
    expect(issues).toEqual([]);
  });

  it('rejects an unknown kind', () => {
    const issues = validateExportsConfig({
      targets: [{ kind: 'datadog', name: 'd', enabled: true }],
    });
    expect(issues.some((i) => i.path === 'targets[0].kind')).toBe(true);
  });

  it('requires a valid URL for grafana and a secret env name', () => {
    const issues = validateExportsConfig({
      targets: [{ kind: 'grafana', name: 'g', enabled: true, url: 'not-a-url', apiKeyEnv: '' }],
    });
    expect(issues.some((i) => i.path === 'targets[0].url')).toBe(true);
    expect(issues.some((i) => i.path === 'targets[0].apiKeyEnv')).toBe(true);
  });

  it('requires a valid otel protocol', () => {
    const issues = validateExportsConfig({
      targets: [
        {
          kind: 'otel',
          name: 'o',
          enabled: true,
          endpoint: 'https://x.example.com',
          protocol: 'ftp',
        },
      ],
    });
    expect(issues.some((i) => i.path === 'targets[0].protocol')).toBe(true);
  });

  it('flags a non-array targets field', () => {
    expect(validateExportsConfig({ targets: 'nope' })).toEqual([
      { path: 'targets', message: 'targets must be an array' },
    ]);
  });

  it('exposes exactly the three OSS export kinds', () => {
    expect([...EXPORT_KINDS]).toEqual(['grafana', 'prometheus_remote_write', 'otel']);
  });
});
