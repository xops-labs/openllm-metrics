// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command quota-risk is the Phase G worker for feature F026.
//
// It subscribes to llm.runtime.normalized (source=gateway|sdk) and
// llm.usage.normalized (source=exporter) on the streaming bus, extracts
// rate-limit signals from provider response headers carried on the event
// payload, maintains a rolling per-(tenant, provider, model, region) view,
// and:
//
//  1. Exposes Prometheus gauges:
//     llm_quota_used_ratio
//     llm_quota_seconds_to_reset
//     llm_quota_risk_score
//
//  2. Publishes a `quota.risk.v1` event per (key, kind) on every refresh
//     cycle so other OSS subscribers can react.
//
// SCOPE GUARDRAIL. This worker MODELS risk. It does NOT enforce routing,
// throttling, fallback, or budget decisions — that logic is outside this worker. Adding any "if risk > X then divert traffic" code here would
// violate this worker's signal-only scope.
//
// Event payload convention. The worker reads provider headers from a
// best-effort `provider_headers` map embedded by the producer (gateway or
// SDK) on the event. When the field is absent the worker still consumes
// the event for ingest accounting but cannot produce a Signal. A future
// schema version may promote this to a top-level field; until then the
// worker tolerates both shapes.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/model"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/quota-risk/internal/parser"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-quota-risk/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("quota-risk exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	topics := cfg.Bus.InputTopics
	if len(topics) == 0 {
		topics = []string{
			telemetrycontracts.TopicRuntimeNormalized,
			telemetrycontracts.TopicUsageNormalized,
		}
	}

	consumer, err := busclient.NewConsumer(busclient.ConsumerConfig{
		Config: busclient.Config{
			Brokers:  cfg.Bus.Brokers,
			ClientID: cfg.Bus.ClientID,
		},
		Group:  cfg.Bus.ConsumerGroup,
		Topics: topics,
	})
	if err != nil {
		return fmt.Errorf("quota-risk: build consumer: %w", err)
	}
	defer consumer.Close()

	producer, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Bus.Brokers,
		ClientID: cfg.Bus.ClientID + "-producer",
	})
	if err != nil {
		return fmt.Errorf("quota-risk: build producer: %w", err)
	}
	emitter := busproducer.New(producer, cfg.Bus.OutputTopic)
	defer emitter.Close()

	mreg := metrics.New()
	mdl := model.New(cfg.Window())

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

	logger.Info("starting quota-risk worker",
		"input_topics", topics,
		"output_topic", cfg.Bus.OutputTopic,
		"window", cfg.Window().String(),
		"refresh", cfg.RefreshInterval().String(),
		"metrics_endpoint", ":"+strconv.Itoa(cfg.Server.Port)+"/metrics",
	)

	// Consumer goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := consumer.Run(ctx, func(_ context.Context, record *kgo.Record) error {
			handleRecord(record, mreg, mdl, cfg.Defaults, logger)
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("consumer loop ended", "err", err)
		}
	}()

	// Refresh goroutine — periodically snapshots the model into the gauge
	// registry and emits one quota.risk.v1 per non-stale row.
	wg.Add(1)
	go func() {
		defer wg.Done()
		refreshLoop(ctx, cfg.RefreshInterval(), mdl, mreg, emitter, logger)
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	wg.Wait()
	return nil
}

// envelope is the subset of normalized-event fields the worker needs.
// We deliberately do NOT unmarshal into the full F008 struct — that would
// brittle-couple this worker to schema additions and we only need a
// handful of fields. Forward-compat: unknown fields are ignored.
//
// `provider` may appear as a flat string (F008 canonical) OR as a nested
// object { name, headers } in producer extensions. decodeEnvelope handles
// both shapes via a permissive map.
type envelope struct {
	Tenant          string
	Provider        string
	Model           string
	Region          string
	ProviderHeaders map[string]string
}

