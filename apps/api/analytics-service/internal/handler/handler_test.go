// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/analytics-service/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/api/analytics-service/internal/store"
)

// fakeStore is an in-memory SavedViewStore for handler tests. It records the
// tenant it was called with so we can assert tenant scoping is threaded
// through, and lets each method return a canned error.
type fakeStore struct {
	listResult  []store.SavedView
	listErr     error
	createErr   error
	deleteErr   error
	gotTenant   uuid.UUID
	gotCreate   store.CreateInput
	gotDeleteID uuid.UUID
}

func (f *fakeStore) List(_ context.Context, tenantID uuid.UUID) ([]store.SavedView, error) {
	f.gotTenant = tenantID
	return f.listResult, f.listErr
}

func (f *fakeStore) Create(_ context.Context, tenantID uuid.UUID, in store.CreateInput) (*store.SavedView, error) {
	f.gotTenant = tenantID
	f.gotCreate = in
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &store.SavedView{
		ID:          uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name:        in.Name,
		Description: in.Description,
		Spec:        in.Spec,
		Position:    in.Position,
	}, nil
}

func (f *fakeStore) SoftDelete(_ context.Context, tenantID, id uuid.UUID) error {
	f.gotTenant = tenantID
	f.gotDeleteID = id
	return f.deleteErr
}

func newDeps(fs *fakeStore) *Deps {
	return &Deps{Store: fs, Metrics: metrics.New()}
}

const testTenant = "00000000-0000-0000-0002-000000000001"

func TestListSavedViews_Envelope(t *testing.T) {
	fs := &fakeStore{listResult: []store.SavedView{{
		ID:          uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:        "Tokens by agent",
		Description: "desc",
		Spec:        json.RawMessage(`{"metric":"llm_total_tokens_total","groupBy":["app"],"viz":"bar"}`),
		Position:    0,
	}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/saved-views", nil)
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(fs).ListSavedViews(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// The console parses `r.views`; the envelope key must be "views".
	var got struct {
		Views []struct {
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Spec     json.RawMessage `json:"spec"`
			Position int             `json:"position"`
		} `json:"views"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Views) != 1 || got.Views[0].Name != "Tokens by agent" {
		t.Fatalf("unexpected views payload: %s", rec.Body.String())
	}
	if fs.gotTenant.String() != testTenant {
		t.Fatalf("store called with tenant %s, want %s", fs.gotTenant, testTenant)
	}
}

func TestListSavedViews_MissingTenant(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/saved-views", nil)
	rec := httptest.NewRecorder()

	newDeps(&fakeStore{}).ListSavedViews(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateSavedView_ReturnsBareViewWithID(t *testing.T) {
	fs := &fakeStore{}
	body := `{"name":"My view","description":"d","position":7,"spec":{"metric":"llm_requests_total","groupBy":["model"],"viz":"timeseries"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/saved-views", strings.NewReader(body))
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(fs).CreateSavedView(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	// The console treats a view with a non-empty `id` as "persisted" and reads
	// the bare object (not an envelope).
	var got store.SavedView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatalf("response missing id: %s", rec.Body.String())
	}
	if got.Name != "My view" || got.Position != 7 {
		t.Fatalf("unexpected create payload: %s", rec.Body.String())
	}
	// Fields decoded from the request must reach the store unchanged.
	if fs.gotCreate.Name != "My view" || fs.gotCreate.Position != 7 {
		t.Fatalf("store input mismatch: %+v", fs.gotCreate)
	}
	if !strings.Contains(string(fs.gotCreate.Spec), "llm_requests_total") {
		t.Fatalf("spec not passed through: %s", fs.gotCreate.Spec)
	}
	if fs.gotTenant.String() != testTenant {
		t.Fatalf("store called with tenant %s, want %s", fs.gotTenant, testTenant)
	}
}

func TestCreateSavedView_MissingName(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/saved-views", strings.NewReader(`{"spec":{}}`))
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(&fakeStore{}).CreateSavedView(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateSavedView_DuplicateIsConflict(t *testing.T) {
	fs := &fakeStore{createErr: store.ErrConflict}
	req := httptest.NewRequest(http.MethodPost, "/v1/saved-views", strings.NewReader(`{"name":"dup","spec":{}}`))
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(fs).CreateSavedView(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSavedView_NoContent(t *testing.T) {
	fs := &fakeStore{}
	id := "33333333-3333-3333-3333-333333333333"
	req := httptest.NewRequest(http.MethodDelete, "/v1/saved-views/"+id, nil)
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(fs).DeleteSavedView(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if fs.gotDeleteID.String() != id {
		t.Fatalf("deleted id = %s, want %s", fs.gotDeleteID, id)
	}
	if fs.gotTenant.String() != testTenant {
		t.Fatalf("store called with tenant %s, want %s", fs.gotTenant, testTenant)
	}
}

func TestDeleteSavedView_NotFound(t *testing.T) {
	fs := &fakeStore{deleteErr: store.ErrNotFound}
	req := httptest.NewRequest(http.MethodDelete, "/v1/saved-views/33333333-3333-3333-3333-333333333333", nil)
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(fs).DeleteSavedView(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteSavedView_InvalidID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/v1/saved-views/not-a-uuid", nil)
	req.Header.Set("X-Tenant-ID", testTenant)
	rec := httptest.NewRecorder()

	newDeps(&fakeStore{}).DeleteSavedView(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
