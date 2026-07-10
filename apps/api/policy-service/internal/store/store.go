// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store is the Postgres persistence layer for the F029 policy
// service: header rows in control_plane.policies and append-only version
// history in control_plane.policy_versions.
//
// All queries scope by tenant_id at the application layer; Postgres RLS
// provides a second line of defense via current_setting('app.tenant_id').
// This package does NOT evaluate policies — it only reads and writes data.
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

// ErrNotFound is returned when the requested policy or version does not
// exist within the tenant's scope. Handlers map this to HTTP 404.
var ErrNotFound = errors.New("store: not found")

// ErrConflict is returned when the requested write would violate a unique
// constraint (e.g. duplicate (tenant_id, name)). Handlers map this to HTTP 409.
var ErrConflict = errors.New("store: conflict")

// PolicyHeader is the in-memory shape of a control_plane.policies row.
type PolicyHeader struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	Name           string     `json:"name"`
	CurrentVersion int        `json:"current_version"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

// PolicyVersion is the in-memory shape of a control_plane.policy_versions row.
type PolicyVersion struct {
	ID        uuid.UUID       `json:"id"`
	PolicyID  uuid.UUID       `json:"policy_id"`
	TenantID  uuid.UUID       `json:"tenant_id"`
	Version   int             `json:"version"`
	Document  json.RawMessage `json:"document"`
	CreatedBy string          `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
	Comment   string          `json:"comment"`
}

