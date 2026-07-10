// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package handler implements the F029 HTTP surface: CRUD over policies and
// their immutable version history, plus a schema-only validation endpoint.
//
// IMPORTANT: this package handles structural validation, storage, and
// versioning only. It NEVER evaluates a policy, computes a budget burn,
// orders rules by precedence, or makes an enforcement decision. Those
// behaviors are owned by F030 in this repository. If
// you are tempted to add such logic here, stop and route the change to
// F030.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/store"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/validator"
)

// Deps bundles the collaborators handlers need. Constructed once in main and
// passed by reference.
type Deps struct {
	Store     *store.Store
	Validator *validator.Validator
	Bus       *busproducer.Producer
	Metrics   *metrics.Counters
}

// tenantHeader is the HTTP header carrying the caller's tenant. In a real
// deployment the API gateway / auth middleware would set this after JWT
// validation. We treat the header as an MVP shim and validate strictly.
const tenantHeader = "X-Tenant-ID"

// actorHeader carries the caller principal (user or service). Recorded into
// audit events as "actor".
const actorHeader = "X-Actor"

// writeJSON serializes v as application/json with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorPayload is the canonical error response body.
type errorPayload struct {
	Error  string            `json:"error"`
	Detail string            `json:"detail,omitempty"`
	Errors []validator.Error `json:"errors,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorPayload{Error: code, Detail: detail})
}

func writeValidationErrors(w http.ResponseWriter, errs []validator.Error) {
	writeJSON(w, http.StatusBadRequest, errorPayload{
		Error:  "validation_failed",
		Errors: errs,
	})
}

// tenantFromRequest extracts and parses the X-Tenant-ID header.
func tenantFromRequest(r *http.Request) (uuid.UUID, error) {
	raw := strings.TrimSpace(r.Header.Get(tenantHeader))
	if raw == "" {
		return uuid.Nil, errors.New("missing X-Tenant-ID header")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errors.New("invalid X-Tenant-ID header")
	}
	return id, nil
}

// actorFromRequest returns the caller principal, defaulting to "unknown" when
// the header is absent. Audit events always carry a non-empty actor.
func actorFromRequest(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get(actorHeader))
	if raw == "" {
		return "unknown"
	}
	return raw
}

// uuidPathParam extracts a UUID from the trailing path segment after prefix.
// We use the stdlib mux so handlers parse path parameters explicitly.
func uuidPathParam(path, prefix string) (uuid.UUID, string, error) {
	trimmed := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return uuid.Nil, "", errors.New("missing id")
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", errors.New("invalid id")
	}
	rest := ""
	if len(parts) == 2 {
		rest = parts[1]
	}
	return id, rest, nil
}
