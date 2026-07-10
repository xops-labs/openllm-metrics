# Seed Data — OpenLLM Metrics

Demo data for local development and integration testing.

**Seed file:** `platform/db/seeds/001_demo_data.sql`  
**Runner:** `./tools/scripts/seed.sh`

## Running the seed

```bash
# Apply seed data (idempotent — safe to re-run)
./tools/scripts/seed.sh

# Preview SQL without executing
./tools/scripts/seed.sh --dry-run
```

Prerequisites:

1. PostgreSQL running (`platform/db/docker-compose.yml`)
2. Schema namespaces created (`platform/db/init/00_schemas.sql`)
3. Control-plane migrations applied (`./tools/scripts/migrate.sh apply control_plane`)

## Demo Tenants

| Tenant           | Slug       | ID                                     |
| ---------------- | ---------- | -------------------------------------- |
| OpenLLM Platform | `platform` | `00000000-0000-0000-0001-000000000001` |
| Acme Corp        | `acme`     | `00000000-0000-0000-0002-000000000001` |
| Beta Industries  | `beta`     | `00000000-0000-0000-0003-000000000001` |

## Demo Users

Authentication uses the dev OIDC provider (configured in F005). Human users do not have stored passwords — they log in via OIDC using `external_sub` as the IdP subject claim.

In dev mode, JWTs can be minted manually using `JWT_SECRET` from `.env`.

### Platform Tenant

| Name           | Email                                | Role             | MFA      | external_sub          |
| -------------- | ------------------------------------ | ---------------- | -------- | --------------------- |
| Platform Admin | `platform.admin@openllm-metrics.dev` | `platform_admin` | enrolled | `sub\|platform-admin` |

`platform_admin` has cross-tenant visibility and is the only role with platform-wide access.

### Acme Corp

| Name          | Email             | Role           | MFA          | external_sub       |
| ------------- | ----------------- | -------------- | ------------ | ------------------ |
| Alice Admin   | `admin@acme.dev`  | `tenant_admin` | enrolled     | `sub\|acme-admin`  |
| Sam SRE       | `sre@acme.dev`    | `sre`          | not enrolled | `sub\|acme-sre`    |
| Fiona FinOps  | `finops@acme.dev` | `finops`       | not enrolled | `sub\|acme-finops` |
| Victor Viewer | `viewer@acme.dev` | `viewer`       | not enrolled | `sub\|acme-viewer` |

### Beta Industries

| Name          | Email             | Role           | MFA          | external_sub       |
| ------------- | ----------------- | -------------- | ------------ | ------------------ |
| Bob Admin     | `admin@beta.dev`  | `tenant_admin` | enrolled     | `sub\|beta-admin`  |
| Sara SRE      | `sre@beta.dev`    | `sre`          | not enrolled | `sub\|beta-sre`    |
| Frank FinOps  | `finops@beta.dev` | `finops`       | not enrolled | `sub\|beta-finops` |
| Vivian Viewer | `viewer@beta.dev` | `viewer`       | not enrolled | `sub\|beta-viewer` |

## Role Capabilities

| Role             | Create/Edit policy | View metrics | View costs  | Manage tenants | Cross-tenant |
| ---------------- | ------------------ | ------------ | ----------- | -------------- | ------------ |
| `platform_admin` | all tenants        | all tenants  | all tenants | yes            | yes          |
| `tenant_admin`   | own tenant         | own tenant   | own tenant  | own tenant     | no           |
| `sre`            | no                 | own tenant   | no          | no             | no           |
| `finops`         | no                 | no           | own tenant  | no             | no           |
| `viewer`         | no                 | own tenant   | no          | no             | no           |

## Demo Teams & Apps

### Acme Corp

| Team                 | Slug           | Apps                         |
| -------------------- | -------------- | ---------------------------- |
| Platform Engineering | `platform-eng` | `chat-assistant` (dev, prod) |
| Analytics            | `analytics`    | `batch-processor` (dev)      |

### Beta Industries

| Team         | Slug           | Apps                            |
| ------------ | -------------- | ------------------------------- |
| Engineering  | `engineering`  | `search-service` (dev, staging) |
| Data Science | `data-science` | `summary-bot` (dev)             |

## Service Principals

Each tenant has one service principal per service, with least-privilege scopes.  
`secret_hash` is `NULL` after seeding — issue credentials via the control-plane API (`POST /service-principals/{id}/rotate-secret`) after F005 ships.

> The `scoring-worker` and `policy-evaluator` principals are **not implemented here**
> roles (their workers ship in this repository) and are not seeded in
> the OSS distribution.

### Acme Corp

| Name           | Scopes                                         | ID                                     |
| -------------- | ---------------------------------------------- | -------------------------------------- |
| `gateway`      | `runtime:write`, `routing:read`, `policy:read` | `00000000-0000-0000-0002-000000000201` |
| `usage-poller` | `usage:write`                                  | `00000000-0000-0000-0002-000000000202` |

### Beta Industries

| Name           | Scopes                                         | ID                                     |
| -------------- | ---------------------------------------------- | -------------------------------------- |
| `gateway`      | `runtime:write`, `routing:read`, `policy:read` | `00000000-0000-0000-0003-000000000201` |
| `usage-poller` | `usage:write`                                  | `00000000-0000-0000-0003-000000000202` |

## DDL ownership

The F005 migration (`platform/db/control_plane/migrations/00002_identity_tenant_model.sql`) owns the DDL for `tenants`, `teams`, `apps`, `users`, `user_roles`, and `service_principals`. The seed file inserts data only; it requires the control-plane migrations to be applied first.

The seed disables row-level security for the duration of its transaction (`SET LOCAL row_security = off`) because it bootstraps data across every tenant in a single connection. Application traffic still hits default-deny RLS on these tables.
