// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package metrics

import (
	"strings"
	"testing"
)

func expose(r *Registry) string {
	var b strings.Builder
	r.write(&b)
	return b.String()
}

// The SLO pack (platform/slo/prometheus/) selects
// llm_gateway_latency_seconds_bucket{le="5"} for the p99 latency objective.
// This test pins the metric name, the seconds unit, and the 5s boundary so a
// rename or a unit regression breaks loudly instead of silently zeroing the
// latency SLO series.
func TestExposition_LatencyHistogramFeedsSLORules(t *testing.T) {
	r := New()
	lbls := Labels{Provider: "openai", Model: "gpt-4o", Tenant: "acme", Env: "prod", Status: "success", StatusCode: 200}

	r.ObserveRequest(lbls, 1.2, 0, false, true)  // fast request: inside le="2.5" and le="5"
	r.ObserveRequest(lbls, 7.5, 0, false, true)  // slow request: outside le="5", inside le="10"
	r.ObserveRequest(lbls, 0.05, 0, false, true) // sub-100ms request

	out := expose(r)

	if strings.Contains(out, "llm_gateway_request_latency_ms") {
		t.Fatalf("exposition still contains the retired millisecond metric name:\n%s", out)
	}
	for _, want := range []string{
		`# TYPE llm_gateway_latency_seconds histogram`,
		// 2 of 3 observations are <= 5s; the le="5" boundary must exist verbatim.
		`llm_gateway_latency_seconds_bucket{provider="openai",model="gpt-4o",tenant="acme",env="prod",status="success",status_code="200",le="5"} 2`,
		`llm_gateway_latency_seconds_bucket{provider="openai",model="gpt-4o",tenant="acme",env="prod",status="success",status_code="200",le="+Inf"} 3`,
		`llm_gateway_latency_seconds_count{provider="openai",model="gpt-4o",tenant="acme",env="prod",status="success",status_code="200"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q\nfull exposition:\n%s", want, out)
		}
	}

	// Sum is in seconds: 1.2 + 7.5 + 0.05 = 8.75. A milliseconds regression
	// would produce 8750 here.
	if !strings.Contains(out, `llm_gateway_latency_seconds_sum{provider="openai",model="gpt-4o",tenant="acme",env="prod",status="success",status_code="200"} 8.75`) {
		t.Errorf("latency sum not in seconds\nfull exposition:\n%s", out)
	}
}

func TestExposition_CountersAndStatusClassification(t *testing.T) {
	r := New()
	ok := Labels{Provider: "anthropic", Model: "claude-sonnet-4-5", Tenant: "acme", Env: "prod", Status: "success", StatusCode: 200}
	bad := Labels{Provider: "anthropic", Model: "claude-sonnet-4-5", Tenant: "acme", Env: "prod", Status: "rate_limited", StatusCode: 429}

	r.ObserveRequest(ok, 0.3, 0, true, true)
	r.ObserveRequest(bad, 0.1, 2, false, false)
	r.ObserveBusPublish(true)
	r.ObserveBusPublish(false)

	out := expose(r)

	checks := []string{
		`llm_gateway_requests_total{provider="anthropic",model="claude-sonnet-4-5",tenant="acme",env="prod",status="success",status_code="200"} 1`,
		`llm_gateway_errors_total{provider="anthropic",model="claude-sonnet-4-5",tenant="acme",env="prod",status="rate_limited",status_code="429"} 1`,
		`llm_gateway_retries_total{provider="anthropic",model="claude-sonnet-4-5",tenant="acme",env="prod",status="rate_limited",status_code="429"} 2`,
		`llm_gateway_streaming_total{provider="anthropic",model="claude-sonnet-4-5",tenant="acme",env="prod",status="success",status_code="200"} 1`,
		`llm_gateway_usage_observed_total{provider="anthropic",model="claude-sonnet-4-5",tenant="acme",env="prod",status="success",status_code="200"} 1`,
		`llm_gateway_usage_unknown_total{provider="anthropic",model="claude-sonnet-4-5",tenant="acme",env="prod",status="rate_limited",status_code="429"} 1`,
		`llm_gateway_bus_publish_total 1`,
		`llm_gateway_bus_publish_errors_total 1`,
	}
	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("exposition missing %q\nfull exposition:\n%s", want, out)
		}
	}
}

func TestLabels_ToKeyEscapesValues(t *testing.T) {
	l := Labels{Provider: `open"ai`, Model: "m\nodel", Tenant: `ac\me`, Env: "prod", Status: "success"}
	key := l.toKey()
	for _, want := range []string{`provider="open\"ai"`, `model="m\nodel"`, `tenant="ac\\me"`} {
		if !strings.Contains(key, want) {
			t.Errorf("toKey() = %q, missing %q", key, want)
		}
	}
}
