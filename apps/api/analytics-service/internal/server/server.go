// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server builds the HTTP surface for the F038 analytics saved-views
// service.
//
// Endpoints:
//
//	GET    /v1/saved-views        List saved views for the tenant
//	POST   /v1/saved-views        Create a saved view
//	DELETE /v1/saved-views/{id}   Soft-delete a saved view
//	GET    /healthz               Liveness
//	GET    /readyz                Readiness
//
// Routing uses the stdlib mux. The handler package owns request parsing,
// persistence, and JSON encoding; this package owns wiring only. The paths and
// JSON shapes are fixed by the admin console client
// (apps/web/admin-console/lib/api/saved-views.ts).
package server

import (
	"encoding/json"
	"net/http"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/analytics-service/internal/handler"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/analytics-service/internal/metrics"
)

// Handler returns the composed http.Handler for the service.
func Handler(d *handler.Deps, counters *metrics.Counters) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/saved-views", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			d.ListSavedViews(w, r)
		case http.MethodPost:
			d.CreateSavedView(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// /v1/saved-views/{id} — currently only DELETE is part of the console
	// contract.
	mux.HandleFunc("/v1/saved-views/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			d.DeleteSavedView(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	})

	mux.HandleFunc("/debug/counters", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(counters.Snapshot())
	})

	return mux
}
