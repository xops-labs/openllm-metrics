// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/aggregator"
)

type stubReady struct{ ready bool }

func (s *stubReady) Ready() bool { return s.ready }

func TestHealthz_ReturnsOK(t *testing.T) {
	h := Handler(aggregator.New(), &stubReady{ready: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status=%d", rec.Code)
	}
}

func TestReadyz_ReturnsServiceUnavailableWhenNotReady(t *testing.T) {
	h := Handler(aggregator.New(), &stubReady{ready: false})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status=%d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ready":false`) {
		t.Errorf("body missing ready=false: %s", rec.Body.String())
	}
}

func TestReadyz_Returns200WhenReady(t *testing.T) {
	h := Handler(aggregator.New(), &stubReady{ready: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status=%d", rec.Code)
	}
}

func TestMetrics_ReturnsPrometheusContentType(t *testing.T) {
	h := Handler(aggregator.New(), &stubReady{ready: true})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status=%d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("metrics content-type=%q, want text/plain prefix", got)
	}
	// Should always emit the rejected-events series scaffolding even with
	// an empty aggregator.
	if !strings.Contains(rec.Body.String(), "llm_aggregator_rejected_events_total") {
		t.Errorf("metrics body missing self-metric:\n%s", rec.Body.String())
	}
}
