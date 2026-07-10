// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command olm-audit is the offline verifier and exporter for the F031
// audit ledger.
//
// Two subcommands:
//
//	olm-audit verify --tenant <uuid> --db <postgres-url> [--from-id N] [--to-id N]
//	olm-audit export --tenant <uuid> --db <postgres-url> --out <file.jsonl>
//	                 [--from <ts>] [--to <ts>]
//
// The CLI talks directly to Postgres so an auditor can re-run the chain
// check without trusting the live audit-service. The hash function used
// here is the same `internal/hasher` package the service uses on insert —
// any byte-level drift between insert-time and verify-time will be caught.
package main

import (
	"fmt"
	"os"

	"github.com/yasvanth511/openllm-metrics-oss/cmd/olm-audit/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "verify":
		if err := cli.RunVerify(args); err != nil {
			fmt.Fprintln(os.Stderr, "verify:", err)
			os.Exit(1)
		}
	case "export":
		if err := cli.RunExport(args); err != nil {
			fmt.Fprintln(os.Stderr, "export:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand:", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `olm-audit — F031 audit ledger CLI

Subcommands:
  verify   Re-compute the per-tenant hash chain against a Postgres connection.
  export   Stream a tenant's audit history as JSONL to a file.

Examples:
  olm-audit verify --tenant 11111111-... --db "$OPENLLM_AUDIT_DSN"
  olm-audit export --tenant 11111111-... --db "$OPENLLM_AUDIT_DSN" \
                   --from 2026-05-01T00:00:00Z --to 2026-05-31T23:59:59Z \
                   --out  acme-may-audit.jsonl`)
}
