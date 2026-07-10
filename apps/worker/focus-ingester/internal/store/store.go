// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store wraps the two Postgres surfaces the FOCUS ingester needs:
// the label_mappings lookup (re-using the same shape as the label
// translator) and the focus_records append-only writer.
package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Mapping is the resolution result for a (provider, tenant_external_id,
// tenancy_id) tuple. Identical shape to the label-translator's mapping;
// duplicated here so the two workers can evolve independently.
type Mapping struct {
	Found            bool
	TenantID         string
	TenantSlug       string
	TeamSlug         string
	AppSlug          string
	CanonicalEnv     string
	CanonicalProject string
	CanonicalRegion  string
}

// Key is the natural-key tuple matching control_plane.label_mappings.
type Key struct {
	Provider         string
	TenantExternalID string
	TenancyID        string
}

// FocusRow is the persistent form of a single FOCUS line item.
type FocusRow struct {
	TenantID                    string
	SourceEventID               string
	Provider                    string
	Model                       string
	BillingAccountID            string
	InvoiceID                   string
	ServiceName                 string
	ChargeCategory              string
	ReconciledCostUSDMinorUnits int64
	ListCostUSDMinorUnits       int64
	PricingCurrency             string
	PeriodStart                 time.Time
	PeriodEnd                   time.Time
	RawFocus                    []byte
}

// Store is the narrow interface the ingester depends on.
type Store interface {
	Lookup(ctx context.Context, k Key) (Mapping, error)
	InsertFocus(ctx context.Context, row FocusRow) error
	Close()
}

// PgStore is the Postgres-backed implementation.
type PgStore struct {
	pool *pgxpool.Pool

	mu    sync.RWMutex
	ttl   time.Duration
	cache map[Key]cacheEntry
	nowFn func() time.Time
}

type cacheEntry struct {
	value     Mapping
	expiresAt time.Time
}

// New constructs a PgStore against the supplied DSN.
func New(ctx context.Context, dsn string, ttl time.Duration) (*PgStore, error) {
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
	return &PgStore{
		pool:  pool,
		ttl:   ttl,
		cache: make(map[Key]cacheEntry, 64),
		nowFn: func() time.Time { return time.Now() },
	}, nil
}

// Lookup resolves k against label_mappings. Returns Found=false on a clean
// no-rows; only database failures produce a non-nil error.
func (p *PgStore) Lookup(ctx context.Context, k Key) (Mapping, error) {
	if v, ok := p.cacheGet(k); ok {
		return v, nil
	}

	const query = `
		SELECT lm.tenant_id::TEXT,
		       t.slug,
		       tm.slug,
		       COALESCE(a.slug, ''),
		       lm.canonical_env,
		       lm.canonical_project,
		       lm.canonical_region
		  FROM control_plane.label_mappings lm
		  JOIN control_plane.tenants t  ON t.id  = lm.tenant_id
		  JOIN control_plane.teams   tm ON tm.id = lm.team_id
		  LEFT JOIN control_plane.apps a ON a.id = lm.app_id
		 WHERE lm.provider           = $1
		   AND lm.tenant_external_id = $2
		   AND lm.tenancy_id         = $3
	`
	row := p.pool.QueryRow(ctx, query, k.Provider, k.TenantExternalID, k.TenancyID)
	var m Mapping
	err := row.Scan(
		&m.TenantID,
		&m.TenantSlug,
		&m.TeamSlug,
		&m.AppSlug,
		&m.CanonicalEnv,
		&m.CanonicalProject,
		&m.CanonicalRegion,
	)
	if err != nil {
		if isNoRows(err) {
			p.cachePut(k, Mapping{Found: false})
			return Mapping{Found: false}, nil
		}
		return Mapping{}, fmt.Errorf("store: lookup %s/%s/%s: %w",
			k.Provider, k.TenantExternalID, k.TenancyID, err)
	}
	m.Found = true
	p.cachePut(k, m)
	return m, nil
}

// InsertFocus writes one row to control_plane.focus_records. Append-only:
// the ingester relies on (source_event_id, ingested_at) as the read-side
// last-write-wins key, not on primary-key conflict resolution.
func (p *PgStore) InsertFocus(ctx context.Context, row FocusRow) error {
	const insert = `
		INSERT INTO control_plane.focus_records (
		    tenant_id,
		    source_event_id,
		    provider,
		    model,
		    billing_account_id,
		    invoice_id,
		    service_name,
		    charge_category,
		    reconciled_cost_usd_minor_units,
		    list_cost_usd_minor_units,
		    pricing_currency,
		    period_start,
		    period_end,
		    raw_focus
		) VALUES (
		    $1,  $2,  $3,  $4,  $5,  $6,  $7,  $8,  $9,  $10, $11, $12, $13, $14
		)
	`
	_, err := p.pool.Exec(ctx, insert,
		row.TenantID,
		row.SourceEventID,
		row.Provider,
		row.Model,
		row.BillingAccountID,
		row.InvoiceID,
		row.ServiceName,
		row.ChargeCategory,
		row.ReconciledCostUSDMinorUnits,
		row.ListCostUSDMinorUnits,
		row.PricingCurrency,
		row.PeriodStart,
		row.PeriodEnd,
		row.RawFocus,
	)
	if err != nil {
		return fmt.Errorf("store: insert focus %s: %w", row.SourceEventID, err)
	}
	return nil
}

// Close releases the underlying pool.
func (p *PgStore) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

func (p *PgStore) cacheGet(k Key) (Mapping, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	e, ok := p.cache[k]
	if !ok {
		return Mapping{}, false
	}
	if p.nowFn().After(e.expiresAt) {
		return Mapping{}, false
	}
	return e.value, true
}

func (p *PgStore) cachePut(k Key, v Mapping) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[k] = cacheEntry{value: v, expiresAt: p.nowFn().Add(p.ttl)}
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
