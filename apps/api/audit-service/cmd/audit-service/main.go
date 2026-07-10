// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command audit-service is the F031 append-only audit ledger service.
//
// Lifecycle:
//
//  1. Load config from --config (default /etc/openllm-audit-service/config.yaml).
//  2. Open the audit Postgres pool.
//  3. Subscribe to the audit.event.v1 bus topic.
//  4. Start an HTTP server exposing:
//     /v1/audit/entries, /v1/audit/entries/{id}, /v1/audit/export,
//     /v1/audit/verify, /metrics, /healthz, /readyz.
//  5. Drain the bus into the store until SIGINT / SIGTERM.
//
// The ledger is append-only at the database level (rules + triggers). The
// service code never issues UPDATE or DELETE against audit.audit_entries.
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

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/consumer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/server"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-audit-service/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("audit-service exited with error", "err", err)
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
			logger.Error("audit http server failed", "err", err)
		}
	}()

	logger.Info("audit-service up",
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

// readyProbe flips to ready on the first observed bus record. The
// readinessAdapter / readinessCounter wrappers below intercept the
// consumer's Append + counter calls to flip the bit.
type readyProbe struct {
	ready atomic.Bool
}

// Ready satisfies the server.ReadinessChecker interface.
func (p *readyProbe) Ready() bool { return p.ready.Load() }

// markReady flips the ready bit on the first observed event.
func (p *readyProbe) markReady() { p.ready.Store(true) }

// readinessAdapter wraps a Sink (the store) so the first successful append
// flips the readiness bit. The bus consumer talks to this adapter; the
// adapter forwards to the underlying store.
type readinessAdapter struct {
	sink  consumer.Sink
	ready *readyProbe
}

func (a readinessAdapter) Append(ctx context.Context, in store.AppendInput) (store.Entry, error) {
	out, err := a.sink.Append(ctx, in)
	if err == nil {
		a.ready.markReady()
	}
	return out, err
}

// readinessCounter wraps the metrics.Registry so the readiness bit also
// flips on validation rejects (an event arrived; the consumer is alive).
type readinessCounter struct {
	wrapped *metrics.Registry
	ready   *readyProbe
}

func (c readinessCounter) IncAppend()           { c.ready.markReady(); c.wrapped.IncAppend() }
func (c readinessCounter) IncAppendFailure()    { c.ready.markReady(); c.wrapped.IncAppendFailure() }
func (c readinessCounter) IncRedactionReject()  { c.ready.markReady(); c.wrapped.IncRedactionReject() }
func (c readinessCounter) IncValidationReject() { c.ready.markReady(); c.wrapped.IncValidationReject() }
