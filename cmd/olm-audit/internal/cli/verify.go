// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package cli implements the olm-audit subcommands.
package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RunVerify is the entry point for `olm-audit verify`.
//
// Flow:
//
//  1. Open the supplied Postgres DSN.
//  2. Stream every audit_entries row for the tenant in id order.
//  3. For each row recompute entry_hash and compare against the stored value.
//  4. Also compare each row's stored prev_hash against the running expected
//     value (the previous row's entry_hash).
//  5. On the first mismatch, print BREAK with expected vs actual hashes
//     (base64) and exit non-zero. On success, print OK with row count and
//     the last id.
func RunVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	tenant := fs.String("tenant", "", "tenant UUID (required)")
	dsn := fs.String("db", "", "postgres DSN (required)")
	fromID := fs.Int64("from-id", 0, "verify from this id forward (default: tenant start)")
	toID := fs.Int64("to-id", 0, "verify up to and including this id (default: latest)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tenant == "" {
		return fmt.Errorf("--tenant is required")
	}
	if *dsn == "" {
		return fmt.Errorf("--db is required")
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

	prev := make([]byte, hashSize)
	first := true
	checked := 0
	var lastID int64

	pgStore := &poolStore{pool: pool}
	err = pgStore.StreamForVerify(ctx, *tenant, *fromID, *toID, func(e Entry) error {
		checked++
		lastID = e.ID

		if !first {
			if !bytes.Equal(e.PrevHash, prev) {
				_, _ = fmt.Fprintf(os.Stdout, "BREAK at id=%d\n", e.ID)
				_, _ = fmt.Fprintf(os.Stdout, "  expected prev_hash = %s\n", base64.StdEncoding.EncodeToString(prev))
				_, _ = fmt.Fprintf(os.Stdout, "  actual   prev_hash = %s\n", base64.StdEncoding.EncodeToString(e.PrevHash))
				_, _ = fmt.Fprintf(os.Stdout, "  reason   = prev_hash does not match prior entry_hash\n")
				return errBreak
			}
		}

		got, err := computeHash(hasherEntry{
			TenantID:  e.TenantID,
			ID:        e.ID,
			Actor:     e.Actor,
			Action:    e.Action,
			Resource:  e.Resource,
			Payload:   e.Payload,
			PrevHash:  e.PrevHash,
			CreatedAt: e.CreatedAt,
		})
		if err != nil {
			return err
		}
		if !bytes.Equal(got, e.EntryHash) {
			_, _ = fmt.Fprintf(os.Stdout, "BREAK at id=%d\n", e.ID)
			_, _ = fmt.Fprintf(os.Stdout, "  expected entry_hash = %s\n", base64.StdEncoding.EncodeToString(got))
			_, _ = fmt.Fprintf(os.Stdout, "  actual   entry_hash = %s\n", base64.StdEncoding.EncodeToString(e.EntryHash))
			_, _ = fmt.Fprintf(os.Stdout, "  reason   = entry_hash recompute mismatch\n")
			return errBreak
		}
		prev = e.EntryHash
		first = false
		return nil
	})
	if err == errBreak {
		os.Exit(1)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "OK  tenant=%s  checked=%d  last_id=%d\n", *tenant, checked, lastID)
	return nil
}

var errBreak = fmt.Errorf("chain break")
