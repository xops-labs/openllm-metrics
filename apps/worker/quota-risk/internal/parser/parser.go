// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package parser holds the five per-provider header parsers. Each provider
// publishes rate-limit telemetry on slightly different header keys, but the
// shape we need downstream is the same: tokens remaining, tokens limit,
// requests remaining, requests limit, and a reset hint.
//
// Each parser is a pure function: (headers map[string]string) -> Signal.
// Callers normalize header keys to lower case before invocation. Missing
// fields are returned as zero values; the Signal.HasTokens / HasRequests
// flags discriminate "absent" from "zero remaining".
//
// This package intentionally carries no I/O and no clock dependency so
// downstream rolling state can stamp the observation timestamp itself.
package parser

import (
	"strconv"
	"strings"
	"time"
)

// Signal is the provider-neutral rate-limit observation extracted from one
// event's response headers.
type Signal struct {
	// HasTokens is true if the parser found a token-quota signal (remaining +
	// limit both observed, or remaining + reset).
	HasTokens bool
	// TokensRemaining is the number of tokens left in the current window.
	TokensRemaining int64
	// TokensLimit is the window's token allowance, when the provider exposes
	// it. Zero when not exposed.
	TokensLimit int64

	// HasRequests is true if the parser found a request-quota signal.
	HasRequests bool
	// RequestsRemaining is the number of requests left in the current window.
	RequestsRemaining int64
	// RequestsLimit is the window's request allowance, when exposed.
	RequestsLimit int64

	// ResetAfter is the duration until the quota window resets, as inferred
	// from the headers. Zero when the provider did not expose a reset hint.
	ResetAfter time.Duration
}

// Parser is implemented by each provider header decoder.
type Parser interface {
	// Provider returns the canonical provider slug for this parser
	// (e.g. "openai", "anthropic"). Matches the `provider` enum in the
	// telemetry contract.
	Provider() string
	// Parse extracts a Signal from the supplied response-header map. Keys
	// are case-insensitive — callers should pass lower-cased keys.
	Parse(headers map[string]string) Signal
}

// all is every parser registered in this package, in a stable order. The
// parsers are stateless, so sharing the instances is safe.
var all = []Parser{
	OpenAI{},
	Anthropic{},
	Gemini{},
	Bedrock{},
	AzureOpenAI{},
}

// All returns every parser registered in this package, in a stable order.
// The router uses this to look up the right decoder by event.provider.
func All() []Parser {
	return all
}

// ByProvider returns the Parser whose Provider() equals the canonical
// provider slug, or nil if no parser is registered for that provider.
func ByProvider(provider string) Parser {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, p := range All() {
		if p.Provider() == provider {
			return p
		}
	}
	return nil
}

// --- shared helpers -------------------------------------------------------

// lower normalizes a header map to lower-case keys. Defensive — callers may
// pass mixed-case input. The returned map is a copy; the input is not
// mutated.
func lower(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[strings.ToLower(k)] = v
	}
	return out
}

func parseInt64(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseDurationLike accepts a value that may be expressed as:
//
//   - integer seconds ("60")
//   - integer milliseconds with `ms` suffix ("250ms")
//   - Go duration syntax ("1m30s")
//   - RFC3339 absolute timestamp; in that case the caller's `now` is used to
//     compute the offset.
//
// Returns zero duration when the value is unparseable or in the past.
func parseDurationLike(s string, now time.Time) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Absolute timestamp (Anthropic emits `anthropic-ratelimit-*-reset` as
	// ISO-8601 in some accounts).
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	// Plain integer — seconds.
	if v, ok := parseInt64(s); ok {
		if v < 0 {
			return 0
		}
		return time.Duration(v) * time.Second
	}
	// Try Go's flexible duration parser ("1m30s", "250ms").
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
