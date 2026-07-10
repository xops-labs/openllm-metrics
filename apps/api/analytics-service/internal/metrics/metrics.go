// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics exposes lightweight counters for the analytics-service. We
// keep the surface intentionally small: full OTel wiring lands when the
// service is added to the deployment harness. For now the counters are
// in-process and read by tests / debug handlers.
//
// Project-specific metrics use the `llm_*` prefix.
package metrics

import "sync/atomic"

// Counters is a snapshotable set of in-process counters.
type Counters struct {
	ViewsListed  atomic.Uint64
	ViewsCreated atomic.Uint64
	ViewsDeleted atomic.Uint64
}

// Snapshot is a point-in-time copy of Counters for exposition.
type Snapshot struct {
	ViewsListed  uint64 `json:"llm_analytics_saved_views_listed_total"`
	ViewsCreated uint64 `json:"llm_analytics_saved_views_created_total"`
	ViewsDeleted uint64 `json:"llm_analytics_saved_views_deleted_total"`
}

// New returns a fresh Counters with all values at zero.
func New() *Counters { return &Counters{} }

// Snapshot returns a copy of all counter values.
func (c *Counters) Snapshot() Snapshot {
	return Snapshot{
		ViewsListed:  c.ViewsListed.Load(),
		ViewsCreated: c.ViewsCreated.Load(),
		ViewsDeleted: c.ViewsDeleted.Load(),
	}
}
