// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package server builds the HTTP surface for the F029 policy service.
//
// Endpoints:
//
//	GET    /v1/policies                       List policy headers (tenant)
//	POST   /v1/policies                       Create policy (version 1)
//	GET    /v1/policies/{id}                  Get policy (current version)
//	PUT    /v1/policies/{id}                  Append a new version
//	DELETE /v1/policies/{id}                  Soft-delete the policy
//	GET    /v1/policies/{id}/versions         List all versions
//	GET    /v1/policies/{id}/versions/{n}     Get a specific version
//	POST   /v1/policies/{id}/validate         Schema validation only
//	GET    /healthz                           Liveness
//	GET    /readyz                            Readiness
//
// Routing uses the stdlib mux. The handler package owns request parsing,
// validation, persistence, and audit emission; this package owns wiring only.
package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/handler"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/metrics"
)

// Handler returns the composed http.Handler for the service.
func Handler(d *handler.Deps, counters *metrics.Counters) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/policies", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			d.ListPolicies(w, r)
		case http.MethodPost:
			d.CreatePolicy(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// /v1/policies/{id} and nested resources are dispatched in a single
	// handler because the stdlib mux does not support path parameters
	// natively in go 1.22's basic pattern form for nested segments.
	mux.HandleFunc("/v1/policies/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimSuffix(r.URL.Path, "/")
		// Sub-routes by path suffix.
		switch {
		case strings.HasSuffix(path, "/validate") && r.Method == http.MethodPost:
			d.ValidateDocument(w, r)
		case strings.Contains(path, "/versions/") && r.Method == http.MethodGet:
			d.GetVersion(w, r)
		case strings.HasSuffix(path, "/versions") && r.Method == http.MethodGet:
			d.ListVersions(w, r)
		default:
			switch r.Method {
			case http.MethodGet:
				d.GetPolicy(w, r)
			case http.MethodPut:
				d.UpdatePolicy(w, r)
			case http.MethodDelete:
				d.DeletePolicy(w, r)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
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
