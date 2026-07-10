// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command policy-service is the F029 policy schema / storage / versioning
// service. It is OSS-safe: it owns the data layer for policy documents and
// does NOT evaluate policies or make enforcement decisions. Evaluation is outside this service.
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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/handler"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/server"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/store"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/validator"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-metrics/policy-service.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("policy-service exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	logger.Info("starting policy-service",
		"port", cfg.Server.Port,
		"schema_path", cfg.Schema.Path,
		"bus_enabled", cfg.Bus.Enabled,
	)

	v, err := validator.New(cfg.Schema.Path)
	if err != nil {
		return err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DB.DSN)
	if err != nil {
		return err
	}
	poolCfg.MaxConns = int32(cfg.DB.MaxOpenConns)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	var bus *busproducer.Producer
	if cfg.Bus.Enabled {
		bus, err = busproducer.New(busproducer.Config{
			Brokers:  cfg.Bus.Brokers,
			ClientID: cfg.Bus.ClientID,
			Topic:    cfg.Bus.AuditTopic,
		})
		if err != nil {
			return err
		}
	} else {
		bus = busproducer.NewDisabled()
		logger.Warn("audit bus disabled — mutations will not emit audit events")
	}
	defer bus.Close()

	counters := metrics.New()
	deps := &handler.Deps{
		Store:     store.New(pool),
		Validator: v,
		Bus:       bus,
		Metrics:   counters,
	}

	httpServer := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           server.Handler(deps, counters),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("policy-service http failed", "err", err)
		}
	}()

	logger.Info("policy-service up",
		"port", cfg.Server.Port,
		"endpoints", []string{
			"/v1/policies",
			"/v1/policies/{id}",
			"/v1/policies/{id}/versions",
			"/v1/policies/{id}/versions/{n}",
			"/v1/policies/{id}/validate",
		},
	)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
