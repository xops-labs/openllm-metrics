// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package scoring defines the public contract for reliability and cost-efficiency
// scoring. The OSS distribution ships a no-op default; deployments can wire their own implementations at boot via the registry package.
package scoring

import "context"

// Target identifies a routable (provider, model, tenant) tuple a score applies to.
type Target struct {
	Provider string
	Model    string
	Tenant   string
}

// Kind enumerates the score families. Adding a new kind is a non-breaking change;
// removing one is a breaking change.
type Kind string

const (
	KindReliability    Kind = "reliability"
	KindCostEfficiency Kind = "cost_efficiency"
	KindQuotaRisk      Kind = "quota_risk"
)

// Score is the bounded output of a scoring computation.
//
// Value is constrained to [0.0, 1.0]. RuleVersion and InputsHash exist so callers
// can record exactly which formula produced the score for downstream explainability
// (F036). Implementations must clamp Value before returning.
type Score struct {
	Kind        Kind
	Target      Target
	Value       float64
	RuleVersion string
	InputsHash  string
}

// Provider computes scores for a given target. Implementations must be deterministic:
// the same inputs and rule version produce the same score.
type Provider interface {
	Score(ctx context.Context, kind Kind, target Target) (Score, error)
}

// Noop returns a Provider that reports every target as fully healthy (1.0). It
// satisfies the interface so OSS services run standalone, but it is not a meaningful
// score — production deployments register this repository instead.
func Noop() Provider { return noopProvider{} }

type noopProvider struct{}

func (noopProvider) Score(_ context.Context, kind Kind, target Target) (Score, error) {
	return Score{
		Kind:        kind,
		Target:      target,
		Value:       1.0,
		RuleVersion: "oss-default",
		InputsHash:  "",
	}, nil
}
