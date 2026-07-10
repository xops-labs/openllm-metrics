// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package fallback defines the public contract for the bounded fallback controller.
// The OSS distribution ships a no-op default that performs no fallback; the
// production controller is intentionally not implemented here.
package fallback

import "context"

// Target identifies a routable (provider, model) destination considered for
// fallback.
type Target struct {
	Provider string
	Model    string
}

// Attempt records a single hop in a fallback chain.
type Attempt struct {
	Target Target
	Error  string
}

// Request describes a failed primary call the controller is asked to recover.
type Request struct {
	Primary     Target
	Tenant      string
	Criticality string
	Candidates  []Target
	History     []Attempt
}

// Decision returns the next hop and the chain budget remaining. Stop=true means
// the controller has exhausted its budget and the caller must surface the
// original error.
type Decision struct {
	Next          Target
	Stop          bool
	Reason        string
	RuleVersion   string
	HopsRemaining int
}

// Controller decides whether to retry a failed primary call against an alternate
// target, and which one. Implementations are bounded — they must not enter
// unbounded retry loops.
type Controller interface {
	Next(ctx context.Context, req Request) (Decision, error)
}

// Noop returns a Controller that never falls back: every call returns Stop=true.
// OSS deployments behave as if no fallback is configured. The custom
// implementation provides the real bounded chain.
func Noop() Controller { return noopController{} }

type noopController struct{}

func (noopController) Next(_ context.Context, _ Request) (Decision, error) {
	return Decision{
		Stop:        true,
		Reason:      "oss-default-no-fallback",
		RuleVersion: "oss-default",
	}, nil
}
