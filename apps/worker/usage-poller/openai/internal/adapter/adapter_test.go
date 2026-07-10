// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package adapter_test

import (
	"testing"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/adapter"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/openaiclient"
)

func mkWindow() openaiclient.CombinedWindow {
	return openaiclient.CombinedWindow{
		Usage: openaiclient.UsageResponse{
			Data: []openaiclient.UsageBucket{
				{
					StartTime: 1747476000,
					EndTime:   1747476300,
					Results: []openaiclient.UsageResult{
						{Model: "gpt-4o-mini", ProjectID: "proj_a", InputTokens: 1000, OutputTokens: 500, NumModelRequests: 2},
						{Model: "gpt-4o", ProjectID: "proj_a", InputTokens: 9000, OutputTokens: 1500, NumModelRequests: 4},
					},
				},
			},
		},
		Cost: openaiclient.CostResponse{
			Data: []openaiclient.CostBucket{
				{
					StartTime: 1747476000,
					EndTime:   1747476300,
					Results: []openaiclient.CostResult{
						{Amount: openaiclient.Amount{Value: 1.20, Currency: "usd"}},
					},
				},
			},
		},
	}
}

func TestNormalize_TenantRequired(t *testing.T) {
	t.Parallel()
	_, err := adapter.Normalize(mkWindow(), adapter.ContextLabels{}, nil)
	if err == nil {
		t.Fatal("expected error when tenant is empty")
	}
}

func TestNormalize_ProducesOneEventPerRow(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Date(2026, 5, 17, 10, 5, 10, 0, time.UTC) }
	events, err := adapter.Normalize(mkWindow(), adapter.ContextLabels{
		Tenant: "tenant-001", Team: "ai-platform", Env: "production", App: "snapcal",
	}, now)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events want 2", len(events))
	}
	for _, ev := range events {
		if ev.Provider != "openai" {
			t.Errorf("provider=%q", ev.Provider)
		}
		if ev.SourceMode != "pull" {
			t.Errorf("source_mode=%q", ev.SourceMode)
		}
		if ev.SchemaVersion != "1" {
			t.Errorf("schema_version=%q", ev.SchemaVersion)
		}
		if ev.NormalizedAt != "2026-05-17T10:05:10Z" {
			t.Errorf("normalized_at=%q", ev.NormalizedAt)
		}
		if ev.TotalTokens != ev.InputTokens+ev.OutputTokens {
			t.Errorf("total_tokens mismatch")
		}
		if ev.PeriodStart == "" || ev.PeriodEnd == "" {
			t.Errorf("period bounds missing")
		}
	}
}

func TestNormalize_CostWeightedByTokens(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Date(2026, 5, 17, 10, 5, 10, 0, time.UTC) }
	events, _ := adapter.Normalize(mkWindow(), adapter.ContextLabels{
		Tenant: "tenant-001", Team: "ai-platform", Env: "production",
	}, now)
	// Bucket totals: row0=1500 tokens, row1=10500 tokens, total=12000.
	// Total cost = $1.20 = 120 minor units.
	// row0 share = 1500/12000 = 12.5% → round(15) = 15 minor units
	// row1 share = 10500/12000 = 87.5% → round(105) = 105 minor units
	if events[0].CostUSDMinorUnits != 15 {
		t.Errorf("row0 cost=%d want 15", events[0].CostUSDMinorUnits)
	}
	if events[1].CostUSDMinorUnits != 105 {
		t.Errorf("row1 cost=%d want 105", events[1].CostUSDMinorUnits)
	}
}

func TestNormalize_OperationMapping(t *testing.T) {
	t.Parallel()
	now := func() time.Time { return time.Date(2026, 5, 17, 10, 5, 10, 0, time.UTC) }
	w := openaiclient.CombinedWindow{
		Usage: openaiclient.UsageResponse{
			Data: []openaiclient.UsageBucket{
				{
					StartTime: 1, EndTime: 2,
					Results: []openaiclient.UsageResult{
						{Model: "gpt-4o", ProjectID: "p", InputTokens: 1, OutputTokens: 1},
						{Model: "text-embedding-3-small", ProjectID: "p", InputTokens: 1, OutputTokens: 0},
						{Model: "dall-e-3", ProjectID: "p", InputTokens: 1, OutputTokens: 0},
						{Model: "whisper-1", ProjectID: "p", InputTokens: 1, OutputTokens: 0},
						{Model: "text-moderation-latest", ProjectID: "p", InputTokens: 1, OutputTokens: 0},
					},
				},
			},
		},
	}
	events, _ := adapter.Normalize(w, adapter.ContextLabels{Tenant: "t", Team: "x", Env: "production"}, now)
	wantOps := []string{"chat", "embedding", "image", "audio", "moderation"}
	for i, want := range wantOps {
		if events[i].Operation != want {
			t.Errorf("row %d op=%q want %q", i, events[i].Operation, want)
		}
	}
}

func TestNormalize_ConfigProjectWinsOverApiProject(t *testing.T) {
	t.Parallel()
	events, _ := adapter.Normalize(mkWindow(), adapter.ContextLabels{
		Tenant: "tenant-001", Team: "ai-platform", Env: "production", Project: "config-project",
	}, nil)
	for _, ev := range events {
		if ev.Project != "config-project" {
			t.Errorf("project=%q want config-project", ev.Project)
		}
	}
}

func TestNormalize_NonUSDCostIsSkipped(t *testing.T) {
	t.Parallel()
	w := mkWindow()
	w.Cost.Data[0].Results = []openaiclient.CostResult{
		{Amount: openaiclient.Amount{Value: 99.99, Currency: "EUR"}},
	}
	events, _ := adapter.Normalize(w, adapter.ContextLabels{
		Tenant: "tenant-001", Team: "ai-platform", Env: "production",
	}, nil)
	for _, ev := range events {
		if ev.CostUSDMinorUnits != 0 {
			t.Errorf("expected 0 cost for non-USD line, got %d", ev.CostUSDMinorUnits)
		}
	}
}
