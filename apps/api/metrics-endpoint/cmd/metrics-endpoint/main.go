// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command metrics-endpoint is the Prometheus /metrics aggregator service
// (F010).
//
// Lifecycle:
//
//  1. Load config from --config (default: /etc/openllm-metrics/metrics-endpoint.yaml).
//  2. Construct an in-memory aggregator, an LRU dedup, and a bus stream
//     subscribed to llm.usage.normalized and llm.runtime.normalized.
//  3. Start an HTTP server exposing /metrics, /healthz, /readyz.
//  4. Drain the bus into the aggregator until SIGINT / SIGTERM.
//
// The aggregator state is purely in-memory: on restart it is rebuilt by
// replaying the bus from the earliest retained offset. Idempotency is
// enforced in the dedup layer so a replay does not double-count.
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

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/aggregator"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/consumer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/server"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-metrics/metrics-endpoint.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("metrics-endpoint exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	logger.Info("starting metrics-endpoint",
		"port", cfg.Server.Port,
		"brokers", cfg.Bus.Brokers,
		"group", cfg.Bus.Group,
		"topics", cfg.Bus.Topics,
		"replay_window_hours", cfg.Replay.WindowHours,
	)

	agg := aggregator.New()

	stream, err := consumer.NewBusStream(consumer.BusStreamConfig{
		Brokers:  cfg.Bus.Brokers,
		Topics:   cfg.Bus.Topics,
		Group:    cfg.Bus.Group,
		ClientID: cfg.Bus.ClientID,
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	// Dedup capacity sized to roughly one window of normalized traffic per
	// metric family. Default works for tens of thousands of events per
	// hour; operators tune via the replay window in config.
	dedupCapacity := 100_000
	dedup := consumer.NewLRUDedup(dedupCapacity)
	cons := consumer.New(stream, agg, dedup)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           server.Handler(agg, cons),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server failed", "err", err)
		}
	}()

	logger.Info("metrics-endpoint up",
		"metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics",
		"healthz", ":"+strconv.Itoa(cfg.Server.Port)+"/healthz",
		"readyz", ":"+strconv.Itoa(cfg.Server.Port)+"/readyz",
	)

	runErr := cons.Run(ctx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)

	if errors.Is(runErr, context.Canceled) {
		return nil
	}
	return runErr
}
