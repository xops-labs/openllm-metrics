// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package handler hosts the audit-service HTTP read API.
//
// Three handlers:
//
//   - GET /v1/audit/entries          paginated query
//   - GET /v1/audit/entries/{id}     single entry
//   - GET /v1/audit/export           streaming JSONL bulk export
//   - GET /v1/audit/verify           server-side chain verification
//
// Tenancy: the ?tenant= query parameter is REQUIRED on every endpoint. In a
// production deployment an upstream auth proxy validates the caller's JWT
// against the requested tenant; the audit-service does NOT itself enforce
// JWT/OIDC at this phase (consistent with other Phase G OSS services).
//
// Cursor pagination: callers receive a JSON object {entries: [...], next:
// <id-cursor>}. To fetch the next page, pass ?cursor=<id> back in.
package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/store"
)

// Counter is the narrow metrics surface the handler depends on.
type Counter interface {
	IncQuery()
	AddExportRows(int)
	IncVerifyCheck()
	IncVerifyBreak()
}

// EntriesHandler serves /v1/audit/entries and /v1/audit/entries/{id}.
type EntriesHandler struct {
	Store   store.Store
	Metrics Counter
	Config  *config.Config
}

// ServeList handles GET /v1/audit/entries.
func (h *EntriesHandler) ServeList(w http.ResponseWriter, r *http.Request) {
	h.Metrics.IncQuery()

	q := r.URL.Query()
	tenant := q.Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}

	f := store.QueryFilter{
		TenantID: tenant,
		Action:   q.Get("action"),
		ActorID:  q.Get("actor_id"),
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
			return
		}
		f.From = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
			return
		}
		f.To = t
	}
	if v := q.Get("cursor"); v != "" {
		c, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		f.Cursor = c
	}
	limit := h.Config.Server.DefaultPageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n > h.Config.Server.MaxPageSize {
			n = h.Config.Server.MaxPageSize
		}
		limit = n
	}
	f.Limit = limit

	rows, err := h.Store.Query(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	next := int64(0)
	if len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	out := map[string]any{
		"entries": entriesToWire(rows),
		"next":    next,
	}
	writeJSON(w, http.StatusOK, out)
}

// ServeGet handles GET /v1/audit/entries/{id}.
func (h *EntriesHandler) ServeGet(w http.ResponseWriter, r *http.Request) {
	h.Metrics.IncQuery()

	tenant := r.URL.Query().Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}
	// Path is /v1/audit/entries/{id}
	idStr := strings.TrimPrefix(r.URL.Path, "/v1/audit/entries/")
	if idStr == "" || strings.Contains(idStr, "/") {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	e, err := h.Store.GetByID(r.Context(), tenant, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entryToWire(e))
}

// EntryWire is the on-the-wire shape for one audit entry. prev_hash and
// entry_hash are returned base64-encoded so the JSON stays compact.
type EntryWire struct {
	ID        int64          `json:"id"`
	TenantID  string         `json:"tenant_id"`
	Actor     map[string]any `json:"actor"`
	Action    string         `json:"action"`
	Resource  map[string]any `json:"resource"`
	Payload   map[string]any `json:"payload"`
	PrevHash  string         `json:"prev_hash"`
	EntryHash string         `json:"entry_hash"`
	CreatedAt string         `json:"created_at"`
}

func entryToWire(e store.Entry) EntryWire {
	return EntryWire{
		ID:        e.ID,
		TenantID:  e.TenantID,
		Actor:     e.Actor,
		Action:    e.Action,
		Resource:  e.Resource,
		Payload:   e.Payload,
		PrevHash:  base64.StdEncoding.EncodeToString(e.PrevHash),
		EntryHash: base64.StdEncoding.EncodeToString(e.EntryHash),
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func entriesToWire(es []store.Entry) []EntryWire {
	out := make([]EntryWire, 0, len(es))
	for _, e := range es {
		out = append(out, entryToWire(e))
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
