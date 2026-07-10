// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command label-translator is the Phase B worker that scrapes the upstream
// llm-usage-exporter /metrics endpoint, enriches each sample with the
// canonical {tenant, team, app, env, project} tuple from
// control_plane.label_mappings, and publishes the result to the streaming
// bus as llm.usage.normalized events with source=exporter.
//
// Lifecycle:
//
//  1. Load YAML config from --config (default: /etc/openllm-label-translator/config.yaml).
//  2. Open Postgres connection pool against the control-plane DSN.
//  3. Construct scraper, translator, and bus producer.
//  4. Serve /metrics + /healthz.
//  5. Run the scrape/translate/emit loop until SIGINT/SIGTERM.
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

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/scraper"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/store"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/label-translator/internal/translator"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-label-translator/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("label-translator exited with error", "err", err)
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

	mappings, err := store.New(ctx, dsn, cfg.MappingCacheTTL())
	if err != nil {
		return err
	}
	defer mappings.Close()

	producer, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Bus.Brokers,
		ClientID: cfg.Bus.ClientID,
	})
	if err != nil {
		return err
	}
	emitter := busproducer.New(producer)
	defer emitter.Close()

	sc := scraper.New(cfg.Exporter.URL, cfg.ScrapeTimeout())
	tr := translator.New(mappings, translator.Defaults{
		Tenant: cfg.Defaults.Tenant,
		Team:   cfg.Defaults.Team,
		Env:    cfg.Defaults.Env,
	})
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

	logger.Info("starting label translator",
		"exporter_url", cfg.Exporter.URL,
		"scrape_interval", cfg.ScrapeInterval().String(),
		"metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics",
	)

	err = runLoop(ctx, cfg.ScrapeInterval(), sc, tr, emitter, mreg, logger)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// runLoop ticks the scrape/translate/emit cycle. Errors in a single cycle
// are logged and counted but do not abort the loop — operators rely on
// llm_label_translator_scrape_failure_total / last-success-timestamp to
// detect sustained failure.
func runLoop(
	ctx context.Context,
	interval time.Duration,
	sc *scraper.Scraper,
	tr *translator.Translator,
	emitter busproducer.Emitter,
	mreg *metrics.Registry,
	logger *slog.Logger,
) error {
	cycle := func() {
		samples, err := sc.Scrape(ctx)
		if err != nil {
			mreg.IncScrapeFailure()
			logger.Warn("scrape failed", "err", err)
			return
		}
		events, res, err := tr.Translate(ctx, samples)
		if err != nil {
			mreg.IncScrapeFailure()
			logger.Warn("translate failed", "err", err)
			return
		}
		mreg.IncScrapeSuccess()
		mreg.AddSkipped(res.Skipped)
		mreg.AddDropped(res.Dropped)
		for _, ev := range events {
			if err := emitter.Emit(ctx, ev); err != nil {
				logger.Warn("emit failed", "event_id", ev.EventID, "err", err)
				continue
			}
		}
		mreg.AddEmitted(res.Emitted)
		// Bump unmapped per-provider. We don't know the per-event provider
		// here without re-iterating, so for v0 we attribute the full count
		// to the first provider in the emitted batch and let the dashboard
		// split. Operators get the signal.
		if res.Unmapped > 0 && len(events) > 0 {
			mreg.AddUnmapped(events[0].Provider, res.Unmapped)
		}
	}

	// First cycle immediately — this is the priming scrape that seeds the
	// counter table; no events are emitted on the first tick.
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
