// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/hasher"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service/internal/store"
)

// VerifyHandler serves GET /v1/audit/verify.
//
// Recomputes the chain hash row by row over the segment fromID..toID
// (inclusive). On the first mismatch returns:
//
//	{
//	  "ok":            false,
//	  "broken_at":     <id>,
//	  "expected_hash": "<base64 sha256>",
//	  "actual_hash":   "<base64 sha256>",
//	  "checked":       <count>
//	}
//
// On success:
//
//	{
//	  "ok":      true,
//	  "checked": <count>,
//	  "last_id": <id>
//	}
type VerifyHandler struct {
	Store   store.Store
	Metrics Counter
}

// ServeHTTP implements http.Handler.
func (h *VerifyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tenant := q.Get("tenant")
	if tenant == "" {
		writeError(w, http.StatusBadRequest, "tenant query param is required")
		return
	}
	fromID, _ := strconv.ParseInt(q.Get("from_id"), 10, 64)
	toID, _ := strconv.ParseInt(q.Get("to_id"), 10, 64)

	var (
		prev    = make([]byte, hasher.HashSize) // running expected prev_hash
		first   = true
		checked int
		lastID  int64
	)
	type verifyResult struct {
		OK           bool   `json:"ok"`
		BrokenAt     int64  `json:"broken_at,omitempty"`
		ExpectedHash string `json:"expected_hash,omitempty"`
		ActualHash   string `json:"actual_hash,omitempty"`
		Reason       string `json:"reason,omitempty"`
		Checked      int    `json:"checked"`
		LastID       int64  `json:"last_id,omitempty"`
	}
	var result verifyResult

	err := h.Store.StreamForVerify(r.Context(), tenant, fromID, toID, func(e store.Entry) error {
		h.Metrics.IncVerifyCheck()
		checked++
		lastID = e.ID

		// If fromID > 1 we cannot validate the first row's prev_hash
		// without re-fetching its predecessor. The first row in the
		// segment is treated as the "anchor" and we begin chain checking
		// from the SECOND row onward.
		if !first {
			if !bytes.Equal(e.PrevHash, prev) {
				h.Metrics.IncVerifyBreak()
				result = verifyResult{
					OK:           false,
					BrokenAt:     e.ID,
					ExpectedHash: base64.StdEncoding.EncodeToString(prev),
					ActualHash:   base64.StdEncoding.EncodeToString(e.PrevHash),
					Reason:       "prev_hash does not match prior entry_hash",
					Checked:      checked,
				}
				return stopErr
			}
		}
		// Recompute this row's entry_hash and compare.
		got, err := hasher.Compute(hasher.Entry{
			TenantID:  e.TenantID,
			ID:        e.ID,
			Actor:     e.Actor,
			Action:    e.Action,
			Resource:  e.Resource,
			Payload:   e.Payload,
			PrevHash:  e.PrevHash,
			CreatedAt: e.CreatedAt,
		})
		if err != nil {
			return err
		}
		if !bytes.Equal(got, e.EntryHash) {
			h.Metrics.IncVerifyBreak()
			result = verifyResult{
				OK:           false,
				BrokenAt:     e.ID,
				ExpectedHash: base64.StdEncoding.EncodeToString(got),
				ActualHash:   base64.StdEncoding.EncodeToString(e.EntryHash),
				Reason:       "entry_hash recompute mismatch",
				Checked:      checked,
			}
			return stopErr
		}
		prev = e.EntryHash
		first = false
		return nil
	})
	if err != nil && err != stopErr {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err == stopErr {
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusOK, verifyResult{
		OK:      true,
		Checked: checked,
		LastID:  lastID,
	})
}

// stopErr is a sentinel returned by the Stream callback when a break is
// found, so the iteration stops on the first mismatch.
var stopErr = stopSentinel{}

type stopSentinel struct{}

func (stopSentinel) Error() string { return "verify: stop iteration" }
