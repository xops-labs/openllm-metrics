// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store wraps the Postgres surface the cost-mapper writes to:
// control_plane.cost_reconciliation_drift (defined by the F017 migration
// at platform/db/control_plane/migrations/2026051801_f017_cost_reconciliation_drift.sql).
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DriftRow is the persistent form of one cost_reconciliation_drift row.
type DriftRow struct {
	TenantID                 string
	Team                     string
	App                      string
	Env                      string
	Project                  string
	Provider                 string
	Model                    string
	PeriodStart              time.Time
	PeriodEnd                time.Time
	EstimatedCostMinorUnits  int64
	ReconciledCostMinorUnits int64
	DriftMinorUnits          int64
	DriftRatio               float64
	CatalogVersion           string
	CorrelationKey           string
}

// DriftStore is the narrow interface the reconciler depends on.
type DriftStore interface {
	UpsertDrift(ctx context.Context, row DriftRow) error
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

// UpsertDrift writes one drift row, idempotent on
// (tenant_id, provider, model, period_start, period_end). The same correlation
// key replayed produces exactly one row whose estimated/reconciled fields
// converge to the latest known values.
func (p *PgStore) UpsertDrift(ctx context.Context, row DriftRow) error {
	const sql = `
		INSERT INTO control_plane.cost_reconciliation_drift (
		    tenant_id,
		    team,
		    app,
		    env,
		    project,
		    provider,
		    model,
		    period_start,
		    period_end,
		    estimated_cost_usd_minor_units,
		    reconciled_cost_usd_minor_units,
		    drift_usd_minor_units,
		    drift_ratio,
		    catalog_version,
		    correlation_key
		) VALUES (
		    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
		ON CONFLICT (tenant_id, provider, model, period_start, period_end)
		DO UPDATE SET
		    team                           = EXCLUDED.team,
		    app                            = EXCLUDED.app,
		    env                            = EXCLUDED.env,
		    project                        = EXCLUDED.project,
		    estimated_cost_usd_minor_units = EXCLUDED.estimated_cost_usd_minor_units,
		    reconciled_cost_usd_minor_units= EXCLUDED.reconciled_cost_usd_minor_units,
		    drift_usd_minor_units          = EXCLUDED.drift_usd_minor_units,
		    drift_ratio                    = EXCLUDED.drift_ratio,
		    catalog_version                = EXCLUDED.catalog_version,
		    correlation_key                = EXCLUDED.correlation_key,
		    updated_at                     = NOW()
	`
	_, err := p.pool.Exec(ctx, sql,
		row.TenantID,
		row.Team,
		row.App,
		row.Env,
		row.Project,
		row.Provider,
		row.Model,
		row.PeriodStart,
		row.PeriodEnd,
		row.EstimatedCostMinorUnits,
		row.ReconciledCostMinorUnits,
		row.DriftMinorUnits,
		row.DriftRatio,
		row.CatalogVersion,
		row.CorrelationKey,
	)
	if err != nil {
		return fmt.Errorf("store: upsert drift %s: %w", row.CorrelationKey, err)
	}
	return nil
}

// Close releases the underlying pool.
func (p *PgStore) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}
