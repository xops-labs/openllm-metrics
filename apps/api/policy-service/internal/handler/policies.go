// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/store"
)

// createPolicyRequest is the body for POST /v1/policies.
type createPolicyRequest struct {
	Name     string          `json:"name"`
	Document json.RawMessage `json:"document"`
	Comment  string          `json:"comment"`
}

// updatePolicyRequest is the body for PUT /v1/policies/{id}.
type updatePolicyRequest struct {
	Document json.RawMessage `json:"document"`
	Comment  string          `json:"comment"`
}

// CreatePolicy handles POST /v1/policies. Validates the document against
// the JSON Schema, stores it as version 1, emits an audit event.
func (d *Deps) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", err.Error())
		return
	}
	var req createPolicyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}
	if len(req.Document) == 0 {
		writeError(w, http.StatusBadRequest, "missing_document", "document is required")
		return
	}

	if errs := d.Validator.Validate(req.Document); len(errs) > 0 {
		d.Metrics.ValidationsFailed.Add(1)
		writeValidationErrors(w, errs)
		return
	}
	d.Metrics.ValidationsSucceeded.Add(1)

	hdr, ver, err := d.Store.CreatePolicy(r.Context(), tenantID, req.Name, req.Document, actorFromRequest(r), req.Comment)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrConflict):
			writeError(w, http.StatusConflict, "policy_exists", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		}
		return
	}
	d.Metrics.PoliciesCreated.Add(1)
	d.emitAudit(r, tenantID, busproducer.ActionCreated, hdr.ID, map[string]any{
		"name":    hdr.Name,
		"version": ver.Version,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      hdr.ID,
		"name":    hdr.Name,
		"version": ver.Version,
	})
}

// ListPolicies handles GET /v1/policies — all policy headers for the tenant.
func (d *Deps) ListPolicies(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}
	policies, err := d.Store.ListPolicies(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

// GetPolicy handles GET /v1/policies/{id}.
func (d *Deps) GetPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}
	policyID, _, err := uuidPathParam(r.URL.Path, "/v1/policies/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}

	hdr, ver, err := d.Store.GetCurrent(r.Context(), tenantID, policyID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "policy not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":              hdr.ID,
		"name":            hdr.Name,
		"current_version": hdr.CurrentVersion,
		"created_at":      hdr.CreatedAt,
		"updated_at":      hdr.UpdatedAt,
		"document":        ver.Document,
	})
}

// UpdatePolicy handles PUT /v1/policies/{id}. Appends a new version row and
// bumps the header pointer. The previous versions remain queryable.
func (d *Deps) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}
	policyID, _, err := uuidPathParam(r.URL.Path, "/v1/policies/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read_body", err.Error())
		return
	}
	var req updatePolicyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if len(req.Document) == 0 {
		writeError(w, http.StatusBadRequest, "missing_document", "document is required")
		return
	}

	if errs := d.Validator.Validate(req.Document); len(errs) > 0 {
		d.Metrics.ValidationsFailed.Add(1)
		writeValidationErrors(w, errs)
		return
	}
	d.Metrics.ValidationsSucceeded.Add(1)

	ver, err := d.Store.AppendVersion(r.Context(), tenantID, policyID, req.Document, actorFromRequest(r), req.Comment)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "policy not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	d.Metrics.PoliciesUpdated.Add(1)
	d.emitAudit(r, tenantID, busproducer.ActionVersionAppended, policyID, map[string]any{
		"version": ver.Version,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      policyID,
		"version": ver.Version,
	})
}

// DeletePolicy handles DELETE /v1/policies/{id} (soft delete).
func (d *Deps) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}
	policyID, _, err := uuidPathParam(r.URL.Path, "/v1/policies/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}

	if err := d.Store.SoftDelete(r.Context(), tenantID, policyID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "policy not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	d.Metrics.PoliciesDeleted.Add(1)
	d.emitAudit(r, tenantID, busproducer.ActionSoftDeleted, policyID, nil)

	w.WriteHeader(http.StatusNoContent)
}

// emitAudit publishes an audit event and increments the counter. A failed
// emit increments AuditEventsDropped but never fails the request.
func (d *Deps) emitAudit(r *http.Request, tenantID uuid.UUID, action string, policyID uuid.UUID, metadata map[string]any) {
	resource := "policy:" + policyID.String()
	if err := d.Bus.Emit(r.Context(), tenantID, actorFromRequest(r), action, resource, metadata); err != nil {
		d.Metrics.AuditEventsDropped.Add(1)
		return
	}
	d.Metrics.AuditEventsEmitted.Add(1)
}
