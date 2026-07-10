-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F005 — Identity and Tenant Model.
-- Defines the canonical control-plane tables for tenants, teams, apps, users,
-- user role grants, and service principals; enables row-level security on
-- every tenant-scoped table per F005 README sections 9-11.
--
-- Tool: goose (https://github.com/pressly/goose)
-- Direction: Up / Down

-- +goose Up
-- +goose StatementBegin

-- ------------------------------------------------------------------
-- Required extensions
-- ------------------------------------------------------------------
-- citext      — case-insensitive email comparisons for users.email.
-- pgcrypto    — gen_random_uuid() for surrogate keys where seeds do
--               not supply explicit UUIDs.
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ------------------------------------------------------------------
-- control_plane.tenants
-- ------------------------------------------------------------------
-- Primary tenant entity. Soft-delete via deleted_at per F005 section
-- 10 (compliance retention requirement). Status is intentionally
-- modeled as a NULLable deleted_at + a CHECK-constrained status column
-- so callers can suspend a tenant without deleting it.
CREATE TABLE IF NOT EXISTS control_plane.tenants (
    id          UUID        NOT NULL PRIMARY KEY,
    slug        TEXT        NOT NULL UNIQUE,
    name        TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'suspended', 'deleted')),
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE control_plane.tenants IS
    'F005 canonical tenant primitive. Every tenant-scoped row in the platform references tenants.id.';
COMMENT ON COLUMN control_plane.tenants.deleted_at IS
    'Soft-delete timestamp. Rows are retained for compliance and audit (F005 sec 10).';

-- ------------------------------------------------------------------
-- control_plane.teams
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS control_plane.teams (
    id          UUID        NOT NULL PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    slug        TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, slug)
);

CREATE INDEX IF NOT EXISTS teams_tenant_id_idx
    ON control_plane.teams (tenant_id);

COMMENT ON TABLE control_plane.teams IS
    'Teams own apps and act as the mid-level grouping within a tenant.';

-- ------------------------------------------------------------------
-- control_plane.apps
-- ------------------------------------------------------------------
-- Apps belong to exactly one team within a tenant and are scoped to a
-- specific environment (dev/staging/prod). The seed treats (slug, env)
-- as unique per tenant; we mirror that constraint here.
CREATE TABLE IF NOT EXISTS control_plane.apps (
    id          UUID        NOT NULL PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    team_id     UUID        NOT NULL REFERENCES control_plane.teams(id)   ON DELETE CASCADE,
    slug        TEXT        NOT NULL,
    name        TEXT        NOT NULL,
    env         TEXT        NOT NULL
                    CHECK (env IN ('dev', 'staging', 'prod')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, slug, env)
);

CREATE INDEX IF NOT EXISTS apps_tenant_id_idx
    ON control_plane.apps (tenant_id);
CREATE INDEX IF NOT EXISTS apps_team_id_idx
    ON control_plane.apps (team_id);

COMMENT ON TABLE control_plane.apps IS
    'Application identity within a (tenant, team, env). Every gateway request resolves to an app.';

