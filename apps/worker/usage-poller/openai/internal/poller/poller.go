// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package poller wires the OpenAI client, adapter, dedup cache, bus
// producer, and metrics registry into a single cycle loop.
//
// The Run loop is intentionally small. Everything provider-specific lives
// behind one of the dependency interfaces below so the scheduler itself is
// reusable verbatim by F013-F016 (Anthropic, Gemini, Azure OpenAI, Bedrock).
package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/adapter"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/dedup"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/openaiclient"
)

// Fetcher is the narrow client surface the loop depends on. The production
// implementation is *openaiclient.Client; tests inject a stub.
type Fetcher interface {
	FetchWindow(ctx context.Context, start, end time.Time) (openaiclient.CombinedWindow, openaiclient.RateLimitInfo, error)
	CircuitOpen() bool
}

// Config holds the runtime knobs for a single poller instance.
type Config struct {
	Interval      time.Duration
	WindowSize    time.Duration // defaults to Interval when zero
	ContextLabels adapter.ContextLabels
	Logger        *slog.Logger
	// Now is injectable for deterministic tests.
	Now func() time.Time
}

// Poller is the cycle runner.
type Poller struct {
	cfg     Config
	fetcher Fetcher
	dedup   *dedup.LRU
	emitter busproducer.Emitter
	metrics *metrics.Registry
}

// New constructs a Poller with sane defaults.
func New(cfg Config, fetcher Fetcher, lru *dedup.LRU, emitter busproducer.Emitter, m *metrics.Registry) *Poller {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = cfg.Interval
	}
	return &Poller{
		cfg:     cfg,
		fetcher: fetcher,
		dedup:   lru,
		emitter: emitter,
		metrics: m,
	}
}

// Run blocks until ctx is cancelled, executing one cycle on entry and
// then every Interval. Errors per cycle are logged + counted but do NOT
// stop the loop.
func (p *Poller) Run(ctx context.Context) error {
	// Tick once immediately so the first scrape happens before the operator
	// has to wait Interval seconds for the first data point.
	p.runCycle(ctx)

	t := time.NewTicker(p.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			p.runCycle(ctx)
		}
	}
}

// RunOnce executes one cycle and returns. Used by tests and by the
// idempotency contract test in tests/provider-adapters/openai.
func (p *Poller) RunOnce(ctx context.Context) (emitted int, dropped int, err error) {
	return p.runCycleReporting(ctx)
}

func (p *Poller) runCycle(ctx context.Context) {
	if _, _, err := p.runCycleReporting(ctx); err != nil {
		p.cfg.Logger.Warn("poll cycle failed", "err", err)
	}
}

// runCycleReporting is the core cycle: pick a window, fetch, normalize,
// dedup, emit. Returns the number of events emitted and dropped (already
// seen) plus any cycle-level error. Per-event emit errors are logged but
// do not abort the cycle.
func (p *Poller) runCycleReporting(ctx context.Context) (int, int, error) {
	now := p.cfg.Now()
	end := now.Truncate(time.Minute)
	start := end.Add(-p.cfg.WindowSize)

	window, rateInfo, err := p.fetcher.FetchWindow(ctx, start, end)
	if rateInfo.HitRateLimit {
		p.metrics.IncRateLimitEvent()
	}
	if err != nil {
		p.metrics.IncScrapeFailure()
		switch {
		case errors.Is(err, openaiclient.ErrCircuitOpen):
			p.metrics.IncProviderAPIError("circuit_open")
		case errors.Is(err, openaiclient.ErrRateLimited):
			p.metrics.IncProviderAPIError("rate_limited")
		case errors.Is(err, openaiclient.ErrServerError):
			p.metrics.IncProviderAPIError("5xx")
		default:
			p.metrics.IncProviderAPIError("network")
		}
		return 0, 0, fmt.Errorf("fetch window [%s,%s): %w", start.Format(time.RFC3339), end.Format(time.RFC3339), err)
	}

	events, err := adapter.Normalize(window, p.cfg.ContextLabels, p.cfg.Now)
	if err != nil {
		p.metrics.IncScrapeFailure()
		p.metrics.IncProviderAPIError("normalize")
		return 0, 0, fmt.Errorf("normalize: %w", err)
	}

	var emitted, dropped int
	for _, ev := range events {
		key := dedup.Key{
			Provider:    ev.Provider,
			WindowStart: ev.PeriodStart,
			WindowEnd:   ev.PeriodEnd,
			Model:       ev.Model,
			App:         ev.App,
			Team:        ev.Team,
			Project:     ev.Project,
		}
		if !p.dedup.Seen(key) {
			dropped++
			continue
		}
		if err := p.emitter.Emit(ctx, ev); err != nil {
			p.metrics.IncProviderAPIError("bus")
			p.cfg.Logger.Warn("emit failed", "event_id", ev.EventID, "err", err)
			continue
		}
		emitted++
	}
	p.metrics.IncScrapeSuccess()
	return emitted, dropped, nil
}
