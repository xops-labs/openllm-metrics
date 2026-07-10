-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Governance demo seed for OpenLLM Metrics local development.
--
-- Populates the OSS-safe governance surfaces for the demo "Acme Corp" tenant so
-- the admin console screens render with data:
--   * control_plane.policies / policy_versions   -> /policies
--   * control_plane.notification_channels / rules -> /notifications
--   * audit.audit_entries                         -> /audit
--   * routing.routing_decisions                   -> /decisions
--
-- Prerequisites:
--   platform/db/control_plane/migrations applied (F029, F033, F036)
--   platform/db/audit/migrations applied (F031)
--   001_demo_data.sql applied (tenants exist)
--
-- Idempotent: safe to re-run. Run via: ./tools/scripts/seed.sh
-- Note: the audit hashes here are demo placeholders forming a linked chain for
-- display; real tamper-evident entries are written by the audit-service at
-- runtime, so `olm-audit verify` is expected to flag seeded rows.

\set ON_ERROR_STOP on
\set acme '00000000-0000-0000-0002-000000000001'

BEGIN;

SET LOCAL row_security = off;

-- ============================================================
-- Policies (F029 — storage only; no evaluation)
-- ============================================================

INSERT INTO control_plane.policies (id, tenant_id, name, current_version) VALUES
    ('00000000-0000-0000-0002-000000000301', :'acme', 'Prod budget guardrail', 1),
    ('00000000-0000-0000-0002-000000000302', :'acme', 'Model allowlist (prod)', 1),
    ('00000000-0000-0000-0002-000000000303', :'acme', 'Require PII redaction', 1)
ON CONFLICT (id) DO NOTHING;

INSERT INTO control_plane.policy_versions (id, policy_id, tenant_id, version, document, created_by, comment) VALUES
    ('00000000-0000-0000-0002-000000000401',
     '00000000-0000-0000-0002-000000000301', :'acme', 1,
     '{"id":"00000000-0000-0000-0002-000000000301","name":"Prod budget guardrail","description":"Monthly spend cap for production apps.","tenant":"acme","version":1,"effective_from":"2026-01-01T00:00:00Z","effective_to":null,"enabled":true,"scope":{"env":"prod"},"rules":[{"type":"budget","window":"monthly","limit_usd":5000,"on_exceed":"deny"}],"labels":{"owner":"platform-eng"}}'::jsonb,
     'admin@acme.dev', 'Initial version'),
    ('00000000-0000-0000-0002-000000000402',
     '00000000-0000-0000-0002-000000000302', :'acme', 1,
     '{"id":"00000000-0000-0000-0002-000000000302","name":"Model allowlist (prod)","description":"Only approved models may serve production traffic.","tenant":"acme","version":1,"effective_from":"2026-01-01T00:00:00Z","effective_to":null,"enabled":true,"scope":{"env":"prod"},"rules":[{"type":"model_access","allow":["openai:gpt-4o","anthropic:claude-sonnet-4-6"],"on_violation":"deny"}],"labels":{"owner":"platform-eng"}}'::jsonb,
     'admin@acme.dev', 'Initial version'),
    ('00000000-0000-0000-0002-000000000403',
     '00000000-0000-0000-0002-000000000303', :'acme', 1,
     '{"id":"00000000-0000-0000-0002-000000000303","name":"Require PII redaction","description":"Apps handling user data must enable redaction.","tenant":"acme","version":1,"effective_from":"2026-02-01T00:00:00Z","effective_to":null,"enabled":false,"scope":{"team":"analytics"},"rules":[{"type":"data_handling","require":"pii_redaction","on_violation":"warn"}],"labels":{"owner":"security"}}'::jsonb,
     'sre@acme.dev', 'Draft — not yet enabled')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- Notification channels + rules (F033)
-- ============================================================

INSERT INTO control_plane.notification_channels (id, tenant_id, name, kind, config) VALUES
    ('00000000-0000-0000-0002-000000000501', :'acme', 'Ops webhook', 'webhook',
        '{"url":"https://example.com/hooks/olm-ops"}'::jsonb),
    ('00000000-0000-0000-0002-000000000502', :'acme', 'FinOps email', 'smtp',
        '{"to":["finops@acme.dev"],"subject_prefix":"[OLM]"}'::jsonb)
ON CONFLICT (id) DO NOTHING;

