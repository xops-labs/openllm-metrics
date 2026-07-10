// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/policy-service/internal/store"
)

// ListVersions handles GET /v1/policies/{id}/versions.
func (d *Deps) ListVersions(w http.ResponseWriter, r *http.Request) {
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

	versions, err := d.Store.ListVersions(r.Context(), tenantID, policyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"policy_id": policyID,
		"versions":  versions,
	})
}

// GetVersion handles GET /v1/policies/{id}/versions/{version}.
func (d *Deps) GetVersion(w http.ResponseWriter, r *http.Request) {
	tenantID, err := tenantFromRequest(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing_tenant", err.Error())
		return
	}
	policyID, rest, err := uuidPathParam(r.URL.Path, "/v1/policies/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", err.Error())
		return
	}

	// rest looks like "versions/<n>". Extract the trailing integer.
	rest = strings.TrimPrefix(rest, "versions/")
	if rest == "" || strings.Contains(rest, "/") {
		writeError(w, http.StatusBadRequest, "invalid_version", "version segment missing or malformed")
		return
	}
	version, err := strconv.Atoi(rest)
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "invalid_version", "version must be a positive integer")
		return
	}

	ver, err := d.Store.GetVersion(r.Context(), tenantID, policyID, version)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "policy version not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ver)
}
