// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package store wraps the routing.routing_decisions Postgres table.
//
// Semantics:
//
//   - Append is idempotent on decision_id. ON CONFLICT DO NOTHING means a
//     re-delivered bus record never produces a duplicate row and never
//     overwrites prior content. This is "append-only at the API level":
//     the store has no UPDATE or DELETE path. (Append-only is enforced
//     procedurally — the SQL layer does not install rules/triggers here
//     because the decision ledger is operational explainability data, not
//     legal-grade audit. The F031 audit-service is the tamper-evident
//     surface; this surface is the renderable record.)
//
//   - reason_chain and alternatives are treated as opaque JSON blobs by
//     this package. The decision-service does NOT interpret factor names,
//     values, thresholds, or weight_hint numbers. That logic belongs to
//     whatever routing.Decider produced the event and lives outside the
//     OSS distribution.
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

// Decision is the in-Go form of one routing.routing_decisions row.
type Decision struct {
	ID                int64
	DecisionID        string
	TenantID          string
	Team              string
	App               string
	Env               string
	Project           string
	ProviderRequested string
	ModelRequested    string
	RouteRequested    string
	RequestIDHash     string
	ProviderChosen    string
	ModelChosen       string
	RouteChosen       string
	// ReasonChain and Alternatives are stored verbatim as JSON blobs.
	// Decoded as json.RawMessage so the handler can forward them to
	// the admin console without re-interpretation.
	ReasonChain    json.RawMessage
	Alternatives   json.RawMessage
	DeciderVersion string
	DecidedAt      time.Time
	IngestedAt     time.Time
}

// AppendInput is the producer-side shape Append accepts.
type AppendInput struct {
	DecisionID        string
	TenantID          string
	Team              string
	App               string
	Env               string
	Project           string
	ProviderRequested string
	ModelRequested    string
	RouteRequested    string
	RequestIDHash     string
	ProviderChosen    string
	ModelChosen       string
	RouteChosen       string
	ReasonChain       json.RawMessage
	Alternatives      json.RawMessage
	DeciderVersion    string
	DecidedAt         time.Time
}

// QueryFilter narrows /v1/decisions results.
type QueryFilter struct {
	TenantID string
	App      string
	From     time.Time
	To       time.Time
	Cursor   int64 // ID cursor — returns decisions with id < Cursor (DESC order).
	Limit    int
}

// StatsFilter narrows /v1/decisions/stats.
type StatsFilter struct {
	TenantID string
	From     time.Time
	To       time.Time
}

// ProviderCount is one row of the stats aggregate.
type ProviderCount struct {
	ProviderChosen string
	ModelChosen    string
	Count          int64
}

// Stats is the aggregate the stats endpoint returns. It is a flat
// bucketed count by (provider_chosen, model_chosen). The OSS layer does
// NOT compute error rates or override counts from inside the decision
// payload — those are decider-emitted fields that the OSS layer would
// have to interpret. Total is the simple row count for the window.
type Stats struct {
	Total      int64
	ByChosen   []ProviderCount
	WindowFrom time.Time
	WindowTo   time.Time
}

// Store is the narrow interface the handlers and consumer depend on.
type Store interface {
	Append(ctx context.Context, in AppendInput) error
	Query(ctx context.Context, f QueryFilter) ([]Decision, error)
	GetByDecisionID(ctx context.Context, tenantID, decisionID string) (Decision, error)
	StatsByChosen(ctx context.Context, f StatsFilter) (Stats, error)
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

// Append inserts one decision row. Idempotent on decision_id:
// re-delivered bus records do not create duplicates and do not
// overwrite prior content.
func (p *PgStore) Append(ctx context.Context, in AppendInput) error {
	if in.DecisionID == "" {
		return fmt.Errorf("store: decision_id is required")
	}
	if in.TenantID == "" {
		return fmt.Errorf("store: tenant_id is required")
	}
	if in.ProviderRequested == "" || in.ModelRequested == "" {
		return fmt.Errorf("store: request.provider/model are required")
	}
	if in.ProviderChosen == "" || in.ModelChosen == "" {
		return fmt.Errorf("store: decision.provider/model are required")
	}
	if in.DeciderVersion == "" {
		return fmt.Errorf("store: decider_version is required")
	}
	if in.DecidedAt.IsZero() {
		return fmt.Errorf("store: decided_at is required")
	}

	reason := jsonOrEmptyArray(in.ReasonChain)
	alts := jsonOrEmptyArray(in.Alternatives)

	_, err := p.pool.Exec(ctx, `
		INSERT INTO routing.routing_decisions (
		    decision_id, tenant_id, team, app, env, project,
		    provider_requested, model_requested, route_requested, request_id_hash,
		    provider_chosen, model_chosen, route_chosen,
		    reason_chain, alternatives,
		    decider_version, decided_at
		) VALUES (
		    $1, $2, $3, $4, $5, $6,
		    $7, $8, $9, $10,
		    $11, $12, $13,
		    $14, $15,
		    $16, $17
		)
		ON CONFLICT (decision_id) DO NOTHING
	`,
		in.DecisionID,
		in.TenantID,
		in.Team,
		in.App,
		in.Env,
		in.Project,
		in.ProviderRequested,
		in.ModelRequested,
		in.RouteRequested,
		in.RequestIDHash,
		in.ProviderChosen,
		in.ModelChosen,
		in.RouteChosen,
		reason,
		alts,
		in.DeciderVersion,
		in.DecidedAt,
	)
	if err != nil {
		return fmt.Errorf("store: insert: %w", err)
	}
	return nil
}

// Query returns decisions matching f, ordered by decided_at descending.
// The caller pages by passing the smallest id from the previous page as
// f.Cursor.
func (p *PgStore) Query(ctx context.Context, f QueryFilter) ([]Decision, error) {
	if f.TenantID == "" {
		return nil, fmt.Errorf("store: tenant_id is required")
	}
	q := `
		SELECT id, decision_id, tenant_id, team, app, env, project,
		       provider_requested, model_requested, route_requested, request_id_hash,
		       provider_chosen, model_chosen, route_chosen,
		       reason_chain, alternatives,
		       decider_version, decided_at, ingested_at
		  FROM routing.routing_decisions
		 WHERE tenant_id = $1
	`
	args := []any{f.TenantID}
	i := 2
	if f.App != "" {
		q += fmt.Sprintf(" AND app = $%d", i)
		args = append(args, f.App)
		i++
	}
	if !f.From.IsZero() {
		q += fmt.Sprintf(" AND decided_at >= $%d", i)
		args = append(args, f.From)
		i++
	}
	if !f.To.IsZero() {
		q += fmt.Sprintf(" AND decided_at <= $%d", i)
		args = append(args, f.To)
		i++
	}
	if f.Cursor > 0 {
		q += fmt.Sprintf(" AND id < $%d", i)
		args = append(args, f.Cursor)
		i++
	}
	q += " ORDER BY id DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", i)
		args = append(args, f.Limit)
	}
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: query rows: %w", err)
	}
	return out, nil
}

