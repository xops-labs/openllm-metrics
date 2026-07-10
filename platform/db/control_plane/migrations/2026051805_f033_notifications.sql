-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Migration: F033 — Notification and Alerting Fan-Out (OSS).
--
-- Three tables in the control_plane schema:
--   * notification_channels — per-tenant sink configurations (webhook|smtp).
--   * notification_rules    — per-tenant match → channel routing rules.
--   * notification_deliveries — append-only delivery attempt ledger, used for
--                               idempotency and operator audit.
--
-- OSS scope is intentionally narrow: generic outbound webhook and SMTP only.
-- Slack, PagerDuty, Teams, ServiceNow, etc. are custom integrations and
-- are intentionally not implemented here (Phase I). The 'kind' CHECK constraint
-- below is the hard wall that keeps OSS from accidentally shipping a Slack
-- adapter — adding a new kind is a deliberate migration in custom that
-- relaxes the constraint, not a config change here.
--
-- Multi-tenant invariants:
--   - Every row carries tenant_id.
--   - RLS is enabled and forced on all three tables.
--   - notification_deliveries unique constraint on (alert_event_id, channel_id)
--     enforces idempotent delivery: re-emitting the same alert never re-sends
--     to the same channel.
--
-- Secrets:
--   - notification_channels.config holds public configuration only.
--   - SMTP passwords are referenced via a `password_ref` string. The notifier
--     resolves the ref against an env-var indirection (OLM_SECRET_<REF>) at
--     send time. No password ever lives in this table.
--   - Webhook HMAC secrets ARE stored in config.secret_hmac for OSS
--     simplicity; column comments below mark this as the OSS posture.
--     custom (Phase J) replaces this with HSM-backed secret references.
--
-- Tool: goose
-- Direction: Up

-- +goose Up
-- +goose StatementBegin

-- ------------------------------------------------------------------
-- notification_channels
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS control_plane.notification_channels (
    id          UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    kind        TEXT        NOT NULL
                    CHECK (kind IN ('webhook', 'smtp')),
    config      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS notification_channels_tenant_idx
    ON control_plane.notification_channels (tenant_id)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE control_plane.notification_channels IS
    'F033 OSS sink configurations. kind is restricted to webhook|smtp; Slack/PagerDuty/Teams/ServiceNow are custom.';
COMMENT ON COLUMN control_plane.notification_channels.config IS
    'Public configuration only. SMTP password is referenced via config.password_ref → OLM_SECRET_<REF> env. Webhook HMAC secret stored inline for OSS; custom replaces with HSM ref.';

-- ------------------------------------------------------------------
-- notification_rules
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS control_plane.notification_rules (
    id           UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    match        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    channel_ids  UUID[]      NOT NULL DEFAULT ARRAY[]::UUID[],
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS notification_rules_tenant_idx
    ON control_plane.notification_rules (tenant_id)
    WHERE deleted_at IS NULL;

COMMENT ON TABLE control_plane.notification_rules IS
    'F033 OSS routing rules. match is a JSON document with severity[] and source[] arrays; semantic match is performed in the notifier worker.';
COMMENT ON COLUMN control_plane.notification_rules.channel_ids IS
    'Fan-out targets. Foreign-key integrity is enforced in the application layer; soft-deleted channels are skipped at send time.';

-- ------------------------------------------------------------------
-- notification_deliveries
-- ------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS control_plane.notification_deliveries (
    id              BIGSERIAL  NOT NULL PRIMARY KEY,
    tenant_id       UUID       NOT NULL REFERENCES control_plane.tenants(id) ON DELETE CASCADE,
    rule_id         UUID       NOT NULL REFERENCES control_plane.notification_rules(id) ON DELETE CASCADE,
    channel_id      UUID       NOT NULL REFERENCES control_plane.notification_channels(id) ON DELETE CASCADE,
    alert_event_id  TEXT       NOT NULL,
    status          TEXT       NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'success', 'failure', 'retrying')),
    attempts        INTEGER    NOT NULL DEFAULT 0
                        CHECK (attempts >= 0),
    last_error      TEXT       NOT NULL DEFAULT '',
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (alert_event_id, channel_id)
);

CREATE INDEX IF NOT EXISTS notification_deliveries_tenant_idx
    ON control_plane.notification_deliveries (tenant_id);
CREATE INDEX IF NOT EXISTS notification_deliveries_rule_idx
    ON control_plane.notification_deliveries (rule_id);
CREATE INDEX IF NOT EXISTS notification_deliveries_status_idx
    ON control_plane.notification_deliveries (status);

COMMENT ON TABLE control_plane.notification_deliveries IS
    'F033 append-only delivery ledger. UNIQUE (alert_event_id, channel_id) enforces idempotent fan-out: replays of the same alert never double-send to the same channel.';

-- ------------------------------------------------------------------
-- Row-level security
-- ------------------------------------------------------------------
ALTER TABLE control_plane.notification_channels    ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.notification_channels    FORCE  ROW LEVEL SECURITY;
ALTER TABLE control_plane.notification_rules       ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.notification_rules       FORCE  ROW LEVEL SECURITY;
ALTER TABLE control_plane.notification_deliveries  ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_plane.notification_deliveries  FORCE  ROW LEVEL SECURITY;

CREATE POLICY notification_channels_tenant_isolation
    ON control_plane.notification_channels
    USING      (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

CREATE POLICY notification_rules_tenant_isolation
    ON control_plane.notification_rules
    USING      (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

CREATE POLICY notification_deliveries_tenant_isolation
    ON control_plane.notification_deliveries
    USING      (tenant_id::TEXT = current_setting('app.tenant_id', true))
    WITH CHECK (tenant_id::TEXT = current_setting('app.tenant_id', true));

-- +goose StatementEnd


-- Copyright 2026 Yasvanth Udayakumar
-- Licensed under the Apache License, Version 2.0.
--
-- Down migration for 2026051805_f033_notifications.up.sql.

-- +goose Down
-- +goose StatementBegin

DROP POLICY IF EXISTS notification_deliveries_tenant_isolation
    ON control_plane.notification_deliveries;
DROP POLICY IF EXISTS notification_rules_tenant_isolation
    ON control_plane.notification_rules;
DROP POLICY IF EXISTS notification_channels_tenant_isolation
    ON control_plane.notification_channels;

DROP TABLE IF EXISTS control_plane.notification_deliveries;
DROP TABLE IF EXISTS control_plane.notification_rules;
DROP TABLE IF EXISTS control_plane.notification_channels;

-- +goose StatementEnd