-- ------------------------------------------------------------------
-- control_plane.users
-- ------------------------------------------------------------------
-- Human actors authenticated via OIDC. external_sub is the IdP subject
-- claim and is the primary auth identifier; there is no password column
-- by design (F005 sec 9). tenant_id is NULLable to support the
-- platform_admin row, which has cross-tenant visibility.
--
-- email is CITEXT for case-insensitive matching and is globally unique;
-- seed data ensures no collisions across tenants.
CREATE TABLE IF NOT EXISTS control_plane.users (
    id              UUID        NOT NULL PRIMARY KEY,
    tenant_id       UUID        REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    external_sub    TEXT        NOT NULL UNIQUE,
    email           CITEXT      NOT NULL UNIQUE,
    name            TEXT        NOT NULL,
    actor_type      TEXT        NOT NULL DEFAULT 'human'
                        CHECK (actor_type IN ('human', 'service')),
    mfa_enrolled    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS users_tenant_id_idx
    ON control_plane.users (tenant_id);

COMMENT ON TABLE control_plane.users IS
    'Human user identities. external_sub is the OIDC subject claim; passwords are never stored.';
COMMENT ON COLUMN control_plane.users.tenant_id IS
    'NULL only for platform-wide users (platform_admin). All other users are tenant-scoped.';

-- ------------------------------------------------------------------
-- control_plane.user_roles
-- ------------------------------------------------------------------
-- Role grants. tenant_id is NULL when the role is platform_admin
-- (cross-tenant access). team_id is currently NULL for all seed rows
-- but is kept on the table to allow finer-grained team-level grants
-- without a follow-on migration. A surrogate id PK is used because
-- composite PKs cannot include the NULLable tenant_id column.
--
-- Two partial unique indexes enforce no-duplicate grants:
--   * one for tenant-scoped grants (tenant_id IS NOT NULL)
--   * one for platform-wide grants (tenant_id IS NULL)
CREATE TABLE IF NOT EXISTS control_plane.user_roles (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES control_plane.users(id)   ON DELETE CASCADE,
    tenant_id   UUID        REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    team_id     UUID        REFERENCES control_plane.teams(id)   ON DELETE CASCADE,
    role        TEXT        NOT NULL
                    CHECK (role IN ('platform_admin', 'tenant_admin', 'sre', 'finops', 'viewer')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (
        (role = 'platform_admin' AND tenant_id IS NULL)
        OR (role <> 'platform_admin' AND tenant_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS user_roles_tenant_scoped_uidx
    ON control_plane.user_roles (user_id, tenant_id, role, team_id)
    WHERE tenant_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS user_roles_platform_uidx
    ON control_plane.user_roles (user_id, role)
    WHERE tenant_id IS NULL;

CREATE INDEX IF NOT EXISTS user_roles_user_id_idx
    ON control_plane.user_roles (user_id);
CREATE INDEX IF NOT EXISTS user_roles_tenant_id_idx
    ON control_plane.user_roles (tenant_id);

COMMENT ON TABLE control_plane.user_roles IS
    'User-to-role grants. platform_admin grants have tenant_id=NULL; all other roles are tenant-scoped.';

-- ------------------------------------------------------------------
-- control_plane.service_principals
-- ------------------------------------------------------------------
-- Service-to-service identities. secret_hash is NULL after creation;
-- credentials are issued post-creation via the control-plane API
-- (POST /service-principals/{id}/rotate-secret). Scopes follow
-- least-privilege per service role.
CREATE TABLE IF NOT EXISTS control_plane.service_principals (
    id          UUID        NOT NULL PRIMARY KEY,
    tenant_id   UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    scopes      TEXT[]      NOT NULL DEFAULT '{}',
    secret_hash TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS service_principals_tenant_id_idx
    ON control_plane.service_principals (tenant_id);

COMMENT ON TABLE control_plane.service_principals IS
    'Service-to-service identities. secret_hash is set out-of-band; never reversible.';

-- ------------------------------------------------------------------
-- Row Level Security
-- ------------------------------------------------------------------
-- F005 sec 9 mandates RLS on every tenant-scoped table with default-deny.
-- The session key 'app.tenant_id' is set by the auth middleware after
-- JWT validation. A NULL or unset value means "no tenant context" and
-- access is denied except for connections using the BYPASSRLS role
-- (reserved for migrations, the platform_admin path, and operational
-- tooling).
--
-- Policies allow rows to be visible only when the row's tenant_id
-- matches the current session's app.tenant_id setting. The setting is
-- read with current_setting('app.tenant_id', true) so an unset value
-- returns NULL rather than raising.

-- tenants: a tenant can only see its own row.
ALTER TABLE control_plane.tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.tenants FORCE ROW LEVEL SECURITY;

CREATE POLICY tenants_tenant_isolation
    ON control_plane.tenants
    USING (id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (id::TEXT = current_setting('app.tenant_id', true));

-- teams
ALTER TABLE control_plane.teams ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.teams FORCE ROW LEVEL SECURITY;

CREATE POLICY teams_tenant_isolation
    ON control_plane.teams
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- apps
ALTER TABLE control_plane.apps ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.apps FORCE ROW LEVEL SECURITY;

CREATE POLICY apps_tenant_isolation
    ON control_plane.apps
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- users: tenant-scoped rows visible inside the tenant; platform-wide
-- users (tenant_id IS NULL) are only visible to BYPASSRLS connections.
ALTER TABLE control_plane.users ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.users FORCE ROW LEVEL SECURITY;

CREATE POLICY users_tenant_isolation
    ON control_plane.users
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- user_roles: tenant-scoped grants visible inside the tenant.
-- platform_admin grants (tenant_id IS NULL) are only visible to
-- BYPASSRLS connections, which is correct: a tenant should never see
-- another tenant's platform-admin grants.
ALTER TABLE control_plane.user_roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.user_roles FORCE ROW LEVEL SECURITY;

CREATE POLICY user_roles_tenant_isolation
    ON control_plane.user_roles
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- service_principals
ALTER TABLE control_plane.service_principals ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.service_principals FORCE ROW LEVEL SECURITY;

CREATE POLICY service_principals_tenant_isolation
    ON control_plane.service_principals
    USING (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS service_principals_tenant_isolation ON control_plane.service_principals;
DROP POLICY IF EXISTS user_roles_tenant_isolation         ON control_plane.user_roles;
DROP POLICY IF EXISTS users_tenant_isolation              ON control_plane.users;
DROP POLICY IF EXISTS apps_tenant_isolation               ON control_plane.apps;
DROP POLICY IF EXISTS teams_tenant_isolation              ON control_plane.teams;
DROP POLICY IF EXISTS tenants_tenant_isolation            ON control_plane.tenants;

DROP TABLE IF EXISTS control_plane.service_principals;
DROP TABLE IF EXISTS control_plane.user_roles;
DROP TABLE IF EXISTS control_plane.users;
DROP TABLE IF EXISTS control_plane.apps;
DROP TABLE IF EXISTS control_plane.teams;
DROP TABLE IF EXISTS control_plane.tenants;

-- Extensions are intentionally left in place; they may be in use by
-- other migrations.

-- +goose StatementEnd
