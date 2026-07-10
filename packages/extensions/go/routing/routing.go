// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package routing defines the public contract for the gateway's routing decision
// hook. The OSS distribution ships a pass-through default; the production
// implementation is intentionally not implemented here.
package routing

import "context"

// Target is a routable (provider, model) destination considered by the decider.
type Target struct {
	Provider string
	Model    string
}

// Request carries the inputs the decider may consult. No raw user content is
// included by construction — only routing-relevant context. Decision records
// downstream reference InputsHash, never InputsSummary verbatim.
type Request struct {
	Tenant        string
	Team          string
	App           string
	Env           string
	Criticality   string
	Candidates    []Target
	InputsHash    string
	InputsSummary map[string]string
}

// Decision is the deciders output. Reason and RuleVersion are mandatory for
// auditability (F036). LatencyBudgetMs records the budget remaining when the
// decision was made so the gateway can enforce its hot-path SLO.
type Decision struct {
	Chosen          Target
	Reason          string
	RuleVersion     string
	LatencyBudgetMs int64
}

// Decider selects a Target from Request.Candidates. Implementations must be
// deterministic and must not call out of the hot-path budget (default 5ms p99).
// Implementations must reject cross-tenant routing by construction.
type Decider interface {
	Decide(ctx context.Context, req Request) (Decision, error)
}

// Noop returns a Decider that picks the first candidate. It exists so OSS services
// run standalone. It is not a routing engine — production deployments register the
// this repository.
func Noop() Decider { return noopDecider{} }

type noopDecider struct{}

func (noopDecider) Decide(_ context.Context, req Request) (Decision, error) {
	if len(req.Candidates) == 0 {
		return Decision{}, ErrNoCandidates
	}
	return Decision{
		Chosen:          req.Candidates[0],
		Reason:          "first-candidate",
		RuleVersion:     "oss-default",
		LatencyBudgetMs: 0,
	}, nil
}

// ErrNoCandidates is returned when Request.Candidates is empty.
var ErrNoCandidates = routingError("routing: no candidates provided")

type routingError string

func (e routingError) Error() string { return string(e) }
