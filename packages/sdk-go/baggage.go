// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package openllm

import (
	"context"

	"go.opentelemetry.io/otel/baggage"
)

// Baggage keys for multi-tenant context propagation. Downstream services pick
// these up from the wire to attribute usage without re-deriving it. The names
// match the .NET, Python, and Node.js SDKs.
const (
	BaggageTenant  = "openllm.tenant"
	BaggageTeam    = "openllm.team"
	BaggageApp     = "openllm.app"
	BaggageEnv     = "openllm.env"
	BaggageProject = "openllm.project"
)

// attachTenantBaggage returns a child context carrying tenant/team/app/env/
// project on OTel baggage. Empty values are skipped so a missing dimension at
// this layer never overwrites one set higher in the call stack. Any baggage
// build error is logged via the OTel error handler and the original context is
// returned — instrumentation must never break the caller.
func attachTenantBaggage(ctx context.Context, c CallOptions) context.Context {
	bag := baggage.FromContext(ctx)
	bag = setMemberIfPresent(bag, BaggageTenant, c.Tenant)
	bag = setMemberIfPresent(bag, BaggageTeam, c.Team)
	bag = setMemberIfPresent(bag, BaggageApp, c.App)
	bag = setMemberIfPresent(bag, BaggageEnv, c.Env)
	bag = setMemberIfPresent(bag, BaggageProject, c.Project)
	return baggage.ContextWithBaggage(ctx, bag)
}

// setMemberIfPresent adds a baggage member if value is non-empty, returning the
// existing baggage unchanged on either an empty value or a build error.
func setMemberIfPresent(b baggage.Baggage, key, value string) baggage.Baggage {
	if value == "" {
		return b
	}
	member, err := baggage.NewMember(key, value)
	if err != nil {
		return b
	}
	out, err := b.SetMember(member)
	if err != nil {
		return b
	}
	return out
}

// CurrentTenantBaggage returns the tenant-context keys currently set on OTel
// baggage in ctx. Keys are short-form names (tenant/team/app/env/project);
// missing keys are omitted.
func CurrentTenantBaggage(ctx context.Context) map[string]string {
	b := baggage.FromContext(ctx)
	out := make(map[string]string, 5)
	for short, full := range map[string]string{
		"tenant":  BaggageTenant,
		"team":    BaggageTeam,
		"app":     BaggageApp,
		"env":     BaggageEnv,
		"project": BaggageProject,
	} {
		if m := b.Member(full); m.Value() != "" {
			out[short] = m.Value()
		}
	}
	return out
}
