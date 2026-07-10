// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics exposes lightweight counters for the policy-service. We
// keep the surface intentionally small: full OTel wiring lands when the
// service is added to the deployment harness. For now the counters are
// in-process and read by tests / debug handlers.
//
// Project-specific metrics use the `llm_*` prefix.
package metrics

import "sync/atomic"

// Counters is a snapshotable set of in-process counters.
type Counters struct {
	PoliciesCreated      atomic.Uint64
	PoliciesUpdated      atomic.Uint64
	PoliciesDeleted      atomic.Uint64
	ValidationsFailed    atomic.Uint64
	ValidationsSucceeded atomic.Uint64
	AuditEventsEmitted   atomic.Uint64
	AuditEventsDropped   atomic.Uint64
}

// Snapshot is a point-in-time copy of Counters for exposition.
type Snapshot struct {
	PoliciesCreated      uint64 `json:"llm_policy_created_total"`
	PoliciesUpdated      uint64 `json:"llm_policy_updated_total"`
	PoliciesDeleted      uint64 `json:"llm_policy_deleted_total"`
	ValidationsFailed    uint64 `json:"llm_policy_validation_failed_total"`
	ValidationsSucceeded uint64 `json:"llm_policy_validation_succeeded_total"`
	AuditEventsEmitted   uint64 `json:"llm_policy_audit_events_emitted_total"`
	AuditEventsDropped   uint64 `json:"llm_policy_audit_events_dropped_total"`
}

// New returns a fresh Counters with all values at zero.
func New() *Counters { return &Counters{} }

// Snapshot returns a copy of all counter values.
func (c *Counters) Snapshot() Snapshot {
	return Snapshot{
		PoliciesCreated:      c.PoliciesCreated.Load(),
		PoliciesUpdated:      c.PoliciesUpdated.Load(),
		PoliciesDeleted:      c.PoliciesDeleted.Load(),
		ValidationsFailed:    c.ValidationsFailed.Load(),
		ValidationsSucceeded: c.ValidationsSucceeded.Load(),
		AuditEventsEmitted:   c.AuditEventsEmitted.Load(),
		AuditEventsDropped:   c.AuditEventsDropped.Load(),
	}
}
