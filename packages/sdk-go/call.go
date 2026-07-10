// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package openllm

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// LlmCall is the handle returned by StartLlmCall. It accumulates token counts
// and dollarized usage during the call, then emits the histogram, counters,
// and span when End is invoked.
//
// LlmCall is not safe for concurrent use by multiple goroutines — instrument a
// single LLM call from a single goroutine. If you have to fan out, start a
// child call per goroutine.
//
// End is idempotent: calling it twice is a no-op on the second invocation.
type LlmCall struct {
	rt        *runtime
	span      trace.Span
	opts      CallOptions
	startedAt time.Time

	promptTokens     int64
	completionTokens int64
	hasUsageDollars  bool
	usageDollars     float64
	errorKind        string

	once sync.Once
}

// StartLlmCall opens a span for an LLM call and returns a handle plus a
// derived context that carries the span and tenant baggage. The caller MUST
// pass the returned context to downstream HTTP/SDK calls so trace context and
// tenant baggage propagate. Always defer op.End().
//
// If Init has not been called (or has been shut down) StartLlmCall returns a
// no-op handle and the input context unchanged — instrumentation must never
// break the caller.
func StartLlmCall(ctx context.Context, opts CallOptions, fnOpts ...CallOption) (*LlmCall, context.Context) {
	for _, fn := range fnOpts {
		fn(&opts)
	}
	if opts.Operation == "" {
		opts.Operation = DefaultOperation
	}

	rt, err := getRuntime()
	if err != nil {
		// No-op handle. End() will check rt == nil and skip emission.
		return &LlmCall{opts: opts, startedAt: time.Now()}, ctx
	}

	// Apply tenant baggage BEFORE starting the span so the span snapshot can
	// reflect the baggage set on the context.
	ctx = attachTenantBaggage(ctx, opts)

	spanAttrs := buildSpanAttributes(opts, rt.defaultTags)
	ctx, span := rt.tracer.Start(ctx, SpanNameLlmCall,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(spanAttrs...),
	)

	return &LlmCall{
		rt:        rt,
		span:      span,
		opts:      opts,
		startedAt: time.Now(),
	}, ctx
}

// SetPromptTokens records the number of prompt (input) tokens consumed by the
// call. Negative values are ignored.
func (c *LlmCall) SetPromptTokens(n int64) {
	if c == nil || n < 0 {
		return
	}
	c.promptTokens = n
}

// SetCompletionTokens records the number of completion (output) tokens.
func (c *LlmCall) SetCompletionTokens(n int64) {
	if c == nil || n < 0 {
		return
	}
	c.completionTokens = n
}

// SetErrorKind tags the call with a normalized error category (e.g.
// "rate_limit", "auth", "timeout"). Empty preserves success-state.
func (c *LlmCall) SetErrorKind(kind string) {
	if c == nil {
		return
	}
	c.errorKind = kind
}

// SetUsageDollars records dollarized usage for the call, surfaced on the
// llm_usage_dollars counter. Provided by the caller because runtime-side cost
// estimation is owned by F017, not by the SDK.
func (c *LlmCall) SetUsageDollars(amount float64) {
	if c == nil {
		return
	}
	c.usageDollars = amount
	c.hasUsageDollars = true
}

// End closes the span and emits the operation-duration histogram, the
// token-usage counters (split by gen_ai.token.type), the llm_requests_total
// counter, and the llm_usage_dollars counter (when set).
//
// End is idempotent; the second call is a no-op.
func (c *LlmCall) End() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		if c.rt == nil || c.span == nil {
			// No-op handle (Init not called, or shut down before End).
			return
		}

		duration := time.Since(c.startedAt)
		ctx := trace.ContextWithSpan(context.Background(), c.span)

		genAIAttrs := genAIAttributeSet(c.opts, c.errorKind)
		c.rt.clientOpDuration.Record(ctx, duration.Seconds(),
			metric.WithAttributes(genAIAttrs...),
		)

		if c.promptTokens > 0 {
			attrs := append(append([]attribute.KeyValue{}, genAIAttrs...),
				attribute.String(GenAITokenType, TokenTypeInput))
			c.rt.clientTokenUsage.Add(ctx, c.promptTokens, metric.WithAttributes(attrs...))
		}
		if c.completionTokens > 0 {
			attrs := append(append([]attribute.KeyValue{}, genAIAttrs...),
				attribute.String(GenAITokenType, TokenTypeOutput))
			c.rt.clientTokenUsage.Add(ctx, c.completionTokens, metric.WithAttributes(attrs...))
		}

		// Project-specific llm_* dimensions: full tenant bundle so dashboards
		// can slice without joins.
		llmAttrs := llmAttributeSet(c.opts, c.errorKind, c.rt.defaultTags)
		c.rt.requestsTotal.Add(ctx, 1, metric.WithAttributes(llmAttrs...))
		if c.hasUsageDollars {
			c.rt.usageDollars.Add(ctx, c.usageDollars, metric.WithAttributes(llmAttrs...))
		}

		// Reflect token totals + error on the span. We deliberately do not
		// attach prompt/completion text — counts only.
		c.span.SetAttributes(
			attribute.Int64("gen_ai.usage.input_tokens", c.promptTokens),
			attribute.Int64("gen_ai.usage.output_tokens", c.completionTokens),
		)
		if c.errorKind != "" {
			c.span.SetAttributes(attribute.String(AttrErrorType, c.errorKind))
			c.span.SetStatus(codes.Error, c.errorKind)
		} else {
			c.span.SetStatus(codes.Ok, "")
		}
		c.span.End()
	})
}

