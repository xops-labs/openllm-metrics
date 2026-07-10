// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Command gateway is the OpenLLM Metrics LLM proxy gateway (F018).
//
// Lifecycle:
//
//  1. Load YAML config from --config (env vars override per-provider
//     upstream URLs).
//  2. Construct the metrics registry, bus producer (or NoopEmitter when
//     no brokers are configured), and observer.
//  3. Build per-provider httputil.ReverseProxy instances.
//  4. Start the proxy listener and the metrics listener on separate ports.
//  5. Block until SIGINT / SIGTERM, then drain both with a bounded
//     shutdown deadline.
//
// Privacy invariant: this binary never logs request bodies, response
// bodies, prompts, completions, or provider API keys.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/observer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/proxy"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/server"
)

func main() {
	configPath := flag.String("config", "/etc/openllm-metrics/gateway.yaml", "path to YAML config")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(*configPath, logger); err != nil {
		logger.Error("gateway exited with error", "err", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	upstreams, err := parseUpstreams(cfg.Upstreams)
	if err != nil {
		return err
	}

	logger.Info("starting gateway",
		"proxy_port", cfg.Server.Port,
		"metrics_port", cfg.Server.MetricsPort,
		"upstream_openai", safeURL(upstreams.OpenAI),
		"upstream_anthropic", safeURL(upstreams.Anthropic),
		"upstream_gemini", safeURL(upstreams.Gemini),
		"upstream_bedrock", safeURL(upstreams.Bedrock),
		"upstream_azure_openai", safeURL(upstreams.AzureOpenAI),
		"bus_enabled", len(cfg.Bus.Brokers) > 0,
	)

	reg := metrics.New()

	emitter, closeEmitter, err := buildEmitter(cfg.Bus, logger)
	if err != nil {
		return err
	}
	defer closeEmitter()

	obs := observer.New(reg, emitter, observer.Defaults{
		Tenant:  cfg.Defaults.Tenant,
		Team:    cfg.Defaults.Team,
		App:     cfg.Defaults.App,
		Env:     cfg.Defaults.Env,
		Project: cfg.Defaults.Project,
	})

	proxyHandler, err := proxy.New(proxy.Options{
		Upstreams:       upstreams,
		Observer:        obs,
		Logger:          logger,
		UpstreamTimeout: time.Duration(cfg.Server.UpstreamTimeoutSeconds) * time.Second,
	})
	if err != nil {
		return err
	}

	srv := server.New(server.Options{
		ProxyPort:         cfg.Server.Port,
		MetricsPort:       cfg.Server.MetricsPort,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		ProxyHandler:      proxyHandler,
		Metrics:           reg,
	})

	logger.Info("gateway up",
		"proxy", srv.ProxyAddr(),
		"metrics", srv.MetricsAddr()+"/metrics",
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return srv.Run(ctx)
}

func parseUpstreams(uc config.UpstreamsConfig) (proxy.UpstreamMap, error) {
	var out proxy.UpstreamMap
	parse := func(raw string) (*url.URL, error) {
		if raw == "" {
			return nil, nil
		}
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, &url.Error{Op: "parse", URL: raw}
		}
		return u, nil
	}
	var err error
	if out.OpenAI, err = parse(uc.OpenAI); err != nil {
		return out, err
	}
	if out.Anthropic, err = parse(uc.Anthropic); err != nil {
		return out, err
	}
	if out.Gemini, err = parse(uc.Gemini); err != nil {
		return out, err
	}
	if out.Bedrock, err = parse(uc.Bedrock); err != nil {
		return out, err
	}
	if out.AzureOpenAI, err = parse(uc.AzureOpenAI); err != nil {
		return out, err
	}
	return out, nil
}

func safeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

func buildEmitter(bc config.BusConfig, logger *slog.Logger) (busproducer.Emitter, func(), error) {
	if len(bc.Brokers) == 0 {
		logger.Warn("gateway: bus.brokers empty — runtime events will be dropped after metrics increment")
		return busproducer.NoopEmitter{}, func() {}, nil
	}
	producer, err := busclient.NewProducer(busclient.Config{
		Brokers:  bc.Brokers,
		ClientID: bc.ClientID,
	})
	if err != nil {
		return nil, func() {}, err
	}
	em := busproducer.New(producer, logger)
	return em, em.Close, nil
}
