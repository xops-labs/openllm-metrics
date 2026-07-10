// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package schemalint validates streaming-bus event payloads and
// Prometheus-style metric observations against the F008 canonical contracts.
//
// The linter is intentionally schema-aware but does not pull in a full
// JSON-Schema validator: it enforces the small set of structural rules F008
// owns (mandatory fields, forbidden LLM-payload fields, label-set membership,
// type sanity) without external dependencies, so every Go service in the
// monorepo can import it cheaply.
//
// Full JSON-Schema draft validation is layered on top by services that need
// it via an off-the-shelf validator; this package's job is the OpenLLM
// Metrics-specific guard rail.
package schemalint

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	metricscontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/metrics/go"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

// Issue is a single lint failure. Code is a stable identifier suitable for
// programmatic filtering (e.g. CI quietening one rule). Path identifies the
// JSON field or label that triggered the issue.
type Issue struct {
	Code    string
	Path    string
	Message string
}

func (i Issue) String() string {
	if i.Path != "" {
		return fmt.Sprintf("[%s] %s: %s", i.Code, i.Path, i.Message)
	}
	return fmt.Sprintf("[%s] %s", i.Code, i.Message)
}

// Result collects every Issue produced by a single lint call. An empty
// Issues slice means the input passes.
type Result struct {
	Issues []Issue
}

// OK reports whether the input passed lint without issues.
func (r Result) OK() bool { return len(r.Issues) == 0 }

// Error returns a multiline summary of every issue, or nil if the result is
// OK. Suitable for direct return from a CLI / CI tool.
func (r Result) Error() error {
	if r.OK() {
		return nil
	}
	lines := make([]string, 0, len(r.Issues)+1)
	lines = append(lines, fmt.Sprintf("%d issue(s):", len(r.Issues)))
	for _, i := range r.Issues {
		lines = append(lines, "  - "+i.String())
	}
	return errors.New(strings.Join(lines, "\n"))
}

// Lint rule codes. These are stable string identifiers so callers (CI rules,
// dashboards) can reference them without depending on message text.
const (
	CodeUnknownTopic       = "OLLM-LINT-001"
	CodeMissingTenant      = "OLLM-LINT-002"
	CodeMissingField       = "OLLM-LINT-003"
	CodeForbiddenField     = "OLLM-LINT-004"
	CodeUnauthorizedLabel  = "OLLM-LINT-005"
	CodeUnknownMetric      = "OLLM-LINT-006"
	CodeBadType            = "OLLM-LINT-007"
	CodeMissingHashedID    = "OLLM-LINT-008"
	CodeSchemaVersionDrift = "OLLM-LINT-009"
)

// forbiddenSet and metricIndex cache the contracts-package lookups at package
// init. LintEvent/LintMetric run per event on the aggregation hot path, and
// both ForbiddenFields() and Registry() rebuild their slices on every call —
// caching keeps the linter allocation-free per lookup. Both structures are
// read-only after init.
var forbiddenSet = func() map[string]struct{} {
	fields := metricscontracts.ForbiddenFields()
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		set[f] = struct{}{}
	}
	return set
}()

var metricIndex = func() map[string]metricscontracts.Metric {
	registry := metricscontracts.Registry()
	idx := make(map[string]metricscontracts.Metric, len(registry))
	for _, m := range registry {
		idx[m.Name] = m
	}
	return idx
}()

// LintEvent validates a JSON event payload bound for `topic`. It enforces:
//
//   - topic is one of the topics registered in packages/contracts/telemetry
//   - schema_version matches the registry's current version
//   - mandatory fields per the schema (tenant, event_id, model, provider, …)
//     are present and non-empty
//   - no forbidden LLM-payload fields appear at any depth
//   - request_id_hash, when present, is a 64-hex-character lowercase SHA-256
//
// LintEvent does not currently validate every JSON-Schema constraint (enum
// membership for non-mandatory fields, numeric ranges) — those are deferred
// to the off-the-shelf validator services may layer on top. F008 owns the
// guard rails that are project-specific.
func LintEvent(topic string, payload []byte) Result {
	var r Result

	if _, err := telemetrycontracts.Schema(topic); err != nil {
		r.Issues = append(r.Issues, Issue{
			Code:    CodeUnknownTopic,
			Path:    "topic",
			Message: fmt.Sprintf("topic %q is not in the F008 contract set %v", topic, telemetrycontracts.Topics()),
		})
		return r
	}

	var event map[string]interface{}
	if err := json.Unmarshal(payload, &event); err != nil {
		r.Issues = append(r.Issues, Issue{
			Code:    CodeBadType,
			Path:    "$",
			Message: fmt.Sprintf("payload is not a JSON object: %v", err),
		})
		return r
	}

	r.Issues = append(r.Issues, checkForbiddenFields(event, "")...)
	r.Issues = append(r.Issues, checkMandatoryFields(topic, event)...)
	r.Issues = append(r.Issues, checkSchemaVersion(event)...)
	r.Issues = append(r.Issues, checkRequestIDHash(event)...)

	return r
}

