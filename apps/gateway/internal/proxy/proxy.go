// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package proxy is the reverse-proxy core of the gateway. It builds one
// httputil.ReverseProxy per provider, wires per-provider upstream URLs,
// and installs a Director + ModifyResponse + ErrorHandler trio that lets
// the observer capture latency / tokens / errors without buffering any
// response body.
//
// Hard invariants (also enforced by reviewer):
//   - Request and response bodies are NEVER logged, traced, or persisted.
//   - The `Authorization` header is forwarded to upstream untouched and
//     never logged.
//   - Streaming responses pass through with no buffering — the snapshot
//     used for usage parsing is the most recent SSE / event-stream chunk
//     (bounded at maxSSEChunkBytes), not the full body.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yasvanth511/openllm-metrics-oss/apps/gateway/internal/observer"
)

// UpstreamMap holds per-provider upstream base URLs (already parsed and
// validated at startup).
type UpstreamMap struct {
	OpenAI      *url.URL
	Anthropic   *url.URL
	Gemini      *url.URL
	Bedrock     *url.URL
	AzureOpenAI *url.URL
}

// Options bundles the proxy construction inputs.
type Options struct {
	Upstreams       UpstreamMap
	Observer        *observer.Observer
	Logger          *slog.Logger
	UpstreamTimeout time.Duration
	// MaxBufferedBytes caps the in-memory snapshot of non-streaming
	// responses kept for usage parsing. Default 256 KiB.
	MaxBufferedBytes int
}

// Handler is the HTTP handler the server mounts at "/". It picks the
// provider from the inbound path, builds the per-request context, and
// dispatches to a per-provider ReverseProxy.
type Handler struct {
	opts        Options
	openai      *httputil.ReverseProxy
	anthropic   *httputil.ReverseProxy
	gemini      *httputil.ReverseProxy
	bedrock     *httputil.ReverseProxy
	azureOpenAI *httputil.ReverseProxy
}

const defaultMaxBufferedBytes = 256 * 1024

// New wires Options into a Handler. Returns an error if every provider
// upstream is unset (a deployment with no upstreams cannot serve any
// route).
func New(opts Options) (*Handler, error) {
	if opts.Observer == nil {
		return nil, fmt.Errorf("proxy: observer is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.MaxBufferedBytes <= 0 {
		opts.MaxBufferedBytes = defaultMaxBufferedBytes
	}
	if opts.Upstreams.OpenAI == nil &&
		opts.Upstreams.Anthropic == nil &&
		opts.Upstreams.Gemini == nil &&
		opts.Upstreams.Bedrock == nil &&
		opts.Upstreams.AzureOpenAI == nil {
		return nil, fmt.Errorf("proxy: at least one provider upstream must be configured")
	}
	h := &Handler{opts: opts}
	h.openai = h.buildReverseProxy(opts.Upstreams.OpenAI)
	h.anthropic = h.buildReverseProxy(opts.Upstreams.Anthropic)
	h.gemini = h.buildReverseProxy(opts.Upstreams.Gemini)
	h.bedrock = h.buildReverseProxy(opts.Upstreams.Bedrock)
	h.azureOpenAI = h.buildReverseProxy(opts.Upstreams.AzureOpenAI)
	return h, nil
}

// ctxKey is the context key for the per-request observer state.
type ctxKey struct{}

type reqState struct {
	rc         observer.RequestContext
	sink       *usageSink
	tap        *tappingReader
	streaming  bool
	statusCode int
	errorType  string
	retryCount int
	modelHint  string
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc := h.opts.Observer.Classify(r)

	rp, upstream := h.selectProxy(rc.Provider)
	if rp == nil || upstream == nil {
		http.Error(w, "gateway: provider upstream not configured for "+r.URL.Path, http.StatusBadGateway)
		comp := observer.Completion{StatusCode: http.StatusBadGateway, ErrorType: "upstream_unconfigured"}
		h.opts.Observer.ObserveCompletion(r.Context(), rc, comp)
		return
	}

	state := &reqState{rc: rc, sink: newUsageSink(h.opts.MaxBufferedBytes)}
	if retryHdr := r.Header.Get(observer.HeaderRetryCount); retryHdr != "" {
		if n, err := strconv.Atoi(retryHdr); err == nil && n >= 0 {
			state.retryCount = n
		}
	}

	ctx := context.WithValue(r.Context(), ctxKey{}, state)
	if h.opts.UpstreamTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.opts.UpstreamTimeout)
		defer cancel()
	}
	r = r.WithContext(ctx)

	rp.ServeHTTP(w, r)

	// Once ServeHTTP returns, httputil has finished draining the upstream
	// body into the client. Capture any trailing un-terminated chunk
	// before we read the snapshot.
	if state.tap != nil {
		state.tap.flushTail()
	}
	// Surface the model captured from the first streaming chunk (if any) as
	// the override hint. For Anthropic streaming the trailing usage chunk
	// omits the model, so this is the only place it can be recovered.
	state.modelHint = state.sink.Model()
	comp := observer.Completion{
		StatusCode:    state.statusCode,
		ErrorType:     state.errorType,
		RetryCount:    state.retryCount,
		IsStreaming:   state.streaming,
		BytesSampled:  state.sink.Snapshot(),
		ModelOverride: state.modelHint,
	}
	// The request context may already be cancelled (upstream timeout) by the
	// time ServeHTTP returns; the observation must still be recorded.
	h.opts.Observer.ObserveCompletion(r.Context(), state.rc, comp) //nolint:contextcheck
}

