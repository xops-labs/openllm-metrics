// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command cost-mapper is the Phase C runtime-side cost-mapping worker.
//
// It subscribes to two bus topics:
//
//   - llm.runtime.normalized — runtime events emitted by the gateway/SDK
//     (F008 source = gateway | sdk). For each event the worker joins
//     input/output token counts against the per-provider pricing catalog
//     (platform/pricing/*.yaml) and publishes a cost.estimated.v1 event on
//     llm.cost.estimated with the canonical tenant context preserved.
//
//   - llm.usage.reconciled — FOCUS-derived billing events emitted by the
//     focus-ingester. For each event the worker joins the accumulated
//     runtime estimate (same (tenant, provider, model, period) bucket) and
//     upserts a row into control_plane.cost_reconciliation_drift.
//
// The worker is OSS-safe by design: cost mapping is the pure-function
// "tokens × rate" operation; reconciliation drift is a subtraction. No
// scoring weights, routing logic, or policy thresholds live in this binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/catalog"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/mapper"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/reconciler"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/cost-mapper/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-cost-mapper/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("cost-mapper exited with error", "err", err)
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

	cat := catalog.New()
	if err := cat.LoadDir(cfg.Catalog.Dir); err != nil {
		return err
	}
	logger.Info("catalog loaded", "models", cat.Size(), "version", cat.Version())

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
	emitter := busproducer.New(producer, cfg.Bus.EstimatedTopic)
	defer emitter.Close()

	consumer, err := busclient.NewConsumer(busclient.ConsumerConfig{
		Config: busclient.Config{
			Brokers:  cfg.Bus.Brokers,
			ClientID: cfg.Bus.ClientID,
		},
		Group:  cfg.Bus.ConsumerGroup,
		Topics: []string{cfg.Bus.RuntimeTopic, cfg.Bus.ReconciledTopic},
	})
	if err != nil {
		return err
	}
	defer consumer.Close()

	m := mapper.New(cat)
	r := reconciler.New(pgStore)
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

	// Slow reload of the pricing catalog so PRs to platform/pricing/ land
	// without a restart. The reloader logs but never crashes the worker.
	go reloadLoop(ctx, cat, cfg.Catalog.Dir, cfg.CatalogReloadInterval(), logger)

	logger.Info("starting cost mapper",
		"runtime_topic", cfg.Bus.RuntimeTopic,
		"reconciled_topic", cfg.Bus.ReconciledTopic,
		"estimated_topic", cfg.Bus.EstimatedTopic,
		"metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics",
	)

	handler := func(hctx context.Context, record *kgo.Record) error {
		switch record.Topic {
		case cfg.Bus.RuntimeTopic:
			return handleRuntime(hctx, record, m, emitter, r, mreg, logger)
		case cfg.Bus.ReconciledTopic:
			return handleReconciled(hctx, record, r, mreg, logger)
		default:
			// Unknown topics get DLQ-ed.
			return errors.New("cost-mapper: unexpected topic " + record.Topic)
		}
	}

	err = consumer.Run(ctx, handler)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func handleRuntime(
	ctx context.Context,
	record *kgo.Record,
	m *mapper.Mapper,
	emitter busproducer.Emitter,
	rec *reconciler.Reconciler,
	mreg *metrics.Registry,
	logger *slog.Logger,
) error {
	mreg.IncRuntimeConsumed()

	var ev mapper.RuntimeEvent
	if err := json.Unmarshal(record.Value, &ev); err != nil {
		// Bad payload — DLQ via consumer.
		return err
	}
	// Only price events that came from a runtime surface. Pull-mode
	// (source=exporter) already carries reconciled cost and would
	// double-count if we re-priced it.
	if ev.Source != "gateway" && ev.Source != "sdk" {
		mreg.IncEstimateSkipped()
		return nil
	}
	if ev.InputTokens == 0 && ev.OutputTokens == 0 {
		mreg.IncEstimateSkipped()
		return nil
	}

	est, err := m.Estimate(ev)
	if err != nil {
		if errors.Is(err, catalog.ErrModelNotPriced) {
			// Counted, not failed — operators add the model to the
			// catalog via a PR.
			mreg.IncUnpriced()
			logger.Warn("unpriced (provider, model)", "provider", ev.Provider, "model", ev.Model)
			return nil
		}
		return err
	}

	if err := emitter.Emit(ctx, est); err != nil {
		return err
	}
	mreg.IncEstimateEmitted()

	// Accumulate the estimate into the reconciler's bucket so the next
	// matching FOCUS event can write a drift row. period_start/period_end
	// are derived from RecordedAt (truncated to the hour) because runtime
	// events do not carry an explicit period.
	if recordedAt, ok := parseTime(ev.RecordedAt); ok {
		key := reconciler.CorrelationKey{
			TenantID:    ev.Tenant,
			Provider:    ev.Provider,
			Model:       ev.Model,
			PeriodStart: recordedAt.Truncate(time.Hour),
			PeriodEnd:   recordedAt.Truncate(time.Hour).Add(time.Hour),
		}
		rec.RecordEstimate(reconciler.Estimate{
			Key:                key,
			Team:               ev.Team,
			App:                ev.App,
			Env:                ev.Env,
			Project:            ev.Project,
			EstimatedCostMinor: est.EstimatedCostUSDMinorUnits,
			CatalogVersion:     est.CatalogVersion,
		})
	}
	return nil
}

// reconciledEvent is the subset of the F008 llm.usage.reconciled payload
// the cost-mapper reads. Keeping it private here avoids a circular import
// with the focus-ingester.
type reconciledEvent struct {
	Tenant                      string `json:"tenant"`
	Provider                    string `json:"provider"`
	Model                       string `json:"model"`
	ReconciledCostUSDMinorUnits int64  `json:"reconciled_cost_usd_minor_units"`
	PeriodStart                 string `json:"period_start"`
	PeriodEnd                   string `json:"period_end"`
}

func handleReconciled(
	ctx context.Context,
	record *kgo.Record,
	rec *reconciler.Reconciler,
	mreg *metrics.Registry,
	logger *slog.Logger,
) error {
	mreg.IncReconciledConsumed()

	var ev reconciledEvent
	if err := json.Unmarshal(record.Value, &ev); err != nil {
		return err
	}
	if ev.Tenant == "" {
		// No tenant — nothing to correlate.
		return nil
	}
	start, ok1 := parseTime(ev.PeriodStart)
	end, ok2 := parseTime(ev.PeriodEnd)
	if !ok1 || !ok2 {
		return nil
	}

	key := reconciler.CorrelationKey{
		TenantID:    ev.Tenant,
		Provider:    ev.Provider,
		Model:       ev.Model,
		PeriodStart: start,
		PeriodEnd:   end,
	}
	if err := rec.ApplyReconciled(ctx, reconciler.Reconciled{
		Key:                 key,
		ReconciledCostMinor: ev.ReconciledCostUSDMinorUnits,
	}); err != nil {
		mreg.IncDriftError()
		logger.Warn("drift upsert failed", "err", err, "correlation_key", key.String())
		return err
	}
	mreg.IncDriftRow()
	return nil
}

func reloadLoop(
	ctx context.Context,
	cat *catalog.Catalog,
	dir string,
	interval time.Duration,
	logger *slog.Logger,
) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := cat.LoadDir(dir); err != nil {
				logger.Warn("catalog reload failed", "err", err, "dir", dir)
				continue
			}
			logger.Info("catalog reloaded", "models", cat.Size(), "version", cat.Version())
		}
	}
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
