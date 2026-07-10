// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package handler hosts the decision-service HTTP read API.
//
// Routes:
//
//   - GET /v1/decisions                paginated list
//   - GET /v1/decisions/{decision_id}  single decision
//   - GET /v1/decisions/stats          bucketed counts for the overview page
//
// Tenancy: the ?tenant= query parameter is REQUIRED on every endpoint.
// In a production deployment an upstream auth proxy validates the
// caller's JWT against the requested tenant; the decision-service does
// NOT itself enforce JWT/OIDC at this phase (consistent with other
// Phase G OSS services).
//
// Render contract: reason_chain and alternatives are returned as the
// same JSON shape the registered decider emitted. The OSS handler does
// not interpret factor names, weight_hint numbers, or score_hint numbers.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/config"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/store"
)

// Counter is the narrow metrics surface the handler depends on.
type Counter interface {
	IncQuery()
	IncStats()
}

// DecisionsHandler serves /v1/decisions and /v1/decisions/{id}.
type DecisionsHandler struct {
	Store   store.Store
	Metrics Counter
	Config  *config.Config
}

// ServeList handles GET /v1/decisions.
func (h *DecisionsHandler) ServeList(w http.ResponseWriter, r *http.Request) {
	h.Metrics.IncQuery()

	q := r.URL.Query()
	tenant := q.Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}

	f := store.QueryFilter{
		TenantID: tenant,
		App:      q.Get("app"),
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
	// Cursor is the smallest id from this page (DESC order) — pass it back
	// as ?cursor= to fetch the next, older page.
	if len(rows) > 0 {
		next = rows[len(rows)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"decisions": decisionsToWire(rows),
		"next":      next,
	})
}

// ServeGet handles GET /v1/decisions/{decision_id}.
func (h *DecisionsHandler) ServeGet(w http.ResponseWriter, r *http.Request, decisionID string) {
	h.Metrics.IncQuery()

	tenant := r.URL.Query().Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}
	if decisionID == "" {
		writeError(w, http.StatusBadRequest, "decision_id is required")
		return
	}
	d, err := h.Store.GetByDecisionID(r.Context(), tenant, decisionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, decisionToWire(d))
}

// DecisionWire is the on-the-wire shape for one decision. reason_chain
// and alternatives are emitted as raw JSON so the admin console renders
// whatever the decider produced without OSS-side reinterpretation.
type DecisionWire struct {
	ID                int64           `json:"id"`
	DecisionID        string          `json:"decision_id"`
	TenantID          string          `json:"tenant_id"`
	Team              string          `json:"team"`
	App               string          `json:"app"`
	Env               string          `json:"env"`
	Project           string          `json:"project"`
	ProviderRequested string          `json:"provider_requested"`
	ModelRequested    string          `json:"model_requested"`
	RouteRequested    string          `json:"route_requested"`
	RequestIDHash     string          `json:"request_id_hash"`
	ProviderChosen    string          `json:"provider_chosen"`
	ModelChosen       string          `json:"model_chosen"`
	RouteChosen       string          `json:"route_chosen"`
	ReasonChain       json.RawMessage `json:"reason_chain"`
	Alternatives      json.RawMessage `json:"alternatives"`
	DeciderVersion    string          `json:"decider_version"`
	DecidedAt         string          `json:"decided_at"`
	IngestedAt        string          `json:"ingested_at"`
}

func decisionToWire(d store.Decision) DecisionWire {
	reason := d.ReasonChain
	if len(reason) == 0 {
		reason = json.RawMessage("[]")
	}
	alts := d.Alternatives
	if len(alts) == 0 {
		alts = json.RawMessage("[]")
	}
	return DecisionWire{
		ID:                d.ID,
		DecisionID:        d.DecisionID,
		TenantID:          d.TenantID,
		Team:              d.Team,
		App:               d.App,
		Env:               d.Env,
		Project:           d.Project,
		ProviderRequested: d.ProviderRequested,
		ModelRequested:    d.ModelRequested,
		RouteRequested:    d.RouteRequested,
		RequestIDHash:     d.RequestIDHash,
		ProviderChosen:    d.ProviderChosen,
		ModelChosen:       d.ModelChosen,
		RouteChosen:       d.RouteChosen,
		ReasonChain:       reason,
		Alternatives:      alts,
		DeciderVersion:    d.DeciderVersion,
		DecidedAt:         d.DecidedAt.UTC().Format(time.RFC3339Nano),
		IngestedAt:        d.IngestedAt.UTC().Format(time.RFC3339Nano),
	}
}

func decisionsToWire(ds []store.Decision) []DecisionWire {
	out := make([]DecisionWire, 0, len(ds))
	for _, d := range ds {
		out = append(out, decisionToWire(d))
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
