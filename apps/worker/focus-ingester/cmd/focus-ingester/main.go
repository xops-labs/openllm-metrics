// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command focus-ingester is the Phase B worker that polls the upstream
// llm-usage-exporter /focus.json endpoint, persists every line item to
// control_plane.focus_records (append-only), and publishes a canonical
// llm.usage.reconciled event for each row to the streaming bus.
//
// Reconciliation drift dashboards join the events this ingester emits
// (reconciled cost) against the events emitted by the label-translator
// (estimated cost from runtime tokens), surfacing the gap between the
// platform's runtime estimate and the vendor's billed amount.
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

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/focus"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/ingester"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/focus-ingester/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-focus-ingester/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("focus-ingester exited with error", "err", err)
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

	pgStore, err := store.New(ctx, dsn, cfg.MappingCacheTTL())
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
	emitter := busproducer.New(producer)
	defer emitter.Close()

	client := focus.New(cfg.Focus.URL, cfg.PollTimeout())
	ing := ingester.New(client, pgStore, emitter)
	mreg := metrics.New(cfg.Defaults.Tenant, cfg.Defaults.Env)

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

	logger.Info("starting focus ingester",
		"focus_url", cfg.Focus.URL,
		"poll_interval", cfg.PollInterval().String(),
		"metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics",
	)

	err = runLoop(ctx, cfg.PollInterval(), ing, mreg, logger)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runLoop(
	ctx context.Context,
	interval time.Duration,
	ing *ingester.Ingester,
	mreg *metrics.Registry,
	logger *slog.Logger,
) error {
	cycle := func() {
		res, err := ing.RunOnce(ctx)
		if err != nil {
			mreg.IncPollFailure()
			logger.Warn("focus poll failed", "err", err)
			return
		}
		mreg.IncPollSuccess()
		mreg.AddFetched(res.Fetched)
		mreg.AddPersisted(res.Persisted)
		mreg.AddEmitted(res.Emitted)
		mreg.AddUnmapped(res.Unmapped)
		mreg.AddDropped(res.Dropped)
	}

	cycle()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			cycle()
		}
	}
}
