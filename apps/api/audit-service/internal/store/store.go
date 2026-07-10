// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store wraps the audit.audit_entries Postgres table.
//
// Append semantics:
//
//   - Append runs inside a SERIALIZABLE transaction. It SELECTs the
//     tenant's last entry_hash with FOR UPDATE, computes the new row's
//     entry_hash, and INSERTs. Concurrent appenders for the same tenant
//     serialize on the SELECT FOR UPDATE; the chain stays linear.
//
//   - The database also rejects UPDATE / DELETE via rules and triggers
//     (see platform/db/audit/migrations/2026051804_f031_audit_ledger.sql). The
//     service's own code never issues either.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/hasher"
)

// Entry is the in-Go form of one audit.audit_entries row.
type Entry struct {
	ID        int64
	TenantID  string
	Actor     map[string]any
	Action    string
	Resource  map[string]any
	Payload   map[string]any
	PrevHash  []byte
	EntryHash []byte
	CreatedAt time.Time
}

// AppendInput is the producer-side shape Append accepts.
type AppendInput struct {
	TenantID string
	Actor    map[string]any
	Action   string
	Resource map[string]any
	Payload  map[string]any
}

// QueryFilter narrows /v1/audit/entries results.
type QueryFilter struct {
	TenantID string
	Action   string
	ActorID  string
	From     time.Time
	To       time.Time
	Cursor   int64 // ID cursor — returns entries with id > Cursor.
	Limit    int
}

