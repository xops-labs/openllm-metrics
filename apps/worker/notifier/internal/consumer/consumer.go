// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package consumer subscribes to alert.event.v1 on the bus and orchestrates
// fan-out:
//
//  1. Decode the inbound alert into a router.Alert.
//  2. Look up rules + channels for the alert's tenant.
//  3. For each matching (rule, channel) pair, claim a delivery row, dispatch
//     to the sink, and record the outcome.
//
// Retries on transient sink errors use bounded exponential backoff. The
// (alert_event_id, channel_id) idempotency guard in notification_deliveries
// ensures re-delivery from a Kafka replay or a consumer restart never
// double-sends.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/router"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/sink"
	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/notifier/internal/store"
)

// RetryParams bounds the retry loop. See config.RetryConfig for the
// user-facing knobs.
type RetryParams struct {
	MaxAttempts       int
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	PerAttemptTimeout time.Duration
}

// Consumer wires the bus subscription to the routing + sink fan-out.
type Consumer struct {
	bus     *busclient.Consumer
	store   store.Store
	webhook *sink.WebhookSink
	smtp    *sink.SMTPSink
	mreg    *metrics.Registry
	logger  *slog.Logger
	retry   RetryParams
}

// New constructs a Consumer.
func New(
	bus *busclient.Consumer,
	s store.Store,
	wh *sink.WebhookSink,
	sm *sink.SMTPSink,
	mreg *metrics.Registry,
	logger *slog.Logger,
	retry RetryParams,
) *Consumer {
	return &Consumer{
		bus:     bus,
		store:   s,
		webhook: wh,
		smtp:    sm,
		mreg:    mreg,
		logger:  logger,
		retry:   retry,
	}
}

// Run blocks on the bus subscription until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	return c.bus.Run(ctx, c.handle)
}

// handle is invoked per consumed record. Returning a non-nil error routes
// the record to the DLQ; we only return errors for un-decodable payloads.
// Sink-level failures are recorded in notification_deliveries and do not
// poison the topic.
func (c *Consumer) handle(ctx context.Context, rec *kgo.Record) error {
	c.mreg.IncAlertConsumed()

	var alert router.Alert
	if err := json.Unmarshal(rec.Value, &alert); err != nil {
		c.logger.Warn("notifier: decode alert", "err", err)
		return fmt.Errorf("decode alert: %w", err)
	}
	if alert.ID == "" || alert.TenantID == "" {
		c.logger.Warn("notifier: alert missing required fields", "event_id", alert.ID, "tenant_id", alert.TenantID)
		return fmt.Errorf("alert missing id or tenant_id")
	}

	rules, chanMap, err := c.store.ListRulesAndChannels(ctx, alert.TenantID)
	if err != nil {
		// Transient DB error — return error to DLQ; operators can replay
		// from the DLQ topic once the DB is back.
		c.logger.Warn("notifier: list rules+channels", "tenant_id", alert.TenantID, "err", err)
		return err
	}
	dispatches := router.Route(alert, rules, chanMap)
	if len(dispatches) == 0 {
		c.mreg.IncAlertUnmatched()
		return nil
	}
	c.mreg.IncAlertMatched()

	for _, d := range dispatches {
		c.fanOut(ctx, alert, rec.Value, d)
	}
	return nil
}

