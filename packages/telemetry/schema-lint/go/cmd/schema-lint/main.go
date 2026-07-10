// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command schema-lint validates a JSON event payload against the F008
// canonical contracts.
//
// Usage:
//
//	schema-lint --topic llm.usage.normalized --file event.json
//	cat event.json | schema-lint --topic llm.runtime.normalized
//
// Exit codes:
//
//	0 — payload passes every project-specific rule
//	1 — payload has one or more lint issues
//	2 — invocation error (unknown flag, unreadable file)
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	schemalint "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("schema-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)

	topic := fs.String("topic", "", "bus topic the payload belongs to (e.g. llm.usage.normalized)")
	file := fs.String("file", "", "path to a JSON file containing the payload; reads stdin when omitted")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *topic == "" {
		fmt.Fprintln(stderr, "schema-lint: --topic is required")
		fs.Usage()
		return 2
	}

	payload, err := readPayload(*file, stdin)
	if err != nil {
		fmt.Fprintf(stderr, "schema-lint: %v\n", err)
		return 2
	}

	result := schemalint.LintEvent(*topic, payload)
	if result.OK() {
		fmt.Fprintf(stdout, "schema-lint: OK (topic=%s)\n", *topic)
		return 0
	}
	fmt.Fprintf(stderr, "schema-lint: %v\n", result.Error())
	return 1
}

func readPayload(path string, stdin io.Reader) ([]byte, error) {
	if path == "" {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path) //nolint:gosec // file path is a CLI arg
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return b, nil
}
