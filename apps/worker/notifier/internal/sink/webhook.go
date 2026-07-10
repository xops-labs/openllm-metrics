// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package sink contains the OSS-shipping notification sinks: generic webhook
// (this file) and SMTP (smtp.go). Slack, PagerDuty, Teams, ServiceNow, and
// any other vendor-branded sinks are custom integrations; do not add
// them here.
package sink

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SignatureHeader is the HMAC signature header receivers verify against.
// The recipe is documented in README.md.
const SignatureHeader = "X-OLM-Signature"

// EventIDHeader carries the alert event ID for correlation on the receiver
// side. Kept separate from the body so log aggregators can index it.
const EventIDHeader = "X-OLM-Alert-Event-Id"

// ErrTransient is returned when a webhook attempt should be retried.
// Currently any 5xx response or transport error qualifies.
var ErrTransient = errors.New("webhook: transient failure (retryable)")

// WebhookConfig is the deserialized shape of notification_channels.config
// when kind='webhook'.
type WebhookConfig struct {
	URL        string            `json:"url"`
	Headers    map[string]string `json:"headers,omitempty"`
	SecretHMAC string            `json:"secret_hmac,omitempty"`
}

// WebhookSink is the dispatcher for kind='webhook' channels.
type WebhookSink struct {
	client *http.Client
}

// NewWebhookSink constructs a sink with the supplied per-attempt timeout.
func NewWebhookSink(timeout time.Duration) *WebhookSink {
	return &WebhookSink{
		client: &http.Client{Timeout: timeout},
	}
}

// Send POSTs the alert payload to the configured URL. The body is the alert
// event itself, JSON-encoded. When secret_hmac is set, the body is signed
// with HMAC-SHA256 and the hex digest is sent as `sha256=<hex>` in the
// X-OLM-Signature header.
//
// Errors:
//   - ErrTransient wraps recoverable failures (transport error, 5xx). The
//     caller should retry these.
//   - All other errors are terminal (4xx, malformed config, etc.).
//
// The Authorization header (when present in cfg.Headers) is forwarded but
// never logged. The HMAC secret is never logged.
func (s *WebhookSink) Send(ctx context.Context, cfg WebhookConfig, eventID string, alertJSON []byte) error {
	if cfg.URL == "" {
		return fmt.Errorf("webhook: url is required")
	}

	body := alertJSON
	if body == nil {
		body = []byte("{}")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(EventIDHeader, eventID)
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if cfg.SecretHMAC != "" {
		mac := hmac.New(sha256.New, []byte(cfg.SecretHMAC))
		mac.Write(body)
		req.Header.Set(SignatureHeader, "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		// Transport errors (DNS, TLS, refused connection, timeout) are
		// always transient from the notifier's perspective.
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain a bounded amount so the connection can be reused but the body
	// payload is not retained or logged. We never log webhook response
	// bodies — receivers may echo headers or secrets back.
	_, _ = io.CopyN(io.Discard, resp.Body, 1<<14)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: status %d", ErrTransient, resp.StatusCode)
	}
	return fmt.Errorf("webhook: status %d", resp.StatusCode)
}
