// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server hosts the HTTP surface for the metrics-endpoint service:
// /metrics, /healthz, /readyz.
//
// Auth is intentionally not enforced here. F010 §5 / §14 defer scrape auth
// to the deployment (sidecar, ingress, mTLS) so this surface stays
// drop-in-compatible with vanilla Prometheus scrape rules.
package server

import (
	"net/http"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/aggregator"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/exposition"
)

// ReadinessChecker reports whether the service is ready to be scraped.
// Implementations should return true once the consumer has observed at
// least one event and the aggregator has populated initial state.
type ReadinessChecker interface {
	Ready() bool
}

// Handler builds the http.Handler that exposes /metrics, /healthz, and
// /readyz against the supplied aggregator and readiness probe.
func Handler(agg *aggregator.Aggregator, ready ReadinessChecker) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler(agg))
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(ready))
	return mux
}

// metricsHandler returns the Prometheus exposition for the current
// aggregator state. Cheap to call (snapshot is a copy-on-read).
func metricsHandler(agg *aggregator.Aggregator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", exposition.ContentType)
		snap := agg.Snapshot()
		self := exposition.SelfMetrics{
			Rejected:    agg.RejectedEvents(),
			Processed:   agg.ProcessedEvents(),
			SeriesCount: agg.SeriesCount(),
		}
		_ = exposition.Write(w, snap, self)
	})
}

// healthzHandler is liveness only: the process is up and the HTTP server is
// serving. It deliberately does NOT consult the bus or aggregator state.
func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readyzHandler returns 200 only once the readiness checker reports true.
// Returns 503 with a small JSON-shaped body while still warming up so
// schedulers (k8s, ECS, nomad) can hold traffic.
func readyzHandler(ready ReadinessChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if ready == nil || !ready.Ready() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ready":false,"reason":"warming"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	}
}
