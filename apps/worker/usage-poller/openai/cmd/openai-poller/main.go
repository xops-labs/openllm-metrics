// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command openai-poller is the OpenAI Usage + Cost poller binary (F009).
//
// Lifecycle:
//
//  1. Load config from --config (default: /etc/openllm-poller/openai.yaml).
//  2. Read the OpenAI Admin API key from the env var named by the config.
//  3. Construct an HTTP client (backoff + circuit breaker), a normalizer
//     adapter, an LRU dedup cache, a bus producer, and a metrics registry.
//  4. Start an HTTP server exposing /metrics and /healthz.
//  5. Run the poll loop until SIGINT / SIGTERM.
//
// The binary is intentionally small; all of the moving parts are in
// internal/ packages, each independently testable.
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

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/adapter"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/dedup"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/openaiclient"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/poller"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-poller/openai.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("poller exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if !cfg.Providers.OpenAI.Enabled {
		logger.Warn("openai provider disabled; nothing to do")
		return nil
	}

	apiKey, err := cfg.OpenAIAPIKey()
	if err != nil {
		return err
	}

	logger.Info("starting openai usage poller",
		"interval_seconds", cfg.Providers.OpenAI.PollingIntervalSeconds,
		"tenant", cfg.Labels.Tenant,
		"env", cfg.Labels.Env,
		"api_key", config.MaskAPIKey(apiKey),
		"base_url", cfg.Providers.OpenAI.BaseURL,
	)

	client := openaiclient.New(openaiclient.Config{
		BaseURL:                 cfg.Providers.OpenAI.BaseURL,
		APIKey:                  apiKey,
		HTTPClient:              &http.Client{Timeout: 30 * time.Second},
		MaxRetries:              cfg.Providers.OpenAI.MaxRetries,
		CircuitBreakerThreshold: cfg.Providers.OpenAI.CircuitBreakerThreshold,
		CircuitBreakerCooldown:  cfg.CircuitBreakerCooldown(),
	})

	producer, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Bus.Brokers,
		ClientID: cfg.Bus.ClientID,
	})
	if err != nil {
		return err
	}
	emitter := busproducer.New(producer)
	defer emitter.Close()

	mreg := metrics.New(adapter.ProviderName, cfg.Labels.Tenant, cfg.Labels.Env)
	lru := dedup.NewLRU(cfg.Providers.OpenAI.DedupCacheSize)

	p := poller.New(poller.Config{
		Interval:   cfg.PollingInterval(),
		WindowSize: cfg.PollingInterval(),
		ContextLabels: adapter.ContextLabels{
			Tenant:  cfg.Labels.Tenant,
			Team:    cfg.Labels.Team,
			App:     cfg.Labels.App,
			Env:     cfg.Labels.Env,
			Project: cfg.Labels.Project,
			Region:  cfg.Labels.Region,
		},
		Logger: logger,
	}, client, lru, emitter, mreg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// HTTP surface: /metrics + /healthz.
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

	logger.Info("running poll loop", "metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics")
	err = p.Run(ctx)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
