// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package registry is the boot-time wiring point where service binaries select
// which scoring, routing, policy, and fallback implementations to use. The default
// binary calls Use(Defaults()); deployments with extensions call Use(Providers{...}).
package registry

import (
	"sync"

	"github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/fallback"
	"github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/policy"
	"github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/routing"
	"github.com/yasvanth511/openllm-metrics-oss/packages/extensions/go/scoring"
)

// Providers is the complete set of pluggable behaviors a binary may register at
// boot. Any nil field is replaced with the matching no-op default.
type Providers struct {
	Scoring  scoring.Provider
	Routing  routing.Decider
	Policy   policy.Evaluator
	Fallback fallback.Controller
}

// Defaults returns a Providers populated with every no-op implementation. This is
// what an OSS service binary registers when it has no extensions to load.
func Defaults() Providers {
	return Providers{
		Scoring:  scoring.Noop(),
		Routing:  routing.Noop(),
		Policy:   policy.Noop(),
		Fallback: fallback.Noop(),
	}
}

var (
	mu      sync.RWMutex
	current = Defaults()
)

// Use replaces the active providers. Any nil field on p falls back to the
// matching no-op default. Use is safe to call concurrently with the accessors.
func Use(p Providers) {
	mu.Lock()
	defer mu.Unlock()
	if p.Scoring == nil {
		p.Scoring = scoring.Noop()
	}
	if p.Routing == nil {
		p.Routing = routing.Noop()
	}
	if p.Policy == nil {
		p.Policy = policy.Noop()
	}
	if p.Fallback == nil {
		p.Fallback = fallback.Noop()
	}
	current = p
}

// Scoring returns the currently registered scoring provider.
func Scoring() scoring.Provider {
	mu.RLock()
	defer mu.RUnlock()
	return current.Scoring
}

// Routing returns the currently registered routing decider.
func Routing() routing.Decider {
	mu.RLock()
	defer mu.RUnlock()
	return current.Routing
}

// Policy returns the currently registered policy evaluator.
func Policy() policy.Evaluator {
	mu.RLock()
	defer mu.RUnlock()
	return current.Policy
}

// Fallback returns the currently registered fallback controller.
func Fallback() fallback.Controller {
	mu.RLock()
	defer mu.RUnlock()
	return current.Fallback
}
