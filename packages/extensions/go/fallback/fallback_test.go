// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package fallback

import (
	"context"
	"testing"
)

func TestNoop_AlwaysStops(t *testing.T) {
	c := Noop()
	req := Request{
		Primary:    Target{Provider: "openai", Model: "gpt-4o-mini"},
		Tenant:     "acme",
		Candidates: []Target{{Provider: "anthropic", Model: "sonnet"}},
	}

	d, err := c.Next(context.Background(), req)
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	if !d.Stop {
		t.Errorf("Stop = false, want true (OSS default never falls back)")
	}
	if d.RuleVersion != "oss-default" {
		t.Errorf("RuleVersion = %q, want oss-default", d.RuleVersion)
	}
}
