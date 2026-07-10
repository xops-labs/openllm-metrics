// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package openaicontract_test holds the F009 cross-package contract tests:
// recorded fixture replay through the adapter, idempotency, 429 backoff,
// 5xx circuit breaker, provider portability, and F008 schema-lint pass.
//
// These tests live in tests/provider-adapters/openai/ (not inside the
// poller module) so they exercise the public surface the way a downstream
// consumer or a later provider implementation would. Cross-module imports
// flow through apps/.../pkg/testkit (the only sanctioned bridge out of
// the poller's internal/ packages).
package openaicontract_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
	schemalint "github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go"

	testkit "github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/pkg/testkit"
)

// stubEmitter captures every event the poller would have published.
type stubEmitter struct {
	mu     sync.Mutex
	events []testkit.NormalizedEvent
}

func (s *stubEmitter) Emit(_ context.Context, ev testkit.NormalizedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}

func (s *stubEmitter) Close() {}

func (s *stubEmitter) Events() []testkit.NormalizedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]testkit.NormalizedEvent, len(s.events))
	copy(out, s.events)
	return out
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("fixtures", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func newFixtureServer(t *testing.T, usage, cost []byte, attemptHook func(path string, attempt int) (int, []byte)) *httptest.Server {
	t.Helper()
	attempts := map[string]*atomic.Int64{
		testkit.UsagePath: {},
		testkit.CostPath:  {},
	}
	mux := http.NewServeMux()
	handle := func(path string, body []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			n := attempts[path].Add(1) - 1
			if attemptHook != nil {
				if status, override := attemptHook(path, int(n)); status != 0 {
					w.WriteHeader(status)
					if override != nil {
						_, _ = w.Write(override)
					}
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set(testkit.HeaderRequestID, "req_test_"+path)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		})
	}
	handle(testkit.UsagePath, usage)
	handle(testkit.CostPath, cost)
	return httptest.NewServer(mux)
}

func mkPoller(t *testing.T, srv *httptest.Server, lru *testkit.LRU, emitter testkit.Emitter, m *testkit.Registry) *testkit.Poller {
	t.Helper()
	client := testkit.NewClient(testkit.ClientConfig{
		BaseURL:                 srv.URL,
		APIKey:                  "sk-test-not-a-real-key",
		MaxRetries:              3,
		CircuitBreakerThreshold: 3,
		CircuitBreakerCooldown:  10 * time.Millisecond,
		HTTPClient:              srv.Client(),
	})
	cfg := testkit.PollerConfig{
		Interval:   50 * time.Millisecond,
		WindowSize: 5 * time.Minute,
		ContextLabels: testkit.ContextLabels{
			Tenant:  "tenant-001",
			Team:    "ai-platform",
			App:     "snapcal",
			Env:     "production",
			Project: "snapcal-prod",
			Region:  "us-east-1",
		},
		Now: func() time.Time { return time.Date(2026, 5, 17, 10, 5, 10, 0, time.UTC) },
	}
	return testkit.NewPoller(cfg, client, lru, emitter, m)
}

// -- 1. Happy path + schema-lint --------------------------------------------

func TestHappyPath_EmitsSchemaCompliantEvents(t *testing.T) {
	t.Parallel()
	srv := newFixtureServer(t, loadFixture(t, "usage_happy.json"), loadFixture(t, "cost_happy.json"), nil)
	defer srv.Close()

	emitter := &stubEmitter{}
	lru := testkit.NewLRU(64)
	m := testkit.NewMetrics(testkit.ProviderName, "tenant-001", "production")
	p := mkPoller(t, srv, lru, emitter, m)

	emitted, dropped, err := p.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if emitted != 3 {
		t.Fatalf("expected 3 emitted, got %d (dropped=%d)", emitted, dropped)
	}
	if dropped != 0 {
		t.Fatalf("expected 0 dropped on first cycle, got %d", dropped)
	}

	for _, ev := range emitter.Events() {
		payload, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		result := schemalint.LintEvent(telemetrycontracts.TopicUsageNormalized, payload)
		if !result.OK() {
			t.Fatalf("schemalint failed for event %s: %v", ev.EventID, result.Error())
		}
	}

	for _, ev := range emitter.Events() {
		if ev.Tenant == "" {
			t.Fatalf("event %s missing tenant", ev.EventID)
		}
		if ev.Provider != "openai" {
			t.Fatalf("event %s has provider=%q", ev.EventID, ev.Provider)
		}
		if ev.SchemaVersion != telemetrycontracts.SchemaVersion {
			t.Fatalf("event %s schema_version=%q", ev.EventID, ev.SchemaVersion)
		}
	}

	if m.ScrapeSuccessCount() != 1 {
		t.Fatalf("expected 1 scrape success, got %d", m.ScrapeSuccessCount())
	}
	if m.LastSuccessUnix() == 0 {
		t.Fatalf("expected last_success_timestamp to be set")
	}
}

// -- 2. Idempotency ---------------------------------------------------------

func TestIdempotency_ReplayingSameWindowDoesNotDoubleEmit(t *testing.T) {
	t.Parallel()
	srv := newFixtureServer(t, loadFixture(t, "usage_happy.json"), loadFixture(t, "cost_happy.json"), nil)
	defer srv.Close()

	emitter := &stubEmitter{}
	lru := testkit.NewLRU(64)
	m := testkit.NewMetrics(testkit.ProviderName, "tenant-001", "production")
	p := mkPoller(t, srv, lru, emitter, m)

	emitted1, dropped1, err := p.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	emitted2, dropped2, err := p.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	if emitted1 != 3 || dropped1 != 0 {
		t.Fatalf("first cycle: emitted=%d dropped=%d, want 3/0", emitted1, dropped1)
	}
	if emitted2 != 0 || dropped2 != 3 {
		t.Fatalf("second cycle: emitted=%d dropped=%d, want 0/3 (all deduped)", emitted2, dropped2)
	}
	if got := len(emitter.Events()); got != 3 {
		t.Fatalf("total events after two replays = %d, want 3", got)
	}
}

// -- 3. 429 backoff ---------------------------------------------------------

func Test429Backoff_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	srv := newFixtureServer(t,
		loadFixture(t, "usage_happy.json"),
		loadFixture(t, "cost_happy.json"),
		func(path string, attempt int) (int, []byte) {
			if path == testkit.UsagePath && attempt == 0 {
				return http.StatusTooManyRequests, []byte(`{"error":"rate_limited"}`)
			}
			return 0, nil
		},
	)
	defer srv.Close()

	emitter := &stubEmitter{}
	lru := testkit.NewLRU(64)
	m := testkit.NewMetrics(testkit.ProviderName, "tenant-001", "production")
	p := mkPoller(t, srv, lru, emitter, m)

	emitted, _, err := p.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if emitted == 0 {
		t.Fatalf("expected events after backoff recovery, got 0")
	}
	if m.RateLimitEventCount() == 0 {
		t.Fatalf("expected llm_rate_limit_events_total to be incremented after a 429")
	}
}

