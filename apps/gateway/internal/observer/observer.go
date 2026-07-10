// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package observer is the request-boundary telemetry capture path for the
// gateway. It owns:
//
//  1. The inbound-context resolver (extracts tenant/team/app/env/project
//     from the X-OLM-* headers, falling back to configured defaults).
//  2. The provider/operation/model classifier driven by the request route.
//  3. The per-request lifecycle: startup snapshot, completion observation,
//     fan-out to the metrics registry, fan-out to the bus emitter.
//
// PRIVACY INVARIANT — the observer NEVER touches the request or response
// body for any purpose other than handing the (already-sent-to-client)
// response bytes to a usage parser. Tokens go onto the bus; bytes do not.
package observer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/busproducer"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/metrics"
	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/usage"
	telemetrycontracts "github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go"
)

// Inbound-header conventions. The gateway is multi-tenant from day one;
// every event MUST carry tenant/team/env. App/project are optional but
// strongly recommended.
const (
	HeaderTenant  = "X-OLM-Tenant"
	HeaderTeam    = "X-OLM-Team"
	HeaderApp     = "X-OLM-App"
	HeaderEnv     = "X-OLM-Env"
	HeaderProject = "X-OLM-Project"

	HeaderTraceparent = "traceparent"
	HeaderTracestate  = "tracestate"

	// HeaderRequestID is honored if the client supplies it; otherwise the
	// gateway generates a fresh ID. Only its SHA-256 ever leaves this
	// process (F008 §11: raw request IDs are forbidden in telemetry).
	HeaderRequestID = "X-Request-ID"

	// HeaderRetryCount lets clients (or upstream retry middleware) report
	// how many retries preceded the current attempt. Defaults to 0.
	HeaderRetryCount = "X-OLM-Retry-Count"
)

// Provider identifiers.
const (
	ProviderOpenAI      = "openai"
	ProviderAnthropic   = "anthropic"
	ProviderGemini      = "google"
	ProviderBedrock     = "bedrock"
	ProviderAzureOpenAI = "azure_openai"
)

// Defaults are the fallback labels applied when an inbound header is
// missing. Configured at boot from config.DefaultLabels.
type Defaults struct {
	Tenant  string
	Team    string
	App     string
	Env     string
	Project string
}

// Observer is the per-process capture surface. It is safe for concurrent
// use across all proxy goroutines.
type Observer struct {
	metrics  *metrics.Registry
	emitter  busproducer.Emitter
	defaults Defaults
}

// New constructs an Observer.
func New(reg *metrics.Registry, emitter busproducer.Emitter, defaults Defaults) *Observer {
	if emitter == nil {
		emitter = busproducer.NoopEmitter{}
	}
	return &Observer{metrics: reg, emitter: emitter, defaults: defaults}
}

// RequestContext is the immutable bundle the proxy passes to the observer
// at request start. It is built from the inbound URL + headers and carries
// the routing-time labels onto the response-time event.
type RequestContext struct {
	Provider  string
	Operation string // chat | completion | embedding | image | audio | moderation | other
	Model     string
	Route     string // e.g. "/v1/chat/completions"

	Tenant  string
	Team    string
	App     string
	Env     string
	Project string

	RequestIDHash string
	TraceID       string
	SpanID        string

	StartedAt time.Time
}

// Classify resolves provider/operation/model + labels from an inbound HTTP
// request. The model is extracted from the URL path where the provider
// surfaces it there (Gemini, Bedrock, Azure); for OpenAI/Anthropic the
// model arrives in the request body and is filled in by ObserveCompletion
// when the response body is parsed.
func (o *Observer) Classify(r *http.Request) RequestContext {
	provider, operation, model := classifyRoute(r.URL.Path)
	traceID, spanID := parseTraceparent(r.Header.Get(HeaderTraceparent))
	return RequestContext{
		Provider:      provider,
		Operation:     operation,
		Model:         model,
		Route:         r.URL.Path,
		Tenant:        firstNonEmpty(r.Header.Get(HeaderTenant), o.defaults.Tenant),
		Team:          firstNonEmpty(r.Header.Get(HeaderTeam), o.defaults.Team),
		App:           firstNonEmpty(r.Header.Get(HeaderApp), o.defaults.App),
		Env:           firstNonEmpty(r.Header.Get(HeaderEnv), o.defaults.Env),
		Project:       firstNonEmpty(r.Header.Get(HeaderProject), o.defaults.Project),
		RequestIDHash: hashRequestID(r),
		TraceID:       traceID,
		SpanID:        spanID,
		StartedAt:     time.Now().UTC(),
	}
}

