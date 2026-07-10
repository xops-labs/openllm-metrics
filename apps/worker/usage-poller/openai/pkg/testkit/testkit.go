// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package testkit re-exports the narrow surface that cross-module contract
// tests (tests/provider-adapters/openai) need. internal/ packages cannot be
// imported across module boundaries; this package is the explicit, audited
// bridge.
//
// Production code MUST NOT depend on testkit. It exists solely so the
// reference contract tests can build the same wiring the binary does
// without relaxing the internal/ guard on the rest of the codebase.
package testkit

import (
	"context"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/adapter"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/dedup"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/openaiclient"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/poller"
)

// Re-exports so the contract test file can avoid importing internal/ paths.

// NormalizedEvent is the canonical event shape (re-export of internal/adapter).
type NormalizedEvent = adapter.NormalizedEvent

// ContextLabels mirrors the operator-supplied labels carried on every event.
type ContextLabels = adapter.ContextLabels

// CombinedWindow pairs a usage + cost response (re-export of internal/openaiclient).
type CombinedWindow = openaiclient.CombinedWindow

// RateLimitInfo describes parsed rate-limit headers from a response.
type RateLimitInfo = openaiclient.RateLimitInfo

// Key is the dedup tuple from F009 §9.
type Key = dedup.Key

// LRU is the in-memory dedup set.
type LRU = dedup.LRU

// Registry is the metrics registry surface used by the poller.
type Registry = metrics.Registry

// Emitter is the bus-emitter interface; tests substitute an in-memory stub.
type Emitter = busproducer.Emitter

// ClientConfig is the OpenAI HTTP client configuration shape.
type ClientConfig = openaiclient.Config

// Client is the OpenAI HTTP client with backoff + breaker.
type Client = openaiclient.Client

// PollerConfig is the cycle scheduler configuration shape.
type PollerConfig = poller.Config

// Poller is the cycle scheduler.
type Poller = poller.Poller

// Path / header constants re-exported for httptest wiring.
const (
	UsagePath          = openaiclient.UsagePath
	CostPath           = openaiclient.CostPath
	HeaderRequestID    = openaiclient.HeaderRequestID
	SourceServiceLabel = adapter.SourceService
	ProviderName       = adapter.ProviderName
)

// Sentinel errors re-exported for test assertions.
var (
	ErrCircuitOpen = openaiclient.ErrCircuitOpen
	ErrServerError = openaiclient.ErrServerError
)

// NewClient builds a new HTTP client.
func NewClient(cfg ClientConfig) *Client { return openaiclient.New(cfg) }

// NewLRU builds a dedup cache.
func NewLRU(cap int) *LRU { return dedup.NewLRU(cap) }

// NewMetrics builds a metrics registry.
func NewMetrics(provider, tenant, env string) *Registry { return metrics.New(provider, tenant, env) }

// NewPoller wires a poller from its dependencies.
func NewPoller(cfg PollerConfig, fetcher poller.Fetcher, lru *LRU, emitter Emitter, m *Registry) *Poller {
	return poller.New(cfg, fetcher, lru, emitter, m)
}

// Normalize lifts the in-tree normalizer to the test boundary.
func Normalize(window CombinedWindow, labels ContextLabels, now func() time.Time) ([]NormalizedEvent, error) {
	return adapter.Normalize(window, labels, now)
}

// FetcherFunc is an adapter so tests can inject a single function as a Fetcher.
type FetcherFunc func(ctx context.Context, start, end time.Time) (CombinedWindow, RateLimitInfo, error)

// FetchWindow satisfies poller.Fetcher.
func (f FetcherFunc) FetchWindow(ctx context.Context, start, end time.Time) (CombinedWindow, RateLimitInfo, error) {
	return f(ctx, start, end)
}

// CircuitOpen always returns false for a function-backed Fetcher.
func (f FetcherFunc) CircuitOpen() bool { return false }