func (c *Consumer) fanOut(ctx context.Context, alert router.Alert, alertJSON []byte, d router.Dispatch) {
	deliveryID, err := c.store.ClaimDelivery(ctx, store.Delivery{
		TenantID:     alert.TenantID,
		RuleID:       d.Rule.ID,
		ChannelID:    d.Channel.ID,
		AlertEventID: alert.ID,
	})
	if errors.Is(err, store.ErrAlreadyDelivered) {
		c.mreg.IncDeliverySkipped()
		return
	}
	if err != nil {
		c.logger.Warn("notifier: claim delivery", "alert_id", alert.ID, "channel_id", d.Channel.ID, "err", err)
		return
	}

	attempts := 0
	var lastErr string
	for attempts < c.retry.MaxAttempts {
		attempts++
		if attempts > 1 {
			c.mreg.IncDeliveryRetry()
			if err := sleepWithCtx(ctx, c.backoff(attempts-1)); err != nil {
				lastErr = err.Error()
				break
			}
		}
		sendErr := c.dispatch(ctx, d.Channel, alert.ID, alertJSON)
		if sendErr == nil {
			c.mreg.IncDeliverySuccess()
			if mErr := c.store.MarkDelivery(ctx, deliveryID, "success", "", attempts); mErr != nil {
				c.logger.Warn("notifier: mark delivery success", "id", deliveryID, "err", mErr)
			}
			return
		}
		lastErr = redactErr(sendErr)
		if !errors.Is(sendErr, sink.ErrTransient) {
			// Terminal error — do not retry.
			break
		}
		// Update the row to 'retrying' so operators inspecting the table
		// while a slow retry is in flight see live state.
		if mErr := c.store.MarkDelivery(ctx, deliveryID, "retrying", lastErr, attempts); mErr != nil {
			c.logger.Warn("notifier: mark retrying", "id", deliveryID, "err", mErr)
		}
	}
	c.mreg.IncDeliveryFailure()
	if mErr := c.store.MarkDelivery(ctx, deliveryID, "failure", lastErr, attempts); mErr != nil {
		c.logger.Warn("notifier: mark failure", "id", deliveryID, "err", mErr)
	}
}

// dispatch routes to the right sink based on channel.kind. Each attempt is
// bounded by PerAttemptTimeout via a derived context.
func (c *Consumer) dispatch(ctx context.Context, ch store.Channel, alertID string, alertJSON []byte) error {
	attemptCtx, cancel := context.WithTimeout(ctx, c.retry.PerAttemptTimeout)
	defer cancel()
	switch ch.Kind {
	case "webhook":
		var cfg sink.WebhookConfig
		if err := json.Unmarshal(ch.Config, &cfg); err != nil {
			return fmt.Errorf("dispatch webhook: decode config: %w", err)
		}
		c.mreg.IncWebhookSent()
		return c.webhook.Send(attemptCtx, cfg, alertID, alertJSON)
	case "smtp":
		var cfg sink.SMTPConfig
		if err := json.Unmarshal(ch.Config, &cfg); err != nil {
			return fmt.Errorf("dispatch smtp: decode config: %w", err)
		}
		c.mreg.IncSMTPSent()
		return c.smtp.Send(attemptCtx, cfg, alertID, alertJSON)
	default:
		// Unknown kinds are terminal — Slack/PD/Teams/ServiceNow live in
		// custom and the DB CHECK constraint prevents them landing
		// here, but defense-in-depth is cheap.
		return fmt.Errorf("dispatch: unknown channel kind %q", ch.Kind)
	}
}

// backoff computes the delay for attempt n (0-indexed: n=0 is the wait
// before the second attempt).
func (c *Consumer) backoff(n int) time.Duration {
	d := c.retry.InitialBackoff
	for i := 0; i < n; i++ {
		d *= 2
		if d > c.retry.MaxBackoff {
			d = c.retry.MaxBackoff
			break
		}
	}
	return d
}

func sleepWithCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// redactErr scrubs anything that looks like a secret or auth header from the
// error message before it is persisted to notification_deliveries.last_error
// (which operators may surface in a console UI). We intentionally favour
// false positives — last_error is best-effort context, not a stack trace.
func redactErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	// Strip anything between "Authorization:" and a whitespace boundary;
	// strip anything between "password=" and the same. Both are
	// belt-and-suspenders — the sinks already avoid these substrings.
	return scrubSensitive(s)
}

func scrubSensitive(s string) string {
	for _, kw := range []string{"Authorization:", "authorization:", "password=", "Password:", "X-OLM-Signature:"} {
		if idx := strings.Index(s, kw); idx >= 0 {
			end := idx + len(kw)
			for end < len(s) && s[end] != ' ' && s[end] != '\n' && s[end] != '\t' {
				end++
			}
			s = s[:idx+len(kw)] + "[REDACTED]" + s[end:]
		}
	}
	return s
}
