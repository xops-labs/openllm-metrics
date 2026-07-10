// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package policy defines the public contract for the declarative policy and budget
// evaluator. The OSS distribution ships a permissive default that allows every
// request; the production evaluator is intentionally not implemented here.
package policy

import "context"

// Verdict is the outcome of a policy evaluation.
type Verdict string

const (
	VerdictAllow Verdict = "allow"
	VerdictDeny  Verdict = "deny"
	VerdictWarn  Verdict = "warn"
)

// Request carries the inputs an evaluator may consult. No raw user
// content is included.
type Request struct {
	Tenant      string
	Team        string
	App         string
	Env         string
	Provider    string
	Model       string
	Criticality string
	InputsHash  string
}

// Decision is the evaluator's output. Reason and RuleVersion are mandatory so the
// audit ledger (F031) and decision explainability surface (F036) can attribute the
// outcome to a specific policy version.
type Decision struct {
	Verdict         Verdict
	Reason          string
	RuleVersion     string
	Recommendations []string
}

// Evaluator decides whether a request is allowed by current policy. Implementations
// must be deterministic at a given RuleVersion.
type Evaluator interface {
	Evaluate(ctx context.Context, req Request) (Decision, error)
}

// Noop returns an Evaluator that always returns VerdictAllow. It exists so OSS
// services run standalone. It is not a policy engine — production deployments
// register this repository.
func Noop() Evaluator { return noopEvaluator{} }

type noopEvaluator struct{}

func (noopEvaluator) Evaluate(_ context.Context, _ Request) (Decision, error) {
	return Decision{
		Verdict:     VerdictAllow,
		Reason:      "oss-default-allow",
		RuleVersion: "oss-default",
	}, nil
}
