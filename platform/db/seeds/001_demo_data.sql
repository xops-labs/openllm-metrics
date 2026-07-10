-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Demo seed data for OpenLLM Metrics local development.
--
-- Prerequisites:
--   platform/db/init/00_schemas.sql applied (schema namespaces exist)
--   platform/db/control_plane/migrations/ applied (F005 owns the DDL)
--
-- Idempotent: safe to re-run at any time.
-- Run via: ./tools/scripts/seed.sh

\set ON_ERROR_STOP on

BEGIN;

-- The control_plane tables enable FORCE ROW LEVEL SECURITY in the
-- F005 migration. Seed scripts bootstrap data across every tenant in a
-- single transaction, so we disable RLS for the duration of this
-- transaction. SET LOCAL keeps the change scoped to the transaction;
-- application connections retain default-deny behavior.
SET LOCAL row_security = off;

-- ============================================================
-- Tenants
-- ============================================================
-- platform  — internal platform tenant for cross-tenant admins
-- acme      — demo customer (Acme Corp)
-- beta      — demo startup customer (Beta Industries)

INSERT INTO control_plane.tenants (id, name, slug) VALUES
    ('00000000-0000-0000-0001-000000000001', 'OpenLLM Platform', 'platform'),
    ('00000000-0000-0000-0002-000000000001', 'Acme Corp',        'acme'),
    ('00000000-0000-0000-0003-000000000001', 'Beta Industries',  'beta')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- Teams
-- ============================================================

INSERT INTO control_plane.teams (id, tenant_id, name, slug) VALUES
    -- Acme Corp
    ('00000000-0000-0000-0002-000000000011', '00000000-0000-0000-0002-000000000001', 'Platform Engineering', 'platform-eng'),
    ('00000000-0000-0000-0002-000000000012', '00000000-0000-0000-0002-000000000001', 'Analytics',            'analytics'),
    -- Beta Industries
    ('00000000-0000-0000-0003-000000000011', '00000000-0000-0000-0003-000000000001', 'Engineering',          'engineering'),
    ('00000000-0000-0000-0003-000000000012', '00000000-0000-0000-0003-000000000001', 'Data Science',         'data-science')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- Apps
-- ============================================================

INSERT INTO control_plane.apps (id, tenant_id, team_id, name, slug, env) VALUES
    -- Acme Corp — Platform Engineering
    ('00000000-0000-0000-0002-000000000021', '00000000-0000-0000-0002-000000000001', '00000000-0000-0000-0002-000000000011', 'Chat Assistant',  'chat-assistant',  'dev'),
    ('00000000-0000-0000-0002-000000000022', '00000000-0000-0000-0002-000000000001', '00000000-0000-0000-0002-000000000011', 'Chat Assistant',  'chat-assistant',  'prod'),
    -- Acme Corp — Analytics
    ('00000000-0000-0000-0002-000000000023', '00000000-0000-0000-0002-000000000001', '00000000-0000-0000-0002-000000000012', 'Batch Processor', 'batch-processor', 'dev'),
    -- Beta Industries — Engineering
    ('00000000-0000-0000-0003-000000000021', '00000000-0000-0000-0003-000000000001', '00000000-0000-0000-0003-000000000011', 'Search Service',  'search-service',  'dev'),
    ('00000000-0000-0000-0003-000000000022', '00000000-0000-0000-0003-000000000001', '00000000-0000-0000-0003-000000000011', 'Search Service',  'search-service',  'staging'),
    -- Beta Industries — Data Science
    ('00000000-0000-0000-0003-000000000023', '00000000-0000-0000-0003-000000000001', '00000000-0000-0000-0003-000000000012', 'Summary Bot',     'summary-bot',     'dev')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- Users
-- ============================================================
-- All human actors authenticate via the dev OIDC provider.
-- external_sub corresponds to the IdP subject claim.
-- Admin roles require MFA enrollment (mfa_enrolled = TRUE).