INSERT INTO control_plane.notification_rules (id, tenant_id, name, match, channel_ids) VALUES
    ('00000000-0000-0000-0002-000000000601', :'acme', 'Budget breaches',
        '{"event":"budget.exceeded"}'::jsonb,
        ARRAY['00000000-0000-0000-0002-000000000501','00000000-0000-0000-0002-000000000502']::uuid[]),
    ('00000000-0000-0000-0002-000000000602', :'acme', 'Quota risk (high)',
        '{"event":"quota.risk","severity":"high"}'::jsonb,
        ARRAY['00000000-0000-0000-0002-000000000501']::uuid[])
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- Audit entries (F031) — demo linked chain for the /audit viewer
-- ============================================================

INSERT INTO audit.audit_entries (tenant_id, actor, action, resource, payload, prev_hash, entry_hash, created_at)
SELECT :'acme', v.actor::jsonb, v.action, v.resource::jsonb, '{}'::jsonb,
       sha256(convert_to(v.prev, 'UTF8')), sha256(convert_to(v.self, 'UTF8')),
       now() - (v.ago || ' minutes')::interval
FROM (VALUES
    ('{"id":"admin@acme.dev","type":"human"}', 'policy.create',  '{"type":"policy","id":"00000000-0000-0000-0002-000000000301","name":"Prod budget guardrail"}', 'genesis', 'e1', 90),
    ('{"id":"admin@acme.dev","type":"human"}', 'policy.create',  '{"type":"policy","id":"00000000-0000-0000-0002-000000000302","name":"Model allowlist (prod)"}', 'e1', 'e2', 60),
    ('{"id":"sre@acme.dev","type":"human"}',   'channel.create', '{"type":"notification_channel","id":"00000000-0000-0000-0002-000000000501","name":"Ops webhook"}', 'e2', 'e3', 30),
    ('{"id":"sre@acme.dev","type":"human"}',   'rule.create',    '{"type":"notification_rule","id":"00000000-0000-0000-0002-000000000601","name":"Budget breaches"}', 'e3', 'e4', 10)
) AS v(actor, action, resource, prev, self, ago)
WHERE NOT EXISTS (SELECT 1 FROM audit.audit_entries WHERE tenant_id = :'acme');

-- ============================================================
-- Routing decisions (F036 — explainability ledger; display only)
-- ============================================================
-- The OSS no-op decider records minimal entries; a real (custom) decider
-- writes richer reason chains. These demo rows let the /decisions screen render.

INSERT INTO routing.routing_decisions
    (decision_id, tenant_id, team, app, env, project,
     provider_requested, model_requested, route_requested, request_id_hash,
     provider_chosen, model_chosen, route_chosen,
     reason_chain, alternatives, decider_version, decided_at)
VALUES
    ('dec-acme-0001', :'acme', 'platform-eng', 'chat-assistant', 'prod', 'acme-chat',
     'openai', 'gpt-4o', '/v1/chat/completions', 'req-hash-0001',
     'openai', 'gpt-4o', '/v1/chat/completions',
     '[{"step":1,"factor":"primary","value":"primary target healthy"}]'::jsonb,
     '[{"provider":"anthropic","model":"claude-sonnet-4-6"}]'::jsonb,
     'oss-noop', now() - interval '20 minutes'),
    ('dec-acme-0002', :'acme', 'platform-eng', 'chat-assistant', 'prod', 'acme-chat',
     'openai', 'gpt-4o', '/v1/chat/completions', 'req-hash-0002',
     'anthropic', 'claude-sonnet-4-6', '/v1/messages',
     '[{"step":1,"factor":"fallback","value":"primary returned 429; fell back"}]'::jsonb,
     '[{"provider":"openai","model":"gpt-4o"}]'::jsonb,
     'oss-noop', now() - interval '12 minutes'),
    ('dec-acme-0003', :'acme', 'analytics', 'batch-processor', 'dev', 'acme-batch',
     'anthropic', 'claude-sonnet-4-6', '/v1/messages', 'req-hash-0003',
     'anthropic', 'claude-sonnet-4-6', '/v1/messages',
     '[{"step":1,"factor":"primary","value":"primary target healthy"}]'::jsonb,
     '[]'::jsonb,
     'oss-noop', now() - interval '5 minutes')
ON CONFLICT (decision_id) DO NOTHING;

COMMIT;

\echo 'Governance demo seed complete.'
