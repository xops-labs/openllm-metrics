// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package testkit is the SOLE sanctioned bridge from the metrics-endpoint
// service's internal/ packages into cross-module tests
// (tests/contract/metrics-endpoint).
//
// The internal/ packages are import-restricted by Go's standard rules so
// downstream callers cannot accidentally couple to private surface area.
// Contract tests need to reach in, however — they are part of the same
// product, just deployed as a separate module so they can exercise the
// public-from-the-service surface without circular imports.
//
// Mirror F009's apps/worker/usage-poller/openai/pkg/testkit pattern so
// reviewers see the same convention across services.
package testkit

import (
	"net/http"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/aggregator"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/consumer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/server"
)

// --- aggregator re-exports -------------------------------------------------

// Aggregator is the re-exported aggregator type.
type Aggregator = aggregator.Aggregator

// RejectReason is the re-exported closed enum of rejection reasons.
type RejectReason = aggregator.RejectReason

// Snapshot is the re-exported deterministic snapshot type.
type Snapshot = aggregator.Snapshot

// Re-exported reason constants.
const (
	ReasonDecode       = aggregator.ReasonDecode
	ReasonSchema       = aggregator.ReasonSchema
	ReasonForbidden    = aggregator.ReasonForbidden
	ReasonCardinality  = aggregator.ReasonCardinality
	ReasonUnknownTopic = aggregator.ReasonUnknownTopic
)

// NewAggregator constructs a fresh in-memory aggregator.
func NewAggregator() *Aggregator { return aggregator.New() }

// AllRejectReasons returns the closed reason list.
func AllRejectReasons() []RejectReason { return aggregator.AllRejectReasons() }

// --- consumer re-exports ---------------------------------------------------

// Event is the re-exported normalized event passed through the consumer.
type Event = consumer.Event

// EventStream is the re-exported stream interface.
type EventStream = consumer.EventStream

// Consumer is the re-exported consumer type.
type Consumer = consumer.Consumer

// LRUDedup is the re-exported in-memory dedup implementation.
type LRUDedup = consumer.LRUDedup

// ErrStreamClosed is the re-exported clean-termination sentinel.
var ErrStreamClosed = consumer.ErrStreamClosed

// NewConsumer constructs a consumer wired to the supplied stream / aggregator / dedup.
func NewConsumer(stream EventStream, agg *Aggregator, dedup *LRUDedup) *Consumer {
	return consumer.New(stream, agg, dedup)
}

// NewLRUDedup constructs a fresh in-memory dedup with the given capacity.
func NewLRUDedup(capacity int) *LRUDedup { return consumer.NewLRUDedup(capacity) }

// --- server re-exports -----------------------------------------------------

// ReadinessChecker is the re-exported readiness interface.
type ReadinessChecker = server.ReadinessChecker

// Handler returns the http.Handler that exposes /metrics, /healthz, /readyz.
func Handler(agg *Aggregator, ready ReadinessChecker) http.Handler {
	return server.Handler(agg, ready)
}
