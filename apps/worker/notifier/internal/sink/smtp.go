// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package sink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig is the deserialized shape of notification_channels.config when
// kind='smtp'.
//
// PasswordRef is an indirection: at send time the notifier resolves the
// password from the env var OLM_SECRET_<REF>. The plaintext password is
// never persisted in Postgres and never logged.
type SMTPConfig struct {
	Server      string   `json:"server"`
	Port        int      `json:"port"`
	Username    string   `json:"username,omitempty"`
	PasswordRef string   `json:"password_ref,omitempty"`
	From        string   `json:"from"`
	To          []string `json:"to"`
}

// SMTPSink dispatches plain-text email via net/smtp.
type SMTPSink struct {
	timeout    time.Duration
	resolveRef func(ref string) string
	sendMail   func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTPSink constructs a sink with the supplied per-attempt timeout. The
// production resolver looks up OLM_SECRET_<REF> in the process environment;
// tests may override either function via the returned struct fields.
func NewSMTPSink(timeout time.Duration) *SMTPSink {
	return &SMTPSink{
		timeout:    timeout,
		resolveRef: defaultSecretResolver,
		sendMail:   smtp.SendMail,
	}
}

// Send dispatches the alert as a plain-text email.
//
// The body is intentionally simple: a subject line derived from the alert
// summary plus a labelled key:value block. We never include LLM prompt or
// completion content (F033 README §11) and never log the SMTP password.
//
// Errors:
//   - ErrTransient wraps SMTP responses in the 4xx range (RFC 5321 transient
//     negative completion) and all transport-level failures.
//   - All other errors are terminal.
func (s *SMTPSink) Send(ctx context.Context, cfg SMTPConfig, eventID string, alertJSON []byte) error {
	if cfg.Server == "" || cfg.Port == 0 || cfg.From == "" || len(cfg.To) == 0 {
		return fmt.Errorf("smtp: server, port, from, to are required")
	}

	password := ""
	if cfg.PasswordRef != "" {
		password = s.resolveRef(cfg.PasswordRef)
		if password == "" {
			// Missing secret is terminal — the operator must seed
			// OLM_SECRET_<REF> before the worker can deliver. Do NOT log
			// the ref's value here; the ref name itself is fine.
			return fmt.Errorf("smtp: password_ref %q not resolvable via OLM_SECRET_*", cfg.PasswordRef)
		}
	}

	subject, body, err := renderEmail(alertJSON, eventID)
	if err != nil {
		return err
	}

	msg := buildMessage(cfg.From, cfg.To, subject, body, eventID)

	addr := net.JoinHostPort(cfg.Server, strconv.Itoa(cfg.Port))
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, password, cfg.Server)
	}

	// net/smtp does not natively honor context cancellation; bound the send
	// with a goroutine + ctx race.
	done := make(chan error, 1)
	go func() {
		done <- s.sendMail(addr, auth, cfg.From, cfg.To, msg)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrTransient, ctx.Err())
	case err := <-done:
		if err == nil {
			return nil
		}
		if isTransientSMTP(err) {
			return fmt.Errorf("%w: %v", ErrTransient, err)
		}
		return fmt.Errorf("smtp: send: %w", err)
	}
}

// alertView is the JSON shape we project for the email body. We rebuild a
// labelled view instead of dumping raw JSON so the email is human-friendly.
type alertView struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	Severity    string            `json:"severity"`
	Source      string            `json:"source"`
	Summary     string            `json:"summary"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	StartedAt   string            `json:"started_at,omitempty"`
	EndedAt     string            `json:"ended_at,omitempty"`
	Resource    string            `json:"resource,omitempty"`
	RunbookURL  string            `json:"runbook_url,omitempty"`
}

func renderEmail(alertJSON []byte, eventID string) (subject, body string, err error) {
	var a alertView
	if uerr := json.Unmarshal(alertJSON, &a); uerr != nil {
		return "", "", fmt.Errorf("smtp: decode alert: %w", uerr)
	}
	severity := strings.ToUpper(a.Severity)
	if severity == "" {
		severity = "INFO"
	}
	summary := a.Summary
	if summary == "" {
		summary = "OpenLLM Metrics alert"
	}
	subject = fmt.Sprintf("[%s] %s", severity, summary)

	var b strings.Builder
	fmt.Fprintf(&b, "Severity:    %s\n", severity)
	fmt.Fprintf(&b, "Source:      %s\n", a.Source)
	fmt.Fprintf(&b, "Tenant:      %s\n", a.TenantID)
	fmt.Fprintf(&b, "Alert ID:    %s\n", a.ID)
	if a.StartedAt != "" {
		fmt.Fprintf(&b, "Started at:  %s\n", a.StartedAt)
	}
	if a.EndedAt != "" {
		fmt.Fprintf(&b, "Ended at:    %s\n", a.EndedAt)
	}
	if a.Resource != "" {
		fmt.Fprintf(&b, "Resource:    %s\n", a.Resource)
	}
	if a.RunbookURL != "" {
		fmt.Fprintf(&b, "Runbook:     %s\n", a.RunbookURL)
	}
	b.WriteString("\nSummary:\n")
	b.WriteString(summary)
	b.WriteString("\n")
	if a.Description != "" {
		b.WriteString("\nDetails:\n")
		b.WriteString(a.Description)
		b.WriteString("\n")
	}
	if len(a.Labels) > 0 {
		b.WriteString("\nLabels:\n")
		for k, v := range a.Labels {
			fmt.Fprintf(&b, "  %s = %s\n", k, v)
		}
	}
	fmt.Fprintf(&b, "\n-- \nDelivered by OpenLLM Metrics notifier (event %s)\n", eventID)
	return subject, b.String(), nil
}

func buildMessage(from string, to []string, subject, body, eventID string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", strings.ReplaceAll(subject, "\n", " "))
	fmt.Fprintf(&b, "X-OLM-Alert-Event-Id: %s\r\n", eventID)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// defaultSecretResolver reads from OLM_SECRET_<REF>. The ref is upper-cased
// and any non-alphanumeric character is collapsed to '_' so callers do not
// have to worry about env-var naming rules.
func defaultSecretResolver(ref string) string {
	if ref == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("OLM_SECRET_")
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		default:
			b.WriteByte('_')
		}
	}
	return os.Getenv(b.String())
}

// isTransientSMTP recognises RFC 5321 4yz (transient negative completion)
// responses and treats all non-protocol errors as transient as well.
func isTransientSMTP(err error) bool {
	if err == nil {
		return false
	}
	// net/smtp surfaces the SMTP response code as a textproto.Error; the
	// concrete type is not exported. Fall back to a textual scan: the
	// first three characters of the error message are typically the code.
	msg := err.Error()
	if len(msg) >= 3 && msg[0] == '4' {
		return true
	}
	// Network errors are transient by definition.
	var netErr net.Error
	return errors.As(err, &netErr)
}