// handleRecord decodes one bus record, runs the matching provider parser,
// and folds the result into the rolling model.
func handleRecord(
	record *kgo.Record,
	mreg *metrics.Registry,
	mdl *model.Model,
	defaults config.DefaultLabels,
	logger *slog.Logger,
) {
	mreg.IncConsumed()

	// `provider` collides with the nested-shape attempt above; decode into
	// a permissive map first to avoid Go's struct tag clash.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(record.Value, &raw); err != nil {
		mreg.IncSkipped("decode")
		return
	}
	ev := decodeEnvelope(raw)

	if ev.Provider == "" {
		mreg.IncSkipped("missing_provider")
		return
	}
	if len(ev.ProviderHeaders) == 0 {
		mreg.IncSkipped("no_headers")
		return
	}

	p := parser.ByProvider(ev.Provider)
	if p == nil {
		mreg.IncSkipped("unknown_provider")
		return
	}
	sig := p.Parse(ev.ProviderHeaders)
	if !sig.HasTokens && !sig.HasRequests {
		mreg.IncSkipped("no_signal")
		return
	}

	tenant := ev.Tenant
	if tenant == "" {
		tenant = defaults.Tenant
	}
	if tenant == "" {
		mreg.IncSkipped("missing_tenant")
		return
	}

	mdl.Observe(model.Key{
		Tenant:   tenant,
		Provider: ev.Provider,
		Model:    ev.Model,
		Region:   ev.Region,
	}, sig)

	_ = logger // reserved for future structured warnings
}

// decodeEnvelope pulls the fields we need out of a permissive map, tolerating
// either flat or nested header conventions.
func decodeEnvelope(raw map[string]json.RawMessage) envelope {
	var e envelope
	if v, ok := raw["tenant"]; ok {
		_ = json.Unmarshal(v, &e.Tenant)
	}
	if v, ok := raw["provider"]; ok {
		// String form is the canonical case (F008 enum). Try that first.
		if err := json.Unmarshal(v, &e.Provider); err != nil || e.Provider == "" {
			// Nested object form: { provider: { name, headers } }.
			var nested struct {
				Name    string            `json:"name"`
				Headers map[string]string `json:"headers"`
			}
			if json.Unmarshal(v, &nested) == nil {
				e.Provider = nested.Name
				if e.ProviderHeaders == nil {
					e.ProviderHeaders = nested.Headers
				}
			}
		}
	}
	if v, ok := raw["model"]; ok {
		_ = json.Unmarshal(v, &e.Model)
	}
	if v, ok := raw["region"]; ok {
		_ = json.Unmarshal(v, &e.Region)
	}
	if v, ok := raw["provider_headers"]; ok && e.ProviderHeaders == nil {
		_ = json.Unmarshal(v, &e.ProviderHeaders)
	}
	// Canonicalize provider slug.
	e.Provider = strings.ToLower(strings.TrimSpace(e.Provider))
	return e
}

func refreshLoop(
	ctx context.Context,
	interval time.Duration,
	mdl *model.Model,
	mreg *metrics.Registry,
	emitter busproducer.Emitter,
	logger *slog.Logger,
) {
	tick := func() {
		rows := mdl.Snapshot()
		mreg.UpsertSnapshot(rows)
		now := time.Now().UTC().Format(time.RFC3339)
		for _, row := range rows {
			used, hasDenom := row.State.UsedRatio()
			risk, _ := row.State.RiskScore()
			ev := busproducer.Event{
				SchemaVersion:   busproducer.SchemaVersion,
				EventID:         uuid.NewString(),
				Source:          "worker",
				SourceService:   busproducer.SourceService,
				Tenant:          row.Key.Tenant,
				Provider:        row.Key.Provider,
				Model:           row.Key.Model,
				Region:          row.Key.Region,
				Kind:            string(row.State.Kind),
				Remaining:       row.State.Remaining,
				Limit:           row.State.Limit,
				UsedRatio:       used,
				HasDenominator:  hasDenom,
				SecondsToReset:  row.State.SecondsToReset(),
				RiskScore:       risk,
				ObservedAt:      row.State.UpdatedAt.Format(time.RFC3339),
				SnapshotEmitted: now,
			}
			if err := emitter.Emit(ctx, ev); err != nil {
				logger.Warn("emit failed", "tenant", row.Key.Tenant, "provider", row.Key.Provider, "kind", ev.Kind, "err", err)
				continue
			}
			mreg.IncEmitted()
		}
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
