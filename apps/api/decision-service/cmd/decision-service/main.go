// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command decision-service is the F036 routing-decision ledger service.
//
// The decision-service stores and exposes routing decisions emitted by a
// registered routing.Decider implementation. It does NOT make decisions,
// it does NOT score candidates, and it does NOT rank alternatives — that
// logic is outside this service. The
// OSS no-op decider (packages/extensions/go/routing) emits trivial events;
// a registered decider may emit rich ones. This service stores whatever
// arrives.
//
// Lifecycle:
//
//  1. Load config from --config (default /etc/openllm-decision-service/config.yaml).
//  2. Open the decision-ledger Postgres pool.
//  3. Subscribe to the routing.decision.v1 bus topic.
//  4. Start an HTTP server exposing:
//     /v1/decisions, /v1/decisions/{decision_id}, /v1/decisions/stats,
//     /metrics, /healthz, /readyz.
//  5. Drain the bus into the store until SIGINT / SIGTERM.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/consumer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/server"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-decision-service/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("decision-service exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	dsn, err := cfg.DSN()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pgStore, err := store.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pgStore.Close()

	mreg := metrics.New()
	probe := &readyProbe{}

	cons, err := consumer.New(consumer.Config{
		Brokers:  cfg.Bus.Brokers,
		ClientID: cfg.Bus.ClientID,
		Group:    cfg.Bus.Group,
		Topic:    cfg.Bus.Topic,
	}, readinessAdapter{sink: pgStore, ready: probe}, readinessCounter{wrapped: mreg, ready: probe})
	if err != nil {
		return err
	}
	defer cons.Close()

	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           server.Handler(cfg, pgStore, mreg, probe),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("decision http server failed", "err", err)
		}
	}()

	logger.Info("decision-service up",
		"port", cfg.Server.Port,
		"brokers", cfg.Bus.Brokers,
		"topic", cfg.Bus.Topic,
		"group", cfg.Bus.Group,
	)

	runErr := cons.Run(ctx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout())
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)

	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}

// readyProbe flips to ready on the first observed bus record (success or
// rejection — both signal that the consumer is alive).
type readyProbe struct {
	ready atomic.Bool
}

// Ready satisfies server.ReadinessChecker.
func (p *readyProbe) Ready() bool { return p.ready.Load() }

// markReady flips the ready bit on the first observed event.
func (p *readyProbe) markReady() { p.ready.Store(true) }

// readinessAdapter wraps a Sink so the first successful append flips the
// readiness bit.
type readinessAdapter struct {
	sink  consumer.Sink
	ready *readyProbe
}

func (a readinessAdapter) Append(ctx context.Context, in store.AppendInput) error {
	err := a.sink.Append(ctx, in)
	if err == nil {
		a.ready.markReady()
	}
	return err
}

// readinessCounter wraps the metrics.Registry so the readiness bit also
// flips on validation rejects (an event arrived; the consumer is alive).
type readinessCounter struct {
	wrapped *metrics.Registry
	ready   *readyProbe
}

func (c readinessCounter) IncAppend()           { c.ready.markReady(); c.wrapped.IncAppend() }
func (c readinessCounter) IncAppendFailure()    { c.ready.markReady(); c.wrapped.IncAppendFailure() }
func (c readinessCounter) IncValidationReject() { c.ready.markReady(); c.wrapped.IncValidationReject() }
