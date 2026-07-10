// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command notifier is the F033 OSS notification fan-out worker.
//
// It subscribes to alert.event.v1 on the streaming bus, matches each event
// against per-tenant routing rules in Postgres, fans out to configured
// generic-webhook and SMTP sinks, retries transient failures with
// exponential backoff, and records every attempt in
// control_plane.notification_deliveries.
//
// The same binary also serves the config CRUD HTTP API
// (/v1/notification/channels, /v1/notification/rules) plus /metrics and
// /healthz. Every successful CRUD mutation emits an audit.event.v1 for
// F031 to hash-chain into the audit ledger.
//
// Vendor-branded integrations (Slack, PagerDuty, Teams, ServiceNow) are
// custom (Phase I); the DB CHECK constraint and the dispatch switch
// statement both reject any kind other than 'webhook' or 'smtp'.
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

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/consumer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/server"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/sink"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/store"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-notifier/config.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("notifier exited with error", "err", err)
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

	// Audit producer.
	auditProducer, err := busclient.NewProducer(busclient.Config{
		Brokers:  cfg.Bus.Brokers,
		ClientID: cfg.Bus.ClientID + "-audit",
	})
	if err != nil {
		return err
	}
	emitter := busproducer.New(auditProducer, cfg.Bus.AuditTopic)
	defer emitter.Close()

	// Alert consumer.
	busConsumer, err := busclient.NewConsumer(busclient.ConsumerConfig{
		Config: busclient.Config{
			Brokers:  cfg.Bus.Brokers,
			ClientID: cfg.Bus.ClientID,
		},
		Group:  cfg.Bus.GroupID,
		Topics: []string{cfg.Bus.AlertTopic},
	})
	if err != nil {
		return err
	}
	defer busConsumer.Close()

	mreg := metrics.New()
	webhookSink := sink.NewWebhookSink(cfg.PerAttemptTimeout())
	smtpSink := sink.NewSMTPSink(cfg.PerAttemptTimeout())

	cons := consumer.New(busConsumer, pgStore, webhookSink, smtpSink, mreg, logger, consumer.RetryParams{
		MaxAttempts:       cfg.Retry.MaxAttempts,
		InitialBackoff:    cfg.InitialBackoff(),
		MaxBackoff:        cfg.MaxBackoff(),
		PerAttemptTimeout: cfg.PerAttemptTimeout(),
	})

	// HTTP server (config CRUD + metrics + healthz).
	apiServer := server.New(pgStore, emitter, mreg, logger)
	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           apiServer.Routes(mreg.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("notifier http server failed", "err", err)
		}
	}()

	logger.Info("starting notifier",
		"alert_topic", cfg.Bus.AlertTopic,
		"audit_topic", cfg.Bus.AuditTopic,
		"group_id", cfg.Bus.GroupID,
		"http", ":"+strconv.Itoa(cfg.Server.Port),
		"max_attempts", cfg.Retry.MaxAttempts,
	)

	// Run the consumer until ctx is cancelled.
	consumerErr := cons.Run(ctx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)

	if errors.Is(consumerErr, context.Canceled) {
		return nil
	}
	return consumerErr
}
