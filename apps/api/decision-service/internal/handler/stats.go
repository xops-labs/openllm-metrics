// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"net/http"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service/internal/store"
)

// StatsHandler serves GET /v1/decisions/stats.
//
// The response is intentionally minimal: a total count plus a flat
// (provider_chosen, model_chosen) breakdown for the requested tenant +
// window. The OSS layer does not derive error rates or override counts
// from inside reason_chain content — that data is decider-defined and
// the OSS layer treats those JSON blobs as opaque. Operators who want
// error-rate or override aggregates render them in the admin console
// from the stored payload at view time.
type StatsHandler struct {
	Store   store.Store
	Metrics Counter
}

// ServeHTTP implements http.Handler.
func (h *StatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Metrics.IncStats()

	q := r.URL.Query()
	tenant := q.Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}

	f := store.StatsFilter{TenantID: tenant}
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

	s, err := h.Store.StatsByChosen(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	by := make([]map[string]any, 0, len(s.ByChosen))
	for _, pc := range s.ByChosen {
		by = append(by, map[string]any{
			"provider_chosen": pc.ProviderChosen,
			"model_chosen":    pc.ModelChosen,
			"count":           pc.Count,
		})
	}
	out := map[string]any{
		"tenant_id":   tenant,
		"total":       s.Total,
		"by_chosen":   by,
		"window_from": iso(s.WindowFrom),
		"window_to":   iso(s.WindowTo),
	}
	writeJSON(w, http.StatusOK, out)
}

func iso(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
