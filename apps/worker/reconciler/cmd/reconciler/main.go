// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command reconciler is the Phase D Pull-Mode / Proxy-Mode Reconciliation
// Framework worker.
//
// It subscribes to two bus topics:
//
//   - llm.cost.estimated   — runtime cost estimates from cost-mapper
//     (source = gateway | sdk).
//   - llm.usage.reconciled — vendor-reconciled cost from focus-ingester
//     (source = exporter).
//
// It joins them in a windowed correlation by
// (tenant, provider, model, window-start), computes
//
//	drift_usd   = reconciled_cost_usd - estimated_cost_usd
//	drift_ratio = drift_usd / max(estimated_cost_usd, 0.0001)
//
// upserts the join into control_plane.reconciliation_results, refreshes the
// Prometheus drift series, and emits a reconciliation.window.v1 event when
// a window closes (after window_end + grace_period).
//
// What the reconciler is NOT (deliberate): it never
// makes routing, fallback, scoring, or budget decisions on the drift. The
// drift number is the signal; what to do with it is F033 (OSS
// notifications), F034 / F035 decisioning, or F027
// (dashboards).
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
	"syscall"
	"time"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/closer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/consumer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/joiner"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/reconciler/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-reconciler/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("reconciler exited with error", "err", err)
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

	producer, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Bus.Brokers,
		ClientID: cfg.Bus.ClientID,
	})
	if err != nil {
		return err
	}
	emitter := busproducer.New(producer, cfg.Bus.WindowTopic)
	defer emitter.Close()

	mreg := metrics.New(cfg.Defaults.Tenant, cfg.Defaults.Env)
	j := joiner.New(pgStore, cfg.WindowSize())
	c := closer.New(pgStore, emitter, mreg, j, closer.Config{
		Grace:     cfg.GracePeriod(),
		BatchSize: 256,
	}, logger)

	busConsumer, err := busclient.NewConsumer(busclient.ConsumerConfig{
		Config: busclient.Config{
			Brokers:  cfg.Bus.Brokers,
			ClientID: cfg.Bus.ClientID,
		},
		Group:  cfg.Bus.ConsumerGroup,
		Topics: []string{cfg.Bus.EstimatedTopic, cfg.Bus.ReconciledTopic},
	})
	if err != nil {
		return err
	}
	defer busConsumer.Close()

	handler := consumer.New(consumer.Config{
		EstimatedTopic:  cfg.Bus.EstimatedTopic,
		ReconciledTopic: cfg.Bus.ReconciledTopic,
	}, j, mreg)

	mux := http.NewServeMux()
	mux.Handle("/metrics", mreg.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	server := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server failed", "err", err)
		}
	}()

	// Closer runs on its own cadence in a background goroutine. The
	// consumer Run loop blocks on the main goroutine.
	closerCtx, cancelCloser := context.WithCancel(ctx)
	defer cancelCloser()
	go func() {
		if err := c.Run(closerCtx, cfg.ScanInterval()); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("closer loop exited", "err", err)
		}
	}()

	logger.Info("starting reconciler",
		"estimated_topic", cfg.Bus.EstimatedTopic,
		"reconciled_topic", cfg.Bus.ReconciledTopic,
		"window_topic", cfg.Bus.WindowTopic,
		"window_size", cfg.WindowSize().String(),
		"grace_period", cfg.GracePeriod().String(),
		"closer_interval", cfg.ScanInterval().String(),
		"metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics",
	)

	err = busConsumer.Run(ctx, handler.Handle)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
