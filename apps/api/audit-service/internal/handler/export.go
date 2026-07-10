// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/store"
)

// ExportHandler serves GET /v1/audit/export?tenant=&from=&to=&format=jsonl.
//
// The response body is streaming JSONL (one EntryWire per line) so consumers
// can begin processing rows immediately and we never hold a large tenant's
// full history in memory.
type ExportHandler struct {
	Store   store.Store
	Metrics Counter
}

// ServeHTTP implements http.Handler.
func (h *ExportHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tenant := q.Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}
	format := q.Get("format")
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" {
		writeError(w, http.StatusBadRequest, "format must be jsonl")
		return
	}

	var from, to time.Time
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
			return
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
			return
		}
		to = t
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	rows := 0
	err := h.Store.Stream(r.Context(), tenant, from, to, func(e store.Entry) error {
		if err := enc.Encode(entryToWire(e)); err != nil {
			return err
		}
		rows++
		if flusher != nil && rows%128 == 0 {
			flusher.Flush()
		}
		return nil
	})
	h.Metrics.AddExportRows(rows)
	if err != nil {
		// The headers are already sent; the best we can do is log on the
		// trailer side via a JSON object the client can detect.
		_ = enc.Encode(map[string]any{"_error": err.Error()})
	}
	if flusher != nil {
		flusher.Flush()
	}
}
