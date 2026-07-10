// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/validator"
)

// validateRequest is the body for POST /v1/policies/{id}/validate. The id in
// the path is ignored for the actual check — this endpoint is a pure
// schema-level dry-run that returns the same structured errors a write
// would produce. It performs NO evaluation and NO storage.
type validateRequest struct {
	Document json.RawMessage `json:"document"`
}

// validateResponse is returned regardless of validity. Errors is empty when
// the document conforms to the schema.
type validateResponse struct {
	Valid  bool              `json:"valid"`
	Errors []validator.Error `json:"errors"`
}

// ValidateDocument handles POST /v1/policies/{id}/validate.
//
// Important: this endpoint is INTENTIONALLY shape-only. It must never
// answer the question "would this policy allow / deny / route request X".
// That question is owned by F030 (not implemented here).
func (d *Deps) ValidateDocument(w http.ResponseWriter, r *http.Request) {
	if _, err := tenantFromRequest(r); err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", err.Error())
		return
	}
	var req validateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(req.Document) == 0 {
		writeError(w, http.StatusBadRequest, "missing_document", "document is required")
		return
	}

	errs := d.Validator.Validate(req.Document)
	if len(errs) > 0 {
		d.Metrics.ValidationsFailed.Add(1)
	} else {
		d.Metrics.ValidationsSucceeded.Add(1)
	}
	writeJSON(w, http.StatusOK, validateResponse{
		Valid:  len(errs) == 0,
		Errors: errs,
	})
}