INSERT INTO control_plane.users (id, tenant_id, external_sub, email, name, actor_type, mfa_enrolled) VALUES
    -- Platform (cross-tenant admin)
    ('00000000-0000-0000-0001-000000000101', '00000000-0000-0000-0001-000000000001',
        'sub|platform-admin',  'platform.admin@openllm-metrics.dev', 'Platform Admin', 'human', TRUE),

    -- Acme Corp
    ('00000000-0000-0000-0002-000000000101', '00000000-0000-0000-0002-000000000001',
        'sub|acme-admin',      'admin@acme.dev',    'Alice Admin',   'human', TRUE),
    ('00000000-0000-0000-0002-000000000102', '00000000-0000-0000-0002-000000000001',
        'sub|acme-sre',        'sre@acme.dev',      'Sam SRE',       'human', FALSE),
    ('00000000-0000-0000-0002-000000000103', '00000000-0000-0000-0002-000000000001',
        'sub|acme-finops',     'finops@acme.dev',   'Fiona FinOps',  'human', FALSE),
    ('00000000-0000-0000-0002-000000000104', '00000000-0000-0000-0002-000000000001',
        'sub|acme-viewer',     'viewer@acme.dev',   'Victor Viewer', 'human', FALSE),

    -- Beta Industries
    ('00000000-0000-0000-0003-000000000101', '00000000-0000-0000-0003-000000000001',
        'sub|beta-admin',      'admin@beta.dev',    'Bob Admin',     'human', TRUE),
    ('00000000-0000-0000-0003-000000000102', '00000000-0000-0000-0003-000000000001',
        'sub|beta-sre',        'sre@beta.dev',      'Sara SRE',      'human', FALSE),
    ('00000000-0000-0000-0003-000000000103', '00000000-0000-0000-0003-000000000001',
        'sub|beta-finops',     'finops@beta.dev',   'Frank FinOps',  'human', FALSE),
    ('00000000-0000-0000-0003-000000000104', '00000000-0000-0000-0003-000000000001',
        'sub|beta-viewer',     'viewer@beta.dev',   'Vivian Viewer', 'human', FALSE)
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- User Roles
-- ============================================================
-- Idempotent via WHERE NOT EXISTS (avoids duplicate-null PK issues
-- on the nullable team_id column in the unique constraint).

-- platform_admin grants have tenant_id IS NULL (cross-tenant role).
-- All other grants are tenant-scoped per the F005 CHECK constraint.
-- Idempotency uses WHERE NOT EXISTS with IS NOT DISTINCT FROM so that
-- NULL tenant_id and NULL team_id compare equal across re-runs.
INSERT INTO control_plane.user_roles (id, user_id, tenant_id, role, team_id)
SELECT gen_random_uuid(), v.user_id::UUID, v.tenant_id::UUID, v.role, v.team_id::UUID
FROM (VALUES
    -- Platform admin (cross-tenant; tenant_id NULL)
    ('00000000-0000-0000-0001-000000000101', NULL,                                   'platform_admin', NULL),
    -- Acme Corp
    ('00000000-0000-0000-0002-000000000101', '00000000-0000-0000-0002-000000000001', 'tenant_admin', NULL),
    ('00000000-0000-0000-0002-000000000102', '00000000-0000-0000-0002-000000000001', 'sre',          NULL),
    ('00000000-0000-0000-0002-000000000103', '00000000-0000-0000-0002-000000000001', 'finops',       NULL),
    ('00000000-0000-0000-0002-000000000104', '00000000-0000-0000-0002-000000000001', 'viewer',       NULL),
    -- Beta Industries
    ('00000000-0000-0000-0003-000000000101', '00000000-0000-0000-0003-000000000001', 'tenant_admin', NULL),
    ('00000000-0000-0000-0003-000000000102', '00000000-0000-0000-0003-000000000001', 'sre',          NULL),
    ('00000000-0000-0000-0003-000000000103', '00000000-0000-0000-0003-000000000001', 'finops',       NULL),
    ('00000000-0000-0000-0003-000000000104', '00000000-0000-0000-0003-000000000001', 'viewer',       NULL)
) AS v(user_id, tenant_id, role, team_id)
WHERE NOT EXISTS (
    SELECT 1 FROM control_plane.user_roles ur
    WHERE ur.user_id   = v.user_id::UUID
      AND ur.role      = v.role
      AND ur.tenant_id IS NOT DISTINCT FROM v.tenant_id::UUID
      AND ur.team_id   IS NOT DISTINCT FROM v.team_id::UUID
);

-- ============================================================
-- Service Principals
-- ============================================================
-- One principal per service per tenant.
-- secret_hash is NULL here — rotate via API after bootstrapping.
-- Scopes follow least-privilege per service role.

-- Note: the `scoring-worker` and `policy-evaluator` principals are not implemented here
-- roles (their workers ship in this repository) and are omitted from the
-- OSS demo seed.
INSERT INTO control_plane.service_principals (id, tenant_id, name, scopes, secret_hash) VALUES
    -- Acme Corp service principals
    ('00000000-0000-0000-0002-000000000201', '00000000-0000-0000-0002-000000000001',
        'gateway',          ARRAY['runtime:write', 'routing:read', 'policy:read'], NULL),
    ('00000000-0000-0000-0002-000000000202', '00000000-0000-0000-0002-000000000001',
        'usage-poller',     ARRAY['usage:write'],                                   NULL),

    -- Beta Industries service principals
    ('00000000-0000-0000-0003-000000000201', '00000000-0000-0000-0003-000000000001',
        'gateway',          ARRAY['runtime:write', 'routing:read', 'policy:read'], NULL),
    ('00000000-0000-0000-0003-000000000202', '00000000-0000-0000-0003-000000000001',
        'usage-poller',     ARRAY['usage:write'],                                   NULL)
ON CONFLICT (id) DO NOTHING;

COMMIT;

\echo 'Seed complete.'
