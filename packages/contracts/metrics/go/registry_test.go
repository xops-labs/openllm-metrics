// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package metricscontracts

import (
	"errors"
	"strings"
	"testing"
)

// Mandatory metrics from F008 §4 / vision §9. Every one of these must exist
// in the registry; removing one is a breaking change.
var mandatoryMetrics = []string{
	"llm_requests_total",
	"llm_input_tokens_total",
	"llm_output_tokens_total",
	"llm_total_tokens_total",
	"llm_cost_usd_total",
	"llm_errors_total",
	"llm_retries_total",
	"llm_timeouts_total",
	"llm_rate_limit_events_total",
	"llm_provider_api_errors_total",
}

func TestRegistry_ContainsEveryMandatoryMetric(t *testing.T) {
	for _, name := range mandatoryMetrics {
		if _, err := FindByName(name); err != nil {
			t.Errorf("missing mandatory metric %q: %v", name, err)
		}
	}
}

func TestRegistry_AllNamesUseLLMPrefix(t *testing.T) {
	for _, m := range Registry() {
		if !strings.HasPrefix(m.Name, "llm_") {
			t.Errorf("metric %q does not use the llm_ prefix", m.Name)
		}
	}
}

func TestRegistry_AllAllowedLabelsAreCanonical(t *testing.T) {
	canonical := map[Label]struct{}{}
	for _, l := range Labels() {
		canonical[l] = struct{}{}
	}
	for _, m := range Registry() {
		for _, l := range m.AllowedLabels {
			if _, ok := canonical[l]; !ok {
				t.Errorf("metric %q allows non-canonical label %q", m.Name, l)
			}
		}
	}
}

func TestRegistry_MandatoryLabelsAreInEveryCoreMetricAllowedSet(t *testing.T) {
	mandatory := MandatoryLabels()
	for _, m := range Registry() {
		for _, ml := range mandatory {
			if !m.IsAllowedLabel(ml) {
				t.Errorf("metric %q must allow mandatory label %q", m.Name, ml)
			}
		}
	}
}

func TestFindByName_UnknownReturnsErrUnknownMetric(t *testing.T) {
	_, err := FindByName("llm_made_up_metric")
	if !errors.Is(err, ErrUnknownMetric) {
		t.Fatalf("want ErrUnknownMetric, got %v", err)
	}
}

func TestNames_IsSortedAndCompleteAndStable(t *testing.T) {
	got := Names()
	if len(got) != len(Registry()) {
		t.Fatalf("Names()=%d, Registry()=%d", len(got), len(Registry()))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Errorf("Names not sorted: %q > %q", got[i-1], got[i])
		}
	}
}

// F008 §13: cardinality budget regression test. The budget is part of the
// contract — silently increasing it would erode TSDB cost guarantees.
func TestRegistry_CardinalityBudgetsMatchExpected(t *testing.T) {
	expected := map[string]int{
		"llm_requests_total":            50000,
		"llm_input_tokens_total":        30000,
		"llm_output_tokens_total":       30000,
		"llm_total_tokens_total":        30000,
		"llm_cost_usd_total":            30000,
		"llm_errors_total":              60000,
		"llm_retries_total":             40000,
		"llm_timeouts_total":            30000,
		"llm_rate_limit_events_total":   30000,
		"llm_provider_api_errors_total": 200,
	}
	for _, m := range Registry() {
		want, ok := expected[m.Name]
		if !ok {
			t.Errorf("metric %q missing expected cardinality budget; add it to the test", m.Name)
			continue
		}
		if m.CardinalityBudget != want {
			t.Errorf("metric %q cardinality budget = %d, want %d", m.Name, m.CardinalityBudget, want)
		}
		if m.CardinalityBudget <= 0 {
			t.Errorf("metric %q has non-positive cardinality budget %d", m.Name, m.CardinalityBudget)
		}
	}
}

func TestForbiddenFields_ContainsCoreLLMPayloadKeys(t *testing.T) {
	must := []string{"prompt", "completion", "input", "output", "messages", "embedding"}
	got := ForbiddenFields()
	for _, m := range must {
		found := false
		for _, g := range got {
			if g == m {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("forbidden field %q missing from ForbiddenFields()", m)
		}
	}
}

func TestRegistry_EveryEntryHasTypeAndUnit(t *testing.T) {
	for _, m := range Registry() {
		if m.Type == "" {
			t.Errorf("metric %q missing Type", m.Name)
		}
		if m.Unit == "" {
			t.Errorf("metric %q missing Unit", m.Name)
		}
	}
}
