// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package registry

import (
	"context"
	"testing"

	"github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/policy"
	"github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/routing"
)

func TestDefaults_ProvidesEveryInterface(t *testing.T) {
	d := Defaults()
	if d.Scoring == nil || d.Routing == nil || d.Policy == nil || d.Fallback == nil {
		t.Fatalf("Defaults missing a provider: %+v", d)
	}
}

func TestUse_NilFieldsBackfillToDefaults(t *testing.T) {
	t.Cleanup(func() { Use(Defaults()) })

	Use(Providers{})

	if Scoring() == nil {
		t.Error("Scoring not backfilled")
	}
	if Routing() == nil {
		t.Error("Routing not backfilled")
	}
	if Policy() == nil {
		t.Error("Policy not backfilled")
	}
	if Fallback() == nil {
		t.Error("Fallback not backfilled")
	}
}

func TestUse_AppliesRegisteredImplementation(t *testing.T) {
	t.Cleanup(func() { Use(Defaults()) })

	fake := &fakePolicy{verdict: policy.VerdictDeny}
	Use(Providers{Policy: fake})

	got, err := Policy().Evaluate(context.Background(), policy.Request{Tenant: "t"})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if got.Verdict != policy.VerdictDeny {
		t.Errorf("Verdict = %v, want %v", got.Verdict, policy.VerdictDeny)
	}
}

func TestRouting_DefaultPicksFirst(t *testing.T) {
	t.Cleanup(func() { Use(Defaults()) })
	Use(Defaults())

	d, err := Routing().Decide(context.Background(), routing.Request{
		Candidates: []routing.Target{{Provider: "openai", Model: "x"}},
	})
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if d.Chosen.Provider != "openai" {
		t.Errorf("Chosen.Provider = %q, want openai", d.Chosen.Provider)
	}
}

type fakePolicy struct{ verdict policy.Verdict }

func (f *fakePolicy) Evaluate(_ context.Context, _ policy.Request) (policy.Decision, error) {
	return policy.Decision{Verdict: f.verdict, Reason: "fake", RuleVersion: "test"}, nil
}
