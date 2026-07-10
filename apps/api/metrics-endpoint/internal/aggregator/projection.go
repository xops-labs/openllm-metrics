// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package aggregator

import (
	"strconv"

	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

// Contribution is one (metric, label-set, value) tuple to add to the
// aggregator. Each event typically produces several contributions (e.g. a
// usage event ticks request_count + input + output + total + cost).
type Contribution struct {
	Metric string
	Labels map[string]string
	Value  float64
}

// projectionsFor returns the contributions a single event produces, or a
// RejectReason if the topic is unknown. An empty `reason` and an empty
// `out` slice means the event was valid but carried nothing to record (this
// is fine â€” Apply will still count it as processed).
//
// The projection logic intentionally lives in one place so the contract
// between the F008 topics and the F008 metric registry is auditable in a
// single file. Every llm_* metric increment a downstream consumer might see
// is traceable to a line in this function.
func projectionsFor(topic string, event map[string]interface{}) ([]Contribution, RejectReason) {
	switch topic {
	case telemetrycontracts.TopicUsageNormalized:
		return projectUsage(event), ""
	case telemetrycontracts.TopicRuntimeNormalized:
		return projectRuntime(event), ""
	default:
		return nil, ReasonUnknownTopic
	}
}

// projectUsage maps an llm.usage.normalized event onto the canonical token /
// cost counters. The pull-mode poller pre-aggregates per window, so each
// event carries one input/output/total/cost tuple plus an optional
// request_count.
func projectUsage(event map[string]interface{}) []Contribution {
	provider := getStr(event, "provider")
	model := getStr(event, "model")
	operation := getStr(event, "operation")
	tenant := getStr(event, "tenant")
	team := getStr(event, "team")
	env := getStr(event, "env")
	app := getStr(event, "app")
	project := getStr(event, "project")
	region := getStr(event, "region")

	baseLabels := map[string]string{
		"provider":  provider,
		"model":     model,
		"operation": operation,
		"tenant":    tenant,
		"team":      team,
		"env":       env,
	}
	addIfNonEmpty(baseLabels, "app", app)
	addIfNonEmpty(baseLabels, "project", project)
	addIfNonEmpty(baseLabels, "region", region)

	out := make([]Contribution, 0, 5)

	if rc, ok := getNum(event, "request_count"); ok && rc > 0 {
		// Pull-mode billing events do not carry an HTTP status_code (the
		// Usage API reports billable counts, not individual HTTP responses).
		// We deliberately omit status_code rather than synthesize one, so
		// dashboards distinguish observed-from-bus runtime traffic
		// (status_code present) from billing-derived counts (absent).
		out = append(out, Contribution{
			Metric: "llm_requests_total",
			Labels: copyLabels(baseLabels),
			Value:  rc,
		})
	}

	if v, ok := getNum(event, "input_tokens"); ok && v > 0 {
		out = append(out, Contribution{
			Metric: "llm_input_tokens_total",
			Labels: copyLabels(baseLabels),
			Value:  v,
		})
	}
	if v, ok := getNum(event, "output_tokens"); ok && v > 0 {
		out = append(out, Contribution{
			Metric: "llm_output_tokens_total",
			Labels: copyLabels(baseLabels),
			Value:  v,
		})
	}
	if v, ok := getNum(event, "total_tokens"); ok && v > 0 {
		out = append(out, Contribution{
			Metric: "llm_total_tokens_total",
			Labels: copyLabels(baseLabels),
			Value:  v,
		})
	}
	if v, ok := getNum(event, "cost_usd_minor_units"); ok && v > 0 {
		// F008 Â§10: USD as float in metrics, integer minor units in payload.
		out = append(out, Contribution{
			Metric: "llm_cost_usd_total",
			Labels: copyLabels(baseLabels),
			Value:  v / 100.0,
		})
	}

	return out
}

// projectRuntime maps an llm.runtime.normalized event onto the canonical
// request / error / retry / timeout counters. One event represents one
// observed request from the gateway / SDK proxy path.
func projectRuntime(event map[string]interface{}) []Contribution {
	provider := getStr(event, "provider")
	model := getStr(event, "model")
	operation := getStr(event, "operation")
	tenant := getStr(event, "tenant")
	team := getStr(event, "team")
	env := getStr(event, "env")
	app := getStr(event, "app")
	project := getStr(event, "project")
	region := getStr(event, "region")
	status := getStr(event, "status")
	errorType := getStr(event, "error_type")

	statusCode := ""
	if sc, ok := getNum(event, "status_code"); ok {
		statusCode = formatInt(int64(sc))
	}

	baseLabels := map[string]string{
		"provider":  provider,
		"model":     model,
		"operation": operation,
		"tenant":    tenant,
		"team":      team,
		"env":       env,
	}
	addIfNonEmpty(baseLabels, "app", app)
	addIfNonEmpty(baseLabels, "project", project)
	addIfNonEmpty(baseLabels, "region", region)

	out := make([]Contribution, 0, 6)

	// llm_requests_total â€” one per runtime event.
	reqLabels := copyLabels(baseLabels)
	if statusCode != "" {
		reqLabels["status_code"] = statusCode
	}
	if errorType != "" {
		reqLabels["error_type"] = errorType
	}
	out = append(out, Contribution{
		Metric: "llm_requests_total",
		Labels: reqLabels,
		Value:  1,
	})

	if v, ok := getNum(event, "input_tokens"); ok && v > 0 {
		out = append(out, Contribution{
			Metric: "llm_input_tokens_total",
			Labels: copyLabels(baseLabels),
			Value:  v,
		})
	}
	if v, ok := getNum(event, "output_tokens"); ok && v > 0 {
		out = append(out, Contribution{
			Metric: "llm_output_tokens_total",
			Labels: copyLabels(baseLabels),
			Value:  v,
		})
	}
	if v, ok := getNum(event, "total_tokens"); ok && v > 0 {
		out = append(out, Contribution{
			Metric: "llm_total_tokens_total",
			Labels: copyLabels(baseLabels),
			Value:  v,
		})
	}

	switch status {
	case "error":
		errLabels := copyLabels(baseLabels)
		if statusCode != "" {
			errLabels["status_code"] = statusCode
		}
		if errorType != "" {
			errLabels["error_type"] = errorType
		}
		out = append(out, Contribution{
			Metric: "llm_errors_total",
			Labels: errLabels,
			Value:  1,
		})
	case "timeout":
		out = append(out, Contribution{
			Metric: "llm_timeouts_total",
			Labels: copyLabels(baseLabels),
			Value:  1,
		})
	case "rate_limited":
		rlLabels := copyLabels(baseLabels)
		if statusCode != "" {
			rlLabels["status_code"] = statusCode
		}
		out = append(out, Contribution{
			Metric: "llm_rate_limit_events_total",
			Labels: rlLabels,
			Value:  1,
		})
	}

	if v, ok := getNum(event, "retry_count"); ok && v > 0 {
		retryLabels := copyLabels(baseLabels)
		if errorType != "" {
			retryLabels["error_type"] = errorType
		}
		out = append(out, Contribution{
			Metric: "llm_retries_total",
			Labels: retryLabels,
			Value:  v,
		})
	}

	return out
}

// --- small JSON helpers (kept package-private so the projection contract is
// audit-friendly without a generic reflection dependency) -------------------

func getStr(event map[string]interface{}, key string) string {
	v, ok := event[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func getNum(event map[string]interface{}, key string) (float64, bool) {
	v, ok := event[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func addIfNonEmpty(m map[string]string, k, v string) {
	if v == "" {
		return
	}
	m[k] = v
}

func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}