// Completion is the response-side telemetry slice the proxy hands back
// after the response has been fully streamed to the client.
type Completion struct {
	StatusCode    int
	ErrorType     string
	RetryCount    int
	IsStreaming   bool
	BytesSampled  []byte // see Observer.SampleResponse; usage-only, never logged
	ModelOverride string // when the response body carried a model field, populated by the proxy
}

// ObserveCompletion records the final outcome of a request: metrics
// increment, usage parse, runtime event publish. Designed to be called
// inside a `defer` even on panics.
func (o *Observer) ObserveCompletion(ctx context.Context, rc RequestContext, comp Completion) {
	latency := time.Since(rc.StartedAt)

	tokens, hasUsage := o.parseUsage(rc.Provider, comp.BytesSampled)

	status := classifyStatus(comp.StatusCode, comp.ErrorType)
	// Model resolution precedence:
	//  1. ModelOverride — the proxy streaming first-chunk hint (captures the
	//     model for Anthropic streaming, whose trailing usage chunk omits it).
	//  2. body-parsed model from the sampled response bytes — covers all
	//     non-streaming responses and OpenAI/Azure streaming (whose trailing
	//     usage chunk still carries the model).
	//  3. rc.Model — the path/route model (Gemini, Bedrock, Azure deployment).
	//  4. "unknown" — applied in buildEvent.
	model := firstNonEmpty(comp.ModelOverride, o.parseModel(rc.Provider, comp.BytesSampled), rc.Model)

	lbls := metrics.Labels{
		Provider:   rc.Provider,
		Model:      model,
		Tenant:     rc.Tenant,
		Env:        rc.Env,
		Status:     status,
		StatusCode: comp.StatusCode,
	}
	o.metrics.ObserveRequest(lbls, latency.Seconds(), comp.RetryCount, comp.IsStreaming, hasUsage)

	ev := o.buildEvent(rc, comp, model, status, latency, tokens, hasUsage)
	if err := o.emitter.Emit(ctx, ev); err != nil {
		o.metrics.ObserveBusPublish(false)
		return
	}
	o.metrics.ObserveBusPublish(true)
}

func (o *Observer) buildEvent(
	rc RequestContext,
	comp Completion,
	model, status string,
	latency time.Duration,
	tokens usage.Tokens,
	hasUsage bool,
) busproducer.RuntimeEvent {
	ev := busproducer.RuntimeEvent{
		SchemaVersion: telemetrycontracts.SchemaVersion,
		EventID:       uuid.NewString(),
		SourceMode:    "proxy",
		SourceService: busproducer.SourceService,
		RequestIDHash: rc.RequestIDHash,
		Provider:      rc.Provider,
		Model:         firstNonEmpty(model, "unknown"),
		Operation:     rc.Operation,
		Tenant:        rc.Tenant,
		Team:          rc.Team,
		App:           rc.App,
		Env:           rc.Env,
		Project:       rc.Project,
		Status:        status,
		StatusCode:    comp.StatusCode,
		ErrorType:     comp.ErrorType,
		LatencyUS:     latency.Microseconds(),
		RetryCount:    comp.RetryCount,
		IsStreaming:   comp.IsStreaming,
		RecordedAt:    time.Now().UTC().Format(time.RFC3339),
		TraceID:       rc.TraceID,
		SpanID:        rc.SpanID,
	}
	if hasUsage {
		in, out, tot := tokens.Input, tokens.Output, tokens.Total
		ev.InputTokens = &in
		ev.OutputTokens = &out
		ev.TotalTokens = &tot
	}
	return ev
}