// selectProxy picks the per-provider reverse proxy + upstream URL.
func (h *Handler) selectProxy(provider string) (*httputil.ReverseProxy, *url.URL) {
	switch provider {
	case observer.ProviderOpenAI:
		return h.openai, h.opts.Upstreams.OpenAI
	case observer.ProviderAnthropic:
		return h.anthropic, h.opts.Upstreams.Anthropic
	case observer.ProviderGemini:
		return h.gemini, h.opts.Upstreams.Gemini
	case observer.ProviderBedrock:
		return h.bedrock, h.opts.Upstreams.Bedrock
	case observer.ProviderAzureOpenAI:
		return h.azureOpenAI, h.opts.Upstreams.AzureOpenAI
	}
	return nil, nil
}

// buildReverseProxy wires a single httputil.ReverseProxy for one provider.
// Returns nil when upstream is nil — the dispatcher will respond 502 for
// requests routed to an unconfigured provider.
func (h *Handler) buildReverseProxy(upstream *url.URL) *httputil.ReverseProxy {
	if upstream == nil {
		return nil
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(upstream)
			// SetURL prepends the upstream path; for these provider APIs
			// the inbound path IS the provider path so we restore it.
			req.Out.URL.Path = singleJoiningSlash(upstream.Path, req.In.URL.Path)
			req.Out.URL.RawQuery = req.In.URL.RawQuery
			// Upstream hostname dominates; provider hosts reject mismatched
			// Host headers (esp. for SigV4-signed AWS endpoints handled
			// upstream).
			req.Out.Host = upstream.Host
			req.SetXForwarded()
		},
		ModifyResponse: h.modifyResponse,
		ErrorHandler:   h.errorHandler,
		// FlushInterval is set per-response in modifyResponse via the
		// internal flushIntervalOverride; the default here is 0 (Go
		// auto-detects text/event-stream and flushes immediately for it).
		// We additionally force flush-on-write for AWS event-stream below.
		FlushInterval: 0,
		Transport:     newUpstreamTransport(h.opts.UpstreamTimeout),
	}
	return rp
}

// modifyResponse runs after the upstream response headers arrive but
// before httputil writes them to the client. We use it to:
//
//  1. Decide if the response is streaming (drives the snapshot strategy).
//  2. Swap the upstream body for a tappingReader that mirrors bytes into
//     a usageSink while httputil continues to copy them to the client.
func (h *Handler) modifyResponse(resp *http.Response) error {
	state := stateFromContext(resp.Request.Context())
	if state == nil {
		// Defensive: dispatcher did not annotate the context.
		return nil
	}
	state.statusCode = resp.StatusCode
	state.streaming = IsStreamingContentType(resp.Header.Get("Content-Type"))

	tap := newTappingReader(resp.Body, state.sink, state.streaming)
	state.tap = tap
	resp.Body = tap
	return nil
}

// errorHandler is invoked when the upstream round-trip fails (DNS error,
// connection refused, TLS error, context timeout). We log an error
// CATEGORY but never the body or query.
func (h *Handler) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	state := stateFromContext(r.Context())
	errorType := classifyTransportError(err)
	status := http.StatusBadGateway
	if errorType == "timeout" {
		status = http.StatusGatewayTimeout
	}

	if state != nil {
		state.statusCode = status
		state.errorType = errorType
	}
	h.opts.Logger.Warn("gateway: upstream error",
		"route", r.URL.Path,
		"provider", providerFromState(state),
		"error_type", errorType,
		"status", status,
	)
	// Do not leak err.Error() to the client — that can include the
	// upstream hostname which leaks deployment topology.
	http.Error(w, http.StatusText(status), status)
}

func providerFromState(s *reqState) string {
	if s == nil {
		return ""
	}
	return s.rc.Provider
}

func stateFromContext(ctx context.Context) *reqState {
	v := ctx.Value(ctxKey{})
	if v == nil {
		return nil
	}
	s, _ := v.(*reqState)
	return s
}

func classifyTransportError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection_refused"
	case strings.Contains(msg, "no such host"):
		return "dns"
	case strings.Contains(msg, "TLS"):
		return "tls"
	}
	return "transport"
}

func singleJoiningSlash(a, b string) string {
	aSlash := strings.HasSuffix(a, "/")
	bSlash := strings.HasPrefix(b, "/")
	switch {
	case aSlash && bSlash:
		return a + b[1:]
	case !aSlash && !bSlash:
		return a + "/" + b
	}
	return a + b
}

// newUpstreamTransport returns a transport tuned for long-lived streaming
// connections. We deliberately disable response-header buffering so SSE
// chunks reach the gateway as soon as the upstream emits them.
func newUpstreamTransport(timeout time.Duration) http.RoundTripper {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout, // 0 = no deadline
		// Critical for streaming: do not let the transport buffer the
		// response body.
		DisableCompression: true,
	}
}
