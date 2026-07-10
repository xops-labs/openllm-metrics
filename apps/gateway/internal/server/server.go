// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server hosts the two HTTP listeners owned by the gateway:
//
//  1. The proxy listener (default :8080) — mounts the proxy handler at
//     "/" so it can match any provider route.
//  2. The metrics listener (default :8081) — exposes /metrics, /healthz,
//     /readyz on a separate port so the proxy port stays pure-proxy and
//     cannot accidentally leak operational endpoints to upstream clients.
//
// Both listeners share lifecycle: the Run method blocks until ctx is
// cancelled, then drains both servers with a bounded shutdown deadline.
package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/proxy"
)

// Options bundles the inputs needed to build a Server.
type Options struct {
	ProxyPort         int
	MetricsPort       int
	ReadHeaderTimeout time.Duration
	ProxyHandler      *proxy.Handler
	Metrics           *metrics.Registry
}

// Server holds the two http.Server instances.
type Server struct {
	proxySrv   *http.Server
	metricsSrv *http.Server
}

// New constructs a Server with the proxy and metrics surfaces wired up.
func New(opts Options) *Server {
	proxySrv := &http.Server{
		Addr:              ":" + strconv.Itoa(opts.ProxyPort),
		Handler:           opts.ProxyHandler,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", opts.Metrics.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	})
	metricsSrv := &http.Server{
		Addr:              ":" + strconv.Itoa(opts.MetricsPort),
		Handler:           mux,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
	}

	return &Server{proxySrv: proxySrv, metricsSrv: metricsSrv}
}

// Run starts both listeners and blocks until ctx is cancelled OR either
// listener exits with a non-ErrServerClosed error.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := s.proxySrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := s.metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		// shutdown deliberately uses a fresh timeout context — the run ctx
		// is already done here, and a cancelled ctx would skip the drain.
		_ = s.shutdown() //nolint:contextcheck
		wg.Wait()
		return err
	}

	_ = s.shutdown() //nolint:contextcheck
	wg.Wait()
	return nil
}

func (s *Server) shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(
		s.proxySrv.Shutdown(shutdownCtx),
		s.metricsSrv.Shutdown(shutdownCtx),
	)
}

// ProxyAddr returns the proxy listener address (useful for logging).
func (s *Server) ProxyAddr() string { return s.proxySrv.Addr }

// MetricsAddr returns the metrics listener address.
func (s *Server) MetricsAddr() string { return s.metricsSrv.Addr }
