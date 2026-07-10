// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunExport is the entry point for `olm-audit export`.
//
// The output is JSONL (one entry per line), matching the wire shape used by
// the audit-service's /v1/audit/export endpoint:
//
//	{"id":..., "tenant_id":"...", "actor":{...}, "action":"...",
//	 "resource":{...}, "payload":{...},
//	 "prev_hash":"<base64>", "entry_hash":"<base64>",
//	 "created_at":"<RFC3339>"}
//
// SIEM connectors and compliance auditors can ingest the file directly.
func RunExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant UUID (required)")
	dsn := fs.String("db", "", "postgres DSN (required)")
	out := fs.String("out", "", "output file path (use '-' for stdout)")
	fromStr := fs.String("from", "", "RFC3339 start timestamp (optional)")
	toStr := fs.String("to", "", "RFC3339 end timestamp (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("--tenant is required")
	}
	if *dsn == "" {
		return fmt.Errorf("--db is required")
	}

	var from, to time.Time
	if *fromStr != "" {
		t, err := time.Parse(time.RFC3339, *fromStr)
		if err != nil {
			return fmt.Errorf("--from: %w", err)
		}
		from = t
	}
	if *toStr != "" {
		t, err := time.Parse(time.RFC3339, *toStr)
		if err != nil {
			return fmt.Errorf("--to: %w", err)
		}
		to = t
	}

	var sink io.Writer
	if *out == "" || *out == "-" {
		sink = os.Stdout
	} else {
		f, err := os.Create(*out)
		if err != nil {
			return fmt.Errorf("open %s: %w", *out, err)
		}
		defer func() { _ = f.Close() }()
		sink = f
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	enc := json.NewEncoder(sink)
	rows := 0
	pgStore := &poolStore{pool: pool}
	err = pgStore.Stream(ctx, *tenant, from, to, func(e Entry) error {
		rec := map[string]any{
			"id":         e.ID,
			"tenant_id":  e.TenantID,
			"actor":      e.Actor,
			"action":     e.Action,
			"resource":   e.Resource,
			"payload":    e.Payload,
			"prev_hash":  base64.StdEncoding.EncodeToString(e.PrevHash),
			"entry_hash": base64.StdEncoding.EncodeToString(e.EntryHash),
			"created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
		rows++
		return nil
	})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stderr, "exported %d rows\n", rows)
	return nil
}
