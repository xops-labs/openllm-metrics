// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server hosts the decision-service HTTP surface.
//
// Routes:
//
//	GET  /v1/decisions                    paginated list
//	GET  /v1/decisions/stats              bucketed counts
//	GET  /v1/decisions/{decision_id}      single decision
//	GET  /metrics                         Prometheus self-metrics
//	GET  /healthz                         liveness
//	GET  /readyz                          readiness
//
// Auth is intentionally not enforced here; a deployment-level proxy is
// the gate (consistent with audit-service and other Phase G OSS surfaces).
package server

import (
	"net/http"
	"strings"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/handler"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/store"
)

// ReadinessChecker reports whether the service has caught up to bus head.
type ReadinessChecker interface {
	Ready() bool
}

// Handler builds the http.Handler that exposes all decision-service routes.
func Handler(cfg *config.Config, s store.Store, mreg *metrics.Registry, ready ReadinessChecker) http.Handler {
	decisions := &handler.DecisionsHandler{Store: s, Metrics: mreg, Config: cfg}
	stats := &handler.StatsHandler{Store: s, Metrics: mreg}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/decisions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		decisions.ServeList(w, r)
	})
	// /v1/decisions/stats and /v1/decisions/{decision_id} share a prefix.
	mux.HandleFunc("/v1/decisions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/decisions/")
		if rest == "" {
			decisions.ServeList(w, r)
			return
		}
		if rest == "stats" {
			stats.ServeHTTP(w, r)
			return
		}
		if strings.Contains(rest, "/") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		decisions.ServeGet(w, r, rest)
	})
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
