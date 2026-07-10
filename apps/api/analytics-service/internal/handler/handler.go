// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package handler implements the F038 analytics saved-views HTTP surface:
// CRUD over per-tenant saved dashboard view specs.
//
// IMPORTANT: this package handles storage and retrieval only. It NEVER
// executes an analytics query, scores a series, routes a request, or applies
// an anomaly rule — those behaviors are custom. The view spec is an opaque
// declarative llm_* selector that is persisted and returned verbatim.
//
// The HTTP contract is defined by the admin console client at
// apps/web/admin-console/lib/api/saved-views.ts and MUST NOT drift from it:
//
//	GET    /v1/saved-views        -> { "views": [SavedView, ...] }
//	POST   /v1/saved-views        body { name, description?, spec, position? }
//	                              -> a bare SavedView object (with generated id)
//	DELETE /v1/saved-views/{id}   -> 204 No Content
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/analytics-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/analytics-service/internal/store"
)

// SavedViewStore is the persistence contract the handlers depend on. The
// concrete *store.Store satisfies it; tests substitute a fake.
type SavedViewStore interface {
	List(ctx context.Context, tenantID uuid.UUID) ([]store.SavedView, error)
	Create(ctx context.Context, tenantID uuid.UUID, in store.CreateInput) (*store.SavedView, error)
	SoftDelete(ctx context.Context, tenantID, id uuid.UUID) error
}

// Deps bundles the collaborators handlers need. Constructed once in main and
// passed by reference.
type Deps struct {
	Store   SavedViewStore
	Metrics *metrics.Counters
}

// tenantHeader is the HTTP header carrying the caller's tenant. The admin
// console sets it via tenantHeaders() in lib/auth.ts. In a real deployment an
// API gateway / auth middleware sets it after JWT validation; we treat the
// header as an MVP shim and validate strictly.
const tenantHeader = "X-Tenant-ID"

// writeJSON serializes v as application/json with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorPayload is the canonical error response body.
type errorPayload struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorPayload{Error: code, Detail: detail})
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

// idPathParam extracts and parses a UUID from the trailing path segment after
// prefix (e.g. "/v1/saved-views/").
func idPathParam(path, prefix string) (uuid.UUID, error) {
	trimmed := strings.TrimPrefix(path, prefix)
	seg := strings.SplitN(trimmed, "/", 2)[0]
	if seg == "" {
		return uuid.Nil, errors.New("missing id")
	}
	id, err := uuid.Parse(seg)
	if err != nil {
		return uuid.Nil, errors.New("invalid id")
	}
	return id, nil
}

// createSavedViewRequest is the body for POST /v1/saved-views. Field names
// mirror the console's CreateSavedViewInput exactly.
type createSavedViewRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Spec        json.RawMessage `json:"spec"`
	Position    int             `json:"position"`
}

// ListSavedViews handles GET /v1/saved-views. Response envelope is
// { "views": [...] } — the key the console parses (r.views).
func (d *Deps) ListSavedViews(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_tenant", err.Error())
		return
	}
	views, err := d.Store.List(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	d.Metrics.ViewsListed.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{"views": views})
}

// CreateSavedView handles POST /v1/saved-views. Returns the persisted view as
// a bare object (with generated id) — the shape the console expects so that a
// non-empty id signals "persisted".
func (d *Deps) CreateSavedView(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_tenant", err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", err.Error())
		return
	}
	var req createSavedViewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}

	view, err := d.Store.Create(r.Context(), tenantID, store.CreateInput{
		Name:        req.Name,
		Description: req.Description,
		Spec:        req.Spec,
		Position:    req.Position,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, http.StatusConflict, "view_exists", "a saved view with that name already exists")
		default:
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}
	d.Metrics.ViewsCreated.Add(1)
	writeJSON(w, http.StatusCreated, view)
}

// DeleteSavedView handles DELETE /v1/saved-views/{id} (soft delete).
func (d *Deps) DeleteSavedView(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_tenant", err.Error())
		return
	}
	id, err := idPathParam(r.URL.Path, "/v1/saved-views/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}

	if err := d.Store.SoftDelete(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "saved view not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	d.Metrics.ViewsDeleted.Add(1)
	w.WriteHeader(http.StatusNoContent)
}