// Store wraps a pgxpool.Pool with the queries the policy service needs.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by the given connection pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreatePolicy inserts a new policy header and its first version in a single
// transaction. Returns the (header, version) pair on success.
func (s *Store) CreatePolicy(
	ctx context.Context,
	tenantID uuid.UUID,
	name string,
	document json.RawMessage,
	createdBy string,
	comment string,
) (*PolicyHeader, *PolicyVersion, error) {
	tx, err := s.beginTenantTx(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	policyID := uuid.New()
	versionID := uuid.New()
	now := time.Now().UTC()

	_, err = tx.Exec(ctx, `
		INSERT INTO control_plane.policies
		    (id, tenant_id, name, current_version, created_at, updated_at)
		VALUES ($1, $2, $3, 1, $4, $4)
	`, policyID, tenantID, name, now)
	if err != nil {
		return nil, nil, classify(fmt.Errorf("insert policy: %w", err))
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO control_plane.policy_versions
		    (id, policy_id, tenant_id, version, document, created_by, created_at, comment)
		VALUES ($1, $2, $3, 1, $4, $5, $6, $7)
	`, versionID, policyID, tenantID, document, createdBy, now, comment)
	if err != nil {
		return nil, nil, fmt.Errorf("insert policy_version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}

	hdr := &PolicyHeader{
		ID: policyID, TenantID: tenantID, Name: name,
		CurrentVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	ver := &PolicyVersion{
		ID: versionID, PolicyID: policyID, TenantID: tenantID, Version: 1,
		Document: document, CreatedBy: createdBy, CreatedAt: now, Comment: comment,
	}
	return hdr, ver, nil
}

// AppendVersion writes a new version row for an existing policy and updates
// the header's current_version pointer atomically. Returns the new version.
func (s *Store) AppendVersion(
	ctx context.Context,
	tenantID uuid.UUID,
	policyID uuid.UUID,
	document json.RawMessage,
	createdBy string,
	comment string,
) (*PolicyVersion, error) {
	tx, err := s.beginTenantTx(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int
	err = tx.QueryRow(ctx, `
		SELECT current_version
		FROM control_plane.policies
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		FOR UPDATE
	`, policyID, tenantID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select header: %w", err)
	}

	next := current + 1
	now := time.Now().UTC()
	versionID := uuid.New()

	_, err = tx.Exec(ctx, `
		INSERT INTO control_plane.policy_versions
		    (id, policy_id, tenant_id, version, document, created_by, created_at, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, versionID, policyID, tenantID, next, document, createdBy, now, comment)
	if err != nil {
		return nil, fmt.Errorf("insert version: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE control_plane.policies
		SET current_version = $1, updated_at = $2
		WHERE id = $3 AND tenant_id = $4
	`, next, now, policyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("update header: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &PolicyVersion{
		ID: versionID, PolicyID: policyID, TenantID: tenantID, Version: next,
		Document: document, CreatedBy: createdBy, CreatedAt: now, Comment: comment,
	}, nil
}

// GetCurrent returns the most recent (header, version) pair for a policy.
func (s *Store) GetCurrent(ctx context.Context, tenantID, policyID uuid.UUID) (*PolicyHeader, *PolicyVersion, error) {
	if err := s.setTenantContext(ctx, tenantID); err != nil {
		return nil, nil, err
	}
	hdr, err := s.getHeader(ctx, tenantID, policyID)
	if err != nil {
		return nil, nil, err
	}
	ver, err := s.GetVersion(ctx, tenantID, policyID, hdr.CurrentVersion)
	if err != nil {
		return nil, nil, err
	}
	return hdr, ver, nil
}

// GetVersion returns a specific version of a policy.
func (s *Store) GetVersion(ctx context.Context, tenantID, policyID uuid.UUID, version int) (*PolicyVersion, error) {
	if err := s.setTenantContext(ctx, tenantID); err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx, `
		SELECT id, policy_id, tenant_id, version, document, created_by, created_at, comment
		FROM control_plane.policy_versions
		WHERE policy_id = $1 AND tenant_id = $2 AND version = $3
	`, policyID, tenantID, version)

	var v PolicyVersion
	var doc []byte
	if err := row.Scan(&v.ID, &v.PolicyID, &v.TenantID, &v.Version, &doc, &v.CreatedBy, &v.CreatedAt, &v.Comment); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("select version: %w", err)
	}
	v.Document = json.RawMessage(doc)
	return &v, nil
}

// ListVersions returns all versions (newest first) for a policy.
func (s *Store) ListVersions(ctx context.Context, tenantID, policyID uuid.UUID) ([]PolicyVersion, error) {
	if err := s.setTenantContext(ctx, tenantID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, policy_id, tenant_id, version, document, created_by, created_at, comment
		FROM control_plane.policy_versions
		WHERE policy_id = $1 AND tenant_id = $2
		ORDER BY version DESC
	`, policyID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	var out []PolicyVersion
	for rows.Next() {
		var v PolicyVersion
		var doc []byte
		if err := rows.Scan(&v.ID, &v.PolicyID, &v.TenantID, &v.Version, &doc, &v.CreatedBy, &v.CreatedAt, &v.Comment); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		v.Document = json.RawMessage(doc)
		out = append(out, v)
	}
	return out, rows.Err()
}

// SoftDelete marks a policy as deleted. Rows remain queryable for audit.
func (s *Store) SoftDelete(ctx context.Context, tenantID, policyID uuid.UUID) error {
	if err := s.setTenantContext(ctx, tenantID); err != nil {
		return err
	}
	res, err := s.pool.Exec(ctx, `
		UPDATE control_plane.policies
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, policyID, tenantID)
	if err != nil {
		return fmt.Errorf("soft delete: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// getHeader looks up the policy header row and translates pgx.ErrNoRows into
// ErrNotFound. Assumes the tenant context has already been set.
func (s *Store) getHeader(ctx context.Context, tenantID, policyID uuid.UUID) (*PolicyHeader, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, current_version, created_at, updated_at, deleted_at
		FROM control_plane.policies
		WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
	`, policyID, tenantID)

	var h PolicyHeader
	if err := row.Scan(&h.ID, &h.TenantID, &h.Name, &h.CurrentVersion, &h.CreatedAt, &h.UpdatedAt, &h.DeletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("select header: %w", err)
	}
	return &h, nil
}

// ListPolicies returns all non-deleted policy headers for the tenant, ordered
// by name. Read inside a tenant-scoped transaction so RLS permits the rows.
func (s *Store) ListPolicies(ctx context.Context, tenantID uuid.UUID) ([]PolicyHeader, error) {
	tx, err := s.beginTenantTx(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, name, current_version, created_at, updated_at, deleted_at
		FROM control_plane.policies
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()

	out := make([]PolicyHeader, 0)
	for rows.Next() {
		var h PolicyHeader
		if err := rows.Scan(&h.ID, &h.TenantID, &h.Name, &h.CurrentVersion, &h.CreatedAt, &h.UpdatedAt, &h.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan policy header: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate policies: %w", err)
	}
	return out, nil
}

// beginTenantTx starts a transaction and sets app.tenant_id so RLS will
// permit the writes that follow.
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

// setTenantContext sets the RLS tenant key for read-only queries that don't
// open their own transaction. For pool sessions this is best-effort: a fresh
// session may not retain the setting. Production deployments should wrap
// reads in a transaction with set_config('app.tenant_id', ..., true).
func (s *Store) setTenantContext(ctx context.Context, tenantID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, "SELECT set_config('app.tenant_id', $1, false)", tenantID.String())
	if err != nil {
		return fmt.Errorf("set tenant context: %w", err)
	}
	return nil
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
