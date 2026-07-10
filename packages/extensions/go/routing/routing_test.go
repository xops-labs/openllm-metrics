// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package routing

import (
	"context"
	"errors"
	"testing"
)

func TestNoop_PicksFirstCandidate(t *testing.T) {
	d := Noop()
	req := Request{
		Tenant: "acme",
		Candidates: []Target{
			{Provider: "openai", Model: "gpt-4o-mini"},
			{Provider: "anthropic", Model: "sonnet"},
		},
	}

	got, err := d.Decide(context.Background(), req)
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if got.Chosen != req.Candidates[0] {
		t.Errorf("Chosen = %+v, want %+v", got.Chosen, req.Candidates[0])
	}
	if got.RuleVersion != "oss-default" {
		t.Errorf("RuleVersion = %q, want oss-default", got.RuleVersion)
	}
}

func TestNoop_NoCandidatesReturnsError(t *testing.T) {
	d := Noop()
	_, err := d.Decide(context.Background(), Request{Tenant: "acme"})
	if !errors.Is(err, ErrNoCandidates) {
		t.Errorf("err = %v, want ErrNoCandidates", err)
	}
}
