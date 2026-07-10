// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package openaiclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/openaiclient"
)

func TestRateLimitHeaderParsing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(openaiclient.HeaderRateLimit, "42")
		w.Header().Set(openaiclient.HeaderRateReset, "8m20s")
		w.Header().Set(openaiclient.HeaderRequestID, "req_abc")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"page","data":[],"has_more":false}`))
	}))
	defer srv.Close()

	c := openaiclient.New(openaiclient.Config{
		BaseURL:    srv.URL,
		APIKey:     "sk-test",
		HTTPClient: srv.Client(),
	})
	_, rl, err := c.FetchWindow(context.Background(), time.Unix(1, 0), time.Unix(2, 0))
	if err != nil {
		t.Fatalf("FetchWindow: %v", err)
	}
	if rl.Remaining != 42 {
		t.Errorf("remaining=%d want 42", rl.Remaining)
	}
	if rl.ResetAfter != 8*time.Minute+20*time.Second {
		t.Errorf("reset=%s want 8m20s", rl.ResetAfter)
	}
	if rl.RequestID != "req_abc" {
		t.Errorf("req id=%q", rl.RequestID)
	}
}

func Test4xxIsNotRetried(t *testing.T) {
	t.Parallel()
	var n atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := openaiclient.New(openaiclient.Config{
		BaseURL:    srv.URL,
		APIKey:     "sk-test",
		MaxRetries: 4,
		HTTPClient: srv.Client(),
	})
	_, _, err := c.FetchWindow(context.Background(), time.Unix(1, 0), time.Unix(2, 0))
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if n.Load() != 1 {
		t.Fatalf("expected exactly 1 call (no retry on 4xx), got %d", n.Load())
	}
}

func TestServerErrorRetriesThenFails(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := openaiclient.New(openaiclient.Config{
		BaseURL:                 srv.URL,
		APIKey:                  "sk-test",
		MaxRetries:              1,
		CircuitBreakerThreshold: 100, // do not trip in this test
		HTTPClient:              srv.Client(),
	})
	_, _, err := c.FetchWindow(context.Background(), time.Unix(1, 0), time.Unix(2, 0))
	if !errors.Is(err, openaiclient.ErrServerError) {
		t.Fatalf("expected ErrServerError, got %v", err)
	}
}
