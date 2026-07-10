// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry mirrors the audit.audit_entries row shape used by the audit-service.
// The CLI deliberately duplicates this struct (rather than importing the
// audit-service's internal package) so the verifier binary is self-contained:
// an auditor can build and run it without compiling the rest of the platform.
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

// poolStore is a thin reader-only Postgres surface used by the CLI. It
// intentionally does NOT depend on the full audit-service `store.PgStore`
// to keep the CLI's surface area minimal — the CLI never inserts, never
// runs migrations, never opens a server, and never touches the bus.
type poolStore struct {
	pool *pgxpool.Pool
}

// Stream iterates every row for tenantID within [from, to] (zero times
// disable the bound) in ascending id order.
func (p *poolStore) Stream(ctx context.Context, tenantID string, from, to time.Time, fn func(Entry) error) error {
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
		return fmt.Errorf("query: %w", err)
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

// StreamForVerify iterates the chain segment fromID..toID (zero values
// remove the bound) in ascending id order.
func (p *poolStore) StreamForVerify(ctx context.Context, tenantID string, fromID, toID int64, fn func(Entry) error) error {
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
		return fmt.Errorf("query: %w", err)
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

func scanEntry(r pgx.Rows) (Entry, error) {
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
