// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package telemetry

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// Log field keys. Every line emitted by the structured logger carries
// service, env, tenant_id, trace_id, and span_id when those fields are
// available on the context or resource.
const (
	LogFieldService  = "service"
	LogFieldEnv      = "env"
	LogFieldTenantID = "tenant_id"
	LogFieldTraceID  = "trace_id"
	LogFieldSpanID   = "span_id"
)

// LoggerOptions configures NewLogger.
type LoggerOptions struct {
	// ServiceName is recorded on every line under LogFieldService.
	ServiceName string
	// Environment is recorded on every line under LogFieldEnv.
	Environment Environment
	// Level is the minimum slog level. Zero value defaults to slog.LevelInfo.
	Level slog.Level
	// Writer is the destination. Nil defaults to os.Stdout.
	Writer io.Writer
	// Redactor is the active redaction interceptor. If nil, a Redactor
	// configured with DefaultRedactionKeys is used.
	Redactor *Redactor
}

// NewLogger returns a slog.Logger that emits redacted JSON lines to the
// configured writer. Trace and span IDs are pulled from each LogContext
// call's context when present.
func NewLogger(opts LoggerOptions) *slog.Logger {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	if opts.Redactor == nil {
		opts.Redactor = NewRedactor(nil)
	}
	if opts.Level == 0 {
		opts.Level = slog.LevelInfo
	}

	base := slog.NewJSONHandler(opts.Writer, &slog.HandlerOptions{
		Level: opts.Level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if opts.Redactor.IsSensitiveKey(a.Key) {
				return slog.String(a.Key, redactedValue)
			}
			if a.Value.Kind() == slog.KindString && looksKeyLike(a.Value.String()) {
				return slog.String(a.Key, redactedValue)
			}
			return a
		},
	})

	handler := &contextHandler{
		Handler: base.WithAttrs([]slog.Attr{
			slog.String(LogFieldService, opts.ServiceName),
			slog.String(LogFieldEnv, string(opts.Environment)),
		}),
	}
	return slog.New(handler)
}

// contextHandler injects trace_id, span_id, and tenant_id (from baggage or
// explicit context value) into every log record without forcing call sites
// to pass them explicitly.
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		r.AddAttrs(
			slog.String(LogFieldTraceID, span.SpanContext().TraceID().String()),
			slog.String(LogFieldSpanID, span.SpanContext().SpanID().String()),
		)
	}
	if t := TenantFromContext(ctx); t != "" {
		r.AddAttrs(slog.String(LogFieldTenantID, t))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

// tenantContextKey is the unexported context key for tenant ID propagation.
type tenantContextKey struct{}

// WithTenant returns a copy of ctx that carries the supplied tenant ID. The
// shared logger reads this and stamps every log line with tenant_id.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantFromContext extracts the tenant ID set by WithTenant. Returns the
// empty string when no tenant is bound.
func TenantFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(tenantContextKey{}).(string)
	return v
}