// GetByDecisionID fetches a single decision scoped to one tenant.
func (p *PgStore) GetByDecisionID(ctx context.Context, tenantID, decisionID string) (Decision, error) {
	if tenantID == "" {
		return Decision{}, fmt.Errorf("store: tenant_id is required")
	}
	if decisionID == "" {
		return Decision{}, fmt.Errorf("store: decision_id is required")
	}
	row := p.pool.QueryRow(ctx, `
		SELECT id, decision_id, tenant_id, team, app, env, project,
		       provider_requested, model_requested, route_requested, request_id_hash,
		       provider_chosen, model_chosen, route_chosen,
		       reason_chain, alternatives,
		       decider_version, decided_at, ingested_at
		  FROM routing.routing_decisions
		 WHERE tenant_id = $1 AND decision_id = $2
	`, tenantID, decisionID)
	d, err := scanDecision(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Decision{}, ErrNotFound
		}
		return Decision{}, err
	}
	return d, nil
}

// StatsByChosen returns a simple bucketed count of decisions by
// (provider_chosen, model_chosen) for the tenant + window. The OSS
// stats endpoint does NOT compute error rates, latency, or override
// counts from inside the reason_chain — those fields are decider-defined
// and OSS treats them as opaque.
func (p *PgStore) StatsByChosen(ctx context.Context, f StatsFilter) (Stats, error) {
	if f.TenantID == "" {
		return Stats{}, fmt.Errorf("store: tenant_id is required")
	}
	q := `
		SELECT provider_chosen, model_chosen, COUNT(*)::bigint
		  FROM routing.routing_decisions
		 WHERE tenant_id = $1
	`
	args := []any{f.TenantID}
	i := 2
	if !f.From.IsZero() {
		q += fmt.Sprintf(" AND decided_at >= $%d", i)
		args = append(args, f.From)
		i++
	}
	if !f.To.IsZero() {
		q += fmt.Sprintf(" AND decided_at <= $%d", i)
		args = append(args, f.To)
	}
	q += `
		 GROUP BY provider_chosen, model_chosen
		 ORDER BY COUNT(*) DESC, provider_chosen ASC, model_chosen ASC
	`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return Stats{}, fmt.Errorf("store: stats query: %w", err)
	}
	defer rows.Close()
	out := Stats{WindowFrom: f.From, WindowTo: f.To}
	for rows.Next() {
		var pc ProviderCount
		if err := rows.Scan(&pc.ProviderChosen, &pc.ModelChosen, &pc.Count); err != nil {
			return Stats{}, fmt.Errorf("store: stats scan: %w", err)
		}
		out.ByChosen = append(out.ByChosen, pc)
		out.Total += pc.Count
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("store: stats rows: %w", err)
	}
	return out, nil
}

// ErrNotFound is returned when no row matches.
var ErrNotFound = errors.New("store: decision not found")

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDecision(r rowScanner) (Decision, error) {
	var d Decision
	if err := r.Scan(
		&d.ID,
		&d.DecisionID,
		&d.TenantID,
		&d.Team,
		&d.App,
		&d.Env,
		&d.Project,
		&d.ProviderRequested,
		&d.ModelRequested,
		&d.RouteRequested,
		&d.RequestIDHash,
		&d.ProviderChosen,
		&d.ModelChosen,
		&d.RouteChosen,
		&d.ReasonChain,
		&d.Alternatives,
		&d.DeciderVersion,
		&d.DecidedAt,
		&d.IngestedAt,
	); err != nil {
		return Decision{}, err
	}
	return d, nil
}

// jsonOrEmptyArray normalizes a possibly-nil RawMessage to a valid JSON
// array literal so the JSONB column never receives SQL NULL.
func jsonOrEmptyArray(m json.RawMessage) []byte {
	if len(m) == 0 {
		return []byte("[]")
	}
	return m
}