// parseModel extracts the model label from the sampled response bytes.
// Mirrors parseUsage: only the scalar model STRING leaves the usage
// package; the body itself is never logged or persisted. Returns "" when
// the model is absent from the body (e.g. Gemini/Bedrock, where the model
// comes from the URL path and rc.Model already carries it).
func (o *Observer) parseModel(provider string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	return usage.ParseModel(provider, body)
}

func (o *Observer) parseUsage(provider string, body []byte) (usage.Tokens, bool) {
	if len(body) == 0 {
		return usage.Tokens{}, false
	}
	switch provider {
	case ProviderOpenAI:
		return usage.ParseOpenAI(body)
	case ProviderAnthropic:
		return usage.ParseAnthropic(body)
	case ProviderGemini:
		return usage.ParseGemini(body)
	case ProviderBedrock:
		return usage.ParseBedrock(body)
	case ProviderAzureOpenAI:
		return usage.ParseAzureOpenAI(body)
	default:
		return usage.Tokens{}, false
	}
}

// classifyRoute maps the inbound URL path onto provider/operation/model.
// Returns ("", "other", "") if the path is not a recognized provider
// endpoint; the proxy responds 502 for routes with no configured provider,
// and this fallback labels such requests provider="" operation="other".
func classifyRoute(path string) (provider, operation, model string) {
	switch {
	case path == "/v1/chat/completions":
		return ProviderOpenAI, "chat", ""
	case path == "/v1/embeddings":
		return ProviderOpenAI, "embedding", ""
	case path == "/v1/responses":
		return ProviderOpenAI, "chat", ""
	case path == "/v1/messages":
		return ProviderAnthropic, "chat", ""
	case strings.HasPrefix(path, "/v1beta/models/") && strings.Contains(path, ":generateContent"):
		return ProviderGemini, "chat", extractGeminiModel(path)
	case strings.HasPrefix(path, "/v1beta/models/") && strings.Contains(path, ":streamGenerateContent"):
		return ProviderGemini, "chat", extractGeminiModel(path)
	case strings.HasPrefix(path, "/model/") && strings.HasSuffix(path, "/invoke"):
		return ProviderBedrock, "chat", extractBedrockModel(path)
	case strings.HasPrefix(path, "/model/") && strings.HasSuffix(path, "/invoke-with-response-stream"):
		return ProviderBedrock, "chat", extractBedrockModel(path)
	case strings.HasPrefix(path, "/openai/deployments/") && strings.Contains(path, "/chat/completions"):
		return ProviderAzureOpenAI, "chat", extractAzureDeployment(path)
	}
	return "", "other", ""
}

func extractGeminiModel(path string) string {
	// /v1beta/models/{model}:generateContent  →  {model}
	const prefix = "/v1beta/models/"
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func extractBedrockModel(path string) string {
	// /model/{modelId}/invoke[-with-response-stream]  →  {modelId}
	const prefix = "/model/"
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func extractAzureDeployment(path string) string {
	// /openai/deployments/{deployment}/chat/completions  →  {deployment}
	const prefix = "/openai/deployments/"
	rest := strings.TrimPrefix(path, prefix)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func classifyStatus(code int, errorType string) string {
	if errorType == "timeout" {
		return "timeout"
	}
	switch {
	case code == 0:
		return "error"
	case code == 429:
		return "rate_limited"
	case code >= 200 && code < 400:
		return "success"
	default:
		return "error"
	}
}

// hashRequestID returns the SHA-256 of the inbound X-Request-ID header.
// If absent, a fresh UUID is hashed so every event still carries a unique
// 64-char hex value (event_id provides per-event idempotency separately).
func hashRequestID(r *http.Request) string {
	id := r.Header.Get(HeaderRequestID)
	if id == "" {
		id = uuid.NewString()
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// parseTraceparent extracts the trace-id and span-id fields from a W3C
// traceparent header. Returns "" for both if the header is missing or
// malformed; downstream consumers tolerate empty values.
func parseTraceparent(traceparent string) (traceID, spanID string) {
	// traceparent format: version-traceid-spanid-flags (4 hex-separated parts).
	if traceparent == "" {
		return "", ""
	}
	parts := strings.Split(traceparent, "-")
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], parts[2]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// (The streaming-tap snapshot lives in internal/proxy; observer consumes
// the Snapshot() bytes via Completion.BytesSampled.)
