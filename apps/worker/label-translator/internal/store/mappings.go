// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store backs the label translator's lookup of
// control_plane.label_mappings with a Postgres query + an in-process TTL
// cache. Cache misses do a single SELECT keyed on the natural-key tuple
// (provider, tenant_external_id, tenancy_id).
//
// Negative caching is intentional and bounded: a tuple that has no row is
// cached as `Mapping{Found: false}` for the same TTL so the translator does
// not hammer the database on a sustained unmapped stream.
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

// Mapping is the resolution result returned by Lookup. When Found is false
// the canonical fields are zero values and the caller should emit with its
// configured default labels (and bump the unmapped counter).
type Mapping struct {
	Found            bool
	TenantSlug       string
	TeamSlug         string
	AppSlug          string
	CanonicalEnv     string
	CanonicalProject string
	CanonicalRegion  string
}

// Key is the natural-key tuple the upstream exporter labels each sample with.
// tenancy_id may be empty for providers that have no sub-account concept.
type Key struct {
	Provider         string
	TenantExternalID string
	TenancyID        string
}

// Mappings is the public interface the translator depends on. Production
// wires this to PgMappings; tests can substitute an in-memory implementation.
type Mappings interface {
	Lookup(ctx context.Context, k Key) (Mapping, error)
	Close()
}

// PgMappings is the Postgres-backed implementation.
type PgMappings struct {
	pool  *pgxpool.Pool
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[Key]cacheEntry
	nowFn func() time.Time
}

type cacheEntry struct {
	value     Mapping
	expiresAt time.Time
}

// New opens a pgx connection pool from the supplied DSN and returns a PgMappings.
func New(ctx context.Context, dsn string, ttl time.Duration) (*PgMappings, error) {
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
	return &PgMappings{
		pool:  pool,
		ttl:   ttl,
		cache: make(map[Key]cacheEntry, 256),
		nowFn: func() time.Time { return time.Now() },
	}, nil
}

// Lookup resolves k against label_mappings. Returns Mapping{Found: false}
// (and a nil error) when no row matches; the caller decides how to handle
// the unmapped case. Returns a non-nil error only on database failure.
func (p *PgMappings) Lookup(ctx context.Context, k Key) (Mapping, error) {
	if v, ok := p.cacheGet(k); ok {
		return v, nil
	}

	const query = `
		SELECT t.slug,
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
		&m.TenantSlug,
		&m.TeamSlug,
		&m.AppSlug,
		&m.CanonicalEnv,
		&m.CanonicalProject,
		&m.CanonicalRegion,
	)
	if err != nil {
		if isNoRows(err) {
			// Cache the miss so a sustained unmapped stream doesn't hammer Postgres.
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

// Close releases the underlying connection pool.
func (p *PgMappings) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

func (p *PgMappings) cacheGet(k Key) (Mapping, bool) {
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

func (p *PgMappings) cachePut(k Key, v Mapping) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache[k] = cacheEntry{value: v, expiresAt: p.nowFn().Add(p.ttl)}
}

// isNoRows returns true when err is pgx's clean no-rows sentinel.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
