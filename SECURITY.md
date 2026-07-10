# Security Policy

## Supported Versions

| Version              | Supported |
| -------------------- | --------- |
| `main` (pre-release) | Yes       |

OpenLLM Metrics has not yet reached a stable release. All security fixes are applied to `main`.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report vulnerabilities by emailing **<yasvanth@live.in>** with:

1. A clear description of the vulnerability.
2. Steps to reproduce (or a proof-of-concept if safe to share).
3. The affected component(s) and version or commit hash.
4. Your assessment of severity and impact.

### What to expect

- **Acknowledgement** within 2 business days.
- **Triage and severity assessment** within 5 business days.
- **Fix or mitigation** timeline communicated after triage.
- Credit in the release notes (unless you prefer to remain anonymous).

## Scope

The following are in scope:

- Authentication and authorization bypass in the control-plane services (`apps/api/*`)
- Cross-tenant data leakage anywhere in the platform
- Prompt or completion data leaking into telemetry, logs, or storage
- Secret or API key exposure via the gateway or pollers
- Injection vulnerabilities in policy storage or the metrics / analytics query path

The following are **out of scope**:

- Denial-of-service attacks on public endpoints
- Issues requiring physical access or social engineering
- Vulnerabilities in upstream dependencies (report those to the upstream project)

## Safe Harbor

We support good-faith security research. If you make a good-faith effort to
comply with this policy during your research, we will:

- Consider your activity authorized under this policy and will not initiate
  legal action against you, recommend law-enforcement involvement, or pursue
  a complaint against you for accidental, good-faith violations of this
  policy.
- Work with you to understand and resolve the issue quickly, and confirm
  publicly (with your permission) that your report led to a fix.

To stay within scope of this safe harbor, please:

- Limit testing to systems you own or have explicit permission to test, and
  to the components listed in scope above.
- Avoid privacy violations, degradation of user experience, disruption of
  production systems, and destruction or modification of data you do not own.
- Use only test accounts, test tenants, and synthetic data. Never use real
  user, tenant, or production provider credentials.
- Give us a reasonable amount of time to resolve the issue before any public
  disclosure, and coordinate the timing of any public write-up with us.
- Do not attempt to access, modify, or exfiltrate data belonging to anyone
  other than yourself.

If at any point you are uncertain whether an action is within scope, email
us first at the address above and ask.

This safe harbor is a statement of intent, not a contract, and applies only
to actions covered by this policy. It does not waive third-party rights
(for example, hosting providers, cloud platforms, or end users) and does
not authorize testing outside the scope above.

## Security Design Principles

- LLM prompts and completions are **never** collected, logged, or stored.
- The gateway never stores provider API keys — caller `Authorization` headers pass through untouched and are never logged or traced.
- Every signal carries `tenant` / `team` / `app` / `env` / `project` attribution (via `X-OLM-*` headers at the gateway), and control-plane services filter by tenant; gateway-level authentication (per-tenant API keys / JWT) is on the roadmap.
- Audit records are append-only with hash-chaining.
- CODEOWNERS enforces explicit maintainer review on sensitive code paths.