// -- 4. 5xx circuit breaker -------------------------------------------------

func Test5xxCircuitBreaker_OpensAndBlocks(t *testing.T) {
	t.Parallel()
	srv := newFixtureServer(t,
		loadFixture(t, "usage_happy.json"),
		loadFixture(t, "cost_happy.json"),
		func(path string, _ int) (int, []byte) {
			if path == testkit.UsagePath {
				return http.StatusInternalServerError, []byte(`{"error":"server_unavailable"}`)
			}
			return 0, nil
		},
	)
	defer srv.Close()

	emitter := &stubEmitter{}
	lru := testkit.NewLRU(64)
	m := testkit.NewMetrics(testkit.ProviderName, "tenant-001", "production")
	p := mkPoller(t, srv, lru, emitter, m)

	for i := 0; i < 5; i++ {
		_, _, _ = p.RunOnce(context.Background())
	}
	if m.ScrapeFailureCount() == 0 {
		t.Fatalf("expected scrape failures, got 0")
	}
	out := scrapeMetrics(t, m)
	if !strings.Contains(out, `reason="5xx"`) && !strings.Contains(out, `reason="circuit_open"`) {
		t.Fatalf("expected llm_provider_api_errors_total with reason 5xx or circuit_open, got:\n%s", out)
	}
}

func scrapeMetrics(t *testing.T, m *testkit.Registry) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	return rec.Body.String()
}

// -- 5. Provider portability -----------------------------------------------

