// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package metrics is the notifier's self-observability surface.
//
// The exposition format matches the rest of the OSS workers (hand-rolled
// Prometheus text, no client_golang dependency) so the binary stays small.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

// Registry collects the notifier's counters and gauges.
type Registry struct {
	alertsConsumed  atomic.Int64
	alertsMatched   atomic.Int64
	alertsUnmatched atomic.Int64

	deliveriesSuccess atomic.Int64
	deliveriesFailure atomic.Int64
	deliveriesRetry   atomic.Int64
	deliveriesSkipped atomic.Int64

	webhookSent atomic.Int64
	smtpSent    atomic.Int64

	configMutations atomic.Int64
}

// New constructs an empty Registry.
func New() *Registry { return &Registry{} }

// IncAlertConsumed records receipt of an alert event from the bus.
func (r *Registry) IncAlertConsumed() { r.alertsConsumed.Add(1) }

// IncAlertMatched records an alert that matched at least one rule.
func (r *Registry) IncAlertMatched() { r.alertsMatched.Add(1) }

// IncAlertUnmatched records an alert that produced no fan-out (no rules).
func (r *Registry) IncAlertUnmatched() { r.alertsUnmatched.Add(1) }

// IncDeliverySuccess records a successful sink delivery.
func (r *Registry) IncDeliverySuccess() { r.deliveriesSuccess.Add(1) }

// IncDeliveryFailure records a delivery that exhausted retries.
func (r *Registry) IncDeliveryFailure() { r.deliveriesFailure.Add(1) }

// IncDeliveryRetry records a single retry attempt (in addition to the first).
func (r *Registry) IncDeliveryRetry() { r.deliveriesRetry.Add(1) }

// IncDeliverySkipped records a delivery skipped by the idempotency guard.
func (r *Registry) IncDeliverySkipped() { r.deliveriesSkipped.Add(1) }

// IncWebhookSent records a webhook payload sent (including retries).
func (r *Registry) IncWebhookSent() { r.webhookSent.Add(1) }

// IncSMTPSent records an SMTP message sent (including retries).
func (r *Registry) IncSMTPSent() { r.smtpSent.Add(1) }

// IncConfigMutation records a successful create/update/delete on a channel
// or rule via the HTTP API.
func (r *Registry) IncConfigMutation() { r.configMutations.Add(1) }

// Handler returns the Prometheus exposition handler.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.write(w)
	})
}

func (r *Registry) write(w io.Writer) {
	emit := func(name, help string, v int64) {
		_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n",
			name, help, name, name, v)
	}
	emit("llm_notifier_alerts_consumed_total",
		"Alert events consumed from the bus.",
		r.alertsConsumed.Load())
	emit("llm_notifier_alerts_matched_total",
		"Alerts that matched at least one routing rule.",
		r.alertsMatched.Load())
	emit("llm_notifier_alerts_unmatched_total",
		"Alerts that produced no fan-out (no rules matched).",
		r.alertsUnmatched.Load())
	emit("llm_notifier_deliveries_success_total",
		"Sink deliveries completed successfully.",
		r.deliveriesSuccess.Load())
	emit("llm_notifier_deliveries_failure_total",
		"Sink deliveries that exhausted retries.",
		r.deliveriesFailure.Load())
	emit("llm_notifier_deliveries_retry_total",
		"Individual retry attempts (excluding the first try).",
		r.deliveriesRetry.Load())
	emit("llm_notifier_deliveries_skipped_total",
		"Sink deliveries skipped by the (alert_event_id, channel_id) idempotency guard.",
		r.deliveriesSkipped.Load())
	emit("llm_notifier_webhook_sent_total",
		"Webhook payloads dispatched (attempts, not unique alerts).",
		r.webhookSent.Load())
	emit("llm_notifier_smtp_sent_total",
		"SMTP messages dispatched (attempts, not unique alerts).",
		r.smtpSent.Load())
	emit("llm_notifier_config_mutations_total",
		"Successful CRUD mutations on channels and rules via the HTTP API.",
		r.configMutations.Load())
}
