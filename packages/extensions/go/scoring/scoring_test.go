// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package scoring

import (
	"context"
	"testing"
)

func TestNoop_ReturnsHealthyScore(t *testing.T) {
	p := Noop()
	target := Target{Provider: "openai", Model: "gpt-4o-mini", Tenant: "acme"}

	got, err := p.Score(context.Background(), KindReliability, target)
	if err != nil {
		t.Fatalf("Score returned error: %v", err)
	}
	if got.Value != 1.0 {
		t.Errorf("Value = %v, want 1.0", got.Value)
	}
	if got.RuleVersion != "oss-default" {
		t.Errorf("RuleVersion = %q, want oss-default", got.RuleVersion)
	}
	if got.Kind != KindReliability {
		t.Errorf("Kind = %v, want %v", got.Kind, KindReliability)
	}
	if got.Target != target {
		t.Errorf("Target = %+v, want %+v", got.Target, target)
	}
}

func TestNoop_DeterministicAcrossKinds(t *testing.T) {
	p := Noop()
	target := Target{Provider: "anthropic", Model: "sonnet", Tenant: "t1"}
	kinds := []Kind{KindReliability, KindCostEfficiency, KindQuotaRisk}

	for _, k := range kinds {
		s, err := p.Score(context.Background(), k, target)
		if err != nil {
			t.Fatalf("Score(%v) error: %v", k, err)
		}
		if s.Value != 1.0 {
			t.Errorf("Score(%v).Value = %v, want 1.0", k, s.Value)
		}
	}
}