// buildSpanAttributes assembles the gen_ai.* + multi-tenant attribute set
// recorded on the LLM-call span at start time. Token totals and error class
// are added at End so they reflect final values.
func buildSpanAttributes(opts CallOptions, defaults map[string]string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 12)
	if opts.Provider != "" {
		attrs = append(attrs, attribute.String(GenAISystem, opts.Provider))
	}
	if opts.Model != "" {
		attrs = append(attrs, attribute.String(GenAIRequestModel, opts.Model))
	}
	if opts.Operation != "" {
		attrs = append(attrs, attribute.String(GenAIOperationName, opts.Operation))
	}
	if opts.ServerAddress != "" {
		attrs = append(attrs, attribute.String(AttrServerAddress, opts.ServerAddress))
	}
	if opts.Route != "" {
		attrs = append(attrs, attribute.String(AttrRoute, opts.Route))
	}
	for k, v := range tenantBundle(opts, defaults) {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}

// genAIAttributeSet returns the GenAI-only attributes recorded on OTel GenAI
// histograms and counters. Multi-tenant fields live on the llm_* counters and
// the span — they do not pollute the gen_ai.* signal.
func genAIAttributeSet(opts CallOptions, errorKind string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 6)
	if opts.Provider != "" {
		attrs = append(attrs, attribute.String(GenAISystem, opts.Provider))
	}
	if opts.Model != "" {
		attrs = append(attrs, attribute.String(GenAIRequestModel, opts.Model))
	}
	if opts.Operation != "" {
		attrs = append(attrs, attribute.String(GenAIOperationName, opts.Operation))
	}
	if opts.ServerAddress != "" {
		attrs = append(attrs, attribute.String(AttrServerAddress, opts.ServerAddress))
	}
	if errorKind != "" {
		attrs = append(attrs, attribute.String(AttrErrorType, errorKind))
	}
	return attrs
}

// llmAttributeSet is the dimension set for llm_requests_total and
// llm_usage_dollars. Multi-tenant fields are first-class.
func llmAttributeSet(opts CallOptions, errorKind string, defaults map[string]string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 10)
	if opts.Provider != "" {
		attrs = append(attrs, attribute.String(AttrProvider, opts.Provider))
	}
	if opts.Model != "" {
		attrs = append(attrs, attribute.String(AttrModel, opts.Model))
	}
	if opts.Route != "" {
		attrs = append(attrs, attribute.String(AttrRoute, opts.Route))
	}
	for k, v := range tenantBundle(opts, defaults) {
		attrs = append(attrs, attribute.String(k, v))
	}
	attrs = append(attrs, attribute.String(AttrErrorKind, errorKind))
	return attrs
}

// tenantBundle merges per-call CallOptions over Options.DefaultTags so the
// per-call value wins. Empty strings never overwrite a default. Returned map
// only contains the multi-tenant keys (tenant/team/app/env/project).
func tenantBundle(opts CallOptions, defaults map[string]string) map[string]string {
	out := make(map[string]string, 5)
	for _, k := range []string{AttrTenant, AttrTeam, AttrApp, AttrEnv, AttrProject} {
		if v, ok := defaults[k]; ok && v != "" {
			out[k] = v
		}
	}
	if opts.Tenant != "" {
		out[AttrTenant] = opts.Tenant
	}
	if opts.Team != "" {
		out[AttrTeam] = opts.Team
	}
	if opts.App != "" {
		out[AttrApp] = opts.App
	}
	if opts.Env != "" {
		out[AttrEnv] = opts.Env
	}
	if opts.Project != "" {
		out[AttrProject] = opts.Project
	}
	return out
}