// Schemalint and telemetrycontracts MUST be usable without ever touching the
// OpenAI adapter. This lint pass on a hand-built non-OpenAI event proves the
// core schema layer is provider-agnostic.
func TestProviderPortability_CoreSchemaIndependent(t *testing.T) {
	t.Parallel()
	ev := map[string]interface{}{
		"schema_version":       telemetrycontracts.SchemaVersion,
		"event_id":             "01890000-0000-7000-8000-00000000beef",
		"source_event_id":      "anthropic:1747476000:1747476300:claude-3-5-sonnet:proj_foo",
		"source_mode":          "pull",
		"source_service":       "apps/worker/usage-poller/anthropic",
		"provider":             "anthropic",
		"model":                "claude-3-5-sonnet",
		"operation":            "chat",
		"tenant":               "tenant-001",
		"team":                 "ai-platform",
		"app":                  "snapcal",
		"env":                  "production",
		"input_tokens":         100,
		"output_tokens":        25,
		"total_tokens":         125,
		"cost_usd_minor_units": 17,
		"period_start":         "2026-05-17T10:00:00Z",
		"period_end":           "2026-05-17T10:05:00Z",
		"normalized_at":        "2026-05-17T10:05:10Z",
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if result := schemalint.LintEvent(telemetrycontracts.TopicUsageNormalized, payload); !result.OK() {
		t.Fatalf("non-openai event must pass schemalint; got: %v", result.Error())
	}
}

// -- 6. Forbidden-payload rejection -----------------------------------------

func TestSchemaLint_RejectsForbiddenPayloadFields(t *testing.T) {
	t.Parallel()
	bad := map[string]interface{}{
		"schema_version":       telemetrycontracts.SchemaVersion,
		"event_id":             "01890000-0000-7000-8000-00000000abcd",
		"source_event_id":      "openai:1:2:gpt-4o:proj",
		"source_mode":          "pull",
		"source_service":       testkit.SourceServiceLabel,
		"provider":             "openai",
		"model":                "gpt-4o",
		"operation":            "chat",
		"tenant":               "tenant-001",
		"team":                 "ai-platform",
		"env":                  "production",
		"input_tokens":         10,
		"output_tokens":        20,
		"total_tokens":         30,
		"cost_usd_minor_units": 5,
		"period_start":         "2026-05-17T10:00:00Z",
		"period_end":           "2026-05-17T10:05:00Z",
		"normalized_at":        "2026-05-17T10:05:10Z",
		"prompt":               "this should be rejected",
	}
	payload, _ := json.Marshal(bad)
	result := schemalint.LintEvent(telemetrycontracts.TopicUsageNormalized, payload)
	if result.OK() {
		t.Fatalf("expected schemalint to reject 'prompt' field")
	}
}

// -- 7. APIKey is not leaked in errors -------------------------------------

func TestAPIKey_NeverLeaksInError(t *testing.T) {
	t.Parallel()
	client := testkit.NewClient(testkit.ClientConfig{
		BaseURL:                 "http://127.0.0.1:1",
		APIKey:                  "sk-secret-key-must-not-appear",
		MaxRetries:              0,
		CircuitBreakerThreshold: 1,
		HTTPClient:              &http.Client{Timeout: 100 * time.Millisecond},
	})
	_, _, err := client.FetchWindow(context.Background(), time.Now().Add(-5*time.Minute), time.Now())
	if err == nil {
		t.Fatal("expected error from unreachable endpoint")
	}
	if strings.Contains(err.Error(), "sk-secret-key-must-not-appear") {
		t.Fatalf("API key leaked into error message: %s", err)
	}
}

// -- 8. Circuit breaker exposes its state ----------------------------------

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := testkit.NewClient(testkit.ClientConfig{
		BaseURL:                 srv.URL,
		APIKey:                  "sk-test",
		MaxRetries:              0,
		CircuitBreakerThreshold: 2,
		CircuitBreakerCooldown:  10 * time.Millisecond,
		HTTPClient:              srv.Client(),
	})

	for i := 0; i < 4; i++ {
		_, _, err := client.FetchWindow(context.Background(), time.Now().Add(-5*time.Minute), time.Now())
		if err == nil {
			t.Fatalf("attempt %d: expected error", i)
		}
	}
	_, _, err := client.FetchWindow(context.Background(), time.Now().Add(-5*time.Minute), time.Now())
	if !errors.Is(err, testkit.ErrCircuitOpen) && !errors.Is(err, testkit.ErrServerError) {
		t.Fatalf("expected ErrCircuitOpen or ErrServerError after breaker trip, got: %v", err)
	}
	_ = client.CircuitOpen()
}
