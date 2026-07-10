// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store wraps the Postgres surface the notifier needs:
//
//   - notification_channels    — CRUD + lookup-by-tenant
//   - notification_rules       — CRUD + lookup-by-tenant
//   - notification_deliveries  — append-only attempt ledger with an
//     (alert_event_id, channel_id) idempotency guard
//
// Every query is tenant-scoped. The CRUD operations are exposed to the HTTP
// server; the lookup + delivery operations are used by the bus consumer.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel is the in-memory shape of a control_plane.notification_channels row.
type Channel struct {
	ID        string
	TenantID  string
	Name      string
	Kind      string
	Config    json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Rule is the in-memory shape of a control_plane.notification_rules row.
type Rule struct {
	ID         string
	TenantID   string
	Name       string
	Match      json.RawMessage
	ChannelIDs []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Delivery is the in-memory shape of a control_plane.notification_deliveries
// row, returned from the read-history API.
type Delivery struct {
	ID           int64
	TenantID     string
	RuleID       string
	ChannelID    string
	AlertEventID string
	Status       string
	Attempts     int
	LastError    string
	SentAt       *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ErrNotFound is returned when a row is not present.
var ErrNotFound = errors.New("store: not found")

// ErrAlreadyDelivered is returned by ClaimDelivery when (alert_event_id,
// channel_id) already exists with status='success'. The caller treats this as
// an idempotency skip (not an error).
var ErrAlreadyDelivered = errors.New("store: already delivered")

// Store is the narrow surface the worker depends on.
type Store interface {
	// Channels.
	CreateChannel(ctx context.Context, ch *Channel) error
	GetChannel(ctx context.Context, tenantID, id string) (*Channel, error)
	ListChannels(ctx context.Context, tenantID string) ([]Channel, error)
	UpdateChannel(ctx context.Context, ch *Channel) error
	DeleteChannel(ctx context.Context, tenantID, id string) error

	// Rules.
	CreateRule(ctx context.Context, r *Rule) error
	GetRule(ctx context.Context, tenantID, id string) (*Rule, error)
	ListRules(ctx context.Context, tenantID string) ([]Rule, error)
	UpdateRule(ctx context.Context, r *Rule) error
	DeleteRule(ctx context.Context, tenantID, id string) error

	// Routing lookup (consumer path).
	ListRulesAndChannels(ctx context.Context, tenantID string) ([]Rule, map[string]Channel, error)

	// Delivery ledger (consumer path).
	ClaimDelivery(ctx context.Context, d Delivery) (int64, error)
	MarkDelivery(ctx context.Context, id int64, status, lastErr string, attempts int) error
	ListDeliveries(ctx context.Context, tenantID, ruleID string, from, to *time.Time) ([]Delivery, error)

	Close()
}

// PgStore is the Postgres-backed implementation.
type PgStore struct {
	pool *pgxpool.Pool
}

// New constructs a PgStore against the supplied DSN.
func New(ctx context.Context, dsn string) (*PgStore, error) {
	if dsn == "" {
		return nil, errors.New("store: dsn is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return &PgStore{pool: pool}, nil
}

// Close releases the underlying pool.
func (p *PgStore) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

// ----------------------------------------------------------------------
// Channels
// ----------------------------------------------------------------------

// CreateChannel inserts a channel. ID may be empty (DB default fills it).
func (p *PgStore) CreateChannel(ctx context.Context, ch *Channel) error {
	const q = `
		INSERT INTO control_plane.notification_channels
		    (id, tenant_id, name, kind, config)
		VALUES (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q, ch.ID, ch.TenantID, ch.Name, ch.Kind, []byte(ch.Config))
	return row.Scan(&ch.ID, &ch.CreatedAt, &ch.UpdatedAt)
}

// GetChannel fetches a non-deleted channel within the tenant.
func (p *PgStore) GetChannel(ctx context.Context, tenantID, id string) (*Channel, error) {
	const q = `
		SELECT id::TEXT, tenant_id::TEXT, name, kind, config, created_at, updated_at
		  FROM control_plane.notification_channels
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL
	`
	row := p.pool.QueryRow(ctx, q, tenantID, id)
	ch := &Channel{}
	var cfg []byte
	if err := row.Scan(&ch.ID, &ch.TenantID, &ch.Name, &ch.Kind, &cfg, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get channel: %w", err)
	}
	ch.Config = cfg
	return ch, nil
}

// ListChannels returns all non-deleted channels for the tenant.
func (p *PgStore) ListChannels(ctx context.Context, tenantID string) ([]Channel, error) {
	const q = `
		SELECT id::TEXT, tenant_id::TEXT, name, kind, config, created_at, updated_at
		  FROM control_plane.notification_channels
		 WHERE tenant_id = $1::uuid AND deleted_at IS NULL
		 ORDER BY created_at
	`
	rows, err := p.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list channels: %w", err)
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var ch Channel
		var cfg []byte
		if err := rows.Scan(&ch.ID, &ch.TenantID, &ch.Name, &ch.Kind, &cfg, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan channel: %w", err)
		}
		ch.Config = cfg
		out = append(out, ch)
	}
	return out, rows.Err()
}

// UpdateChannel mutates a non-deleted channel.
func (p *PgStore) UpdateChannel(ctx context.Context, ch *Channel) error {
	const q = `
		UPDATE control_plane.notification_channels
		   SET name = $3, kind = $4, config = $5, updated_at = NOW()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL
		RETURNING updated_at
	`
	row := p.pool.QueryRow(ctx, q, ch.TenantID, ch.ID, ch.Name, ch.Kind, []byte(ch.Config))
	if err := row.Scan(&ch.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("store: update channel: %w", err)
	}
	return nil
}

// DeleteChannel soft-deletes a channel.
func (p *PgStore) DeleteChannel(ctx context.Context, tenantID, id string) error {
	const q = `
		UPDATE control_plane.notification_channels
		   SET deleted_at = NOW(), updated_at = NOW()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL
	`
	tag, err := p.pool.Exec(ctx, q, tenantID, id)
	if err != nil {
		return fmt.Errorf("store: delete channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------
// Rules
// ----------------------------------------------------------------------

// CreateRule inserts a routing rule.
func (p *PgStore) CreateRule(ctx context.Context, r *Rule) error {
	const q = `
		INSERT INTO control_plane.notification_rules
		    (id, tenant_id, name, match, channel_ids)
		VALUES (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), $2, $3, $4, $5::uuid[])
		RETURNING id, created_at, updated_at
	`
	row := p.pool.QueryRow(ctx, q, r.ID, r.TenantID, r.Name, []byte(r.Match), r.ChannelIDs)
	return row.Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
}

// GetRule fetches a non-deleted rule within the tenant.
func (p *PgStore) GetRule(ctx context.Context, tenantID, id string) (*Rule, error) {
	const q = `
		SELECT id::TEXT, tenant_id::TEXT, name, match, channel_ids::TEXT[], created_at, updated_at
		  FROM control_plane.notification_rules
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL
	`
	row := p.pool.QueryRow(ctx, q, tenantID, id)
	r := &Rule{}
	var match []byte
	if err := row.Scan(&r.ID, &r.TenantID, &r.Name, &match, &r.ChannelIDs, &r.CreatedAt, &r.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get rule: %w", err)
	}
	r.Match = match
	return r, nil
}

// ListRules returns all non-deleted rules for the tenant.
func (p *PgStore) ListRules(ctx context.Context, tenantID string) ([]Rule, error) {
	const q = `
		SELECT id::TEXT, tenant_id::TEXT, name, match, channel_ids::TEXT[], created_at, updated_at
		  FROM control_plane.notification_rules
		 WHERE tenant_id = $1::uuid AND deleted_at IS NULL
		 ORDER BY created_at
	`
	rows, err := p.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("store: list rules: %w", err)
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var match []byte
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &match, &r.ChannelIDs, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan rule: %w", err)
		}
		r.Match = match
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRule mutates a non-deleted rule.
func (p *PgStore) UpdateRule(ctx context.Context, r *Rule) error {
	const q = `
		UPDATE control_plane.notification_rules
		   SET name = $3, match = $4, channel_ids = $5::uuid[], updated_at = NOW()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL
		RETURNING updated_at
	`
	row := p.pool.QueryRow(ctx, q, r.TenantID, r.ID, r.Name, []byte(r.Match), r.ChannelIDs)
	if err := row.Scan(&r.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("store: update rule: %w", err)
	}
	return nil
}

// DeleteRule soft-deletes a rule.
func (p *PgStore) DeleteRule(ctx context.Context, tenantID, id string) error {
	const q = `
		UPDATE control_plane.notification_rules
		   SET deleted_at = NOW(), updated_at = NOW()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND deleted_at IS NULL
	`
	tag, err := p.pool.Exec(ctx, q, tenantID, id)
	if err != nil {
		return fmt.Errorf("store: delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ----------------------------------------------------------------------
// Routing lookup
// ----------------------------------------------------------------------

// ListRulesAndChannels returns all non-deleted rules for the tenant plus a
// lookup map of every non-deleted channel keyed by ID. The map intentionally
// includes channels not referenced by any rule so a single query suffices.
func (p *PgStore) ListRulesAndChannels(ctx context.Context, tenantID string) ([]Rule, map[string]Channel, error) {
	rules, err := p.ListRules(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	chans, err := p.ListChannels(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	chanMap := make(map[string]Channel, len(chans))
	for _, c := range chans {
		chanMap[c.ID] = c
	}
	return rules, chanMap, nil
}

// ----------------------------------------------------------------------
// Delivery ledger
// ----------------------------------------------------------------------

// ClaimDelivery is the idempotency gate. It tries to INSERT a new pending row
// for (alert_event_id, channel_id). If a row already exists:
//   - status='success' → return ErrAlreadyDelivered (skip; do not re-send)
//   - any other status → return the existing row's id (the previous worker
//     died mid-retry; we may continue from there)
func (p *PgStore) ClaimDelivery(ctx context.Context, d Delivery) (int64, error) {
	const ins = `
		INSERT INTO control_plane.notification_deliveries
		    (tenant_id, rule_id, channel_id, alert_event_id, status, attempts)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'pending', 0)
		ON CONFLICT (alert_event_id, channel_id) DO NOTHING
		RETURNING id
	`
	var id int64
	err := p.pool.QueryRow(ctx, ins, d.TenantID, d.RuleID, d.ChannelID, d.AlertEventID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("store: claim delivery insert: %w", err)
	}

	// Row already exists. Read its current state.
	const sel = `
		SELECT id, status
		  FROM control_plane.notification_deliveries
		 WHERE alert_event_id = $1 AND channel_id = $2::uuid
	`
	var status string
	if err := p.pool.QueryRow(ctx, sel, d.AlertEventID, d.ChannelID).Scan(&id, &status); err != nil {
		return 0, fmt.Errorf("store: claim delivery lookup: %w", err)
	}
	if status == "success" {
		return id, ErrAlreadyDelivered
	}
	return id, nil
}

// MarkDelivery updates a delivery row with its terminal (or transitional)
// state. sent_at is stamped when status == 'success'.
func (p *PgStore) MarkDelivery(ctx context.Context, id int64, status, lastErr string, attempts int) error {
	const q = `
		UPDATE control_plane.notification_deliveries
		   SET status     = $2,
		       last_error = $3,
		       attempts   = $4,
		       sent_at    = CASE WHEN $2 = 'success' THEN NOW() ELSE sent_at END,
		       updated_at = NOW()
		 WHERE id = $1
	`
	tag, err := p.pool.Exec(ctx, q, id, status, lastErr, attempts)
	if err != nil {
		return fmt.Errorf("store: mark delivery: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListDeliveries returns delivery history with optional filters.
func (p *PgStore) ListDeliveries(ctx context.Context, tenantID, ruleID string, from, to *time.Time) ([]Delivery, error) {
	const q = `
		SELECT id, tenant_id::TEXT, rule_id::TEXT, channel_id::TEXT,
		       alert_event_id, status, attempts, last_error, sent_at,
		       created_at, updated_at
		  FROM control_plane.notification_deliveries
		 WHERE tenant_id = $1::uuid
		   AND ($2 = '' OR rule_id = NULLIF($2,'')::uuid)
		   AND ($3::timestamptz IS NULL OR created_at >= $3::timestamptz)
		   AND ($4::timestamptz IS NULL OR created_at <= $4::timestamptz)
		 ORDER BY created_at DESC
		 LIMIT 500
	`
	rows, err := p.pool.Query(ctx, q, tenantID, ruleID, from, to)
	if err != nil {
		return nil, fmt.Errorf("store: list deliveries: %w", err)
	}
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.TenantID, &d.RuleID, &d.ChannelID,
			&d.AlertEventID, &d.Status, &d.Attempts, &d.LastError, &d.SentAt,
			&d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: scan delivery: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