// Store is the narrow interface the handlers and consumer depend on.
type Store interface {
	Append(ctx context.Context, in AppendInput) (Entry, error)
	Query(ctx context.Context, f QueryFilter) ([]Entry, error)
	GetByID(ctx context.Context, tenantID string, id int64) (Entry, error)
	Stream(ctx context.Context, tenantID string, from, to time.Time, fn func(Entry) error) error
	StreamForVerify(ctx context.Context, tenantID string, fromID, toID int64, fn func(Entry) error) error
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

// Append writes a new row to the tenant's chain. Serializable transaction +
// FOR UPDATE on the previous row guarantee a linear chain per tenant.
func (p *PgStore) Append(ctx context.Context, in AppendInput) (Entry, error) {
	if in.TenantID == "" {
		return Entry{}, fmt.Errorf("store: tenant_id is required")
	}
	if in.Action == "" {
		return Entry{}, fmt.Errorf("store: action is required")
	}

	var out Entry
	err := pgx.BeginTxFunc(ctx, p.pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, func(tx pgx.Tx) error {
		// Resolve prev_hash: the tenant's most recent entry_hash, or the
		// zero hash if this is the first row.
		var prev []byte
		row := tx.QueryRow(ctx, `
			SELECT entry_hash
			  FROM audit.audit_entries
			 WHERE tenant_id = $1
			 ORDER BY id DESC
			 LIMIT 1
			   FOR UPDATE
		`, in.TenantID)
		if err := row.Scan(&prev); err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("store: select prev_hash: %w", err)
			}
			prev = make([]byte, hasher.HashSize)
		}

		// Reserve the id and created_at via INSERT ... RETURNING; the row
		// gets a zero entry_hash sentinel, then we UPDATE-via-app-only
		// would violate append-only. Instead we COMPUTE the hash from a
		// candidate (id, created_at) — get them from a CTE that returns
		// the values we'll insert.
		//
		// We use `currval` semantics: pre-fetch the next id from the
		// sequence and the server's NOW(), then insert with both the
		// pre-known prev_hash and the freshly-computed entry_hash.
		var nextID int64
		var createdAt time.Time
		if err := tx.QueryRow(ctx, `
			SELECT nextval(pg_get_serial_sequence('audit.audit_entries', 'id')),
			       NOW()
		`).Scan(&nextID, &createdAt); err != nil {
			return fmt.Errorf("store: reserve id/now: %w", err)
		}

		he := hasher.Entry{
			TenantID:  in.TenantID,
			ID:        nextID,
			Actor:     in.Actor,
			Action:    in.Action,
			Resource:  in.Resource,
			Payload:   in.Payload,
			PrevHash:  prev,
			CreatedAt: createdAt,
		}
		entryHash, err := hasher.Compute(he)
		if err != nil {
			return fmt.Errorf("store: compute hash: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO audit.audit_entries (
			    id, tenant_id, actor, action, resource, payload,
			    prev_hash, entry_hash, created_at
			) VALUES (
			    $1, $2, $3, $4, $5, $6, $7, $8, $9
			)
		`,
			nextID,
			in.TenantID,
			emptyMapIfNil(in.Actor),
			in.Action,
			emptyMapIfNil(in.Resource),
			emptyMapIfNil(in.Payload),
			prev,
			entryHash,
			createdAt,
		)
		if err != nil {
			return fmt.Errorf("store: insert: %w", err)
		}

		out = Entry{
			ID:        nextID,
			TenantID:  in.TenantID,
			Actor:     emptyMapIfNil(in.Actor),
			Action:    in.Action,
			Resource:  emptyMapIfNil(in.Resource),
			Payload:   emptyMapIfNil(in.Payload),
			PrevHash:  prev,
			EntryHash: entryHash,
			CreatedAt: createdAt,
		}
		return nil
	})
	if err != nil {
		return Entry{}, err
	}
	return out, nil
}

// Query returns entries matching f, ordered by id ascending. The caller
// pages by passing the largest id from the previous page as f.Cursor.
func (p *PgStore) Query(ctx context.Context, f QueryFilter) ([]Entry, error) {
	if f.TenantID == "" {
		return nil, fmt.Errorf("store: tenant_id is required")
	}
	q := `
		SELECT id, tenant_id, actor, action, resource, payload,
		       prev_hash, entry_hash, created_at
		  FROM audit.audit_entries
		 WHERE tenant_id = $1
	`
	args := []any{f.TenantID}
	i := 2
	if f.Action != "" {
		q += fmt.Sprintf(" AND action = $%d", i)
		args = append(args, f.Action)
		i++
	}
	if f.ActorID != "" {
		q += fmt.Sprintf(" AND actor ->> 'id' = $%d", i)
		args = append(args, f.ActorID)
		i++
	}
	if !f.From.IsZero() {
		q += fmt.Sprintf(" AND created_at >= $%d", i)
		args = append(args, f.From)
		i++
	}
	if !f.To.IsZero() {
		q += fmt.Sprintf(" AND created_at <= $%d", i)
		args = append(args, f.To)
		i++
	}
	if f.Cursor > 0 {
		q += fmt.Sprintf(" AND id > $%d", i)
		args = append(args, f.Cursor)
		i++
	}
	q += " ORDER BY id ASC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", i)
		args = append(args, f.Limit)
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: query rows: %w", err)
	}
	return out, nil
}

// GetByID returns one entry within a tenant.
func (p *PgStore) GetByID(ctx context.Context, tenantID string, id int64) (Entry, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, tenant_id, actor, action, resource, payload,
		       prev_hash, entry_hash, created_at
		  FROM audit.audit_entries
		 WHERE tenant_id = $1 AND id = $2
	`, tenantID, id)
	e, err := scanEntry(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, err
	}
	return e, nil
}

// Stream invokes fn for each entry in [from, to] for tenantID, ordered by
// id ascending. Used by the JSONL export path so a tenant's full history
// can be streamed without loading it all into memory.
func (p *PgStore) Stream(ctx context.Context, tenantID string, from, to time.Time, fn func(Entry) error) error {
	if tenantID == "" {
		return fmt.Errorf("store: tenant_id is required")
	}
	q := `
		SELECT id, tenant_id, actor, action, resource, payload,
		       prev_hash, entry_hash, created_at
		  FROM audit.audit_entries
		 WHERE tenant_id = $1
	`
	args := []any{tenantID}
	i := 2
	if !from.IsZero() {
		q += fmt.Sprintf(" AND created_at >= $%d", i)
		args = append(args, from)
		i++
	}
	if !to.IsZero() {
		q += fmt.Sprintf(" AND created_at <= $%d", i)
		args = append(args, to)
	}
	q += " ORDER BY id ASC"
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("store: stream: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// StreamForVerify invokes fn over the chain segment fromID..toID. The
// rows come out in id order; the verifier compares each row's prev_hash
// against the running hash and each row's entry_hash against the freshly
// recomputed hash.
func (p *PgStore) StreamForVerify(ctx context.Context, tenantID string, fromID, toID int64, fn func(Entry) error) error {
	if tenantID == "" {
		return fmt.Errorf("store: tenant_id is required")
	}
	q := `
		SELECT id, tenant_id, actor, action, resource, payload,
		       prev_hash, entry_hash, created_at
		  FROM audit.audit_entries
		 WHERE tenant_id = $1
	`
	args := []any{tenantID}
	i := 2
	if fromID > 0 {
		q += fmt.Sprintf(" AND id >= $%d", i)
		args = append(args, fromID)
		i++
	}
	if toID > 0 {
		q += fmt.Sprintf(" AND id <= $%d", i)
		args = append(args, toID)
	}
	q += " ORDER BY id ASC"
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("store: stream verify: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ErrNotFound is returned by GetByID when no row matches.
var ErrNotFound = errors.New("store: entry not found")

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(r rowScanner) (Entry, error) {
	var e Entry
	if err := r.Scan(
		&e.ID,
		&e.TenantID,
		&e.Actor,
		&e.Action,
		&e.Resource,
		&e.Payload,
		&e.PrevHash,
		&e.EntryHash,
		&e.CreatedAt,
	); err != nil {
		return Entry{}, err
	}
	return e, nil
}

func emptyMapIfNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
