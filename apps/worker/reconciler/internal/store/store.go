// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store wraps the Postgres surface the reconciler writes to:
// control_plane.reconciliation_results (defined by the F023 migration at
// platform/db/control_plane/migrations/2026051806_f023_reconciliation.sql).
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status is the lifecycle marker recorded on every row.
type Status string

const (
	// StatusOpen — current cycle is still inside the window (or inside
	// the grace period waiting for the late side).
	StatusOpen Status = "open"
	// StatusClosed — window_end + grace has elapsed; closer flipped the
	// row out of 'open' but the side-balance decision is deferred.
	StatusClosed Status = "closed"
	// StatusReconciled — closed with both estimated and reconciled
	// contributions present.
	StatusReconciled Status = "reconciled"
	// StatusUnreconciled — closed with exactly one side present (the
	// other never arrived inside the grace period).
	StatusUnreconciled Status = "unreconciled"
)

// Row is the persistent form of one reconciliation_results row.
type Row struct {
	ID                int64
	TenantID          string
	Team              string
	App               string
	Env               string
	Project           string
	Provider          string
	Model             string
	WindowStart       time.Time
	WindowEnd         time.Time
	EstimatedCostUSD  float64
	ReconciledCostUSD float64
	DriftUSD          float64
	DriftRatio        float64
	Status            Status
}

// Store is the narrow interface the joiner + closer depend on.
type Store interface {
	UpsertResult(ctx context.Context, row Row) error
	ScanClosable(ctx context.Context, cutoff time.Time, limit int) ([]Row, error)
	MarkStatus(ctx context.Context, id int64, status Status) error
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

// UpsertResult writes one reconciliation row, idempotent on
// (tenant_id, provider, model, window_start). The same window replayed
// produces exactly one row whose cost columns converge to the latest known
// values. The status is NOT overwritten by an upsert from the joiner —
// only the closer flips it to closed/reconciled/unreconciled.
func (p *PgStore) UpsertResult(ctx context.Context, row Row) error {
	const sql = `
		INSERT INTO control_plane.reconciliation_results (
		    tenant_id,
		    team,
		    app,
		    env,
		    project,
		    provider,
		    model,
		    window_start,
		    window_end,
		    estimated_cost_usd,
		    reconciled_cost_usd,
		    drift_usd,
		    drift_ratio,
		    status
		) VALUES (
		    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (tenant_id, provider, model, window_start)
		DO UPDATE SET
		    team                = EXCLUDED.team,
		    app                 = EXCLUDED.app,
		    env                 = EXCLUDED.env,
		    project             = EXCLUDED.project,
		    window_end          = EXCLUDED.window_end,
		    estimated_cost_usd  = EXCLUDED.estimated_cost_usd,
		    reconciled_cost_usd = EXCLUDED.reconciled_cost_usd,
		    drift_usd           = EXCLUDED.drift_usd,
		    drift_ratio         = EXCLUDED.drift_ratio,
		    updated_at          = NOW()
		WHERE control_plane.reconciliation_results.status = 'open'
	`
	_, err := p.pool.Exec(ctx, sql,
		row.TenantID,
		row.Team,
		row.App,
		row.Env,
		row.Project,
		row.Provider,
		row.Model,
		row.WindowStart,
		row.WindowEnd,
		row.EstimatedCostUSD,
		row.ReconciledCostUSD,
		row.DriftUSD,
		row.DriftRatio,
		string(row.Status),
	)
	if err != nil {
		return fmt.Errorf("store: upsert reconciliation %s/%s/%s/%s: %w",
			row.TenantID, row.Provider, row.Model, row.WindowStart.Format(time.RFC3339), err)
	}
	return nil
}

// ScanClosable returns up to `limit` 'open' rows whose window_end is at or
// before `cutoff`. The closer uses this to find windows past the grace
// horizon.
func (p *PgStore) ScanClosable(ctx context.Context, cutoff time.Time, limit int) ([]Row, error) {
	const sql = `
		SELECT id,
		       tenant_id::TEXT,
		       team,
		       app,
		       env,
		       project,
		       provider,
		       model,
		       window_start,
		       window_end,
		       estimated_cost_usd,
		       reconciled_cost_usd,
		       drift_usd,
		       drift_ratio,
		       status
		  FROM control_plane.reconciliation_results
		 WHERE status = 'open'
		   AND window_end <= $1
		 ORDER BY window_end ASC
		 LIMIT $2
	`
	rows, err := p.pool.Query(ctx, sql, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("store: scan closable: %w", err)
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		var status string
		if err := rows.Scan(
			&r.ID,
			&r.TenantID,
			&r.Team,
			&r.App,
			&r.Env,
			&r.Project,
			&r.Provider,
			&r.Model,
			&r.WindowStart,
			&r.WindowEnd,
			&r.EstimatedCostUSD,
			&r.ReconciledCostUSD,
			&r.DriftUSD,
			&r.DriftRatio,
			&status,
		); err != nil {
			return nil, fmt.Errorf("store: scan row: %w", err)
		}
		r.Status = Status(status)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: scan iter: %w", err)
	}
	return out, nil
}

// MarkStatus flips a row's status without touching any cost column.
func (p *PgStore) MarkStatus(ctx context.Context, id int64, status Status) error {
	const sql = `
		UPDATE control_plane.reconciliation_results
		   SET status     = $2,
		       updated_at = NOW()
		 WHERE id = $1
	`
	_, err := p.pool.Exec(ctx, sql, id, string(status))
	if err != nil {
		return fmt.Errorf("store: mark status id=%d → %s: %w", id, status, err)
	}
	return nil
}

// Close releases the underlying pool.
func (p *PgStore) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

// IsNoRows is a convenience for callers that want to special-case empty
// query results.
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