// LintMetric validates a single Prometheus-style metric observation against
// the canonical registry: name is known, every emitted label is in the
// metric's AllowedLabels set, mandatory labels are present, and the tenant
// label is non-empty.
func LintMetric(name string, labels map[string]string) Result {
	var r Result

	m, ok := metricIndex[name]
	if !ok {
		r.Issues = append(r.Issues, Issue{
			Code:    CodeUnknownMetric,
			Path:    name,
			Message: fmt.Sprintf("metric %q is not registered in packages/contracts/metrics", name),
		})
		return r
	}

	allowed := map[metricscontracts.Label]struct{}{}
	for _, l := range m.AllowedLabels {
		allowed[l] = struct{}{}
	}

	for k := range labels {
		if _, ok := allowed[metricscontracts.Label(k)]; !ok {
			r.Issues = append(r.Issues, Issue{
				Code:    CodeUnauthorizedLabel,
				Path:    name + "{" + k + "}",
				Message: fmt.Sprintf("label %q not allowed on metric %q", k, name),
			})
		}
	}

	for _, ml := range metricscontracts.MandatoryLabels() {
		v, ok := labels[string(ml)]
		if !ok || v == "" {
			code := CodeMissingField
			if ml == metricscontracts.LabelTenant {
				code = CodeMissingTenant
			}
			r.Issues = append(r.Issues, Issue{
				Code:    code,
				Path:    name + "{" + string(ml) + "}",
				Message: fmt.Sprintf("mandatory label %q missing or empty on metric %q", ml, name),
			})
		}
	}

	for k := range labels {
		if _, banned := forbiddenSet[strings.ToLower(k)]; banned {
			r.Issues = append(r.Issues, Issue{
				Code:    CodeForbiddenField,
				Path:    name + "{" + k + "}",
				Message: fmt.Sprintf("label %q is a forbidden LLM-payload key (value redacted)", k),
			})
		}
	}

	return r
}

// usageNormalizedRequired and runtimeNormalizedRequired enumerate the JSON
// fields that must appear on every event for the matching topic. These are a
// subset of the full JSON-Schema "required" list in
// packages/contracts/telemetry/go/schemas and must be updated by hand when
// those schemas change.
var usageNormalizedRequired = []string{
	"schema_version", "event_id", "source_event_id", "source_mode",
	"source_service", "provider", "model", "operation", "tenant", "team",
	"env", "input_tokens", "output_tokens", "total_tokens",
	"cost_usd_minor_units", "period_start", "period_end", "normalized_at",
}

var runtimeNormalizedRequired = []string{
	"schema_version", "event_id", "source_mode", "source_service",
	"request_id_hash", "provider", "model", "operation", "tenant", "team",
	"env", "status", "latency_us", "recorded_at",
}

func checkMandatoryFields(topic string, event map[string]interface{}) []Issue {
	var required []string
	switch topic {
	case telemetrycontracts.TopicUsageNormalized:
		required = usageNormalizedRequired
	case telemetrycontracts.TopicRuntimeNormalized:
		required = runtimeNormalizedRequired
	default:
		return nil
	}

	var issues []Issue
	for _, key := range required {
		v, present := event[key]
		if !present || isZero(v) {
			code := CodeMissingField
			if key == "tenant" {
				code = CodeMissingTenant
			}
			issues = append(issues, Issue{
				Code:    code,
				Path:    key,
				Message: fmt.Sprintf("mandatory field %q missing or empty", key),
			})
		}
	}
	return issues
}

func checkForbiddenFields(node interface{}, path string) []Issue {
	var issues []Issue
	switch n := node.(type) {
	case map[string]interface{}:
		for k, v := range n {
			if _, banned := forbiddenSet[strings.ToLower(k)]; banned {
				issues = append(issues, Issue{
					Code:    CodeForbiddenField,
					Path:    joinPath(path, k),
					Message: fmt.Sprintf("field %q is a forbidden LLM-payload key", k),
				})
			}
			issues = append(issues, checkForbiddenFields(v, joinPath(path, k))...)
		}
	case []interface{}:
		for i, v := range n {
			issues = append(issues, checkForbiddenFields(v, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return issues
}

func checkSchemaVersion(event map[string]interface{}) []Issue {
	v, ok := event["schema_version"]
	if !ok {
		return nil // covered by checkMandatoryFields
	}
	s, ok := v.(string)
	if !ok {
		return []Issue{{
			Code:    CodeBadType,
			Path:    "schema_version",
			Message: "schema_version must be a string",
		}}
	}
	if s != telemetrycontracts.SchemaVersion {
		return []Issue{{
			Code:    CodeSchemaVersionDrift,
			Path:    "schema_version",
			Message: fmt.Sprintf("schema_version=%q does not match current contract version %q", s, telemetrycontracts.SchemaVersion),
		}}
	}
	return nil
}

func checkRequestIDHash(event map[string]interface{}) []Issue {
	v, ok := event["request_id_hash"]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return []Issue{{
			Code:    CodeBadType,
			Path:    "request_id_hash",
			Message: "request_id_hash must be a string",
		}}
	}
	if !isHex64(s) {
		return []Issue{{
			Code:    CodeMissingHashedID,
			Path:    "request_id_hash",
			Message: "request_id_hash must be a 64-character lowercase hex SHA-256",
		}}
	}
	return nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		if !isDigit && !isLowerHex {
			return false
		}
	}
	return true
}

func isZero(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []interface{}:
		return len(x) == 0
	case map[string]interface{}:
		return len(x) == 0
	}
	return false
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}
