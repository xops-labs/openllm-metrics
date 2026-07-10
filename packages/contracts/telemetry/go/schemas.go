// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package telemetrycontracts embeds the canonical JSON-Schema definitions for
// the normalized streaming-bus event payloads owned by F008 and the related
// runtime/cost/quota topics produced by the Phase B/C/D/G services.
//
// Other Go packages (the schema-lint library, services that produce or consume
// these events) load the bytes via Schema() rather than reading from disk so
// the contracts are atomically versioned with the binary.
//
// Scope note. This package exposes event SHAPES only. It
// does NOT define routing algorithms, scoring weights, governance decision
// logic, or fallback selection logic; those are outside the telemetry schema
// contract.
package telemetrycontracts

import (
	_ "embed"
	"fmt"
)

// TopicUsageNormalized is the bus topic for cross-provider normalized usage
// events.
const TopicUsageNormalized = "llm.usage.normalized"

// TopicRuntimeNormalized is the bus topic for runtime telemetry events emitted
// by the gateway/SDK.
const TopicRuntimeNormalized = "llm.runtime.normalized"

// TopicUsageReconciled is the bus topic for reconciled billing events derived
// from the upstream llm-usage-exporter /focus.json endpoint. One event per
// (tenant, provider, model, period) tuple; carries the finalized vendor billing
// amount used by the reconciliation drift dashboards.
const TopicUsageReconciled = "llm.usage.reconciled"

// TopicCostEstimated is the bus topic for runtime-side cost estimates produced
// by apps/worker/cost-mapper from llm.runtime.normalized events and the
// pricing catalog. The output is a pure-function "tokens × rate" transform.
const TopicCostEstimated = "llm.cost.estimated"

// TopicReconciliationWindow is the bus topic for window-close events emitted
// by apps/worker/reconciler when a reconciliation bucket closes. Carries the
// per-window estimated vs. reconciled cost and the trivial drift ratio.
const TopicReconciliationWindow = "llm.reconciliation.window"

// TopicQuotaRiskV1 is the bus topic for per-(tenant, provider, model, region,
// kind) rolling snapshots of provider rate-limit signals emitted by
// apps/worker/quota-risk. Signal-only — carries no enforcement decision.
const TopicQuotaRiskV1 = "llm.quota.risk.v1"

// SchemaVersion is the current schema version for the F008 normalized
// contracts. Bump only on non-breaking additions; breaking changes create a
// new schema file. The Phase B/C/D/G topics carry their own per-topic
// "schema_version" field stamped by the producing service.
const SchemaVersion = "1"

// Source enumerates the surface that produced a normalized event. The value
// lives in the `source` field of every event and is the primary discriminator
// for downstream consumers that need to know whether the data came from the
// billing-side pull pipeline (exporter) or one of the runtime surfaces.
const (
	// SourceGateway is the in-repo reverse-proxy gateway (F018, runtime).
	SourceGateway = "gateway"
	// SourceSDK is an in-process SDK-emitted event (F019-F022, runtime).
	SourceSDK = "sdk"
	// SourceExporter is the upstream llm-usage-exporter delivered via the
	// label translator (pull, billing-side).
	SourceExporter = "exporter"
	// SourceOTel is an OTel Collector receiver-shaped event.
	SourceOTel = "otel"
)

//go:embed schemas/llm.usage.normalized.v1.json
var usageNormalizedV1 []byte

//go:embed schemas/llm.runtime.normalized.v1.json
var runtimeNormalizedV1 []byte

//go:embed schemas/llm.usage.reconciled.v1.json
var usageReconciledV1 []byte

//go:embed schemas/llm.cost.estimated.v1.json
var costEstimatedV1 []byte

//go:embed schemas/llm.reconciliation.window.v1.json
var reconciliationWindowV1 []byte

//go:embed schemas/llm.quota.risk.v1.json
var quotaRiskV1 []byte

// ErrUnknownTopic is returned by Schema when the supplied topic name is not
// owned by this package.
var ErrUnknownTopic = fmt.Errorf("telemetrycontracts: unknown topic")

// Schema returns the canonical JSON-Schema bytes for the supplied topic name
// at the current SchemaVersion. The returned slice is read-only — callers
// must not mutate it.
func Schema(topic string) ([]byte, error) {
	switch topic {
	case TopicUsageNormalized:
		return usageNormalizedV1, nil
	case TopicRuntimeNormalized:
		return runtimeNormalizedV1, nil
	case TopicUsageReconciled:
		return usageReconciledV1, nil
	case TopicCostEstimated:
		return costEstimatedV1, nil
	case TopicReconciliationWindow:
		return reconciliationWindowV1, nil
	case TopicQuotaRiskV1:
		return quotaRiskV1, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownTopic, topic)
	}
}

// Topics returns the list of topic names whose schemas live in this package.
// The order is stable so callers can iterate deterministically.
func Topics() []string {
	return []string{
		TopicUsageNormalized,
		TopicRuntimeNormalized,
		TopicUsageReconciled,
		TopicCostEstimated,
		TopicReconciliationWindow,
		TopicQuotaRiskV1,
	}
}
