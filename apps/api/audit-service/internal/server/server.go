// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server hosts the audit-service HTTP surface.
//
// Routes:
//
//	GET  /v1/audit/entries          paginated query
//	GET  /v1/audit/entries/{id}     single entry
//	GET  /v1/audit/export           streaming JSONL bulk export
//	GET  /v1/audit/verify           server-side chain verification
//	GET  /metrics                   Prometheus self-metrics
//	GET  /healthz                   liveness
//	GET  /readyz                    readiness
//
// Auth is intentionally not enforced here; a deployment-level proxy is the
// gate (consistent with the metrics-endpoint pattern).
package server

import (
	"net/http"
	"strings"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/handler"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/store"
)

// ReadinessChecker reports whether the service has caught up to bus head.
// The audit consumer satisfies it.
type ReadinessChecker interface {
	Ready() bool
}

// Handler builds the http.Handler that exposes all audit-service routes.
func Handler(cfg *config.Config, s store.Store, mreg *metrics.Registry, ready ReadinessChecker) http.Handler {
	entries := &handler.EntriesHandler{Store: s, Metrics: mreg, Config: cfg}
	export := &handler.ExportHandler{Store: s, Metrics: mreg}
	verify := &handler.VerifyHandler{Store: s, Metrics: mreg}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/audit/entries", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		entries.ServeList(w, r)
	})
	// /v1/audit/entries/{id}
	mux.HandleFunc("/v1/audit/entries/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Don't shadow the list handler at exactly "/v1/audit/entries".
		if strings.TrimPrefix(r.URL.Path, "/v1/audit/entries/") == "" {
			entries.ServeList(w, r)
			return
		}
		entries.ServeGet(w, r)
	})
	mux.Handle("/v1/audit/export", methodOnly(http.MethodGet, export))
	mux.Handle("/v1/audit/verify", methodOnly(http.MethodGet, verify))
	mux.Handle("/metrics", mreg.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if ready != nil && !ready.Ready() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ready":false,"reason":"warming"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	})

	return mux
}

func methodOnly(method string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.ServeHTTP(w, r)
	})
}
