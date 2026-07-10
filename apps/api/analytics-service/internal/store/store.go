// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store is the Postgres persistence layer for the F038 analytics
// saved-views service: rows in control_plane.analytics_saved_views.
//
// All queries scope by tenant_id at the application layer; Postgres RLS
// provides a second line of defense via current_setting('app.tenant_id').
// Reads and writes run inside a transaction that first sets app.tenant_id so
// the analytics_saved_views_tenant_isolation policy permits exactly the
// caller's rows — a tenant can never read or delete another tenant's views.
//
// This package does NOT execute analytics queries, score series, or apply
// routing/anomaly logic — it only reads and writes the declarative view spec.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when the requested view does not exist within the
// tenant's scope. Handlers map this to HTTP 404.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when the requested write would violate a unique
// constraint (duplicate (tenant_id, name)). Handlers map this to HTTP 409.
var ErrConflict = errors.New("store: conflict")

// SavedView is the in-memory shape of a control_plane.analytics_saved_views
// row. The JSON tags match the admin console's SavedView contract
// (apps/web/admin-console/lib/api/saved-views.ts) exactly: id, name,
// description, spec, position. spec is stored as JSONB and passed through
// verbatim as a raw JSON object.
type SavedView struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Spec        json.RawMessage `json:"spec"`
	Position    int             `json:"position"`
}

// CreateInput is the validated input for Create. Spec is the declarative
// llm_* selector spec persisted as JSONB.
type CreateInput struct {
	Name        string
	Description string
	Spec        json.RawMessage
	Position    int
}

// Store wraps a pgxpool.Pool with the queries the analytics service needs.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// List returns all non-deleted saved views for the tenant, ordered by
// position then name. Read inside a tenant-scoped transaction so RLS permits
// the rows.
func (s *Store) List(ctx context.Context, tenantID uuid.UUID) ([]SavedView, error) {
	tx, err := s.beginTenantTx(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, name, description, spec, position
		FROM control_plane.analytics_saved_views
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY position, name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list saved views: %w", err)
	}
	defer rows.Close()

	out := make([]SavedView, 0)
	for rows.Next() {
		var v SavedView
		var spec []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &spec, &v.Position); err != nil {
			return nil, fmt.Errorf("scan saved view: %w", err)
		}
		v.Spec = json.RawMessage(spec)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved views: %w", err)
	}
	return out, nil
}

// Create inserts a new saved view for the tenant and returns the persisted
// row (including its generated id). A duplicate (tenant_id, name) returns
// ErrConflict. The write runs inside a tenant-scoped transaction so RLS's
// WITH CHECK clause permits the insert.
func (s *Store) Create(ctx context.Context, tenantID uuid.UUID, in CreateInput) (*SavedView, error) {
	tx, err := s.beginTenantTx(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	id := uuid.New()
	now := time.Now().UTC()
	spec := in.Spec
	if len(spec) == 0 {
		spec = json.RawMessage(`{}`)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO control_plane.analytics_saved_views
		    (id, tenant_id, name, spec, description, position, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
	`, id, tenantID, in.Name, spec, in.Description, in.Position, now)
	if err != nil {
		return nil, classify(fmt.Errorf("insert saved view: %w", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &SavedView{
		ID:          id,
		Name:        in.Name,
		Description: in.Description,
		Spec:        spec,
		Position:    in.Position,
	}, nil
}

// SoftDelete marks a saved view as deleted (deleted_at = NOW()) so the row
// remains for audit but disappears from List. Returns ErrNotFound when no
// matching, not-already-deleted row exists for the tenant.
func (s *Store) SoftDelete(ctx context.Context, tenantID, id uuid.UUID) error {
	tx, err := s.beginTenantTx(ctx, tenantID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := tx.Exec(ctx, `
		UPDATE control_plane.analytics_saved_views
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, id, tenantID)
	if err != nil {
		return fmt.Errorf("soft delete saved view: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// beginTenantTx starts a transaction and sets app.tenant_id (transaction-local)
// so the analytics_saved_views_tenant_isolation RLS policy permits exactly the
// caller's rows for the reads/writes that follow.
func (s *Store) beginTenantTx(ctx context.Context, tenantID uuid.UUID) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String()); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set tenant context: %w", err)
	}
	return tx, nil
}

// classify maps Postgres unique-violation errors to ErrConflict so handlers
// can return HTTP 409.
//
// pgx wraps the protocol error in *pgconn.PgError; we string-match the
// SQLSTATE substring to avoid pulling in pgconn just for one constant.
// 23505 = unique_violation per the Postgres SQLSTATE catalog.
func classify(wrapped error) error {
	if wrapped == nil {
		return nil
	}
	if strings.Contains(wrapped.Error(), "23505") {
		return ErrConflict
	}
	return wrapped
}
